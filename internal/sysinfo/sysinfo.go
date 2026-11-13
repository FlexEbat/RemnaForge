// Package sysinfo detects the host the installer runs on: distribution,
// architecture, kernel, package manager, systemd, memory, free disk and the
// state of the apt repositories. Everything that reads the system goes
// through an Env, so tests feed fixtures instead of the real host.
package sysinfo

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/FlexEbat/RemnaForge/internal/execx"
)

// Thresholds. Each one names the reason it exists.
const (
	// MinRAM is the memory below which the install is refused: the Docker
	// daemon, Postgres and Xray get killed by the OOM killer while the
	// stack starts.
	MinRAM uint64 = 1 << 30
	// WarnRAM is the memory below which the install warns: image pulls and
	// the xcaddy build for Caddy push such a host into heavy swapping.
	WarnRAM uint64 = 2 << 30
	// MinFreeDisk is the free space on /opt and /var/lib/docker below which
	// the install warns: the panel, node and Caddy images plus the database
	// need about this much.
	MinFreeDisk uint64 = 10 << 30
	// MinKernelMajor and MinKernelMinor give the oldest kernel without a
	// warning: BBR (tcp_bbr) arrived in 4.9.
	MinKernelMajor = 4
	MinKernelMinor = 9
)

// DiskPaths are the directories whose free space matters for an install.
var DiskPaths = []string{"/opt", "/var/lib/docker"}

// packageManagers are looked up in this order; yum comes after dnf because
// it is often only a link to dnf.
var packageManagers = []string{"apt-get", "dnf", "yum", "zypper", "pacman", "apk"}

// Disk is the free space of one path.
type Disk struct {
	Path string
	Free uint64
	Err  error
}

// RepoState is the result of an apt repository check.
type RepoState struct {
	// UpdateErr is the error of `apt-get update`, nil when it succeeded.
	UpdateErr error
	// NoCandidate lists the wanted packages apt cannot install.
	NoCandidate []string
}

// Info describes the host.
type Info struct {
	OSID           string // "ubuntu", "debian", ...
	OSVersion      string // "24.04"
	OSName         string // "Ubuntu 24.04 LTS"
	Arch           string
	Kernel         string
	PackageManager string // "apt", "dnf", "yum", "zypper", "pacman", "apk" or ""
	Systemd        bool
	RAM            uint64 // bytes, 0 when unknown
	Disks          []Disk
	Repo           *RepoState // nil until CheckRepos ran
}

// Env holds every source of host data. DefaultEnv reads the real host.
type Env struct {
	OSReleasePath string
	MeminfoPath   string
	KernelPath    string
	SystemdDir    string
	Arch          string
	LookPath      func(string) (string, error)
	FreeBytes     func(path string) (uint64, error)
	Run           func(name string, args ...string) ([]byte, error)
}

// DefaultEnv returns the Env of the running host.
func DefaultEnv() Env {
	return Env{
		OSReleasePath: "/etc/os-release",
		MeminfoPath:   "/proc/meminfo",
		KernelPath:    "/proc/sys/kernel/osrelease",
		SystemdDir:    "/run/systemd/system",
		Arch:          runtime.GOARCH,
		LookPath:      lookPath,
		FreeBytes:     nearestFree,
		Run: func(name string, args ...string) ([]byte, error) {
			return execx.Command(name, args...).Output()
		},
	}
}

// Collect gathers what can be read without changing the host. Fields that
// cannot be read stay at their zero value. The returned error is set only
// when os-release cannot be read, because every decision about support
// starts from it.
func Collect(env Env) (Info, error) {
	info := Info{Arch: env.Arch}
	var osErr error
	if b, err := os.ReadFile(env.OSReleasePath); err != nil {
		osErr = err
	} else {
		info.OSID, info.OSVersion, info.OSName = ParseOSRelease(string(b))
	}
	if b, err := os.ReadFile(env.KernelPath); err == nil {
		info.Kernel = strings.TrimSpace(string(b))
	}
	if b, err := os.ReadFile(env.MeminfoPath); err == nil {
		info.RAM = ParseMemTotal(string(b))
	}
	if st, err := os.Stat(env.SystemdDir); err == nil && st.IsDir() {
		info.Systemd = true
	}
	info.PackageManager = DetectPackageManager(env.LookPath)
	for _, p := range DiskPaths {
		free, err := env.FreeBytes(p)
		info.Disks = append(info.Disks, Disk{Path: p, Free: free, Err: err})
	}
	return info, osErr
}

