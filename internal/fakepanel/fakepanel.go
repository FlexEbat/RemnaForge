// Package fakepanel is a stand-in for the Remnawave panel API, for tests of
// the code that provisions a fresh panel. It answers the endpoints the
// install flows call with the response shapes the api package reads, hands
// out predictable ids, and records every request so a test can compare the
// whole sequence.
package fakepanel

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// Token is the access token the fake issues at registration.
const Token = "fake-access-token"

// Ids the fake always uses for the objects a fresh panel starts with.
const (
	SquadUUID          = "11111111-1111-4111-8111-111111111111"
	OldInboundUUID     = "22222222-2222-4222-8222-222222222222"
	DefaultProfileUUID = "33333333-3333-4333-8333-333333333333"
)

// Request is one call the code under test made.
type Request struct {
	Method string
	Path   string
	Body   string // as sent; empty for requests without a body
	Authed bool   // the request carried the token issued at registration
}

// Panel is a running fake.
type Panel struct {
	server *httptest.Server

	mu       sync.Mutex
	requests []Request
	nextID   int

	// FailOn makes the endpoint "METHOD /path" answer with an error body,
	// so a test can see how the code reacts to a refusal.
	FailOn map[string]bool
	// NoSquads makes the squad list empty.
	NoSquads bool
}

// New starts a fake panel. Close it with Close.
func New() *Panel {
	p := &Panel{FailOn: map[string]bool{}}
	p.server = httptest.NewServer(http.HandlerFunc(p.handle))
	return p
}

// Close stops the server.
func (p *Panel) Close() { p.server.Close() }

// Host is the address to pass as the domain URL, host:port without a scheme,
// as the install flows do for 127.0.0.1:3000.
func (p *Panel) Host() string { return strings.TrimPrefix(p.server.URL, "http://") }

// Requests returns what was received, in order.
func (p *Panel) Requests() []Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Request(nil), p.requests...)
}

// Reset forgets the recorded requests and the counters, keeping the
// settings, so one fake can serve several runs in a test.
func (p *Panel) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests, p.nextID = nil, 0
}

// uuid hands out ids in the format the panel uses; the api package refuses
// anything else.
func (p *Panel) uuid() string {
	p.nextID++
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", p.nextID)
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func (p *Panel) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	p.mu.Lock()
	defer p.mu.Unlock()
	authed := r.Header.Get("Authorization") == "Bearer "+Token
	p.requests = append(p.requests, Request{r.Method, r.URL.Path, string(body), authed})

	key := r.Method + " " + r.URL.Path
	if p.FailOn[key] {
		reply(w, http.StatusBadRequest, map[string]any{"errorCode": "A000", "message": "refused by the test"})
		return
	}
	resp := func(v any) { reply(w, http.StatusOK, map[string]any{"response": v}) }

	switch {
	case key == "POST /api/auth/register":
		resp(map[string]any{"accessToken": Token})
	case !authed:
		reply(w, http.StatusUnauthorized, map[string]any{"message": "Unauthorized"})
	case key == "GET /api/keygen":
		resp(map[string]any{"secretKey": "SECRET-FROM-PANEL"})
	case key == "GET /api/system/tools/x25519/generate":
		resp(map[string]any{"keypairs": []map[string]any{{"privateKey": "PRIVATE-KEY", "publicKey": "PUBLIC-KEY"}}})
	case key == "GET /api/config-profiles":
		resp(map[string]any{"configProfiles": []map[string]any{{"name": "Default-Profile", "uuid": DefaultProfileUUID}}})
	case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/api/config-profiles/"):
		reply(w, http.StatusNoContent, nil)
	case key == "POST /api/config-profiles":
		var req struct {
			Config struct {
				Inbounds []struct {
					Tag string `json:"tag"`
				} `json:"inbounds"`
			} `json:"config"`
		}
		_ = json.Unmarshal(body, &req)
		var inbounds []map[string]any
		for _, ib := range req.Config.Inbounds {
			inbounds = append(inbounds, map[string]any{"uuid": p.uuid(), "tag": ib.Tag})
		}
		resp(map[string]any{"uuid": p.uuid(), "inbounds": inbounds})
	case key == "POST /api/nodes":
		resp(map[string]any{"uuid": p.uuid()})
	case key == "POST /api/hosts":
		resp(map[string]any{"uuid": p.uuid()})
	case key == "GET /api/internal-squads":
		squads := []map[string]any{{"uuid": SquadUUID, "inbounds": []map[string]any{{"uuid": OldInboundUUID}}}}
		if p.NoSquads {
			squads = nil
		}
		resp(map[string]any{"internalSquads": squads})
	case key == "PATCH /api/internal-squads":
		resp(map[string]any{"uuid": SquadUUID})
	case key == "POST /api/tokens":
		resp(map[string]any{"token": "API-TOKEN-FOR-SUBSCRIPTION"})
	default:
		reply(w, http.StatusNotFound, map[string]any{"message": "no such endpoint in the fake: " + key})
	}
}
