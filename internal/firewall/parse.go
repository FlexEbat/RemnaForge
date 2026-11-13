package firewall

import (
	"strconv"
	"strings"
)

// Listener is a TCP socket in the LISTEN state.
type Listener struct {
	Addr    string
	Port    int
	Process string
}

// Public reports whether the socket accepts connections on every address.
func (l Listener) Public() bool {
	switch l.Addr {
	case "0.0.0.0", "*", "[::]", "::":
		return true
	}
	return false
}

// ParseListeners reads `ss -ltnpH` output.
func ParseListeners(out string) []Listener {
	var res []Listener
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[0] != "LISTEN" {
			continue
		}
		local := f[3]
		i := strings.LastIndex(local, ":")
		if i < 0 {
			continue
		}
		port, err := strconv.Atoi(local[i+1:])
		if err != nil {
			continue
		}
		addr := local[:i]
		if j := strings.Index(addr, "%"); j >= 0 { // 127.0.0.53%lo
			addr = addr[:j]
		}
		l := Listener{Addr: addr, Port: port}
		if m := usersRE.FindStringSubmatch(line); m != nil {
			l.Process = m[1]
		}
		res = append(res, l)
	}
	return res
}

// ParseIPTablesBackend reads `iptables --version`: "iptables v1.8.7 (nf_tables)".
func ParseIPTablesBackend(out string) string {
	switch {
	case strings.Contains(out, "(nf_tables)"):
		return "nf_tables"
	case strings.Contains(out, "(legacy)"):
		return "legacy"
	}
	return ""
}

// CountNATRules counts the rules of `iptables -t nat -S` and says whether
// Docker owns some of them.
func CountNATRules(out string) (rules int, docker bool) {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "-A ") {
			rules++
			if strings.Contains(line, "DOCKER") {
				docker = true
			}
		}
	}
	return rules, docker
}

// portSet is the answer of a firewall about inbound TCP ports.
type portSet struct {
	// Restrictive is true when anything not allowed is dropped (a default
	// deny policy).
	Restrictive bool
	// Unknown is true when the rules hand traffic to chains this package
	// does not read, so a closed port cannot be claimed.
	Unknown bool
	Allowed map[int]bool
	Denied  map[int]bool
}

func newPortSet() portSet {
	return portSet{Allowed: map[int]bool{}, Denied: map[int]bool{}}
}

// parsePortSpec reads "80", "80/tcp", "80,443/tcp", "6000:6007/tcp" and
// returns the TCP ports; a spec for another protocol returns none.
func parsePortSpec(spec string) []int {
	proto := ""
	if i := strings.Index(spec, "/"); i >= 0 {
		proto = spec[i+1:]
		spec = spec[:i]
	}
	if proto != "" && proto != "tcp" {
		return nil
	}
	spec = strings.ReplaceAll(spec, "-", ":") // firewalld and nft write ranges as a-b
	var ports []int
	for _, part := range strings.Split(spec, ",") {
		if lo, hi, isRange := strings.Cut(part, ":"); isRange {
			a, e1 := strconv.Atoi(lo)
			b, e2 := strconv.Atoi(hi)
			if e1 != nil || e2 != nil || a > b || b-a > 2000 {
				continue
			}
			for p := a; p <= b; p++ {
				ports = append(ports, p)
			}
			continue
		}
		if p, err := strconv.Atoi(part); err == nil && p >= 1 && p <= 65535 {
			ports = append(ports, p)
		}
	}
	return ports
}

// ParseUFW reads `ufw status verbose`. It returns active=false for an
// inactive ufw.
func ParseUFW(out string) (set portSet, active bool) {
	set = newPortSet()
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Status:"):
			active = strings.TrimSpace(strings.TrimPrefix(line, "Status:")) == "active"
		case strings.HasPrefix(line, "Default:"):
			set.Restrictive = strings.Contains(line, "deny (incoming)") || strings.Contains(line, "reject (incoming)")
		}
	}
	if !active {
		return newPortSet(), false
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		// "80/tcp ALLOW IN Anywhere" or "443 DENY Anywhere (v6)"
		if len(f) < 3 {
			continue
		}
		action := f[1]
		if action != "ALLOW" && action != "DENY" && action != "REJECT" && action != "LIMIT" {
			continue
		}
		rest := f[2:]
		if rest[0] == "OUT" || (rest[0] == "FWD") {
			continue
		}
		if rest[0] == "IN" {
			rest = rest[1:]
		}
		if len(rest) == 0 || rest[0] != "Anywhere" { // limited to a source
			continue
		}
		for _, p := range parsePortSpec(f[0]) {
			if action == "DENY" || action == "REJECT" {
				set.Denied[p] = true
			} else {
				set.Allowed[p] = true
			}
		}
	}
	return set, true
}

