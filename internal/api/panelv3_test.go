// This file guards two behavior changes introduced by Remnawave Panel
// v3.2.0 that would otherwise regress silently: GET /api/keygen renamed
// its response field from pubKey to secretKey, and DELETE endpoints
// switched from 200+JSON body to 204 No Content on success. See the
// BUG FIX comments on GetPublicKey and DeleteConfigProfile in api.go.
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
