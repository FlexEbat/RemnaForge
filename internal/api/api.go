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

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

// DirRemnawave is this tool's own config/state directory.
var DirRemnawave = "/usr/local/remnawave_reverse/"

// PanelDomain is the panel domain set during panel install, used by
// GetPanelToken's create-API-token instructions.
var PanelDomain string

var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// httpClient is reused across requests rather than constructed fresh
// each time.
var httpClient = &http.Client{}

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
	req.Header.Set("X-Forwarded-For", strings.TrimPrefix(url, "http://"))
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Remnawave-Client-Type", "browser")

	resp, err := httpClient.Do(req)
	if err != nil {
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
	registerData := fmt.Sprintf(`{"username":"%s","password":"%s"}`, username, password)
	resp := MakeAPIRequest("POST", "http://"+domainURL+"/api/auth/register", token, registerData)

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
	authStatus := MakeAPIRequest("GET", "http://"+domainURL+"/api/auth/status", "", "")
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
		testResponse := MakeAPIRequest("GET", domainURL+"/api/config-profiles", token, "")

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

			testResponse := MakeAPIRequest("GET", domainURL+"/api/config-profiles", token, "")
			if !hasConfigProfiles(testResponse) {
				fmt.Printf("%s%s: %s%s\n", ui.ColorRed, i18n.T("INVALID_SAVED_TOKEN"), string(testResponse), ui.ColorReset)
				return "", fmt.Errorf("invalid token")
			}
		} else {
			username := ui.Reading(i18n.T("ENTER_PANEL_USERNAME"))
			password := ui.Reading(i18n.T("ENTER_PANEL_PASSWORD"))

			loginBody, _ := json.Marshal(map[string]string{"username": username, "password": password})
			loginResponse := MakeAPIRequest("POST", domainURL+"/api/auth/login", "", string(loginBody))

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

		_ = os.WriteFile(tokenFile, []byte(token), 0600)
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("TOKEN_RECEIVED_AND_SAVED"), ui.ColorReset)
	} else {
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("TOKEN_USED_SUCCESSFULLY"), ui.ColorReset)
	}

	finalTestResponse := MakeAPIRequest("GET", domainURL+"/api/config-profiles", token, "")
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

// GetPublicKey fetches the node's public key from the panel API and
// substitutes it into the node's docker-compose.yml, replacing the
// placeholder SECRET_KEY value written at file-creation time.
//
// This does not stop on error: it prints the error and falls through to
// attempt the file edit regardless, matching every other error-reporting
// step in this package.
//
// FIXED: Remnawave Panel v3.2.0 renamed the /api/keygen response field
// from response.pubKey to response.secretKey.
func GetPublicKey(domainURL, token, targetDir string) {
	resp := MakeAPIRequest("GET", "http://"+domainURL+"/api/keygen", token, "")
	if len(resp) == 0 {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_PUBLIC_KEY"), ui.ColorReset)
	}

	var parsed struct {
		Response struct {
			SecretKey string `json:"secretKey"`
		} `json:"response"`
	}
	_ = json.Unmarshal(resp, &parsed)
	pubkey := parsed.Response.SecretKey
	if pubkey == "" {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_EXTRACT_PUBLIC_KEY"), ui.ColorReset)
	}

	replaceInFile(targetDir+"/docker-compose.yml",
		`SECRET_KEY="PUBLIC KEY FROM REMNAWAVE-PANEL"`,
		fmt.Sprintf(`SECRET_KEY="%s"`, pubkey))

	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("PUBLIC_KEY_SUCCESS"), ui.ColorReset)
}

func GenerateXrayKeys(domainURL, token string) string {
	resp := MakeAPIRequest("GET", "http://"+domainURL+"/api/system/tools/x25519/generate", token, "")
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
	resp := MakeAPIRequest("GET", "http://"+domainURL+"/api/nodes", token, "")
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

func CreateNode(domainURL, token, configProfileUUID, inboundUUID string, nodeAddress, nodeName string) {
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

	resp := MakeAPIRequest("POST", "http://"+domainURL+"/api/nodes", token, string(body))
	if len(resp) == 0 {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_EMPTY_RESPONSE_NODE"), ui.ColorReset)
	}

	var parsed struct {
		Response struct {
			UUID string `json:"uuid"`
		} `json:"response"`
	}
	if err := json.Unmarshal(resp, &parsed); err == nil && parsed.Response.UUID != "" {
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("NODE_CREATED"), ui.ColorReset)
	} else {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_CREATE_NODE"), ui.ColorReset)
	}
}

