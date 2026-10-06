// Package api holds all the functions that talk to the Remnawave
// panel's HTTP API over net/http, with JSON handled by encoding/json.
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// DirRemnawave is this tool's own config/state directory.
var DirRemnawave = "/usr/local/remnawave_reverse/"

// PanelDomain is the panel domain set during panel install, used by
// GetPanelToken's create-API-token instructions.
var PanelDomain string

var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// httpClient is shared by all panel API calls.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// MakeAPIRequest sends an HTTP request to the panel's API with the
// standard set of headers (bearer token, content type, and the
// X-Forwarded-*/X-Remnawave-Client-Type headers the panel expects from
// a browser-originated request) and returns the raw response body.
// Callers parse the JSON themselves.
func MakeAPIRequest(method, url, token, data string) []byte {
	_, body := makeAPIRequestWithStatus(method, url, token, data)
	return body
}

// makeAPIRequestWithStatus is MakeAPIRequest plus the HTTP status code.
// DeleteConfigProfile needs this: as of Remnawave Panel v3.2.0, a
// successful DELETE returns 204 No Content with an empty body, so success
// can no longer be inferred from body content the way every other
// endpoint here still allows (see DeleteConfigProfile's comment for the
// bug this fixes). statusCode is 0 if the request never reached the
// server.
func makeAPIRequestWithStatus(method, url, token, data string) (statusCode int, respBody []byte) {
	var body io.Reader
	if data != "" {
		body = strings.NewReader(data)
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return 0, nil
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Remnawave-Client-Type", "browser")

	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Printf("%s%v%s\n", ui.ColorRed, err, ui.ColorReset)
		return 0, nil
	}
	defer resp.Body.Close()

	respBody, err = io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil
	}
	return resp.StatusCode, respBody
}

// RegisterRemnawave registers the initial superadmin account on a
// freshly installed panel.
func RegisterRemnawave(domainURL, username, password, token string) string {
	registerBody, _ := json.Marshal(map[string]string{"username": username, "password": password})
	resp := MakeAPIRequest("POST", BaseURL(domainURL)+"/api/auth/register", token, string(registerBody))

	if len(resp) == 0 {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_EMPTY_RESPONSE_REGISTER"), ui.ColorReset)
		return ""
	}
	if strings.Contains(string(resp), "accessToken") {
		var parsed struct {
			Response struct {
				AccessToken string `json:"accessToken"`
			} `json:"response"`
		}
		_ = json.Unmarshal(resp, &parsed)
		return parsed.Response.AccessToken
	}
	fmt.Printf("%s%s: %s%s\n", ui.ColorRed, i18n.T("ERROR_REGISTER"), string(resp), ui.ColorReset)
	return ""
}

