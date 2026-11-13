package firewall

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParsePortSpec(t *testing.T) {
	cases := map[string][]int{
		"80":            {80},
		"80/tcp":        {80},
		"80/udp":        nil,
		"80,443/tcp":    {80, 443},
		"6000:6002/tcp": {6000, 6001, 6002},
		"0":             nil,
		"70000":         nil,
		"abc":           nil,
		"Anywhere":      nil,
		"":              nil,
		"9:1":           nil,
	}
	for in, want := range cases {
		if got := parsePortSpec(in); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %v, want %v", in, got, want)
		}
	}
}

func TestParseUFW(t *testing.T) {
	set, active := ParseUFW(ufwActive)
	if !active || !set.Restrictive {
		t.Fatalf("active=%v restrictive=%v", active, set.Restrictive)
	}
	for _, p := range []int{22, 80, 443, 6000, 6003, 53} {
		if !set.Allowed[p] {
			t.Errorf("port %d should be allowed", p)
		}
	}
	if !set.Denied[8443] || set.Allowed[8443] {
		t.Error("8443 should be denied")
	}
	if set.Allowed[2222] {
		t.Error("a rule limited to one source does not open the port to everyone")
	}
	if _, active := ParseUFW(ufwInactive); active {
		t.Error("inactive ufw reported active")
	}
	open, active := ParseUFW(ufwOpenAll)
	if !active || open.Restrictive || !open.Denied[25] {
		t.Errorf("default allow: %+v", open)
	}
}

func TestParseFirewalld(t *testing.T) {
	set := ParseFirewalld(firewalldList)
	if !set.Restrictive {
		t.Error("target default rejects the rest")
	}
	for _, p := range []int{22, 80, 443, 6000, 6001} {
		if !set.Allowed[p] {
			t.Errorf("port %d should be allowed", p)
		}
	}
	if set.Allowed[9000] {
		t.Error("a UDP port is not a TCP port")
	}
	if set.Unknown {
		t.Error("no rich rules, nothing unknown")
	}
	if !ParseFirewalld(firewalldRich).Unknown {
		t.Error("rich rules must make the answer uncertain")
	}
}

func TestParseIPTablesInput(t *testing.T) {
	set := ParseIPTablesInput(iptablesInputDrop)
	if !set.Restrictive || !set.Allowed[22] || !set.Allowed[80] || !set.Allowed[443] || !set.Denied[8443] || set.Unknown {
		t.Errorf("%+v", set)
	}
	ufw := ParseIPTablesInput(iptablesInputUFW)
	if !ufw.Restrictive || !ufw.Unknown {
		t.Errorf("jumps to other chains must make the answer uncertain: %+v", ufw)
	}
	open := ParseIPTablesInput(iptablesInputOpen)
	if open.Restrictive || len(open.Allowed)+len(open.Denied) != 0 {
		t.Errorf("%+v", open)
	}
}

func TestParseNft(t *testing.T) {
	set, has := ParseNft(nftRuleset)
	if !has || !set.Restrictive {
		t.Fatalf("has=%v %+v", has, set)
	}
	for _, p := range []int{22, 80, 443, 5000, 5001, 5002} {
		if !set.Allowed[p] {
			t.Errorf("port %d should be allowed", p)
		}
	}
	if !set.Denied[8443] {
		t.Error("8443 should be denied")
	}
	if set.Denied[9999] {
		t.Error("a rule of the forward chain was read as an input rule")
	}
	if _, has := ParseNft(nftDockerOnly); has {
		t.Error("Docker's tables alone are not an input firewall")
	}
	jump, _ := ParseNft(nftJump)
	if !jump.Unknown {
		t.Error("a jump must make the answer uncertain")
	}
	if _, has := ParseNft(""); has {
		t.Error("empty ruleset has an input chain")
	}
}

