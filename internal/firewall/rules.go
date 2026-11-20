package firewall

import (
	"strings"

	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// Rule is one ufw rule an install flow needs. Args follow "ufw"; Reason is
// the text the operator sees next to the rule.
type Rule struct {
	Args   []string
	Reason string
}

func (r Rule) command() string { return "ufw " + strings.Join(r.Args, " ") }

// AllowRules is how an install flow gets the ufw rules it needs. It shows
// every rule with its reason, asks, records the answer in the consent log
// and adds the rules only after a yes. A refusal, a missing ufw or a failed
// command does not stop the install: the commands are printed so the
// operator can run them by hand.
func AllowRules(env Env, ask func(string) string, rules []Rule) {
	if len(rules) == 0 {
		return
	}
	out(ui.ColorYellow, tr("FIREWALL_RULES_TITLE"))
	cmds := make([]Cmd, 0, len(rules)+1)
	var names []string
	for _, r := range rules {
		out(ui.ColorGray, "  "+r.command()+"  ("+r.Reason+")")
		cmds = append(cmds, Cmd{Name: "ufw", Args: r.Args})
		names = append(names, r.command())
	}
	manual := func() {
		out(ui.ColorYellow, tr("FIREWALL_RULES_MANUAL"))
		for _, n := range names {
			out(ui.ColorWhite, "  "+n)
		}
	}
	if _, err := env.LookPath("ufw"); err != nil {
		out(ui.ColorYellow, tr("FIREWALL_RULES_NO_UFW"))
		return
	}
	if status, err := env.Run("ufw", "status"); err == nil {
		if _, active := ParseUFW(status); !active {
			out(ui.ColorYellow, tr("FIREWALL_RULES_INACTIVE"))
		}
	}
	cmds = append(cmds, Cmd{Name: "ufw", Args: []string{"reload"}})
	c := RequestConsent(env, ask, "ufw rules: "+strings.Join(names, "; "), tr("FIREWALL_RULES_CONSENT"))
	if !c.Valid() {
		out(ui.ColorYellow, tr("FIREWALL_RULES_DECLINED"))
		manual()
		return
	}
	if err := Apply(env, Plan{Kind: UFW, Ports: []int{0}, EnableIndex: -1, Cmds: cmds}, c); err != nil {
		out(ui.ColorRed, tr("FIREWALL_FAILED", err))
		manual()
		return
	}
	out(ui.ColorGreen, tr("FIREWALL_RULES_DONE"))
}

// HTTPRule opens port 80 for the certificate check and the HTTP redirect.
func HTTPRule() Rule {
	return Rule{Args: []string{"allow", "80/tcp", "comment", "HTTP"}, Reason: tr("FIREWALL_RULE_HTTP")}
}

// NodePanelRule lets the panel at panelIP reach the node API on port 2222.
func NodePanelRule(panelIP string) Rule {
	return Rule{
		Args:   []string{"allow", "from", panelIP, "to", "any", "port", "2222"},
		Reason: tr("FIREWALL_RULE_NODE_PANEL", panelIP),
	}
}

// NodeLocalRule lets the node of the same server reach its API on port 2222
// from the Docker network the stack runs in.
func NodeLocalRule() Rule {
	return Rule{
		Args:   []string{"allow", "from", "172.30.0.0/16", "to", "any", "port", "2222", "proto", "tcp"},
		Reason: tr("FIREWALL_RULE_NODE_LOCAL"),
	}
}
