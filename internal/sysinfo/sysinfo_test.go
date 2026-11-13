package sysinfo

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const ubuntuRelease = `PRETTY_NAME="Ubuntu 24.04.1 LTS"
NAME="Ubuntu"
VERSION_ID="24.04"
VERSION="24.04.1 LTS (Noble Numbat)"
ID=ubuntu
ID_LIKE=debian
`

func TestParseOSRelease(t *testing.T) {
	cases := []struct {
		name, in            string
		id, version, pretty string
	}{
		{"ubuntu", ubuntuRelease, "ubuntu", "24.04", "Ubuntu 24.04.1 LTS"},
		{"debian", "PRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\nID=debian\nVERSION_ID=\"12\"\n", "debian", "12", "Debian GNU/Linux 12 (bookworm)"},
		{"fedora", "NAME=\"Fedora Linux\"\nVERSION_ID=40\nID=fedora\nPRETTY_NAME=\"Fedora Linux 40 (Server Edition)\"\n", "fedora", "40", "Fedora Linux 40 (Server Edition)"},
		{"alpine without quotes", "ID=alpine\nVERSION_ID=3.19.1\nPRETTY_NAME=\"Alpine Linux v3.19\"\n", "alpine", "3.19.1", "Alpine Linux v3.19"},
		{"ID_LIKE does not win", "ID=debian\nID_LIKE=ubuntu\n", "debian", "", ""},
		{"comments and junk", "# c\n\nbroken\nID=debian\n", "debian", "", ""},
		{"empty", "", "", "", ""},
	}
	for _, c := range cases {
		id, v, n := ParseOSRelease(c.in)
		if id != c.id || v != c.version || n != c.pretty {
			t.Errorf("%s: got %q %q %q", c.name, id, v, n)
		}
	}
}

func TestParseMemTotal(t *testing.T) {
	cases := map[string]uint64{
		"MemTotal:        2048000 kB\nMemFree: 1 kB\n": 2048000 * 1024,
		"MemFree: 1 kB\nMemTotal: 1024 kB\n":           1024 * 1024,
		"MemTotal: 0 kB\n":                             0,
		"MemTotal:\n":                                  0,
		"MemTotal: lots kB\n":                          0,
		"":                                             0,
		"SwapTotal: 100 kB\n":                          0,
	}
	for in, want := range cases {
		if got := ParseMemTotal(in); got != want {
			t.Errorf("%q: got %d, want %d", in, got, want)
		}
	}
}

func TestParseKernel(t *testing.T) {
	cases := []struct {
		in           string
		major, minor int
		ok           bool
	}{
		{"5.15.0-91-generic", 5, 15, true},
		{"6.8.0-45-generic\n", 6, 8, true},
		{"4.9.0-19-amd64", 4, 9, true},
		{"6.1.0", 6, 1, true},
		{"6.1", 6, 1, true},
		{"5.10-rc1", 5, 10, true},
		{"3.10.0-1160.el7.x86_64", 3, 10, true},
		{"", 0, 0, false},
		{"6", 0, 0, false},
		{"x.y.z", 0, 0, false},
		{"6.x", 0, 0, false},
	}
	for _, c := range cases {
		major, minor, ok := ParseKernel(c.in)
		if ok != c.ok || (ok && (major != c.major || minor != c.minor)) {
			t.Errorf("%q: got %d.%d %v", c.in, major, minor, ok)
		}
	}
}

func TestDetectPackageManager(t *testing.T) {
	have := func(names ...string) func(string) (string, error) {
		return func(n string) (string, error) {
			for _, h := range names {
				if h == n {
					return "/usr/bin/" + n, nil
				}
			}
			return "", errors.New("missing")
		}
	}
	cases := []struct {
		have []string
		want string
	}{
		{[]string{"apt-get", "dpkg"}, "apt"},
		{[]string{"dnf", "yum"}, "dnf"},
		{[]string{"yum"}, "yum"},
		{[]string{"zypper"}, "zypper"},
		{[]string{"pacman"}, "pacman"},
		{[]string{"apk"}, "apk"},
		{[]string{"apt-get", "dnf"}, "apt"},
		{nil, ""},
	}
	for _, c := range cases {
		if got := DetectPackageManager(have(c.have...)); got != c.want {
			t.Errorf("%v: got %q, want %q", c.have, got, c.want)
		}
	}
}