func TestParseListeners(t *testing.T) {
	ls := ParseListeners(ssOutput)
	if len(ls) != 7 {
		t.Fatalf("got %d listeners", len(ls))
	}
	if ls[0].Port != 80 || ls[0].Process != "docker-proxy" || !ls[0].Public() {
		t.Errorf("%+v", ls[0])
	}
	if ls[3].Addr != "127.0.0.53" || ls[3].Public() {
		t.Errorf("a local resolver address kept its zone: %+v", ls[3])
	}
	if ls[5].Addr != "[::]" || !ls[5].Public() || ls[5].Port != 22 {
		t.Errorf("%+v", ls[5])
	}
	if got := ParseListeners("garbage\nLISTEN\n"); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestBackendAndNAT(t *testing.T) {
	if ParseIPTablesBackend(iptablesVersionNft) != "nf_tables" || ParseIPTablesBackend(iptablesVersionLegacy) != "legacy" || ParseIPTablesBackend("iptables v1.4") != "" {
		t.Error("backend detection")
	}
	if n, d := CountNATRules(natDocker); n != 4 || !d {
		t.Errorf("docker NAT: %d %v", n, d)
	}
	if n, d := CountNATRules(natNone); n != 0 || d {
		t.Errorf("no NAT: %d %v", n, d)
	}
}

// host scripts a server: which tools exist and what each prints.
type host struct {
	tools    map[string]bool
	out      map[string]string
	fail     map[string]error
	dialErr  map[string]error
	ssh      []int
	execs    []string
	timeline []string // consent lines and executed commands, in order
	files    map[string]string
	consent  []string
	execErr  map[string]error
	logFail  bool
	t        *testing.T
}

func newHost(t *testing.T) *host {
	return &host{t: t, tools: map[string]bool{}, out: map[string]string{}, fail: map[string]error{},
		dialErr: map[string]error{}, ssh: []int{22}, files: map[string]string{}, execErr: map[string]error{}}
}

func (h *host) env() Env {
	return Env{
		LookPath: func(n string) (string, error) {
			if h.tools[n] {
				return "/usr/sbin/" + n, nil
			}
			return "", errors.New("missing")
		},
		Run: func(name string, args ...string) (string, error) {
			line := name + " " + strings.Join(args, " ")
			if err, ok := h.fail[line]; ok {
				return "", err
			}
			if o, ok := h.out[line]; ok {
				return o, nil
			}
			return "", errors.New("unscripted: " + line)
		},
		Exec: func(name string, args ...string) error {
			line := strings.TrimSpace(name + " " + strings.Join(args, " "))
			h.execs = append(h.execs, line)
			h.timeline = append(h.timeline, "exec "+line)
			return h.execErr[line]
		},
		Dial:     func(addr string) error { return h.dialErr[addr] },
		SSHPorts: func() []int { return h.ssh },
		WriteFile: func(path, content string, _ os.FileMode) error {
			h.files[path] = content
			h.timeline = append(h.timeline, "write "+path)
			return nil
		},
		AppendConsent: func(line string) error {
			if h.logFail {
				return errors.New("read-only file system")
			}
			h.consent = append(h.consent, line)
			h.timeline = append(h.timeline, "consent "+line)
			return nil
		},
		Now: func() time.Time { return time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC) },
	}
}

// serverWith builds a host with the given tools active as recorded.
func fullServer(t *testing.T) *host {
	h := newHost(t)
	for _, n := range []string{"ufw", "firewall-cmd", "nft", "iptables"} {
		h.tools[n] = true
	}
	h.out["ufw status verbose"] = ufwActive
	h.out["firewall-cmd --state"] = "running\n"
	h.out["firewall-cmd --list-all"] = firewalldList
	h.out["nft list ruleset"] = nftRuleset
	h.out["iptables --version"] = iptablesVersionNft
	h.out["iptables -S INPUT"] = iptablesInputDrop
	h.out["iptables -t nat -S"] = natDocker
	h.out["ss -ltnpH"] = ssOutput
	return h
}

func TestDetectEverything(t *testing.T) {
	h := fullServer(t)
	h.dialErr["github.com:443"] = errors.New("i/o timeout")
	st := Detect(h.env(), true)
	if len(st.Tools) != 4 {
		t.Fatalf("tools = %+v", st.Tools)
	}
	for _, k := range Kinds {
		if tool := st.Tool(k); !tool.Installed || !tool.Active {
			t.Errorf("%s: %+v", k, tool)
		}
	}
	if st.Tool(IPTables).Backend != "nf_tables" {
		t.Errorf("backend = %q", st.Tool(IPTables).Backend)
	}
	if st.NATRules != 4 || !st.NATDocker {
		t.Errorf("NAT = %d %v", st.NATRules, st.NATDocker)
	}
	if !reflect.DeepEqual(st.Needed, []int{22, 80, 443}) {
		t.Errorf("needed = %v", st.Needed)
	}
	if !reflect.DeepEqual(st.PublicPorts(), []int{22, 80, 443}) {
		t.Errorf("public ports = %v", st.PublicPorts())
	}
	if len(st.Reach) != len(RequiredHosts) {
		t.Fatalf("reach = %+v", st.Reach)
	}
	failed := 0
	for _, r := range st.Reach {
		if r.Err != nil {
			failed++
			if r.Addr != "github.com:443" {
				t.Errorf("unexpected failure %v", r)
			}
		}
	}
	if failed != 1 {
		t.Errorf("failed = %d", failed)
	}
	if got := st.ActiveKinds(); len(got) != 4 {
		t.Errorf("active = %v", got)
	}
}

