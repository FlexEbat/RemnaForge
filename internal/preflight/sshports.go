package preflight

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// sshPorts returns the TCP ports ufw must keep open for SSH, never
// empty. Sources, in order of trust:
//
//  1. SSH_CONNECTION: the server-side port of the session running this
//     tool. It is the only source that stays correct under systemd
//     socket activation (ssh.socket on Ubuntu 24.04), where the port
//     comes from the socket unit and sshd_config's Port is ignored.
//  2. `sshd -T`: the effective sshd_config, for runs from a console.
//  3. 22, when neither source yields a port.
func sshPorts() []int {
	var ports []int
	if p, ok := portFromSSHConnection(os.Getenv("SSH_CONNECTION")); ok {
		ports = append(ports, p)
	}
	if out, err := exec.Command(sshdBinary(), "-T").Output(); err == nil {
		ports = append(ports, parseSSHDPorts(string(out))...)
	}
	return uniquePorts(ports)
}

func sshdBinary() string {
	if p, err := exec.LookPath("sshd"); err == nil {
		return p
	}
	return "/usr/sbin/sshd"
}

// portFromSSHConnection extracts the server port from an SSH_CONNECTION
// value of the form "client_ip client_port server_ip server_port".
func portFromSSHConnection(v string) (int, bool) {
	fields := strings.Fields(v)
	if len(fields) != 4 {
		return 0, false
	}
	return validPort(fields[3])
}

// parseSSHDPorts reads the "port N" lines of `sshd -T` output.
func parseSSHDPorts(out string) []int {
	var ports []int
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.EqualFold(fields[0], "port") {
			if p, ok := validPort(fields[1]); ok {
				ports = append(ports, p)
			}
		}
	}
	return ports
}

func validPort(s string) (int, bool) {
	p, err := strconv.Atoi(s)
	if err != nil || p < 1 || p > 65535 {
		return 0, false
	}
	return p, true
}

// uniquePorts drops duplicates, keeps order, and falls back to 22.
func uniquePorts(in []int) []int {
	seen := make(map[int]bool, len(in))
	var out []int
	for _, p := range in {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []int{22}
	}
	return out
}