// ParseOSRelease reads ID, VERSION_ID and PRETTY_NAME out of the content of
// /etc/os-release (KEY=VALUE lines, values optionally double-quoted).
func ParseOSRelease(content string) (id, versionID, name string) {
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		val = strings.Trim(val, `"`)
		switch key {
		case "ID":
			id = val
		case "VERSION_ID":
			versionID = val
		case "PRETTY_NAME":
			name = val
		}
	}
	return id, versionID, name
}

// ParseMemTotal returns MemTotal of /proc/meminfo in bytes, 0 when absent.
func ParseMemTotal(content string) uint64 {
	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			return 0
		}
		kb, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb * 1024
	}
	return 0
}

// ParseKernel turns "5.15.0-91-generic" into (5, 15, true).
func ParseKernel(release string) (major, minor int, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(release), ".", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	digits := parts[1]
	for i, r := range digits {
		if r < '0' || r > '9' {
			digits = digits[:i]
			break
		}
	}
	minor, err = strconv.Atoi(digits)
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

// DetectPackageManager names the first package manager found: "apt" for
// apt-get, otherwise the command name. It returns "" when none is found.
func DetectPackageManager(lookPath func(string) (string, error)) string {
	for _, pm := range packageManagers {
		if _, err := lookPath(pm); err == nil {
			if pm == "apt-get" {
				return "apt"
			}
			return pm
		}
	}
	return ""
}

// CheckRepos runs `apt-get update` and asks `apt-cache policy` whether each
// wanted package has an installable version. It changes the package lists
// only. On hosts without apt it records nothing.
func CheckRepos(env Env, info *Info, packages []string) {
	if info.PackageManager != "apt" {
		return
	}
	state := &RepoState{}
	if _, err := env.Run("apt-get", "update"); err != nil {
		state.UpdateErr = err
	}
	out, err := env.Run("apt-cache", append([]string{"policy"}, packages...)...)
	if err != nil {
		state.NoCandidate = append([]string(nil), packages...)
	} else {
		have := parsePolicyCandidates(string(out))
		for _, p := range packages {
			if !have[p] {
				state.NoCandidate = append(state.NoCandidate, p)
			}
		}
	}
	info.Repo = state
}

// parsePolicyCandidates returns the packages of `apt-cache policy` output
// that have an installable candidate.
func parsePolicyCandidates(output string) map[string]bool {
	have := map[string]bool{}
	current := ""
	for _, line := range strings.Split(output, "\n") {
		switch {
		case line != "" && !strings.HasPrefix(line, " ") && strings.HasSuffix(line, ":"):
			current = strings.TrimSuffix(line, ":")
		case current != "" && strings.HasPrefix(strings.TrimSpace(line), "Candidate:"):
			cand := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "Candidate:"))
			if cand != "" && cand != "(none)" {
				have[current] = true
			}
		}
	}
	return have
}

func lookPath(name string) (string, error) {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0111 != 0 {
			return p, nil
		}
	}
	return "", errors.New("not found: " + name)
}

// nearestFree returns the free bytes of the file system that holds path. A
// directory that does not exist yet (/var/lib/docker before Docker is
// installed) is measured at its nearest existing parent.
func nearestFree(path string) (uint64, error) {
	p := path
	for {
		var st syscall.Statfs_t
		err := syscall.Statfs(p, &st)
		if err == nil {
			return st.Bavail * uint64(st.Bsize), nil
		}
		parent := filepath.Dir(p)
		if parent == p {
			return 0, fmt.Errorf("statfs %s: %w", path, err)
		}
		p = parent
	}
}
