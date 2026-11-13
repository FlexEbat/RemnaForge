package firewall

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func cmdLines(p Plan) []string {
	var out []string
	for _, c := range p.Cmds {
		out = append(out, c.String())
	}
	return out
}

func indexOf(lines []string, want string) int {
	for i, l := range lines {
		if l == want {
			return i
		}
	}
	return -1
}

func TestBuildPlanUFW(t *testing.T) {
	p, err := BuildPlan(UFW, []int{2200}, []int{8443, 80}, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ufw allow 2200/tcp", "ufw allow 80/tcp", "ufw allow 443/tcp", "ufw allow 8443/tcp", "ufw --force enable"}
	if got := cmdLines(p); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	if p.EnableIndex != 4 || !reflect.DeepEqual(p.Ports, []int{2200, 80, 443, 8443}) {
		t.Errorf("enable %d ports %v", p.EnableIndex, p.Ports)
	}
}

func TestBuildPlanUFWAlreadyActiveOnlyAddsRules(t *testing.T) {
	p, _ := BuildPlan(UFW, []int{22}, nil, true)
	if p.EnableIndex != -1 {
		t.Error("a running ufw must not be enabled again")
	}
	for _, l := range cmdLines(p) {
		if strings.Contains(l, "enable") {
			t.Errorf("%q", l)
		}
	}
}

func TestBuildPlanFirewalld(t *testing.T) {
	p, _ := BuildPlan(Firewalld, []int{22}, nil, false)
	lines := cmdLines(p)
	enable := indexOf(lines, "systemctl enable --now firewalld")
	if enable < 0 || p.EnableIndex != enable {
		t.Fatalf("enable at %d, plan %v", p.EnableIndex, lines)
	}
	for _, port := range []string{"22", "80", "443"} {
		i := indexOf(lines, "firewall-offline-cmd --add-port="+port+"/tcp")
		if i < 0 || i > enable {
			t.Errorf("port %s rule at %d, enable at %d", port, i, enable)
		}
	}
	live, _ := BuildPlan(Firewalld, []int{22}, nil, true)
	if live.EnableIndex != -1 || indexOf(cmdLines(live), "firewall-cmd --reload") < 0 || indexOf(cmdLines(live), "firewall-cmd --permanent --add-port=22/tcp") < 0 {
		t.Errorf("running firewalld: %v", cmdLines(live))
	}
}

func TestBuildPlanNftables(t *testing.T) {
	p, _ := BuildPlan(Nftables, []int{22}, []int{8443}, false)
	rules := p.Files[0].Content
	for _, want := range []string{"table inet remnaforge", "policy drop", "ct state established,related accept", `iif "lo" accept`, "tcp dport { 22, 80, 443, 8443 } accept"} {
		if !strings.Contains(rules, want) {
			t.Errorf("rules miss %q:\n%s", want, rules)
		}
	}
	if strings.Contains(rules, "flush ruleset") {
		t.Error("flush ruleset would remove the tables of Docker")
	}
	if !strings.Contains(p.Files[1].Content, "ExecStart=nft -f /etc/remnaforge/firewall.nft") {
		t.Errorf("unit:\n%s", p.Files[1].Content)
	}
	if got := cmdLines(p); got[p.EnableIndex] != "systemctl enable --now remnaforge-firewall.service" {
		t.Errorf("%v", got)
	}
}

func TestBuildPlanIPTables(t *testing.T) {
	p, _ := BuildPlan(IPTables, []int{22, 2200}, nil, false)
	script := p.Files[0].Content
	i22 := strings.Index(script, "--dport 22 -j ACCEPT")
	i2200 := strings.Index(script, "--dport 2200 -j ACCEPT")
	idrop := strings.Index(script, "-A REMNAFORGE-INPUT -j DROP")
	ihook := strings.Index(script, "-I INPUT 1 -j REMNAFORGE-INPUT")
	if i22 < 0 || i2200 < i22 || idrop < i2200 || ihook < idrop {
		t.Errorf("order broken: ssh %d %d, drop %d, hook %d\n%s", i22, i2200, idrop, ihook, script)
	}
	for _, want := range []string{"iptables ip6tables", "-N REMNAFORGE-INPUT 2>/dev/null || $ipt -F REMNAFORGE-INPUT", "-C INPUT -j REMNAFORGE-INPUT 2>/dev/null ||", "--ctstate RELATED,ESTABLISHED"} {
		if !strings.Contains(script, want) {
			t.Errorf("script misses %q", want)
		}
	}
	if !strings.Contains(p.Files[1].Content, "ExecStart=/bin/sh /etc/remnaforge/firewall-iptables.sh") {
		t.Errorf("unit:\n%s", p.Files[1].Content)
	}
}

// Whatever the tool, the SSH ports are in the plan before the command that
// turns the firewall on.
func TestEveryPlanOpensSSHBeforeEnabling(t *testing.T) {
	for _, kind := range Kinds {
		for _, active := range []bool{false, true} {
			p, err := BuildPlan(kind, []int{2200}, []int{9000}, active)
			if err != nil {
				t.Fatal(err)
			}
			if p.Ports[0] != 2200 {
				t.Errorf("%s: SSH port is not first: %v", kind, p.Ports)
			}
			joined := strings.Join(cmdLines(p), "\n")
			for _, f := range p.Files {
				joined += "\n" + f.Content
			}
			if !strings.Contains(joined, "2200") {
				t.Errorf("%s active=%v: the SSH port is nowhere in the plan:\n%s", kind, active, joined)
			}
			if p.EnableIndex > 0 {
				before := strings.Join(cmdLines(p)[:p.EnableIndex], "\n")
				for _, f := range p.Files {
					before += "\n" + f.Content
				}
				if !strings.Contains(before, "2200") {
					t.Errorf("%s: SSH port not prepared before enabling:\n%s", kind, before)
				}
			}
		}
	}
}

func TestBuildPlanRefusesWithoutSSHPort(t *testing.T) {
	for _, kind := range Kinds {
		if _, err := BuildPlan(kind, nil, nil, false); err == nil {
			t.Errorf("%s: a plan without an SSH port was built", kind)
		}
	}
	if _, err := BuildPlan("pf", []int{22}, nil, false); err == nil {
		t.Error("an unknown tool was accepted")
	}
}

func TestRequestConsentRecordsEveryAnswer(t *testing.T) {
	h := newHost(t)
	for answer, want := range map[string]bool{"y": true, "Y": true, " yes ": true, "n": false, "": false, "yep": false, "да": false} {
		h.consent = nil
		c := RequestConsent(h.env(), func(string) string { return answer }, "enable ufw", "Go?")
		if c.Valid() != want {
			t.Errorf("answer %q: valid = %v", answer, c.Valid())
		}
		if len(h.consent) != 1 || !strings.Contains(h.consent[0], "enable ufw") || !strings.HasPrefix(h.consent[0], "2026-11-13T12:00:00Z") {
			t.Errorf("answer %q: log %v", answer, h.consent)
		}
		verdict := "declined"
		if want {
			verdict = "granted"
		}
		if !strings.Contains(h.consent[0], verdict) {
			t.Errorf("answer %q: log %q misses %q", answer, h.consent[0], verdict)
		}
	}
}

func TestConsentThatCannotBeRecordedIsRefused(t *testing.T) {
	h := newHost(t)
	h.logFail = true
	c := RequestConsent(h.env(), func(string) string { return "y" }, "enable ufw", "Go?")
	if c.Valid() {
		t.Error("a consent that could not be written down was accepted")
	}
}

func TestApplyWithoutConsentChangesNothing(t *testing.T) {
	h := newHost(t)
	p, _ := BuildPlan(UFW, []int{22}, nil, false)
	for _, c := range []Consent{{}, {valid: false, action: "x"}} {
		if err := Apply(h.env(), p, c); !errors.Is(err, ErrNoConsent) {
			t.Errorf("err = %v", err)
		}
	}
	if len(h.execs) != 0 || len(h.files) != 0 {
		t.Errorf("commands ran without consent: %v %v", h.execs, h.files)
	}
	fp, _ := BuildIntrusionPlan(Fail2ban, []int{22}, UFW)
	if err := Apply(h.env(), fp, Consent{}); !errors.Is(err, ErrNoConsent) || len(h.execs) != 0 {
		t.Errorf("intrusion plan: %v %v", err, h.execs)
	}
}

func grant(h *host, action string) Consent {
	return RequestConsent(h.env(), func(string) string { return "y" }, action, "?")
}

func TestApplyRunsInOrderAndStopsOnFailure(t *testing.T) {
	h := newHost(t)
	p, _ := BuildPlan(UFW, []int{22}, nil, false)
	if err := Apply(h.env(), p, grant(h, "ufw")); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(h.execs, cmdLines(p)) {
		t.Errorf("execs = %v", h.execs)
	}

	h2 := newHost(t)
	h2.execErr["ufw allow 80/tcp"] = errors.New("exit 1")
	err := Apply(h2.env(), p, grant(h2, "ufw"))
	if err == nil || !strings.Contains(err.Error(), "ufw allow 80/tcp") {
		t.Fatalf("err = %v", err)
	}
	for _, e := range h2.execs {
		if strings.Contains(e, "enable") {
			t.Errorf("the firewall was enabled after a failed rule: %v", h2.execs)
		}
	}
}

func TestApplyWritesFilesBeforeTheEnableCommand(t *testing.T) {
	h := newHost(t)
	p, _ := BuildPlan(Nftables, []int{22}, nil, false)
	if err := Apply(h.env(), p, grant(h, "nft")); err != nil {
		t.Fatal(err)
	}
	wrote := indexOf(h.timeline, "write /etc/remnaforge/firewall.nft")
	enabled := indexOf(h.timeline, "exec systemctl enable --now remnaforge-firewall.service")
	if wrote < 0 || enabled < 0 || wrote > enabled {
		t.Errorf("timeline: %v", h.timeline)
	}
}

func TestIntrusionPlans(t *testing.T) {
	f, err := BuildIntrusionPlan(Fail2ban, []int{22, 2200}, UFW)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.Files[0].Content, "port = 22,2200") || !strings.Contains(f.Files[0].Content, "backend = systemd") {
		t.Errorf("jail:\n%s", f.Files[0].Content)
	}
	h := newHost(t)
	if err := Apply(h.env(), f, grant(h, "fail2ban")); err != nil {
		t.Fatal(err)
	}
	install := indexOf(h.timeline, "exec apt-get install -y fail2ban")
	wrote := indexOf(h.timeline, "write /etc/fail2ban/jail.d/remnaforge.local")
	restart := indexOf(h.timeline, "exec systemctl restart fail2ban")
	if install < 0 || wrote < install || restart < wrote {
		t.Errorf("order: %v", h.timeline)
	}

	cs, _ := BuildIntrusionPlan(CrowdSec, []int{22}, Nftables)
	if !strings.Contains(strings.Join(cmdLines(cs), "\n"), "crowdsec-firewall-bouncer-nftables") {
		t.Errorf("%v", cmdLines(cs))
	}
	cs, _ = BuildIntrusionPlan(CrowdSec, []int{22}, UFW)
	if !strings.Contains(strings.Join(cmdLines(cs), "\n"), "crowdsec-firewall-bouncer-iptables") {
		t.Errorf("%v", cmdLines(cs))
	}
	if _, err := BuildIntrusionPlan("snort", []int{22}, UFW); err == nil {
		t.Error("unknown tool accepted")
	}
	if _, err := BuildIntrusionPlan(Fail2ban, nil, UFW); err == nil {
		t.Error("a plan without an SSH port was built")
	}
}
