package api

import (
	"encoding/json"
	"sort"
	"testing"
)

// The key sets below are the fields of the Xray documentation
// (xtls.github.io/en/config/transports/reality.html, hysteria.html and
// finalmask.html). A generated inbound that uses another key is either
// ignored by Xray or means a field was put in the wrong place.
var (
	realityServerKeys = set("show", "target", "dest", "xver", "serverNames", "privateKey", "minClientVer", "maxClientVer",
		"maxTimeDiff", "shortIds", "mldsa65Seed", "limitFallbackUpload", "limitFallbackDownload")
	hysteriaSettingsKeys = set("version", "auth", "udpIdleTimeout", "masquerade")
	masqueradeKeys       = set("type", "dir", "url", "rewriteHost", "insecure", "content", "headers", "statusCode")
	quicParamsKeys       = set("congestion", "bbrProfile", "debug", "brutalUp", "brutalDown", "udpHop",
		"initStreamReceiveWindow", "maxStreamReceiveWindow", "initConnectionReceiveWindow", "maxConnectionReceiveWindow",
		"maxIdleTimeout", "keepAlivePeriod", "disablePathMTUDiscovery", "maxIncomingStreams")
	hysteriaInboundKeys = set("version", "users")
)

func set(keys ...string) map[string]bool {
	m := map[string]bool{}
	for _, k := range keys {
		m[k] = true
	}
	return m
}

// asMap round-trips v through JSON, so the test sees what Xray would read.
func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func sub(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("%q is missing or not an object in %v", key, m)
	}
	return v
}