// See inline comments below for the line-by-line mapping; this is the
// longest/most stateful function in the module.
func GetPanelToken() (string, error) {
	tokenFile := DirRemnawave + "/token"
	domainURL := "127.0.0.1:3000"

	// local auth_status=$(make_api_request "GET" "http://${domain_url}/api/auth/status" "")
	authStatus := MakeAPIRequest("GET", BaseURL(domainURL)+"/api/auth/status", "", "")
	oauthEnabled := false

	if len(authStatus) > 0 {
		var parsed struct {
			Response struct {
				Authentication struct {
					OAuth2 struct {
						Providers struct {
							Github   bool `json:"github"`
							Yandex   bool `json:"yandex"`
							PocketID bool `json:"pocketid"`
						} `json:"providers"`
					} `json:"oauth2"`
					TgAuth struct {
						Enabled bool `json:"enabled"`
					} `json:"tgAuth"`
				} `json:"authentication"`
			} `json:"response"`
		}
		_ = json.Unmarshal(authStatus, &parsed)
		auth := parsed.Response.Authentication
		if auth.OAuth2.Providers.Github || auth.OAuth2.Providers.Yandex ||
			auth.OAuth2.Providers.PocketID || auth.TgAuth.Enabled {
			oauthEnabled = true
		}
	}

	var token string
	if data, err := os.ReadFile(tokenFile); err == nil {
		token = strings.TrimSpace(string(data))
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("USING_SAVED_TOKEN"), ui.ColorReset)
		testResponse := MakeAPIRequest("GET", BaseURL(domainURL)+"/api/config-profiles", token, "")

		if !hasConfigProfiles(testResponse) {
			if bytes.Contains(testResponse, []byte(`"statusCode":401`)) || isUnauthorizedMessage(testResponse) {
				fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("INVALID_SAVED_TOKEN"), ui.ColorReset)
			} else {
				fmt.Printf("%s%s: %s%s\n", ui.ColorRed, i18n.T("INVALID_SAVED_TOKEN"), string(testResponse), ui.ColorReset)
			}
			token = ""
		}
	}

	if token == "" {
		if oauthEnabled {
			fmt.Printf("%s=================================================%s\n", ui.ColorYellow, ui.ColorReset)
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("WARNING_LABEL"), ui.ColorReset)
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("TELEGRAM_OAUTH_WARNING"), ui.ColorReset)
			fmt.Printf("%s%s%s\n", ui.ColorYellow, fmt.Sprintf(i18n.T("CREATE_API_TOKEN_INSTRUCTION"), PanelDomain), ui.ColorReset)
			token = ui.Reading(i18n.T("ENTER_API_TOKEN"))
			if token == "" {
				fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("EMPTY_TOKEN_ERROR"), ui.ColorReset)
				return "", fmt.Errorf("empty token")
			}

			testResponse := MakeAPIRequest("GET", BaseURL(domainURL)+"/api/config-profiles", token, "")
			if !hasConfigProfiles(testResponse) {
				fmt.Printf("%s%s: %s%s\n", ui.ColorRed, i18n.T("INVALID_SAVED_TOKEN"), string(testResponse), ui.ColorReset)
				return "", fmt.Errorf("invalid token")
			}
		} else {
			username := ui.Reading(i18n.T("ENTER_PANEL_USERNAME"))
			password := ui.Reading(i18n.T("ENTER_PANEL_PASSWORD"))

			loginBody, _ := json.Marshal(map[string]string{"username": username, "password": password})
			loginResponse := MakeAPIRequest("POST", BaseURL(domainURL)+"/api/auth/login", "", string(loginBody))

			var parsed struct {
				Response struct {
					AccessToken string `json:"accessToken"`
				} `json:"response"`
				AccessToken string `json:"accessToken"`
			}
			_ = json.Unmarshal(loginResponse, &parsed)
			token = parsed.Response.AccessToken
			if token == "" {
				token = parsed.AccessToken
			}
			if token == "" {
				fmt.Printf("%s%s: %s%s\n", ui.ColorRed, i18n.T("ERROR_TOKEN"), string(loginResponse), ui.ColorReset)
				return "", fmt.Errorf("login failed")
			}
		}

		if err := os.MkdirAll(DirRemnawave, 0700); err == nil {
			_ = os.WriteFile(tokenFile, []byte(token), 0600)
		}
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("TOKEN_RECEIVED_AND_SAVED"), ui.ColorReset)
	} else {
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("TOKEN_USED_SUCCESSFULLY"), ui.ColorReset)
	}

	finalTestResponse := MakeAPIRequest("GET", BaseURL(domainURL)+"/api/config-profiles", token, "")
	if !hasConfigProfiles(finalTestResponse) {
		fmt.Printf("%s%s: %s%s\n", ui.ColorRed, i18n.T("INVALID_SAVED_TOKEN"), string(finalTestResponse), ui.ColorReset)
		return "", fmt.Errorf("invalid token")
	}

	return token, nil
}

func hasConfigProfiles(resp []byte) bool {
	if len(resp) == 0 {
		return false
	}
	var parsed struct {
		Response struct {
			ConfigProfiles json.RawMessage `json:"configProfiles"`
		} `json:"response"`
	}
	if err := json.Unmarshal(resp, &parsed); err != nil {
		return false
	}
	return parsed.Response.ConfigProfiles != nil
}

func isUnauthorizedMessage(resp []byte) bool {
	var parsed struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(resp, &parsed); err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(parsed.Message), "unauthorized")
}

