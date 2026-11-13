package firewall

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ErrNoConsent is returned by every function that changes the system when it
// is called without a recorded consent.
var ErrNoConsent = errors.New("firewall: no consent recorded for this change")

// Consent proves that the operator was asked about one action and said yes.
// Only RequestConsent creates a valid one, so a function that takes a
// Consent cannot run without a recorded answer.
type Consent struct {
	action string
	valid  bool
}

// Valid reports whether the consent was granted.
func (c Consent) Valid() bool { return c.valid }

// RequestConsent shows the plan, asks, and records the answer in the consent
// log whether it is yes or no. A failure to record is a refusal: a change
// that cannot be traced is not made.
func RequestConsent(env Env, ask func(string) string, action, prompt string) Consent {
	answer := strings.ToLower(strings.TrimSpace(ask(prompt)))
	granted := answer == "y" || answer == "yes"
	verdict := "declined"
	if granted {
		verdict = "granted"
	}
	line := fmt.Sprintf("%s %s: %s", env.Now().UTC().Format("2006-01-02T15:04:05Z"), verdict, action)
	if err := env.AppendConsent(line); err != nil {
		return Consent{action: action}
	}
	return Consent{action: action, valid: granted}
}

// Cmd is one command of a plan.
type Cmd struct {
	Name string
	Args []string
}

func (c Cmd) String() string { return strings.TrimSpace(c.Name + " " + strings.Join(c.Args, " ")) }

// File is a file a plan writes before it runs its commands.
type File struct {
	Path    string
	Content string
	Mode    os.FileMode
}

// Plan is the ordered list of changes for one tool.
type Plan struct {
	Kind  Kind
	Ports []int // every TCP port the plan opens, SSH first
	Files []File
	// FileStep is the index of the command before which Files are written.
	FileStep int
	Cmds     []Cmd
	// EnableIndex is the index in Cmds of the command that turns the
	// firewall on, -1 when the plan only adds rules to a running one.
	// Every command before it only prepares.
	EnableIndex int
}

// Describe lists the plan for the operator.
func (p Plan) Describe() []string {
	var out []string
	for _, f := range p.Files {
		out = append(out, "write "+f.Path)
	}
	for _, c := range p.Cmds {
		out = append(out, c.String())
	}
	return out
}

// BuildPlan builds the plan for a tool. SSH ports come first in every plan
// and are always present: a firewall that turns on without them locks the
// operator out.
func BuildPlan(kind Kind, sshPorts, extra []int, active bool) (Plan, error) {
	if len(sshPorts) == 0 {
		return Plan{}, errors.New("firewall: no SSH port known, refusing to build a plan that could lock the operator out")
	}
	ports := unique(append(append(append([]int(nil), sshPorts...), BasePorts...), extra...))
	p := Plan{Kind: kind, Ports: ports, EnableIndex: -1}
	switch kind {
	case UFW:
		for _, port := range ports {
			p.Cmds = append(p.Cmds, Cmd{"ufw", []string{"allow", fmt.Sprintf("%d/tcp", port)}})
		}
		if !active {
			p.EnableIndex = len(p.Cmds)
			p.Cmds = append(p.Cmds, Cmd{"ufw", []string{"--force", "enable"}})
		}
	case Firewalld:
		if active {
			for _, port := range ports {
				p.Cmds = append(p.Cmds, Cmd{"firewall-cmd", []string{"--permanent", fmt.Sprintf("--add-port=%d/tcp", port)}})
			}
			p.Cmds = append(p.Cmds, Cmd{"firewall-cmd", []string{"--reload"}})
			break
		}
		p.Cmds = append(p.Cmds, Cmd{"systemctl", []string{"unmask", "firewalld"}})
		for _, port := range ports {
			p.Cmds = append(p.Cmds, Cmd{"firewall-offline-cmd", []string{fmt.Sprintf("--add-port=%d/tcp", port)}})
		}
		p.EnableIndex = len(p.Cmds)
		p.Cmds = append(p.Cmds, Cmd{"systemctl", []string{"enable", "--now", "firewalld"}})
	case Nftables:
		p.Files = []File{
			{nftRulesPath, nftRules(ports), 0600},
			{nftUnitPath, unitText("nft -f " + nftRulesPath), 0644},
		}
		p.Cmds = []Cmd{{"systemctl", []string{"daemon-reload"}}}
		p.EnableIndex = len(p.Cmds)
		p.Cmds = append(p.Cmds, Cmd{"systemctl", []string{"enable", "--now", "remnaforge-firewall.service"}})
	case IPTables:
		p.Files = []File{
			{iptScriptPath, iptScript(ports), 0700},
			{nftUnitPath, unitText("/bin/sh " + iptScriptPath), 0644},
		}
		p.Cmds = []Cmd{{"systemctl", []string{"daemon-reload"}}}
		p.EnableIndex = len(p.Cmds)
		p.Cmds = append(p.Cmds, Cmd{"systemctl", []string{"enable", "--now", "remnaforge-firewall.service"}})
	default:
		return Plan{}, fmt.Errorf("firewall: unknown tool %q", kind)
	}
	return p, nil
}

const (
	nftRulesPath  = "/etc/remnaforge/firewall.nft"
	iptScriptPath = "/etc/remnaforge/firewall-iptables.sh"
	nftUnitPath   = "/etc/systemd/system/remnaforge-firewall.service"
)

