// Tests for two Remnawave Panel 3.2.0 API changes: GET /api/keygen
// returns the key as secretKey (it was pubKey), and DELETE endpoints
// answer 204 No Content on success.
package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestGetPublicKeyUsesSecretKeyField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/keygen") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"response":{"secretKey":"abc123"}}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	composePath := dir + "/docker-compose.yml"
	writeFile(t, composePath, `SECRET_KEY="PUBLIC KEY FROM REMNAWAVE-PANEL"`)

	GetPublicKey(strings.TrimPrefix(srv.URL, "http://"), "tok", dir)

	data := readFile(t, composePath)
	if !strings.Contains(data, `SECRET_KEY="abc123"`) {
		t.Errorf("expected SECRET_KEY to be patched from response.secretKey, got: %s", data)
	}
}

func TestDeleteConfigProfileTreats204EmptyBodyAsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Fatalf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(204) // no body written, matching Panel v3.2.0
	}))
	defer srv.Close()

	if err := DeleteConfigProfile(strings.TrimPrefix(srv.URL, "http://"), "tok", "some-uuid"); err != nil {
		t.Errorf("expected nil error for 204 No Content, got: %v", err)
	}
}

func TestDeleteConfigProfileTreatsErrorStatusAsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"message":"not found"}`))
	}))
	defer srv.Close()

	if err := DeleteConfigProfile(strings.TrimPrefix(srv.URL, "http://"), "tok", "some-uuid"); err == nil {
		t.Error("expected an error for a 404 response, got nil")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRequestWithoutTokenSendsNoAuthorizationHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
	}))
	defer srv.Close()
	MakeAPIRequest("GET", srv.URL, "", "")
	if got != "" {
		t.Errorf("Authorization = %q, want no header", got)
	}
	MakeAPIRequest("GET", srv.URL, "tok-123456", "")
	if got != "Bearer tok-123456" {
		t.Errorf("Authorization = %q, want the bearer token", got)
	}
}