var firewalldServices = map[string]int{"ssh": 22, "http": 80, "https": 443}

// ParseFirewalld reads `firewall-cmd --list-all` for one zone.
func ParseFirewalld(out string) portSet {
	set := newPortSet()
	set.Restrictive = true // the default target of a zone rejects the rest
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "target:"):
			t := strings.TrimSpace(strings.TrimPrefix(line, "target:"))
			set.Restrictive = t == "default" || t == "DROP" || t == "REJECT"
		case strings.HasPrefix(line, "services:"):
			for _, s := range strings.Fields(strings.TrimPrefix(line, "services:")) {
				if p, ok := firewalldServices[s]; ok {
					set.Allowed[p] = true
				}
			}
		case strings.HasPrefix(line, "ports:"):
			for _, spec := range strings.Fields(strings.TrimPrefix(line, "ports:")) {
				for _, p := range parsePortSpec(spec) {
					set.Allowed[p] = true
				}
			}
		case strings.HasPrefix(line, "rich rules:"):
			if strings.TrimSpace(strings.TrimPrefix(line, "rich rules:")) != "" {
				set.Unknown = true
			}
		case strings.HasPrefix(line, "rule "):
			// rich rules are listed on the lines below their heading
			set.Unknown = true
		}
	}
	return set
}

// ParseIPTablesInput reads `iptables -S INPUT`.
func ParseIPTablesInput(out string) portSet {
	set := newPortSet()
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		if f[0] == "-P" && f[1] == "INPUT" {
			set.Restrictive = f[2] == "DROP" || f[2] == "REJECT"
			continue
		}
		if f[0] != "-A" || f[1] != "INPUT" {
			continue
		}
		target := ""
		for i := 0; i+1 < len(f); i++ {
			if f[i] == "-j" {
				target = f[i+1]
			}
		}
		var ports []int
		for i := 0; i+1 < len(f); i++ {
			if f[i] == "--dport" || f[i] == "--dports" {
				ports = append(ports, parsePortSpec(f[i+1])...)
			}
		}
		switch target {
		case "ACCEPT":
			for _, p := range ports {
				set.Allowed[p] = true
			}
		case "DROP", "REJECT":
			for _, p := range ports {
				set.Denied[p] = true
			}
		case "":
		default:
			if target != "LOG" && target != "RETURN" {
				set.Unknown = true // a jump to a chain of its own
			}
		}
	}
	return set
}

// ParseNft reads `nft list ruleset` and looks at the chains that hook input.
func ParseNft(out string) (set portSet, hasInputChain bool) {
	set = newPortSet()
	inInput := false
	depth := 0
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasSuffix(line, "{") && strings.HasPrefix(line, "chain ") {
			inInput = false
			depth = 1
			continue
		}
		if line == "}" {
			inInput = false
			continue
		}
		if strings.Contains(line, "hook input") {
			inInput = true
			hasInputChain = true
			if strings.Contains(line, "policy drop") {
				set.Restrictive = true
			}
			continue
		}
		if !inInput {
			continue
		}
		verdict := ""
		switch {
		case strings.HasSuffix(line, " accept"):
			verdict = "accept"
		case strings.HasSuffix(line, " drop"), strings.HasSuffix(line, " reject"):
			verdict = "drop"
		case strings.HasPrefix(line, "jump ") || strings.HasPrefix(line, "goto ") ||
			strings.Contains(line, " jump ") || strings.Contains(line, " goto "):
			set.Unknown = true
			continue
		default:
			continue
		}
		i := strings.Index(line, "tcp dport ")
		if i < 0 {
			continue
		}
		spec := strings.TrimSpace(line[i+len("tcp dport "):])
		spec = strings.TrimSuffix(strings.TrimSuffix(spec, " accept"), " drop")
		spec = strings.TrimSuffix(spec, " reject")
		spec = strings.Trim(spec, "{} ")
		spec = strings.ReplaceAll(spec, " ", "")
		spec = strings.ReplaceAll(spec, "-", ":")
		for _, p := range parsePortSpec(spec) {
			if verdict == "accept" {
				set.Allowed[p] = true
			} else {
				set.Denied[p] = true
			}
		}
	}
	_ = depth
	return set, hasInputChain
}
