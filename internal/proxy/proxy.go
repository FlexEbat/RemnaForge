// Package proxy builds the reverse proxy configuration of the panel and the
// node. The install flows used to carry one copy of each template; they now
// ask a Backend, so a proxy is added in one place.
package proxy

import "fmt"

// Kind names a reverse proxy.
type Kind string

const (
	Nginx   Kind = "nginx"
	Caddy   Kind = "caddy"
	HAProxy Kind = "haproxy"
)

// Layout says who owns port 443.
type Layout int

const (
	// Direct: the proxy listens on 443 itself (a panel without a node on
	// the same host).
	Direct Layout = iota
	// Socket: Xray owns 443 and passes the SNI of the proxy's names to it
	// over a unix socket with PROXY protocol.
	Socket
)

// Params is everything a configuration depends on.
type Params struct {
	Layout Layout

	// The panel is reached with the query or cookie CookieName=CookieValue.
	CookieName, CookieValue string

	PanelDomain, PanelCert         string // Cert is the directory name under the certificate store
	SubDomain, SubCert             string
	SelfstealDomain, SelfstealCert string // a node's selfsteal name, empty on a panel without a node
}

// Backend renders the configuration file of one proxy.
type Backend interface {
	Kind() Kind
	// ConfigFile is the file name inside the stack directory.
	ConfigFile() string
	// Panel renders the panel's configuration: the panel and subscription
	// names, plus the selfsteal name when the node is co-located.
	Panel(p Params) (string, error)
	// Node renders the configuration of a node on its own host.
	Node(p Params) (string, error)
	// Extra returns the files besides ConfigFile that the stack needs, by
	// name: HAProxy's static selfsteal site. It is nil for the others.
	Extra(p Params) map[string]string
}

// For returns the backend of kind k.
func For(k Kind) (Backend, error) {
	switch k {
	case Nginx:
		return nginx{}, nil
	case Caddy:
		return caddy{}, nil
	case HAProxy:
		return haproxy{}, nil
	}
	return nil, fmt.Errorf("proxy %q is not supported", k)
}

// Must is For for a kind the caller knows to be supported.
func Must(k Kind) Backend {
	b, err := For(k)
	if err != nil {
		panic(err)
	}
	return b
}