// GetPublicKey fetches the node's secret key from the panel API and
// substitutes it into the node's docker-compose.yml, replacing the
// placeholder SECRET_KEY value written when the file was created. The
// file is left untouched if the key could not be fetched.
//
// Since Remnawave Panel v3.2.0 the /api/keygen response carries the key
// in response.secretKey (it used to be response.pubKey).
func GetPublicKey(domainURL, token, targetDir string) error {
	resp := MakeAPIRequest("GET", BaseURL(domainURL)+"/api/keygen", token, "")
	if len(resp) == 0 {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_PUBLIC_KEY"), ui.ColorReset)
		return fmt.Errorf("empty response from /api/keygen")
	}

	var parsed struct {
		Response struct {
			SecretKey string `json:"secretKey"`
		} `json:"response"`
	}
	_ = json.Unmarshal(resp, &parsed)
	secretKey := parsed.Response.SecretKey
	if secretKey == "" {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_EXTRACT_PUBLIC_KEY"), ui.ColorReset)
		return fmt.Errorf("no secretKey in response: %s", resp)
	}

	if err := replaceInFile(targetDir+"/docker-compose.yml",
		`SECRET_KEY="PUBLIC KEY FROM REMNAWAVE-PANEL"`,
		fmt.Sprintf(`SECRET_KEY="%s"`, secretKey)); err != nil {
		return err
	}

	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("PUBLIC_KEY_SUCCESS"), ui.ColorReset)
	return nil
}

func GenerateXrayKeys(domainURL, token string) string {
	resp := MakeAPIRequest("GET", BaseURL(domainURL)+"/api/system/tools/x25519/generate", token, "")
	if len(resp) == 0 {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_GENERATE_KEYS"), ui.ColorReset)
		return ""
	}

	var errParsed struct {
		ErrorCode json.RawMessage `json:"errorCode"`
		Message   string          `json:"message"`
	}
	_ = json.Unmarshal(resp, &errParsed)
	if errParsed.ErrorCode != nil {
		fmt.Printf("%s%s: %s%s\n", ui.ColorRed, i18n.T("ERROR_GENERATE_KEYS"), errParsed.Message, ui.ColorReset)
		return ""
	}

	var parsed struct {
		Response struct {
			Keypairs []struct {
				PrivateKey string `json:"privateKey"`
			} `json:"keypairs"`
		} `json:"response"`
	}
	_ = json.Unmarshal(resp, &parsed)

	var privateKey string
	if len(parsed.Response.Keypairs) > 0 {
		privateKey = parsed.Response.Keypairs[0].PrivateKey
	}
	if privateKey == "" {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_EXTRACT_PRIVATE_KEY"), ui.ColorReset)
	}

	return privateKey
}

func CheckNodeDomain(domainURL, token, domain string) error {
	resp := MakeAPIRequest("GET", BaseURL(domainURL)+"/api/nodes", token, "")
	if len(resp) == 0 {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_CHECK_DOMAIN"), ui.ColorReset)
		return fmt.Errorf("empty response")
	}

	var parsed struct {
		Response []struct {
			Address string `json:"address"`
		} `json:"response"`
	}
	if err := json.Unmarshal(resp, &parsed); err != nil {
		var errParsed struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(resp, &errParsed)
		msg := errParsed.Message
		if msg == "" {
			msg = "Unknown error"
		}
		fmt.Printf("%s%s: %s%s\n", ui.ColorRed, i18n.T("ERROR_CHECK_DOMAIN"), msg, ui.ColorReset)
		return fmt.Errorf("%s", msg)
	}

	for _, node := range parsed.Response {
		if node.Address == domain {
			fmt.Printf("%s%s: %s%s\n", ui.ColorRed, i18n.T("DOMAIN_ALREADY_EXISTS"), domain, ui.ColorReset)
			return fmt.Errorf("domain already exists")
		}
	}
	return nil
}

func CreateNode(domainURL, token, configProfileUUID, inboundUUID string, nodeAddress, nodeName string) error {
	if nodeAddress == "" {
		nodeAddress = "172.30.0.1"
	}
	if nodeName == "" {
		nodeName = "Steal"
	}

	nodeData := map[string]any{
		"name":    nodeName,
		"address": nodeAddress,
		"port":    2222,
		"configProfile": map[string]any{
			"activeConfigProfileUuid": configProfileUUID,
			"activeInbounds":          []string{inboundUUID},
		},
		"isTrafficTrackingActive": false,
		"trafficLimitBytes":       0,
		"notifyPercent":           0,
		"trafficResetDay":         31,
		"excludedInbounds":        []string{},
		"countryCode":             "XX",
		"consumptionMultiplier":   1.0,
	}
	body, _ := json.Marshal(nodeData)

	resp := MakeAPIRequest("POST", BaseURL(domainURL)+"/api/nodes", token, string(body))
	if len(resp) == 0 {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_EMPTY_RESPONSE_NODE"), ui.ColorReset)
		return fmt.Errorf("empty response creating node")
	}

	var parsed struct {
		Response struct {
			UUID string `json:"uuid"`
		} `json:"response"`
	}
	if err := json.Unmarshal(resp, &parsed); err == nil && parsed.Response.UUID != "" {
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("NODE_CREATED"), ui.ColorReset)
		return nil
	}
	fmt.Printf("%s%s: %s%s\n", ui.ColorRed, i18n.T("ERROR_CREATE_NODE"), string(resp), ui.ColorReset)
	return fmt.Errorf("create node failed: %s", resp)
}

