// Package firewall detects the firewall state of the host, checks it
// against the ports this tool needs, and, only after the operator agrees,
// configures one of ufw, firewalld, nftables or iptables and installs an
// intrusion tool (fail2ban or CrowdSec). No code path turns a firewall on
// without a consent that was asked for and recorded.
package firewall

import (
	"fmt"
	"net"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/FlexEbat/RemnaForge/internal/execx"
)

var usersRE = regexp.MustCompile(`users:\(\("([^"]+)"`)

// Kind names a firewall tool.
type Kind string

const (
	UFW       Kind = "ufw"
	Firewalld Kind = "firewalld"
	Nftables  Kind = "nftables"
	IPTables  Kind = "iptables"
)

// Kinds lists the tools the operator can pick, in menu order.
var Kinds = []Kind{UFW, Firewalld, Nftables, IPTables}

// BasePorts are the TCP ports the tool needs open besides SSH.
var BasePorts = []int{80, 443}

// RequiredHosts are the external addresses an install talks to.
var RequiredHosts = []string{
	"registry-1.docker.io:443",
	"download.docker.com:443",
	"acme-v02.api.letsencrypt.org:443",
	"github.com:443",
}

// Tool is one firewall tool as found on the host.
type Tool struct {
	Kind      Kind
	Installed bool
	// Active means the tool filters inbound traffic right now.
	Active bool
	// Backend is "nf_tables" or "legacy" for iptables, empty otherwise.
	Backend string
	// Ports is what the tool says about inbound TCP ports; nil when the
	// tool is not active or could not be read.
	ports *portSet
}

// Conflict is a port this tool needs that an active firewall closes.
type Conflict struct {
	Tool   Kind
	Port   int
	Denied bool // true: a rule denies it, false: no rule opens it
}

// Reach is the result of a connection test to an external address.
type Reach struct {
	Addr string
	Err  error
}

// State is everything Detect found.
type State struct {
	Tools     []Tool
	Listeners []Listener
	NATRules  int
	NATDocker bool
	Needed    []int // SSH ports and BasePorts
	Conflicts []Conflict
	Reach     []Reach
}

// Tool returns the entry of kind k.
func (s State) Tool(k Kind) Tool {
	for _, t := range s.Tools {
		if t.Kind == k {
			return t
		}
	}
	return Tool{Kind: k}
}

// ActiveKinds lists the tools that filter traffic now.
func (s State) ActiveKinds() []Kind {
	var out []Kind
	for _, t := range s.Tools {
		if t.Active {
			out = append(out, t.Kind)
		}
	}
	return out
}

// Env holds every way the package reaches the host. DefaultEnv uses the
// real one; tests use recorded outputs.
type Env struct {
	LookPath func(string) (string, error)
	// Run returns the standard output of a command.
	Run func(name string, args ...string) (string, error)
	// Exec runs a command whose output goes to the terminal.
	Exec      func(name string, args ...string) error
	Dial      func(addr string) error
	SSHPorts  func() []int
	WriteFile func(path, content string, mode os.FileMode) error
	// AppendConsent writes one line to the consent record.
	AppendConsent func(line string) error
	Now           func() time.Time
}

// ConsentLogPath is where consents are recorded.
var ConsentLogPath = "/var/log/remnaforge-consent.log"

