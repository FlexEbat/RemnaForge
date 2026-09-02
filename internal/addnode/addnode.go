// Package addnode implements the "Add Node to Panel" flow that talks
// to internal/api.
package addnode

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/api"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/certs"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

var entityNameRE = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)

// selectNodeWebserver asks which webserver the node being registered
// runs behind. CreateConfigProfile's Hysteria2 inbound needs a
// certificate path convention, and that convention is different for a
// node installed via internal/nginxnode (certbot, /etc/letsencrypt/live)
// versus internal/caddynode (Caddy's own ACME client, its data volume).
// This flow doesn't install or inspect the node itself, so it has no
// other way to know which one applies.
func selectNodeWebserver() string {
	for {
		fmt.Println()
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("SELECT_WEBSERVER_TITLE"), ui.ColorReset)
		fmt.Println()
		fmt.Printf("%s1. Nginx%s\n", ui.ColorYellow, ui.ColorReset)
		fmt.Printf("%s2. Caddy%s\n", ui.ColorYellow, ui.ColorReset)
		fmt.Println()
		choice := ui.Reading(i18n.T("SELECT_WEBSERVER_PROMPT"))
		if choice == "1" || choice == "2" {
			return choice
		}
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("INVALID_CHOICE"), ui.ColorReset)
	}
}

// AddNodeToPanel registers a new node with the panel over its local
// API: it warns the user this must run on the panel's own server,
// prompts for the node's details, creates a config profile, host, and
// inbound for it, and prints the resulting node config.
func AddNodeToPanel() {
	domainURL := "127.0.0.1:3000"

	// Warning banner + confirmation prompt.
	fmt.Println()
	fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("WARNING_LABEL"), ui.ColorReset)
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("WARNING_NODE_PANEL"), ui.ColorReset)
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CONFIRM_SERVER_PANEL"), ui.ColorReset)
	fmt.Println()
	confirm := ui.Reading(i18n.T("CONFIRM_PROMPT"))
	fmt.Println()

	// Bail out unless the user confirmed with y/Y.
	if confirm != "y" && confirm != "Y" {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
		ui.Exit(0)
	}

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("ADD_NODE_TO_PANEL"), ui.ColorReset)
	time.Sleep(1 * time.Second)

	// Get_panel_token / read token file.
	token, err := api.GetPanelToken()
	if err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_TOKEN"), ui.ColorReset)
		return
	}

	// Prompt for the node's selfsteal domain until it checks out.
	var selfstealDomain string
	for {
		selfstealDomain = ui.Reading(i18n.T("ENTER_NODE_DOMAIN"))
		if api.CheckNodeDomain(domainURL, token, selfstealDomain) == nil {
			break
		}
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("TRY_ANOTHER_DOMAIN"), ui.ColorReset)
	}

	// Prompt for a valid, unused config-profile/entity name.
	var entityName string
	for {
		entityName = ui.Reading(i18n.T("ENTER_NODE_NAME"))
		if !entityNameRE.MatchString(entityName) {
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CF_INVALID_CHARS"), ui.ColorReset)
			continue
		}
		if len(entityName) < 3 || len(entityName) > 20 {
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CF_INVALID_LENGTH"), ui.ColorReset)
			continue
		}

		nameTaken, checkErr := configProfileNameExists(domainURL, token, entityName)
		if checkErr != nil {
			// A failed lookup is treated the same as "name not taken":
			// there's nothing more useful to do here than let the create
			// call downstream surface any real conflict.
			break
		}
		if nameTaken {
			fmt.Printf("%s%s%s\n", ui.ColorRed, fmt.Sprintf(i18n.T("CF_INVALID_NAME"), entityName), ui.ColorReset)
			continue
		}
		break
	}

	// Generate xray keys.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("GENERATE_KEYS"), ui.ColorReset)
	privateKey := api.GenerateXrayKeys(domainURL, token)
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("GENERATE_KEYS_SUCCESS"), ui.ColorReset)

	// Which webserver the node runs behind, needed for the Hysteria2
	// inbound's certificate path convention (see selectNodeWebserver).
	webserver := selectNodeWebserver()
	var certFullchain, certPrivkey string
	if webserver == "1" {
		certFullchain, certPrivkey = certs.NginxCertPaths(selfstealDomain)
	} else {
		certFullchain, certPrivkey = certs.CaddyCertPaths(selfstealDomain)
	}

	// Create the config profile.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CREATING_CONFIG_PROFILE"), ui.ColorReset)
	configProfileUUID, inboundUUID := api.CreateConfigProfile(domainURL, token, entityName, selfstealDomain, privateKey, entityName, certFullchain, certPrivkey, api.ConfigProfileInbounds{Raw: true})
	fmt.Printf("%s%s: %s%s\n", ui.ColorGreen, i18n.T("CONFIG_PROFILE_CREATED"), entityName, ui.ColorReset)

	// Create the node.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, fmt.Sprintf(i18n.T("CREATE_NEW_NODE"), selfstealDomain), ui.ColorReset)
	api.CreateNode(domainURL, token, configProfileUUID, inboundUUID, selfstealDomain, entityName)

	// Create the host.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CREATE_HOST"), ui.ColorReset)
	api.CreateHost(domainURL, token, inboundUUID, selfstealDomain, configProfileUUID, entityName)

	// Fetch default squads and add this inbound to each.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("GET_DEFAULT_SQUAD"), ui.ColorReset)
	squadUUIDs, squadErr := api.GetDefaultSquad(domainURL, token)
	switch {
	case squadErr != nil:
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_GET_SQUAD_LIST"), ui.ColorReset)
	case len(squadUUIDs) == 0:
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("NO_SQUADS_TO_UPDATE"), ui.ColorReset)
	default:
		for _, squadUUID := range squadUUIDs {
			fmt.Printf("%s%s %s%s\n", ui.ColorYellow, i18n.T("UPDATING_SQUAD"), squadUUID, ui.ColorReset)
			if err := api.UpdateSquad(domainURL, token, squadUUID, inboundUUID); err == nil {
				fmt.Printf("%s%s %s%s\n", ui.ColorGreen, i18n.T("UPDATE_SQUAD"), squadUUID, ui.ColorReset)
			} else {
				fmt.Printf("%s%s %s%s\n", ui.ColorRed, i18n.T("ERROR_UPDATE_SQUAD"), squadUUID, ui.ColorReset)
			}
		}
	}

	// Final success + post-install instructions.
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("NODE_ADDED_SUCCESS"), ui.ColorReset)
	fmt.Printf("%s-------------------------------------------------%s\n", ui.ColorRed, ui.ColorReset)
	fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("POST_PANEL_INSTRUCTION"), ui.ColorReset)
	fmt.Printf("%s-------------------------------------------------%s\n", ui.ColorRed, ui.ColorReset)
}

// configProfileNameExists reports whether a config profile with the
// given name already exists on the panel.
func configProfileNameExists(domainURL, token, name string) (bool, error) {
	resp := api.MakeAPIRequest("GET", "http://"+domainURL+"/api/config-profiles", token, "")
	if len(resp) == 0 {
		return false, fmt.Errorf("empty response")
	}
	var parsed struct {
		Response struct {
			ConfigProfiles []struct {
				Name string `json:"name"`
			} `json:"configProfiles"`
		} `json:"response"`
	}
	if err := json.Unmarshal(resp, &parsed); err != nil {
		return false, err
	}
	for _, p := range parsed.Response.ConfigProfiles {
		if p.Name == name {
			return true, nil
		}
	}
	return false, nil
}