func TestDetectFindsNothingOnABareServer(t *testing.T) {
	h := newHost(t)
	h.out["ss -ltnpH"] = ssOutput
	st := Detect(h.env(), false)
	for _, tool := range st.Tools {
		if tool.Installed || tool.Active {
			t.Errorf("%+v", tool)
		}
	}
	if len(st.Conflicts) != 0 || len(st.Reach) != 0 {
		t.Errorf("conflicts %v reach %v", st.Conflicts, st.Reach)
	}
}

func TestDetectInactiveToolsAndOpenIPTables(t *testing.T) {
	h := newHost(t)
	h.tools["ufw"], h.tools["iptables"], h.tools["nft"] = true, true, true
	h.out["ufw status verbose"] = ufwInactive
	h.out["iptables --version"] = iptablesVersionLegacy
	h.out["iptables -S INPUT"] = iptablesInputOpen
	h.out["iptables -t nat -S"] = natNone
	h.out["nft list ruleset"] = nftDockerOnly
	st := Detect(h.env(), false)
	if len(st.ActiveKinds()) != 0 {
		t.Errorf("active = %v", st.ActiveKinds())
	}
	if !st.Tool(UFW).Installed || st.Tool(IPTables).Backend != "legacy" {
		t.Errorf("%+v", st.Tools)
	}
}

func TestConflictsAgainstNeededPorts(t *testing.T) {
	// ufw: 8443 denied is not needed; 22, 80, 443 allowed: no conflict.
	h := newHost(t)
	h.tools["ufw"] = true
	h.out["ufw status verbose"] = ufwActive
	if got := Detect(h.env(), false).Conflicts; len(got) != 0 {
		t.Errorf("conflicts = %+v", got)
	}

	// A non-standard SSH port nothing opens is a conflict of kind "closed".
	h.ssh = []int{2200}
	got := Detect(h.env(), false).Conflicts
	if len(got) != 1 || got[0].Port != 2200 || got[0].Tool != UFW || got[0].Denied {
		t.Errorf("conflicts = %+v", got)
	}

	// An explicit deny of a needed port.
	h2 := newHost(t)
	h2.tools["iptables"] = true
	h2.out["iptables --version"] = iptablesVersionNft
	h2.out["iptables -S INPUT"] = "-P INPUT ACCEPT\n-A INPUT -p tcp -m tcp --dport 443 -j DROP\n"
	h2.out["iptables -t nat -S"] = natNone
	got = Detect(h2.env(), false).Conflicts
	if len(got) != 1 || got[0].Port != 443 || !got[0].Denied {
		t.Errorf("conflicts = %+v", got)
	}

	// A chain handing traffic to other chains is not judged closed.
	h3 := newHost(t)
	h3.tools["iptables"] = true
	h3.out["iptables --version"] = iptablesVersionNft
	h3.out["iptables -S INPUT"] = iptablesInputUFW
	h3.out["iptables -t nat -S"] = natNone
	if got := Detect(h3.env(), false).Conflicts; len(got) != 0 {
		t.Errorf("an uncertain ruleset produced conflicts: %+v", got)
	}

	// firewalld that opens ssh and http but not https.
	h4 := newHost(t)
	h4.tools["firewall-cmd"] = true
	h4.out["firewall-cmd --state"] = "running\n"
	h4.out["firewall-cmd --list-all"] = strings.Replace(firewalldList, "443/tcp", "", 1)
	got = Detect(h4.env(), false).Conflicts
	if len(got) != 1 || got[0].Port != 443 || got[0].Tool != Firewalld {
		t.Errorf("conflicts = %+v", got)
	}
}

func TestStateLines(t *testing.T) {
	h := fullServer(t)
	h.dialErr["github.com:443"] = errors.New("i/o timeout")
	h.ssh = []int{2200}
	text := strings.Join(StateLines(Detect(h.env(), true)), "\n")
	for _, want := range []string{"ufw: active", "iptables (nf_tables): active", "ports 22, 80, 443", "belong to Docker", "does not open port 2200", "Cannot reach github.com:443"} {
		if !strings.Contains(text, want) {
			t.Errorf("state misses %q:\n%s", want, text)
		}
	}
	bare := strings.Join(StateLines(Detect(newHost(t).env(), false)), "\n")
	if !strings.Contains(bare, "ufw: not installed") {
		t.Errorf("bare:\n%s", bare)
	}
}

func TestParsePortList(t *testing.T) {
	good := map[string][]int{"": nil, "8443": {8443}, "80, 8443 ,2222": {80, 8443, 2222}, "80,80": {80}, "1 65535": {1, 65535}}
	for in, want := range good {
		got, err := ParsePortList(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %v %v", in, got, err)
		}
	}
	for _, in := range []string{"0", "65536", "abc", "80;443", "-1"} {
		if _, err := ParsePortList(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}
