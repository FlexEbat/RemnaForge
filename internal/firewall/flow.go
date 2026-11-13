package firewall

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

func tr(key string, args ...any) string {
	if len(args) == 0 {
		return i18n.T(key)
	}
	return fmt.Sprintf(i18n.T(key), args...)
}

var packageOf = map[Kind]string{UFW: "ufw", Firewalld: "firewalld", Nftables: "nftables", IPTables: "iptables"}

// ParsePortList reads "80, 8443 ,2222" into ports. Empty input is no ports.
func ParsePortList(s string) ([]int, error) {
	var out []int
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		p, err := strconv.Atoi(part)
		if err != nil || p < 1 || p > 65535 {
			return nil, fmt.Errorf("%q", part)
		}
		out = append(out, p)
	}
	return unique(out), nil
}

// StateLines renders the detected state for the operator.
func StateLines(st State) []string {
	var lines []string
	for _, t := range st.Tools {
		state := tr("FIREWALL_STATE_MISSING")
		switch {
		case t.Active:
			state = tr("FIREWALL_STATE_ACTIVE")
		case t.Installed:
			state = tr("FIREWALL_STATE_INACTIVE")
		}
		name := string(t.Kind)
		if t.Backend != "" {
			name += " (" + t.Backend + ")"
		}
		lines = append(lines, fmt.Sprintf("%s: %s", name, state))
	}
	if ports := st.PublicPorts(); len(ports) > 0 {
		lines = append(lines, tr("FIREWALL_LISTEN", portList(ports)))
	}
	if st.NATRules > 0 {
		if st.NATDocker {
			lines = append(lines, tr("FIREWALL_NAT_DOCKER", st.NATRules))
		} else {
			lines = append(lines, tr("FIREWALL_NAT", st.NATRules))
		}
	}
	for _, c := range st.Conflicts {
		if c.Denied {
			lines = append(lines, tr("FIREWALL_CONFLICT_DENIED", c.Tool, c.Port))
		} else {
			lines = append(lines, tr("FIREWALL_CONFLICT_CLOSED", c.Tool, c.Port))
		}
	}
	for _, r := range st.Reach {
		if r.Err != nil {
			lines = append(lines, tr("FIREWALL_REACH_FAIL", r.Addr, r.Err))
		}
	}
	return lines
}

var out = func(color, msg string) { fmt.Printf("%s%s%s\n", color, msg, ui.ColorReset) }

func yes(ans string) bool {
	a := strings.ToLower(strings.TrimSpace(ans))
	return a == "y" || a == "yes"
}

// Run is the interactive flow: show the state, let the operator pick a
// firewall tool, show the plan, ask for consent, apply, then offer an
// intrusion tool. Every change waits for a recorded yes.
func Run(env Env, ask func(string) string) {
	out(ui.ColorGreen, tr("FIREWALL_TITLE"))
	st := Detect(env, true)
	for _, l := range StateLines(st) {
		out(ui.ColorGray, l)
	}
	if !yes(ask(tr("FIREWALL_ASK_START"))) {
		out(ui.ColorYellow, tr("FIREWALL_DECLINED"))
		offerIntrusion(env, ask, "")
		return
	}
	kind, ok := pickTool(st, ask)
	if !ok {
		out(ui.ColorYellow, tr("FIREWALL_DECLINED"))
		offerIntrusion(env, ask, "")
		return
	}
	tool := st.Tool(kind)
	if !tool.Installed {
		pkg := packageOf[kind]
		c := RequestConsent(env, ask, "install package "+pkg, tr("FIREWALL_INSTALL_CONSENT", pkg))
		if !c.Valid() {
			out(ui.ColorYellow, tr("FIREWALL_DECLINED"))
			return
		}
		if err := Apply(env, Plan{Kind: kind, Ports: []int{0}, EnableIndex: -1,
			Cmds: []Cmd{{"apt-get", []string{"install", "-y", pkg}}}}, c); err != nil {
			out(ui.ColorRed, tr("FIREWALL_FAILED", err))
			return
		}
	}
	var extra []int
	for {
		list, err := ParsePortList(ask(tr("FIREWALL_EXTRA_PORTS")))
		if err == nil {
			extra = list
			break
		}
		out(ui.ColorRed, tr("FIREWALL_BAD_PORTS", err))
	}
	ssh := env.SSHPorts()
	plan, err := BuildPlan(kind, ssh, extra, tool.Active)
	if err != nil {
		out(ui.ColorRed, tr("FIREWALL_FAILED", err))
		return
	}
	out(ui.ColorYellow, tr("FIREWALL_PLAN"))
	for _, l := range plan.Describe() {
		out(ui.ColorGray, "  "+l)
	}
	out(ui.ColorYellow, tr("FIREWALL_SSH_NOTE", portList(ssh)))
	out(ui.ColorYellow, tr("FIREWALL_DOCKER_NOTE"))
	c := RequestConsent(env, ask, fmt.Sprintf("configure %s, ports %s", kind, portList(plan.Ports)), tr("FIREWALL_CONSENT_ENABLE", kind))
	if !c.Valid() {
		out(ui.ColorYellow, tr("FIREWALL_DECLINED"))
		return
	}
	if err := Apply(env, plan, c); err != nil {
		out(ui.ColorRed, tr("FIREWALL_FAILED", err))
		return
	}
	out(ui.ColorGreen, tr("FIREWALL_DONE", kind))
	offerIntrusion(env, ask, kind)
}

func pickTool(st State, ask func(string) string) (Kind, bool) {
	out(ui.ColorYellow, tr("FIREWALL_CHOOSE"))
	for i, k := range Kinds {
		out(ui.ColorYellow, fmt.Sprintf("%d. %s", i+1, k))
	}
	out(ui.ColorYellow, "0. "+tr("FIREWALL_SKIP"))
	n, err := strconv.Atoi(strings.TrimSpace(ask(tr("FIREWALL_PROMPT"))))
	if err != nil || n < 1 || n > len(Kinds) {
		return "", false
	}
	return Kinds[n-1], true
}

func offerIntrusion(env Env, ask func(string) string, fw Kind) {
	out(ui.ColorYellow, tr("FIREWALL_INTRUSION_TITLE"))
	out(ui.ColorYellow, "1. fail2ban: "+tr("FIREWALL_FAIL2BAN_DESC"))
	out(ui.ColorYellow, "2. CrowdSec: "+tr("FIREWALL_CROWDSEC_DESC"))
	out(ui.ColorYellow, "0. "+tr("FIREWALL_NEITHER"))
	var tool IntrusionTool
	switch strings.TrimSpace(ask(tr("FIREWALL_PROMPT_INTRUSION"))) {
	case "1":
		tool = Fail2ban
	case "2":
		tool = CrowdSec
		out(ui.ColorYellow, tr("FIREWALL_CROWDSEC_NOTE"))
	default:
		return
	}
	plan, err := BuildIntrusionPlan(tool, env.SSHPorts(), fw)
	if err != nil {
		out(ui.ColorRed, tr("FIREWALL_FAILED", err))
		return
	}
	for _, l := range plan.Describe() {
		out(ui.ColorGray, "  "+l)
	}
	c := RequestConsent(env, ask, "install "+string(tool), tr("FIREWALL_CONSENT_INTRUSION", tool))
	if !c.Valid() {
		out(ui.ColorYellow, tr("FIREWALL_DECLINED"))
		return
	}
	if err := Apply(env, plan, c); err != nil {
		out(ui.ColorRed, tr("FIREWALL_FAILED", err))
		return
	}
	out(ui.ColorGreen, tr("FIREWALL_INTRUSION_DONE", tool))
}