// fixtureEnv builds an Env over files in a temp dir.
func fixtureEnv(t *testing.T, osRelease, meminfo, kernel string, systemd bool, free map[string]uint64, tools ...string) Env {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if content != "" {
			if err := os.WriteFile(p, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
		}
		return p
	}
	sd := filepath.Join(dir, "systemd")
	if systemd {
		if err := os.MkdirAll(sd, 0755); err != nil {
			t.Fatal(err)
		}
	}
	return Env{
		OSReleasePath: write("os-release", osRelease),
		MeminfoPath:   write("meminfo", meminfo),
		KernelPath:    write("osrelease", kernel),
		SystemdDir:    sd,
		Arch:          "amd64",
		LookPath: func(n string) (string, error) {
			for _, h := range tools {
				if h == n {
					return n, nil
				}
			}
			return "", errors.New("missing")
		},
		FreeBytes: func(p string) (uint64, error) {
			if v, ok := free[p]; ok {
				return v, nil
			}
			return 0, errors.New("no such path")
		},
		Run: func(string, ...string) ([]byte, error) { return nil, errors.New("not used") },
	}
}

const gib = uint64(1 << 30)

func TestCollectFixture(t *testing.T) {
	env := fixtureEnv(t, ubuntuRelease, "MemTotal: 4194304 kB\n", "6.8.0-45-generic\n", true,
		map[string]uint64{"/opt": 30 * gib, "/var/lib/docker": 25 * gib}, "apt-get")
	info, err := Collect(env)
	if err != nil {
		t.Fatal(err)
	}
	want := Info{
		OSID: "ubuntu", OSVersion: "24.04", OSName: "Ubuntu 24.04.1 LTS",
		Arch: "amd64", Kernel: "6.8.0-45-generic", PackageManager: "apt", Systemd: true,
		RAM:   4 * gib,
		Disks: []Disk{{Path: "/opt", Free: 30 * gib}, {Path: "/var/lib/docker", Free: 25 * gib}},
	}
	if !reflect.DeepEqual(info, want) {
		t.Errorf("got  %+v\nwant %+v", info, want)
	}
	if f := Evaluate(info); len(f) != 0 {
		t.Errorf("a healthy host produced findings: %v", f)
	}
}

func TestCollectMissingFiles(t *testing.T) {
	env := fixtureEnv(t, "", "", "", false, nil)
	info, err := Collect(env)
	if err == nil {
		t.Error("an unreadable os-release must be reported")
	}
	if info.RAM != 0 || info.Kernel != "" || info.Systemd || info.PackageManager != "" {
		t.Errorf("unreadable sources must leave zero values: %+v", info)
	}
	for _, d := range info.Disks {
		if d.Err == nil {
			t.Errorf("disk %s should carry an error", d.Path)
		}
	}
}

func hostWith(ram, opt, docker uint64, kernel string, systemd bool) Info {
	return Info{
		OSID: "ubuntu", OSVersion: "24.04", Kernel: kernel, Systemd: systemd, RAM: ram,
		Disks: []Disk{{Path: "/opt", Free: opt}, {Path: "/var/lib/docker", Free: docker}},
	}
}

func severities(fs []Finding) (refuse, warn int) {
	for _, f := range fs {
		if f.Severity == Refuse {
			refuse++
		} else {
			warn++
		}
	}
	return
}

func TestEvaluateRAMThresholds(t *testing.T) {
	cases := []struct {
		ram          uint64
		refuse, warn int
	}{
		{0, 0, 0}, // unknown is not judged
		{512 << 20, 1, 0},
		{MinRAM - 1, 1, 0},
		{MinRAM, 0, 1},
		{WarnRAM - 1, 0, 1},
		{WarnRAM, 0, 0},
		{8 * gib, 0, 0},
	}
	for _, c := range cases {
		r, w := severities(Evaluate(hostWith(c.ram, 50*gib, 50*gib, "6.8.0", true)))
		if r != c.refuse || w != c.warn {
			t.Errorf("ram %d: refuse=%d warn=%d, want %d/%d", c.ram, r, w, c.refuse, c.warn)
		}
	}
}

