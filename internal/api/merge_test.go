package api

import (
	"os"
	"testing"
)

func TestMergeProfileConfigKeepsCustomisations(t *testing.T) {
	existing := map[string]any{
		"routing":   map[string]any{"rules": []any{"custom"}},
		"outbounds": []any{map[string]any{"tag": "WARP"}},
		"inbounds": []any{
			map[string]any{"tag": "Raw", "port": 443},
			map[string]any{"tag": "MY-OWN", "port": 8080},
		},
	}
	byTag := map[string]map[string]any{"Raw": existing["inbounds"].([]any)[0].(map[string]any)}
	got := MergeProfileConfig(existing, ConfigProfileInbounds{Raw: true, Hysteria2: true}, "example.com", "key", "Raw", "/f", "/k", byTag)

	if _, ok := got["outbounds"].([]any); !ok {
		t.Fatal("outbounds were not preserved")
	}
	if got["routing"] == nil {
		t.Fatal("routing was not preserved")
	}
	tags := map[string]bool{}
	for _, ib := range got["inbounds"].([]any) {
		tags[ib.(map[string]any)["tag"].(string)] = true
	}
	for _, want := range []string{"Raw", "HYSTERIA-BBR", "MY-OWN"} {
		if !tags[want] {
			t.Errorf("inbound %s missing: %v", want, tags)
		}
	}
}

func TestBaseURL(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:3000":         "http://127.0.0.1:3000",
		"https://panel.example/": "https://panel.example",
		" http://a:1 ":           "http://a:1",
	}
	for in, want := range cases {
		if got := BaseURL(in); got != want {
			t.Errorf("BaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSetComposeEnvKeepsIndent(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/docker-compose.yml"
	orig := "    environment:\n      - APP_PORT=3010\n      - REMNAWAVE_API_TOKEN=$api_token\n"
	if err := writeKeepMode(p, []byte(orig)); err != nil {
		t.Fatal(err)
	}
	if err := setComposeEnv(p, "REMNAWAVE_API_TOKEN", "abc.def"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	got := string(b)
	want := "    environment:\n      - APP_PORT=3010\n      - REMNAWAVE_API_TOKEN=abc.def\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if err := setComposeEnv(p, "MISSING", "x"); err == nil {
		t.Fatal("expected error for missing key")
	}
}