func onlyKeys(t *testing.T, where string, m map[string]any, allowed map[string]bool) {
	t.Helper()
	var bad []string
	for k := range m {
		if !allowed[k] {
			bad = append(bad, k)
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Errorf("%s has keys that the Xray documentation does not list: %v", where, bad)
	}
}

func allInbounds(t *testing.T) map[string]map[string]any {
	t.Helper()
	list := buildInboundConfig(ConfigProfileInbounds{Raw: true, Hysteria2: true, XHTTP: true},
		"node.example.com", "PRIVATEKEY", "RAW-TAG", "/cert/fullchain.pem", "/cert/privkey.pem", nil)
	out := map[string]map[string]any{}
	for _, ib := range list {
		m := asMap(t, ib)
		out[m["tag"].(string)] = m
	}
	return out
}

func TestRealityInboundUsesTheDocumentedFields(t *testing.T) {
	raw := allInbounds(t)["RAW-TAG"]
	reality := sub(t, sub(t, raw, "streamSettings"), "realitySettings")
	onlyKeys(t, "realitySettings", reality, realityServerKeys)
	if reality["target"] != "/dev/shm/nginx.sock" {
		t.Errorf("target = %v", reality["target"])
	}
	if _, has := reality["spiderX"]; has {
		t.Error("spiderX is a client field and does not belong in an inbound")
	}
	for _, id := range reality["shortIds"].([]any) {
		s := id.(string)
		if len(s) > 16 || len(s)%2 != 0 {
			t.Errorf("shortId %q must have an even length of at most 16 hex digits", s)
		}
	}
	if settings := sub(t, raw, "settings"); settings["decryption"] != "none" {
		t.Errorf("a VLESS inbound needs decryption none, got %v", settings["decryption"])
	}
}

func TestHysteriaInboundUsesTheDocumentedFields(t *testing.T) {
	hy := allInbounds(t)["HYSTERIA-BBR"]
	onlyKeys(t, "hysteria inbound settings", sub(t, hy, "settings"), hysteriaInboundKeys)
	stream := sub(t, hy, "streamSettings")
	hs := sub(t, stream, "hysteriaSettings")
	onlyKeys(t, "hysteriaSettings", hs, hysteriaSettingsKeys)
	masq := sub(t, hs, "masquerade")
	onlyKeys(t, "masquerade", masq, masqueradeKeys)
	if masq["type"] != "proxy" || masq["url"] != "https://node.example.com" || masq["rewriteHost"] != true {
		t.Errorf("masquerade = %v, want a flat proxy to the node's own site", masq)
	}
	onlyKeys(t, "quicParams", sub(t, sub(t, stream, "finalmask"), "quicParams"), quicParamsKeys)
	if stream["security"] != "tls" || sub(t, stream, "tlsSettings")["alpn"].([]any)[0] != "h3" {
		t.Error("a Hysteria inbound needs TLS with the h3 ALPN")
	}
}

// The first versions of this tool wrote the masquerade url into a nested
// "proxy" object, which Xray does not read, and "clients" for the users.
func TestNormalizeHysteriaInboundFlattensTheOldShape(t *testing.T) {
	old := map[string]any{
		"tag":      "HYSTERIA-BBR",
		"settings": map[string]any{"clients": []any{}, "version": 2},
		"streamSettings": map[string]any{"hysteriaSettings": map[string]any{
			"version": 2,
			"masquerade": map[string]any{
				"type":  "proxy",
				"proxy": map[string]any{"url": "https://node.example.com", "rewriteHost": true},
			},
			"udpIdleTimeout": 90,
		}},
	}
	got := asMap(t, normalizeHysteriaInbound(old))
	masq := sub(t, sub(t, sub(t, got, "streamSettings"), "hysteriaSettings"), "masquerade")
	if masq["url"] != "https://node.example.com" || masq["rewriteHost"] != true || masq["proxy"] != nil {
		t.Errorf("masquerade = %v", masq)
	}
	settings := sub(t, got, "settings")
	if _, has := settings["users"]; !has {
		t.Error("clients were not renamed to users")
	}
	if _, has := settings["clients"]; has {
		t.Error("clients stayed")
	}
	// Idempotent: a second pass changes nothing.
	again := asMap(t, normalizeHysteriaInbound(got))
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(again)
	if string(a) != string(b) {
		t.Errorf("a second pass changed the inbound:\n%s\n%s", a, b)
	}
}

func TestNormalizeHysteriaInboundKeepsWhatTheOperatorSet(t *testing.T) {
	in := map[string]any{
		"settings": map[string]any{"users": []any{map[string]any{"auth": "x"}}, "version": 2},
		"streamSettings": map[string]any{"hysteriaSettings": map[string]any{
			"masquerade": map[string]any{"type": "string", "content": "hello", "statusCode": 200},
		}},
	}
	got := asMap(t, normalizeHysteriaInbound(in))
	masq := sub(t, sub(t, sub(t, got, "streamSettings"), "hysteriaSettings"), "masquerade")
	if masq["type"] != "string" || masq["content"] != "hello" {
		t.Errorf("an operator's masquerade was changed: %v", masq)
	}
	if users := sub(t, got, "settings")["users"].([]any); len(users) != 1 {
		t.Errorf("users = %v", users)
	}
}

func TestReusedHysteriaInboundIsNormalizedByTheMerge(t *testing.T) {
	existing := map[string]map[string]any{"HYSTERIA-BBR": {
		"tag":      "HYSTERIA-BBR",
		"settings": map[string]any{"clients": []any{}, "version": 2},
		"streamSettings": map[string]any{"hysteriaSettings": map[string]any{
			"masquerade": map[string]any{"type": "proxy", "proxy": map[string]any{"url": "https://x.example.com"}},
		}},
	}}
	list := buildInboundConfig(ConfigProfileInbounds{Hysteria2: true}, "x.example.com", "", "RAW", "", "", existing)
	m := asMap(t, list[0])
	masq := sub(t, sub(t, sub(t, m, "streamSettings"), "hysteriaSettings"), "masquerade")
	if masq["url"] != "https://x.example.com" {
		t.Errorf("masquerade = %v", masq)
	}
}