func TestEvaluateDisk(t *testing.T) {
	f := Evaluate(hostWith(4*gib, MinFreeDisk-1, MinFreeDisk, "6.8.0", true))
	if r, w := severities(f); r != 0 || w != 1 || !strings.Contains(f[0].Message, "/opt") {
		t.Errorf("findings = %v", f)
	}
	f = Evaluate(hostWith(4*gib, 1*gib, 2*gib, "6.8.0", true))
	if _, w := severities(f); w != 2 {
		t.Errorf("both paths low must warn twice: %v", f)
	}
	// A path that could not be measured is skipped.
	info := hostWith(4*gib, 50*gib, 0, "6.8.0", true)
	info.Disks[1].Err = errors.New("boom")
	if f := Evaluate(info); len(f) != 0 {
		t.Errorf("a read error was judged as low space: %v", f)
	}
}

func TestEvaluateKernelAndSystemd(t *testing.T) {
	cases := []struct {
		kernel string
		warn   bool
	}{
		{"4.8.17", true}, {"3.10.0-1160.el7", true}, {"4.9.0", false},
		{"4.19.0-26-amd64", false}, {"6.8.0-45-generic", false}, {"", false}, {"weird", false},
	}
	for _, c := range cases {
		_, w := severities(Evaluate(hostWith(4*gib, 50*gib, 50*gib, c.kernel, true)))
		if (w == 1) != c.warn {
			t.Errorf("kernel %q: warnings = %d, want warn=%v", c.kernel, w, c.warn)
		}
	}
	if r, w := severities(Evaluate(hostWith(4*gib, 50*gib, 50*gib, "6.8.0", false))); r != 0 || w != 1 {
		t.Errorf("missing systemd: refuse=%d warn=%d", r, w)
	}
}

func TestEvaluateRepoState(t *testing.T) {
	info := hostWith(4*gib, 50*gib, 50*gib, "6.8.0", true)
	info.Repo = &RepoState{}
	if f := Evaluate(info); len(f) != 0 {
		t.Errorf("clean repos produced %v", f)
	}
	info.Repo = &RepoState{UpdateErr: errors.New("exit 100"), NoCandidate: []string{"certbot", "jq"}}
	f := Evaluate(info)
	if len(f) != 2 || !strings.Contains(f[0].Message, "exit 100") || !strings.Contains(f[1].Message, "certbot jq") {
		t.Errorf("findings = %v", f)
	}
}

const policyOutput = `curl:
  Installed: 8.5.0-2ubuntu10
  Candidate: 8.5.0-2ubuntu10.1
  Version table:
 *** 8.5.0-2ubuntu10 500
        500 http://archive.ubuntu.com/ubuntu noble/main amd64 Packages
jq:
  Installed: (none)
  Candidate: 1.7.1-3build1
ghost:
  Installed: (none)
  Candidate: (none)
`