func unitText(execStart string) string {
	return "[Unit]\nDescription=RemnaForge firewall rules\nAfter=network-pre.target\nBefore=network.target\nWants=network-pre.target\n\n" +
		"[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=" + execStart + "\n\n[Install]\nWantedBy=multi-user.target\n"
}

func portList(ports []int) string {
	s := make([]string, len(ports))
	for i, p := range ports {
		s[i] = strconv.Itoa(p)
	}
	return strings.Join(s, ", ")
}

// nftRules is a table of its own: it leaves the tables of Docker and of
// other tools alone and only filters traffic to the host itself.
func nftRules(ports []int) string {
	return "table inet remnaforge\ndelete table inet remnaforge\n" +
		"table inet remnaforge {\n\tchain input {\n\t\ttype filter hook input priority 0; policy drop;\n" +
		"\t\tct state established,related accept\n\t\tiif \"lo\" accept\n" +
		"\t\tip protocol icmp accept\n\t\tip6 nexthdr icmpv6 accept\n" +
		"\t\ttcp dport { " + portList(ports) + " } accept\n\t}\n}\n"
}

// iptScript builds an idempotent script: it rebuilds its own chain and
// hooks it into INPUT once, so running it twice changes nothing.
func iptScript(ports []int) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n# Written by RemnaForge. Safe to run more than once.\n")
	b.WriteString("for ipt in iptables ip6tables; do\n\tcommand -v $ipt >/dev/null 2>&1 || continue\n")
	b.WriteString("\t$ipt -N REMNAFORGE-INPUT 2>/dev/null || $ipt -F REMNAFORGE-INPUT\n")
	b.WriteString("\t$ipt -A REMNAFORGE-INPUT -i lo -j ACCEPT\n")
	b.WriteString("\t$ipt -A REMNAFORGE-INPUT -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT\n")
	b.WriteString("\tif [ \"$ipt\" = ip6tables ]; then $ipt -A REMNAFORGE-INPUT -p ipv6-icmp -j ACCEPT; else $ipt -A REMNAFORGE-INPUT -p icmp -j ACCEPT; fi\n")
	for _, p := range ports {
		fmt.Fprintf(&b, "\t$ipt -A REMNAFORGE-INPUT -p tcp --dport %d -j ACCEPT\n", p)
	}
	b.WriteString("\t$ipt -A REMNAFORGE-INPUT -j DROP\n")
	b.WriteString("\t$ipt -C INPUT -j REMNAFORGE-INPUT 2>/dev/null || $ipt -I INPUT 1 -j REMNAFORGE-INPUT\n")
	b.WriteString("done\n")
	return b.String()
}

// Apply runs a plan. It refuses without a valid consent, then runs the
// commands in order, writing the plan's files before the command at
// FileStep, and stops at the first failure.
func Apply(env Env, plan Plan, c Consent) error {
	if !c.Valid() {
		return ErrNoConsent
	}
	if len(plan.Ports) == 0 {
		return errors.New("firewall: empty plan")
	}
	writeFiles := func() error {
		for _, f := range plan.Files {
			if err := env.WriteFile(f.Path, f.Content, f.Mode); err != nil {
				return err
			}
		}
		return nil
	}
	wrote := len(plan.Files) == 0
	for i, cmd := range plan.Cmds {
		if !wrote && i == plan.FileStep {
			if err := writeFiles(); err != nil {
				return err
			}
			wrote = true
		}
		if err := env.Exec(cmd.Name, cmd.Args...); err != nil {
			return fmt.Errorf("%s: %w", cmd, err)
		}
	}
	if !wrote {
		return writeFiles()
	}
	return nil
}

// IntrusionTool is fail2ban or CrowdSec.
type IntrusionTool string

const (
	Fail2ban IntrusionTool = "fail2ban"
	CrowdSec IntrusionTool = "crowdsec"
)

// BuildIntrusionPlan builds the plan that installs an intrusion tool.
// fw is the firewall kind chosen by the operator; CrowdSec picks the
// bouncer that matches it.
func BuildIntrusionPlan(tool IntrusionTool, sshPorts []int, fw Kind) (Plan, error) {
	if len(sshPorts) == 0 {
		return Plan{}, errors.New("firewall: no SSH port known")
	}
	p := Plan{Ports: sshPorts, EnableIndex: -1}
	switch tool {
	case Fail2ban:
		// The jail file is written after the package exists and before the
		// service restarts, so the SSH port of this server is watched.
		p.Cmds = []Cmd{
			{"apt-get", []string{"install", "-y", "fail2ban"}},
			{"systemctl", []string{"enable", "--now", "fail2ban"}},
			{"systemctl", []string{"restart", "fail2ban"}},
		}
		p.FileStep = 1
		p.Files = []File{{"/etc/fail2ban/jail.d/remnaforge.local",
			"[sshd]\nenabled = true\nport = " + strings.ReplaceAll(portList(sshPorts), " ", "") + "\nbackend = systemd\n", 0644}}
	case CrowdSec:
		bouncer := "crowdsec-firewall-bouncer-iptables"
		if fw == Nftables {
			bouncer = "crowdsec-firewall-bouncer-nftables"
		}
		p.Cmds = []Cmd{
			{"sh", []string{"-c", "curl -fsSL https://install.crowdsec.net | sh"}},
			{"apt-get", []string{"install", "-y", "crowdsec", bouncer}},
			{"systemctl", []string{"enable", "--now", "crowdsec"}},
		}
	default:
		return Plan{}, fmt.Errorf("firewall: unknown intrusion tool %q", tool)
	}
	return p, nil
}