func GetConfigProfiles(domainURL, token string) (string, error) {
	resp := MakeAPIRequest("GET", BaseURL(domainURL)+"/api/config-profiles", token, "")
	if len(resp) == 0 || !json.Valid(resp) {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_NO_CONFIGS"), ui.ColorReset)
		return "", fmt.Errorf("no configs")
	}

	var parsed struct {
		Response struct {
			ConfigProfiles []struct {
				Name string `json:"name"`
				UUID string `json:"uuid"`
			} `json:"configProfiles"`
		} `json:"response"`
	}
	_ = json.Unmarshal(resp, &parsed)

	for _, p := range parsed.Response.ConfigProfiles {
		if p.Name == "Default-Profile" {
			return p.UUID, nil
		}
	}
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("NO_DEFAULT_PROFILE"), ui.ColorReset)
	return "", nil
}

// DeleteConfigProfile removes a config profile. Panel 3.2.0 and later
// answer a successful DELETE with 204 No Content and an empty body, so
// success is decided by the HTTP status code, not the body.
func DeleteConfigProfile(domainURL, token, profileUUID string) error {
	if profileUUID == "" {
		uuid, err := GetConfigProfiles(domainURL, token)
		if err != nil || uuid == "" {
			return nil
		}
		profileUUID = uuid
	}

	status, resp := makeAPIRequestWithStatus("DELETE", BaseURL(domainURL)+"/api/config-profiles/"+profileUUID, token, "")
	if status < 200 || status >= 300 {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_DELETE_PROFILE"), ui.ColorReset)
		return fmt.Errorf("delete failed with status %d: %s", status, resp)
	}
	return nil
}

// FindConfigProfileByName looks up a config profile by its exact name,
// returning its UUID and current config content. Used by the node
// profile menu to locate an existing profile before changing its
// inbound selection.
func FindConfigProfileByName(domainURL, token, name string) (uuid string, config map[string]any, err error) {
	resp := MakeAPIRequest("GET", BaseURL(domainURL)+"/api/config-profiles", token, "")
	if len(resp) == 0 || !json.Valid(resp) {
		return "", nil, fmt.Errorf("no configs")
	}

	var parsed struct {
		Response struct {
			ConfigProfiles []struct {
				Name string `json:"name"`
				UUID string `json:"uuid"`
			} `json:"configProfiles"`
		} `json:"response"`
	}
	if err := json.Unmarshal(resp, &parsed); err != nil {
		return "", nil, err
	}
	for _, p := range parsed.Response.ConfigProfiles {
		if p.Name == name {
			uuid = p.UUID
			break
		}
	}
	if uuid == "" {
		return "", nil, fmt.Errorf("config profile %q not found", name)
	}

	detailResp := MakeAPIRequest("GET", BaseURL(domainURL)+"/api/config-profiles/"+uuid, token, "")
	if len(detailResp) == 0 || !json.Valid(detailResp) {
		return uuid, nil, fmt.Errorf("empty response fetching config profile %s", uuid)
	}
	var detail struct {
		Response struct {
			Config map[string]any `json:"config"`
		} `json:"response"`
	}
	if err := json.Unmarshal(detailResp, &detail); err != nil {
		return uuid, nil, err
	}
	return uuid, detail.Response.Config, nil
}

// UpdateConfigProfile replaces a config profile's config in place.
// Per the panel's actual contract, this is PATCH /api/config-profiles
// (no UUID in the URL path) with a {uuid, config} body, and it replaces
// the whole config rather than merging fields into it - so config must
// already be the complete document the caller wants stored, not a
// partial patch.
func UpdateConfigProfile(domainURL, token, profileUUID string, config map[string]any) error {
	body, _ := json.Marshal(map[string]any{"uuid": profileUUID, "config": config})
	status, resp := makeAPIRequestWithStatus("PATCH", BaseURL(domainURL)+"/api/config-profiles", token, string(body))
	if status < 200 || status >= 300 {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_UPDATE_PROFILE"), ui.ColorReset)
		return fmt.Errorf("update config profile failed with status %d: %s", status, resp)
	}
	return nil
}

