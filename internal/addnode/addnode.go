// Package addnode is a port of src/modules/add_node.sh (96 lines): the
// "Add Node to Panel" flow that talks to internal/api.
package addnode

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/api"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

var entityNameRE = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)

// Original bash (src/modules/add_node.sh:5-96): add_node_to_panel().
// Inline comments mark the corresponding original line ranges.
func AddNodeToPanel() {
	domainURL := "127.0.0.1:3000"

	// Lines 8-14: warning banner + confirmation prompt.
	fmt.Println()
	fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("WARNING_LABEL"), ui.ColorReset)
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("WARNING_NODE_PANEL"), ui.ColorReset)
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CONFIRM_SERVER_PANEL"), ui.ColorReset)
	fmt.Println()
	confirm := ui.Reading(i18n.T("CONFIRM_PROMPT"))
	fmt.Println()

	// Lines 17-20: bail out unless the user confirmed with y/Y.
	if confirm != "y" && confirm != "Y" {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
		os.Exit(0)
	}

	// Lines 22-23.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("ADD_NODE_TO_PANEL"), ui.ColorReset)
	time.Sleep(1 * time.Second)

	// Lines 25-30: get_panel_token / read token file.
	token, err := api.GetPanelToken()
	if err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_TOKEN"), ui.ColorReset)
		return
	}

	// Lines 32-39: prompt for the node's selfsteal domain until it checks out.
	var selfstealDomain string
	for {
		selfstealDomain = ui.Reading(i18n.T("ENTER_NODE_DOMAIN"))
		if api.CheckNodeDomain(domainURL, token, selfstealDomain) == nil {
			break
		}
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("TRY_ANOTHER_DOMAIN"), ui.ColorReset)
	}

	// Lines 41-58: prompt for a valid, unused config-profile/entity name.
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
			// Original bash has no explicit handling for a failed lookup here;
			// it just falls through to the jq -e check evaluating to false,
			// i.e. behaves the same as "name not taken". Mirrored here.
			break
		}
		if nameTaken {
			fmt.Printf("%s%s%s\n", ui.ColorRed, fmt.Sprintf(i18n.T("CF_INVALID_NAME"), entityName), ui.ColorReset)
			continue
		}
		break
	}

	// Lines 60-62: generate xray keys.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("GENERATE_KEYS"), ui.ColorReset)
	privateKey := api.GenerateXrayKeys(domainURL, token)
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("GENERATE_KEYS_SUCCESS"), ui.ColorReset)

	// Lines 64-66: create the config profile.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CREATING_CONFIG_PROFILE"), ui.ColorReset)
	configProfileUUID, inboundUUID := api.CreateConfigProfile(domainURL, token, entityName, selfstealDomain, privateKey, entityName)
	fmt.Printf("%s%s: %s%s\n", ui.ColorGreen, i18n.T("CONFIG_PROFILE_CREATED"), entityName, ui.ColorReset)

	// Lines 68-69: create the node.
	fmt.Printf("%s%s%s%s\n", ui.ColorYellow, i18n.T("CREATE_NEW_NODE"), selfstealDomain, ui.ColorReset)
	api.CreateNode(domainURL, token, configProfileUUID, inboundUUID, selfstealDomain, entityName)

	// Lines 71-72: create the host.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CREATE_HOST"), ui.ColorReset)
	api.CreateHost(domainURL, token, inboundUUID, selfstealDomain, configProfileUUID, entityName)

	// Lines 74-90: fetch default squads and add this inbound to each.
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

	// Lines 92-95: final success + post-install instructions.
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("NODE_ADDED_SUCCESS"), ui.ColorReset)
	fmt.Printf("%s-------------------------------------------------%s\n", ui.ColorRed, ui.ColorReset)
	fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("POST_PANEL_INSTRUCTION"), ui.ColorReset)
	fmt.Printf("%s-------------------------------------------------%s\n", ui.ColorRed, ui.ColorReset)
}

// configProfileNameExists is the Go equivalent of:
//
//	local response=$(make_api_request "GET" "http://$domain_url/api/config-profiles" "$token")
//	if echo "$response" | jq -e ".response.configProfiles[] | select(.name == \"$entity_name\")" > /dev/null; then
//
// (src/modules/add_node.sh:45-47).
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
