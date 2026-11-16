package execx

import "strings"

// ReadOnly reports whether the command only reads state, so a dry run may
// still execute it. The list is closed: a command or a form that is not
// named here counts as changing the system and is only announced.
func ReadOnly(name string, args ...string) bool {
	base := name
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	has := func(want ...string) bool { return hasArg(args, want...) }
	first := ""
	if len(args) > 0 {
		first = args[0]
	}
	switch base {
	case "which", "uname", "df", "free", "ss", "getent", "dig", "nslookup", "host",
		"hostname", "id", "whoami", "date", "lsb_release", "apt-cache", "lsmod",
		"sysctl", "nproc", "stat", "cat", "ls", "test", "command":
		// sysctl -w and the key=value form write, the rest only reads.
		if base == "sysctl" && (has("-w", "--write", "-p", "--system") || strings.Contains(strings.Join(args, " "), "=")) {
			return false
		}
		return true
	case "dpkg":
		return has("-s", "-l", "-L", "--print-architecture", "--get-selections", "--status")
	case "apt-get", "apt":
		return has("-s", "--simulate", "--dry-run", "--just-print") || first == "policy" || first == "list"
	case "systemctl":
		return has("is-active", "is-enabled", "status", "show", "list-units", "list-timers", "cat", "list-unit-files")
	case "timedatectl":
		return first == "" || first == "show" || first == "status"
	case "ufw":
		return first == "status"
	case "iptables", "ip6tables", "nft", "iptables-save", "ip6tables-save":
		return has("-S", "-L", "-nvL", "list", "--list", "-C", "--check") || strings.HasSuffix(base, "-save")
	case "firewall-cmd":
		for _, a := range args {
			if a == "--state" || strings.HasPrefix(a, "--list") || strings.HasPrefix(a, "--get") || strings.HasPrefix(a, "--query") {
				return true
			}
		}
		return false
	case "ip":
		return !has("add", "del", "delete", "set", "flush", "replace", "change")
	case "sshd":
		return has("-T")
	case "crontab":
		return first == "-l"
	case "curl":
		return !has("-o", "-O", "--output", "--remote-name", "-X", "--request", "-d", "--data", "-F", "--form", "-T", "--upload-file")
	case "openssl":
		return !has("-out", "-keyout", "-in-place")
	case "certbot":
		return first == "certificates" || has("--dry-run")
	case "docker":
		return dockerReadOnly(args)
	}
	return false
}

func dockerReadOnly(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "info", "version", "ps", "images", "inspect", "ps -a", "logs", "port", "top", "stats", "manifest":
		return true
	case "network", "volume", "context", "image", "container":
		return len(args) > 1 && (args[1] == "ls" || args[1] == "list" || args[1] == "inspect" || args[1] == "show")
	case "run":
		// A throwaway container: nginx -t, caddy validate, key generation.
		return hasArg(args, "--rm")
	case "compose":
		rest := args[1:]
		for len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
			rest = rest[1:]
		}
		if len(rest) == 0 {
			return false
		}
		switch rest[0] {
		case "ps", "config", "ls", "version", "logs", "images", "top", "port":
			return true
		}
	}
	return false
}
