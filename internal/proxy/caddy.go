package proxy

import "fmt"

type caddy struct{}

func (caddy) Kind() Kind         { return Caddy }
func (caddy) ConfigFile() string { return "Caddyfile" }

// Panel renders a Caddyfile whose domains stay as {$VAR} references that
// Caddy resolves from the container environment, so only the cookie is
// substituted here.
func (caddy) Panel(p Params) (string, error) {
	if p.Layout == Socket {
		return fmt.Sprintf(caddySocketTemplate, p.CookieName, p.CookieValue), nil
	}
	return fmt.Sprintf(caddyDirectTemplate, p.CookieName, p.CookieValue), nil
}

func (caddy) Node(Params) (string, error) { return caddyNodeTemplate, nil }

func (caddy) Extra(Params) map[string]string { return nil }
