// Package nodeprofile implements the "Manage Node Profile" menu item:
// changing which inbounds (Raw Reality, Hysteria2, XHTTP) an
// already-registered node's config profile carries.
package nodeprofile

import (
	"fmt"
	"strconv"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/api"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/certs"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

// selectWebserver asks which webserver the node behind this profile
// runs, needed for the Hysteria2 inbound's certificate path convention
// (internal/certs.NginxCertPaths vs CaddyCertPaths). Duplicated from
// internal/addnode's identical helper rather than shared, the same
// pattern isValidIPv4's duplication follows elsewhere in this codebase.
func selectWebserver() string {
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

// inboundOption is one line of the checkbox picker.
type inboundOption struct {
	label   string
	checked *bool
}

// pickInbounds shows a checkbox-style menu ("[ ] 1. Raw Reality", ...):
// entering a number toggles that item between "[ ]" and "[x]" and
// redraws the menu; entering an empty line confirms the current
// selection, refusing to return until at least one item is checked.
func pickInbounds(initial api.ConfigProfileInbounds) api.ConfigProfileInbounds {
	sel := initial
	options := []inboundOption{
		{i18n.T("NODE_PROFILE_RAW"), &sel.Raw},
		{i18n.T("NODE_PROFILE_HYSTERIA2"), &sel.Hysteria2},
		{i18n.T("NODE_PROFILE_XHTTP"), &sel.XHTTP},
	}

	for {
		fmt.Println()
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("NODE_PROFILE_PICKER_TITLE"), ui.ColorReset)
		fmt.Println()
		for i, opt := range options {
			mark := " "
			if *opt.checked {
				mark = "x"
			}
			fmt.Printf("%s[%s] %d. %s%s\n", ui.ColorYellow, mark, i+1, opt.label, ui.ColorReset)
		}
		fmt.Println()
		choice := ui.Reading(i18n.T("NODE_PROFILE_PICKER_PROMPT"))

		if choice == "" {
			for _, opt := range options {
				if *opt.checked {
					return sel
				}
			}
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("NODE_PROFILE_PICKER_EMPTY"), ui.ColorReset)
			continue
		}

		idx, err := strconv.Atoi(choice)
		if err != nil || idx < 1 || idx > len(options) {
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("INVALID_CHOICE"), ui.ColorReset)
			continue
		}
		*options[idx-1].checked = !*options[idx-1].checked
	}
}

// findRealityTag returns the tag of whichever inbound in byTag is the
// Reality one, identified by streamSettings.security == "reality"
// rather than assumed to be tagged "Raw": internal/addnode lets the
// operator's own entity name become the inbound tag instead of always
// using "Raw" (see its CreateConfigProfile call), so a profile created
// that way has its Reality inbound under a different tag. Returns ""
// if no Reality inbound is present at all.
func findRealityTag(byTag map[string]map[string]any) string {
	for tag, ib := range byTag {
		stream, _ := ib["streamSettings"].(map[string]any)
		if security, _ := stream["security"].(string); security == "reality" {
			return tag
		}
	}
	return ""
}

// inboundsByTag pulls each inbound out of an existing config's
// "inbounds" array into a map keyed by tag, so api.BuildProfileConfig
// can reuse an inbound's already-issued secrets instead of generating
// new ones just because the selection changed.
func inboundsByTag(config map[string]any) map[string]map[string]any {
	byTag := map[string]map[string]any{}
	inbounds, _ := config["inbounds"].([]any)
	for _, raw := range inbounds {
		ib, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if tag, _ := ib["tag"].(string); tag != "" {
			byTag[tag] = ib
		}
	}
	return byTag
}

// currentSelection reports which of Raw/Hysteria2/XHTTP are present in
// an existing config, so the picker opens already reflecting reality
// instead of always starting from a blank slate. Raw is detected by
// realityTag rather than a fixed tag name; see findRealityTag.
func currentSelection(byTag map[string]map[string]any, realityTag string) api.ConfigProfileInbounds {
	_, hasHy := byTag["HYSTERIA-BBR"]
	_, hasXHTTP := byTag["XHTTP-TLS"]
	return api.ConfigProfileInbounds{Raw: realityTag != "", Hysteria2: hasHy, XHTTP: hasXHTTP}
}