// ConfigProfileInbounds selects which inbounds a config profile's
// config carries. Raw (VLESS+Reality) is this project's stock,
// always-available inbound; Hysteria2 and XHTTP are optional additions
// a node can turn on later (see internal/nodeprofile).
type ConfigProfileInbounds struct {
	Raw       bool
	Hysteria2 bool
	XHTTP     bool
}

// buildInboundConfig returns the Xray inbounds array for the requested
// ConfigProfileInbounds selection, reusing existingByTag's settings for
// any inbound kind that was already present under that tag (so toggling
// Hysteria2/XHTTP on or off later doesn't rotate the Raw inbound's
// privateKey/shortIds and silently break every client already using
// it), and generating fresh secrets only for a kind that's being turned
// on for the first time. existingByTag may be nil.
func buildInboundConfig(sel ConfigProfileInbounds, domain, privateKey, rawTag, certFullchain, certPrivkey string, existingByTag map[string]map[string]any) []map[string]any {
	var inbounds []map[string]any

	if sel.Raw {
		if existing, ok := existingByTag[rawTag]; ok {
			inbounds = append(inbounds, existing)
		} else {
			shortID := randHex(8)
			inbounds = append(inbounds, map[string]any{
				"tag":      rawTag,
				"port":     443,
				"protocol": "vless",
				"settings": map[string]any{"clients": []any{}, "decryption": "none"},
				"sniffing": map[string]any{"enabled": true, "destOverride": []string{"http", "tls", "quic"}},
				"streamSettings": map[string]any{
					"network":  "tcp",
					"security": "reality",
					"realitySettings": map[string]any{
						"show":        false,
						"xver":        1,
						"dest":        "/dev/shm/nginx.sock",
						"spiderX":     "",
						"shortIds":    []string{shortID},
						"privateKey":  privateKey,
						"serverNames": []string{domain},
					},
				},
			})
		}
	}

	if sel.Hysteria2 {
		if existing, ok := existingByTag["HYSTERIA-BBR"]; ok {
			inbounds = append(inbounds, existing)
		} else {
			salamanderPassword := randHex(8)
			inbounds = append(inbounds, map[string]any{
				"tag":      "HYSTERIA-BBR",
				"port":     443,
				"listen":   "0.0.0.0",
				"protocol": "hysteria",
				"settings": map[string]any{"clients": []any{}, "version": 2},
				"sniffing": map[string]any{"enabled": true, "destOverride": []string{"http", "tls", "quic"}},
				"streamSettings": map[string]any{
					"network":  "hysteria",
					"security": "tls",
					"finalmask": map[string]any{
						"udp": []map[string]any{
							{"type": "salamander", "settings": map[string]any{"password": salamanderPassword}},
						},
						"quicParams": map[string]any{
							"debug":           false,
							"bbrProfile":      "standard",
							"congestion":      "bbr",
							"maxIdleTimeout":  90,
							"keepAlivePeriod": 20,
						},
					},
					"tlsSettings": map[string]any{
						"alpn": []string{"h3"},
						"certificates": []map[string]any{
							{"keyFile": certPrivkey, "certificateFile": certFullchain},
						},
					},
					"hysteriaSettings": map[string]any{
						"version": 2,
						"masquerade": map[string]any{
							"type": "proxy",
							"proxy": map[string]any{
								"url":         "https://" + domain,
								"rewriteHost": true,
							},
						},
						"udpIdleTimeout":        90,
						"ignoreClientBandwidth": true,
					},
				},
			})
		}
	}

	if sel.XHTTP {
		if existing, ok := existingByTag["XHTTP-TLS"]; ok {
			inbounds = append(inbounds, existing)
		} else {
			inbounds = append(inbounds, map[string]any{
				"tag":      "XHTTP-TLS",
				"port":     0,
				"listen":   "/dev/shm/xhttp.sock,0666",
				"protocol": "vless",
				"settings": map[string]any{"clients": []any{}, "decryption": "none"},
				"sniffing": map[string]any{"enabled": true, "destOverride": []string{"http", "tls", "quic"}},
				"streamSettings": map[string]any{
					"network":  "xhttp",
					"security": "none",
					"xhttpSettings": map[string]any{
						"host": domain,
						"path": "/api/v2/stream-events",
					},
				},
			})
		}
	}

	return inbounds
}