func TestParsePolicyCandidates(t *testing.T) {
	got := parsePolicyCandidates(policyOutput)
	want := map[string]bool{"curl": true, "jq": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := parsePolicyCandidates(""); len(got) != 0 {
		t.Errorf("empty output gave %v", got)
	}
}

func TestCheckRepos(t *testing.T) {
	var calls []string
	env := Env{Run: func(name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if name == "apt-cache" {
			return []byte(policyOutput), nil
		}
		return nil, nil
	}}
	info := Info{PackageManager: "apt"}
	CheckRepos(env, &info, []string{"curl", "jq", "ghost", "unknown-pkg"})
	if info.Repo == nil || info.Repo.UpdateErr != nil {
		t.Fatalf("repo state = %+v", info.Repo)
	}
	if want := []string{"ghost", "unknown-pkg"}; !reflect.DeepEqual(info.Repo.NoCandidate, want) {
		t.Errorf("NoCandidate = %v, want %v", info.Repo.NoCandidate, want)
	}
	if len(calls) != 2 || calls[0] != "apt-get update" || !strings.HasPrefix(calls[1], "apt-cache policy curl jq") {
		t.Errorf("calls = %v", calls)
	}
}

func TestCheckReposUpdateFailure(t *testing.T) {
	env := Env{Run: func(name string, args ...string) ([]byte, error) {
		if name == "apt-get" {
			return nil, errors.New("exit status 100")
		}
		return []byte(policyOutput), nil
	}}
	info := Info{PackageManager: "apt"}
	CheckRepos(env, &info, []string{"curl"})
	if info.Repo == nil || info.Repo.UpdateErr == nil || len(info.Repo.NoCandidate) != 0 {
		t.Errorf("repo state = %+v", info.Repo)
	}
}

func TestCheckReposSkipsOtherPackageManagers(t *testing.T) {
	env := Env{Run: func(string, ...string) ([]byte, error) {
		t.Fatal("a command ran on a host without apt")
		return nil, nil
	}}
	for _, pm := range []string{"dnf", "apk", ""} {
		info := Info{PackageManager: pm}
		CheckRepos(env, &info, []string{"curl"})
		if info.Repo != nil {
			t.Errorf("%q: repo state recorded", pm)
		}
	}
}

func TestCheckReposPolicyFailureMarksAllMissing(t *testing.T) {
	env := Env{Run: func(name string, args ...string) ([]byte, error) {
		if name == "apt-cache" {
			return nil, errors.New("boom")
		}
		return nil, nil
	}}
	info := Info{PackageManager: "apt"}
	CheckRepos(env, &info, []string{"a", "b"})
	if !reflect.DeepEqual(info.Repo.NoCandidate, []string{"a", "b"}) {
		t.Errorf("got %v", info.Repo.NoCandidate)
	}
}

func TestNearestFreeWalksUp(t *testing.T) {
	dir := t.TempDir()
	free, err := nearestFree(filepath.Join(dir, "does", "not", "exist"))
	if err != nil || free == 0 {
		t.Errorf("free = %d, err = %v", free, err)
	}
}

func TestLinesAndFormat(t *testing.T) {
	info := Info{OSID: "debian", OSVersion: "12", OSName: "Debian GNU/Linux 12 (bookworm)", Arch: "arm64",
		Kernel: "6.1.0-18-arm64", PackageManager: "apt", Systemd: true, RAM: 2 * gib,
		Disks: []Disk{{Path: "/opt", Free: 12*gib + gib/2}, {Path: "/var/lib/docker", Err: errors.New("x")}},
		Repo:  &RepoState{}}
	lines := strings.Join(Lines(info), "\n")
	for _, want := range []string{"Debian GNU/Linux 12 (bookworm), arm64, kernel 6.1.0-18-arm64", "Package manager: apt, systemd: yes", "Memory: 2.0 GiB", "/opt 12.5 GiB", "Package repositories: ok"} {
		if !strings.Contains(lines, want) {
			t.Errorf("block misses %q:\n%s", want, lines)
		}
	}
	if strings.Contains(lines, "/var/lib/docker") {
		t.Errorf("an unreadable disk is printed:\n%s", lines)
	}
	if got := FormatBytes(3*gib + gib/4); got != "3.2 GiB" {
		t.Errorf("FormatBytes = %q", got)
	}
}

func TestPrintRefusesSmallHost(t *testing.T) {
	err := Print(hostWith(512<<20, 50*gib, 50*gib, "6.8.0", true))
	if err == nil || !strings.Contains(err.Error(), "Not enough memory") {
		t.Errorf("err = %v", err)
	}
	if err := Print(hostWith(4*gib, 1*gib, 1*gib, "4.4.0", false)); err != nil {
		t.Errorf("warnings must not refuse: %v", err)
	}
}

// The real host must be readable: the fixture tests above prove the
// parsing, this proves the default paths and calls work on Linux.
func TestCollectRealHost(t *testing.T) {
	info, err := Collect(DefaultEnv())
	if err != nil {
		t.Skipf("no /etc/os-release here: %v", err)
	}
	if info.Arch == "" || info.OSID == "" || info.RAM == 0 || info.Kernel == "" {
		t.Errorf("incomplete: %+v", info)
	}
	if len(info.Disks) != len(DiskPaths) {
		t.Errorf("disks = %v", info.Disks)
	}
	if len(Lines(info)) < 3 {
		t.Errorf("block too short: %v", Lines(info))
	}
}