func GetConfigProfiles(domainURL, token string) (string, error) {
	resp := MakeAPIRequest("GET", "http://"+domainURL+"/api/config-profiles", token, "")
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

// FIXED: before Remnawave Panel v3.2.0, a successful DELETE returned
// 200 with a JSON body, and an empty response body reliably meant
// failure. Since v3.2.0, a successful DELETE returns 204 No Content
// with an empty body instead, so checking len(resp) == 0 for failure
// now misreports every successful deletion as an error. This checks
// the actual HTTP status code instead.
func DeleteConfigProfile(domainURL, token, profileUUID string) error {
	if profileUUID == "" {
		uuid, err := GetConfigProfiles(domainURL, token)
		if err != nil || uuid == "" {
			return nil
		}
		profileUUID = uuid
	}

	status, resp := makeAPIRequestWithStatus("DELETE", "http://"+domainURL+"/api/config-profiles/"+profileUUID, token, "")
	if status < 200 || status >= 300 {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_DELETE_PROFILE"), ui.ColorReset)
		return fmt.Errorf("delete failed with status %d: %s", status, resp)
	}
	return nil
}

func CreateConfigProfile(domainURL, token, name, domain, privateKey, inboundTag string) (string, string) {
	if inboundTag == "" {
		inboundTag = "Steal"
	}
	shortID := randHex(8)

	requestBody := map[string]any{
		"name": name,
		"config": map[string]any{
			"log": map[string]any{"loglevel": "warning"},
			"dns": map[string]any{
				"queryStrategy": "UseIPv4",
				"servers": []map[string]any{
					{"address": "https://dns.google/dns-query", "skipFallback": false},
				},
			},
			"inbounds": []map[string]any{
				{
					"tag":      inboundTag,
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
				},
			},
			"outbounds": []map[string]any{
				{"tag": "DIRECT", "protocol": "freedom"},
				{"tag": "BLOCK", "protocol": "blackhole"},
			},
			"routing": map[string]any{
				"rules": []map[string]any{
					{"ip": []string{"geoip:private"}, "type": "field", "outboundTag": "BLOCK"},
					{"type": "field", "protocol": []string{"bittorrent"}, "outboundTag": "BLOCK"},
				},
			},
		},
	}
	body, _ := json.Marshal(requestBody)

	resp := MakeAPIRequest("POST", "http://"+domainURL+"/api/config-profiles", token, string(body))

	var parsed struct {
		Response struct {
			UUID     string `json:"uuid"`
			Inbounds []struct {
				UUID string `json:"uuid"`
			} `json:"inbounds"`
		} `json:"response"`
	}
	unmarshalErr := json.Unmarshal(resp, &parsed)
	if len(resp) == 0 || unmarshalErr != nil || parsed.Response.UUID == "" {
		fmt.Printf("%s%s: %s%s\n", ui.ColorRed, i18n.T("ERROR_CREATE_CONFIG_PROFILE"), string(resp), ui.ColorReset)
	}

	configUUID := parsed.Response.UUID
	var inboundUUID string
	if len(parsed.Response.Inbounds) > 0 {
		inboundUUID = parsed.Response.Inbounds[0].UUID
	}
	if configUUID == "" || inboundUUID == "" {
		fmt.Printf("%s%s: Invalid UUIDs in response: %s%s\n", ui.ColorRed, i18n.T("ERROR_CREATE_CONFIG_PROFILE"), string(resp), ui.ColorReset)
	}

	return configUUID, inboundUUID
}

func CreateHost(domainURL, token, inboundUUID, address, configUUID, hostRemark string) {
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

	resp := MakeAPIRequest("POST", "http://"+domainURL+"/api/hosts", token, string(body))
	if len(resp) == 0 {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_EMPTY_RESPONSE_HOST"), ui.ColorReset)
	}

	var parsed struct {
		Response struct {
			UUID string `json:"uuid"`
		} `json:"response"`
	}
	if err := json.Unmarshal(resp, &parsed); err == nil && parsed.Response.UUID != "" {
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("HOST_CREATED"), ui.ColorReset)
	} else {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_CREATE_HOST"), ui.ColorReset)
	}
}

// Returns the list of valid squad UUIDs, filtering out anything that
// doesn't look like a UUID.
func GetDefaultSquad(domainURL, token string) ([]string, error) {
	resp := MakeAPIRequest("GET", "http://"+domainURL+"/api/internal-squads", token, "")

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

	squadResp := MakeAPIRequest("GET", "http://"+domainURL+"/api/internal-squads", token, "")
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

	inboundsSet := map[string]bool{inboundUUID: true}
	for _, u := range existingInbounds {
		inboundsSet[u] = true
	}
	var inboundsArray []string
	for u := range inboundsSet {
		inboundsArray = append(inboundsArray, u)
	}

	requestBody, _ := json.Marshal(map[string]any{
		"uuid":     squadUUID,
		"inbounds": inboundsArray,
	})

	resp := MakeAPIRequest("PATCH", "http://"+domainURL+"/api/internal-squads", token, string(requestBody))
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
	resp := MakeAPIRequest("POST", "http://"+domainURL+"/api/tokens", token, string(tokenData))

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

	replaceLineInFile(targetDir+"/docker-compose.yml", "REMNAWAVE_API_TOKEN=", "REMNAWAVE_API_TOKEN="+apiToken)

	fmt.Fprintf(os.Stderr, "%s%s%s\n", ui.ColorGreen, i18n.T("API_TOKEN_ADDED"), ui.ColorReset)
	return nil
}