// DefaultEnv returns the Env of the running host.
func DefaultEnv() Env {
	return Env{
		LookPath: lookPath,
		Run: func(name string, args ...string) (string, error) {
			out, err := execx.Command(name, args...).Output()
			return string(out), err
		},
		Exec: func(name string, args ...string) error {
			cmd := execx.Command(name, args...)
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			return cmd.Run()
		},
		Dial: func(addr string) error {
			c, err := net.DialTimeout("tcp", addr, 5*time.Second)
			if err != nil {
				return err
			}
			return c.Close()
		},
		SSHPorts: SSHPorts,
		WriteFile: func(path, content string, mode os.FileMode) error {
			if i := strings.LastIndex(path, "/"); i > 0 {
				if err := os.MkdirAll(path[:i], 0755); err != nil {
					return err
				}
			}
			return os.WriteFile(path, []byte(content), mode)
		},
		AppendConsent: func(line string) error {
			f, err := os.OpenFile(ConsentLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = f.WriteString(line + "\n")
			return err
		},
		Now: time.Now,
	}
}

func lookPath(name string) (string, error) {
	for _, dir := range strings.Split(os.Getenv("PATH"), ":") {
		p := dir + "/" + name
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0111 != 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("not found: %s", name)
}

// Detect reads the firewall state. It only reads and tests connections; it
// changes nothing.
func Detect(env Env, withReach bool) State {
	var st State
	st.Needed = unique(append(append([]int(nil), env.SSHPorts()...), BasePorts...))

	// ufw
	ufw := Tool{Kind: UFW}
	if _, err := env.LookPath("ufw"); err == nil {
		ufw.Installed = true
		if out, err := env.Run("ufw", "status", "verbose"); err == nil {
			if set, active := ParseUFW(out); active {
				ufw.Active, ufw.ports = true, &set
			}
		}
	}
	// firewalld
	fwd := Tool{Kind: Firewalld}
	if _, err := env.LookPath("firewall-cmd"); err == nil {
		fwd.Installed = true
		if out, err := env.Run("firewall-cmd", "--state"); err == nil && strings.TrimSpace(out) == "running" {
			fwd.Active = true
			if all, err := env.Run("firewall-cmd", "--list-all"); err == nil {
				set := ParseFirewalld(all)
				fwd.ports = &set
			}
		}
	}
	// nftables
	nft := Tool{Kind: Nftables}
	if _, err := env.LookPath("nft"); err == nil {
		nft.Installed = true
		if out, err := env.Run("nft", "list", "ruleset"); err == nil {
			if set, has := ParseNft(out); has {
				nft.Active, nft.ports = true, &set
			}
		}
	}
	// iptables
	ipt := Tool{Kind: IPTables}
	if _, err := env.LookPath("iptables"); err == nil {
		ipt.Installed = true
		if out, err := env.Run("iptables", "--version"); err == nil {
			ipt.Backend = ParseIPTablesBackend(out)
		}
		if out, err := env.Run("iptables", "-S", "INPUT"); err == nil {
			set := ParseIPTablesInput(out)
			// An INPUT chain with a default accept policy and no rules of
			// its own filters nothing.
			if set.Restrictive || len(set.Allowed)+len(set.Denied) > 0 || set.Unknown {
				ipt.Active, ipt.ports = true, &set
			}
		}
		if out, err := env.Run("iptables", "-t", "nat", "-S"); err == nil {
			st.NATRules, st.NATDocker = CountNATRules(out)
		}
	}
	st.Tools = []Tool{ufw, fwd, nft, ipt}

	if out, err := env.Run("ss", "-ltnpH"); err == nil {
		st.Listeners = ParseListeners(out)
	}
	st.Conflicts = findConflicts(st.Tools, st.Needed)
	if withReach {
		for _, a := range RequiredHosts {
			st.Reach = append(st.Reach, Reach{Addr: a, Err: env.Dial(a)})
		}
	}
	return st
}

// findConflicts checks every active tool against the ports that are needed.
func findConflicts(tools []Tool, needed []int) []Conflict {
	var out []Conflict
	for _, t := range tools {
		if !t.Active || t.ports == nil {
			continue
		}
		for _, p := range needed {
			switch {
			case t.ports.Denied[p] && !t.ports.Allowed[p]:
				out = append(out, Conflict{Tool: t.Kind, Port: p, Denied: true})
			case t.ports.Restrictive && !t.ports.Allowed[p] && !t.ports.Unknown:
				out = append(out, Conflict{Tool: t.Kind, Port: p})
			}
		}
	}
	return out
}

func unique(in []int) []int {
	seen := map[int]bool{}
	var out []int
	for _, p := range in {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// PublicPorts returns the sorted ports that listen on every address.
func (s State) PublicPorts() []int {
	seen := map[int]bool{}
	for _, l := range s.Listeners {
		if l.Public() {
			seen[l.Port] = true
		}
	}
	var out []int
	for p := range seen {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}