func buildProfileConfig(inbounds []map[string]any) map[string]any {
	return map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"dns": map[string]any{
			"queryStrategy": "UseIPv4",
			"servers": []map[string]any{
				{"address": "https://dns.google/dns-query", "skipFallback": false},
			},
		},
		"inbounds": inbounds,
		"outbounds": []map[string]any{
			{"tag": "DIRECT", "protocol": "freedom"},
			{"tag": "BLOCK", "protocol": "blackhole"},
		},
		"routing": map[string]any{
			"rules": []map[string]any{
				{"type": "field", "domain": []string{"geosite:private", "geosite:category-ru"}, "outboundTag": "BLOCK"},
				{"ip": []string{"geoip:private", "geoip:ru"}, "type": "field", "outboundTag": "BLOCK"},
				{"type": "field", "protocol": []string{"bittorrent"}, "outboundTag": "BLOCK"},
			},
		},
	}
}

// BuildProfileConfig builds a full profile config document for the
// given inbound selection, reusing existingByTag's settings for any
// inbound kind already present under its tag (existingByTag may be
// nil). Exported for internal/nodeprofile, which builds a replacement
// config for an already-existing profile before calling
// UpdateConfigProfile.
func BuildProfileConfig(sel ConfigProfileInbounds, domain, privateKey, rawTag, certFullchain, certPrivkey string, existingByTag map[string]map[string]any) map[string]any {
	return buildProfileConfig(buildInboundConfig(sel, domain, privateKey, rawTag, certFullchain, certPrivkey, existingByTag))
}

// MergeProfileConfig returns existing with its "inbounds" replaced by the
// managed inbounds for the given selection, keeping everything else the
// operator may have customised: log, dns, outbounds, routing and any
// inbound whose tag this tool does not manage. A nil existing config
// yields the stock document.
func MergeProfileConfig(existing map[string]any, sel ConfigProfileInbounds, domain, privateKey, rawTag, certFullchain, certPrivkey string, existingByTag map[string]map[string]any) map[string]any {
	managed := buildInboundConfig(sel, domain, privateKey, rawTag, certFullchain, certPrivkey, existingByTag)
	if existing == nil {
		return buildProfileConfig(managed)
	}

	managedTags := map[string]bool{rawTag: true, "HYSTERIA-BBR": true, "XHTTP-TLS": true}
	inbounds := make([]any, 0, len(managed))
	for _, ib := range managed {
		inbounds = append(inbounds, ib)
	}
	if old, ok := existing["inbounds"].([]any); ok {
		for _, raw := range old {
			ib, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if tag, _ := ib["tag"].(string); !managedTags[tag] {
				inbounds = append(inbounds, ib)
			}
		}
	}

	merged := make(map[string]any, len(existing))
	for k, v := range existing {
		merged[k] = v
	}
	merged["inbounds"] = inbounds
	return merged
}