// existingDomain finds the domain already baked into whichever inbound
// is present, checked in the order a fresh profile would have set them
// up: the Reality inbound's serverName, then XHTTP's host. All three
// inbounds carry the same domain in every profile this project
// creates, so any one of them answers the question.
func existingDomain(byTag map[string]map[string]any, realityTag string) string {
	if realityTag != "" {
		raw := byTag[realityTag]
		stream, _ := raw["streamSettings"].(map[string]any)
		reality, _ := stream["realitySettings"].(map[string]any)
		if names, ok := reality["serverNames"].([]any); ok && len(names) > 0 {
			if s, ok := names[0].(string); ok {
				return s
			}
		}
	}
	if xhttp, ok := byTag["XHTTP-TLS"]; ok {
		stream, _ := xhttp["streamSettings"].(map[string]any)
		xset, _ := stream["xhttpSettings"].(map[string]any)
		if host, _ := xset["host"].(string); host != "" {
			return host
		}
	}
	return ""
}

func existingPrivateKey(byTag map[string]map[string]any, realityTag string) string {
	if realityTag == "" {
		return ""
	}
	raw := byTag[realityTag]
	stream, _ := raw["streamSettings"].(map[string]any)
	reality, _ := stream["realitySettings"].(map[string]any)
	pk, _ := reality["privateKey"].(string)
	return pk
}

// ManageNodeProfile lets an operator change which inbounds an
// already-registered node's config profile carries. Unlike
// internal/addnode, this asks for the panel URL and API token
// directly instead of assuming it's running on the panel's own server
// with a saved token: the profile being edited may belong to any panel
// the operator has access to, run from anywhere.
func ManageNodeProfile() {
	fmt.Println()
	domainURL := ui.Reading(i18n.T("NODE_PROFILE_ENTER_PANEL_URL"))
	token := ui.Reading(i18n.T("NODE_PROFILE_ENTER_TOKEN"))
	profileName := ui.Reading(i18n.T("NODE_PROFILE_ENTER_NAME"))

	profileUUID, config, err := api.FindConfigProfileByName(domainURL, token, profileName)
	if err != nil {
		fmt.Printf("%s%s: %v%s\n", ui.ColorRed, i18n.T("NODE_PROFILE_NOT_FOUND"), err, ui.ColorReset)
		return
	}

	byTag := inboundsByTag(config)
	// realityTag is "" for a profile with no Reality inbound yet, and
	// "Raw" is the right tag to create it under in that case, matching
	// this project's own install flows' default.
	realityTag := findRealityTag(byTag)
	rawTag := realityTag
	if rawTag == "" {
		rawTag = "Raw"
	}

	selection := pickInbounds(currentSelection(byTag, realityTag))

	domainName := existingDomain(byTag, realityTag)
	if domainName == "" {
		domainName = ui.Reading(i18n.T("ENTER_NODE_DOMAIN"))
	}

	privateKey := existingPrivateKey(byTag, realityTag)
	if privateKey == "" && selection.Raw {
		privateKey = api.GenerateXrayKeys(domainURL, token)
	}

	var certFullchain, certPrivkey string
	if _, hysteria2AlreadyActive := byTag["HYSTERIA-BBR"]; selection.Hysteria2 && !hysteria2AlreadyActive {
		// Only newly enabling Hysteria2 needs a cert path: an
		// already-active Hysteria2 inbound is reused verbatim by
		// api.BuildProfileConfig, so asking here would be a pointless
		// prompt whose answer gets thrown away.
		//
		// This flow doesn't provision the node's files itself, so it
		// can't tell a per-domain certificate from a wildcard one on
		// its own; certs.AskCertDomain asks the operator instead of
		// guessing.
		certDomain := certs.AskCertDomain(domainName)
		if selectWebserver() == "1" {
			certFullchain, certPrivkey = certs.NginxCertPaths(certDomain)
		} else {
			certFullchain, certPrivkey = certs.CaddyCertPaths(certDomain)
		}
	}

	newConfig := api.BuildProfileConfig(selection, domainName, privateKey, rawTag, certFullchain, certPrivkey, byTag)
	if err := api.UpdateConfigProfile(domainURL, token, profileUUID, newConfig); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("NODE_PROFILE_UPDATE_FAILED"), ui.ColorReset)
		return
	}
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("NODE_PROFILE_UPDATE_SUCCESS"), ui.ColorReset)
}