// CreateConfigProfile creates a config profile. By default (inbounds
// left as its zero value) it carries only the stock Raw (VLESS+Reality)
// inbound; internal/nodeprofile can add Hysteria2 and/or XHTTP to it
// later via UpdateConfigProfile. See ConfigProfileInbounds and
// buildInboundConfig for what each inbound needs.
func CreateConfigProfile(domainURL, token, name, domain, privateKey, inboundTag, certFullchain, certPrivkey string, inbounds ConfigProfileInbounds) (string, string) {
	if inboundTag == "" {
		inboundTag = "Raw"
	}
	if !inbounds.Raw && !inbounds.Hysteria2 && !inbounds.XHTTP {
		inbounds.Raw = true
	}

	requestBody := map[string]any{
		"name":   name,
		"config": buildProfileConfig(buildInboundConfig(inbounds, domain, privateKey, inboundTag, certFullchain, certPrivkey, nil)),
	}
	body, _ := json.Marshal(requestBody)

	resp := MakeAPIRequest("POST", BaseURL(domainURL)+"/api/config-profiles", token, string(body))

	var parsed struct {
		Response struct {
			UUID     string `json:"uuid"`
			Inbounds []struct {
				UUID string `json:"uuid"`
				Tag  string `json:"tag"`
			} `json:"inbounds"`
		} `json:"response"`
	}
	unmarshalErr := json.Unmarshal(resp, &parsed)
	if len(resp) == 0 || unmarshalErr != nil || parsed.Response.UUID == "" {
		fmt.Printf("%s%s: %s%s\n", ui.ColorRed, i18n.T("ERROR_CREATE_CONFIG_PROFILE"), string(resp), ui.ColorReset)
		return "", ""
	}

	configUUID := parsed.Response.UUID
	// Match the primary inbound by tag rather than assuming it's first
	// in the response: nothing guarantees the panel echoes inbounds
	// back in submission order, and getting this wrong would silently
	// hand CreateNode/CreateHost/UpdateSquad a Hysteria2 or XHTTP
	// inbound's UUID instead of Raw's.
	var inboundUUID string
	for _, ib := range parsed.Response.Inbounds {
		if ib.Tag == inboundTag {
			inboundUUID = ib.UUID
			break
		}
	}
	if inboundUUID == "" && len(parsed.Response.Inbounds) > 0 {
		inboundUUID = parsed.Response.Inbounds[0].UUID
	}
	if configUUID == "" || inboundUUID == "" {
		fmt.Printf("%s%s: Invalid UUIDs in response: %s%s\n", ui.ColorRed, i18n.T("ERROR_CREATE_CONFIG_PROFILE"), string(resp), ui.ColorReset)
	}

	return configUUID, inboundUUID
}

func CreateHost(domainURL, token, inboundUUID, address, configUUID, hostRemark string) error {
	if hostRemark == "" {
		hostRemark = "Steal"
	}

	requestBody := map[string]any{
		"inbound": map[string]any{
			"configProfileUuid":        configUUID,
			"configProfileInboundUuid": inboundUUID,
		},
		"remark":        hostRemark,
		"address":       address,
		"port":          443,
		"path":          "",
		"sni":           address,
		"host":          "",
		"alpn":          nil,
		"fingerprint":   "chrome",
		"allowInsecure": false,
		"isDisabled":    false,
		"securityLayer": "DEFAULT",
	}
	body, _ := json.Marshal(requestBody)

	resp := MakeAPIRequest("POST", BaseURL(domainURL)+"/api/hosts", token, string(body))
	if len(resp) == 0 {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_EMPTY_RESPONSE_HOST"), ui.ColorReset)
		return fmt.Errorf("empty response creating host")
	}

	var parsed struct {
		Response struct {
			UUID string `json:"uuid"`
		} `json:"response"`
	}
	if err := json.Unmarshal(resp, &parsed); err == nil && parsed.Response.UUID != "" {
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("HOST_CREATED"), ui.ColorReset)
		return nil
	}
	fmt.Printf("%s%s: %s%s\n", ui.ColorRed, i18n.T("ERROR_CREATE_HOST"), string(resp), ui.ColorReset)
	return fmt.Errorf("create host failed: %s", resp)
}

// Returns the list of valid squad UUIDs, filtering out anything that
// doesn't look like a UUID.
func GetDefaultSquad(domainURL, token string) ([]string, error) {
	resp := MakeAPIRequest("GET", BaseURL(domainURL)+"/api/internal-squads", token, "")

	var parsed struct {
		Response struct {
			InternalSquads []struct {
				UUID string `json:"uuid"`
			} `json:"internalSquads"`
		} `json:"response"`
	}
	if len(resp) == 0 || json.Unmarshal(resp, &parsed) != nil {
		fmt.Printf("%s%s: %s%s\n", ui.ColorRed, i18n.T("ERROR_GET_SQUAD"), string(resp), ui.ColorReset)
		return nil, fmt.Errorf("error getting squads")
	}

	if len(parsed.Response.InternalSquads) == 0 {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("NO_SQUADS_FOUND"), ui.ColorReset)
		return nil, nil
	}

	var valid []string
	for _, s := range parsed.Response.InternalSquads {
		if s.UUID == "" {
			continue
		}
		if uuidRE.MatchString(s.UUID) {
			valid = append(valid, s.UUID)
		} else {
			fmt.Printf("%s%s: %s%s\n", ui.ColorRed, i18n.T("INVALID_UUID_FORMAT"), s.UUID, ui.ColorReset)
		}
	}

	if len(valid) == 0 {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("NO_VALID_SQUADS_FOUND"), ui.ColorReset)
		return nil, nil
	}

	return valid, nil
}

func UpdateSquad(domainURL, token, squadUUID, inboundUUID string) error {
	if !uuidRE.MatchString(squadUUID) {
		fmt.Printf("%s%s: %s%s\n", ui.ColorRed, i18n.T("INVALID_SQUAD_UUID"), squadUUID, ui.ColorReset)
		return fmt.Errorf("invalid squad uuid")
	}
	if !uuidRE.MatchString(inboundUUID) {
		fmt.Printf("%s%s: %s%s\n", ui.ColorRed, i18n.T("INVALID_INBOUND_UUID"), inboundUUID, ui.ColorReset)
		return fmt.Errorf("invalid inbound uuid")
	}

	squadResp := MakeAPIRequest("GET", BaseURL(domainURL)+"/api/internal-squads", token, "")
	var parsed struct {
		Response struct {
			InternalSquads []struct {
				UUID     string `json:"uuid"`
				Inbounds []struct {
					UUID string `json:"uuid"`
				} `json:"inbounds"`
			} `json:"internalSquads"`
		} `json:"response"`
	}
	if len(squadResp) == 0 || json.Unmarshal(squadResp, &parsed) != nil {
		fmt.Printf("%s%s: %s%s\n", ui.ColorRed, i18n.T("ERROR_GET_SQUAD"), string(squadResp), ui.ColorReset)
		return fmt.Errorf("error getting squads")
	}

	var existingInbounds []string
	for _, s := range parsed.Response.InternalSquads {
		if s.UUID == squadUUID {
			for _, ib := range s.Inbounds {
				existingInbounds = append(existingInbounds, ib.UUID)
			}
		}
	}

	inboundsArray := append([]string{}, existingInbounds...)
	found := false
	for _, u := range existingInbounds {
		if u == inboundUUID {
			found = true
			break
		}
	}
	if !found {
		inboundsArray = append(inboundsArray, inboundUUID)
	}

	requestBody, _ := json.Marshal(map[string]any{
		"uuid":     squadUUID,
		"inbounds": inboundsArray,
	})

	resp := MakeAPIRequest("PATCH", BaseURL(domainURL)+"/api/internal-squads", token, string(requestBody))
	var respParsed struct {
		Response struct {
			UUID string `json:"uuid"`
		} `json:"response"`
	}
	if len(resp) == 0 || json.Unmarshal(resp, &respParsed) != nil || respParsed.Response.UUID == "" {
		fmt.Printf("%s%s: %s%s\n", ui.ColorRed, i18n.T("ERROR_UPDATE_SQUAD"), string(resp), ui.ColorReset)
		return fmt.Errorf("update failed")
	}
	return nil
}

func CreateAPIToken(domainURL, token, targetDir, tokenName string) error {
	if tokenName == "" {
		tokenName = "subscription-page"
	}

	tokenData, _ := json.Marshal(map[string]string{"tokenName": tokenName})
	resp := MakeAPIRequest("POST", BaseURL(domainURL)+"/api/tokens", token, string(tokenData))

	if len(resp) == 0 {
		fmt.Fprintf(os.Stderr, "%s%s%s\n", ui.ColorRed, i18n.T("ERROR_CREATE_API_TOKEN"), ui.ColorReset)
		return fmt.Errorf("empty response")
	}

	var parsed struct {
		Response struct {
			Token string `json:"token"`
		} `json:"response"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(resp, &parsed)
	apiToken := parsed.Response.Token
	if apiToken == "" {
		msg := parsed.Message
		if msg == "" {
			msg = "Unknown error"
		}
		fmt.Fprintf(os.Stderr, "%s%s: %s%s\n", ui.ColorRed, i18n.T("ERROR_CREATE_API_TOKEN"), msg, ui.ColorReset)
		return fmt.Errorf("%s", msg)
	}

	if err := setComposeEnv(targetDir+"/docker-compose.yml", "REMNAWAVE_API_TOKEN", apiToken); err != nil {
		fmt.Fprintf(os.Stderr, "%s%s: %v%s\n", ui.ColorRed, i18n.T("ERROR_CREATE_API_TOKEN"), err, ui.ColorReset)
		return err
	}

	fmt.Fprintf(os.Stderr, "%s%s%s\n", ui.ColorGreen, i18n.T("API_TOKEN_ADDED"), ui.ColorReset)
	return nil
}
