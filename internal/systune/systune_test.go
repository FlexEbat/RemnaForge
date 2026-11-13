package systune

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fakeHost points every path at a temporary directory and replaces the
// command runners with a recorder. It returns the recorded command lines.
type fakeHost struct {
	calls       []string
	failOn      string            // a command line (prefix) that fails
	outputs     map[string]string // command line prefix -> standard output
	bbrOnSysctl bool              // a sysctl -p makes the runtime file say bbr
	dir         string
}

func newFakeHost(t *testing.T) *fakeHost {
	t.Helper()
	h := &fakeHost{dir: t.TempDir(), outputs: map[string]string{}}
	oldBBR, oldRT, oldAuto, oldReboot := bbrConfPath, bbrRuntimePath, autoUpgradesPath, rebootRequiredPath
	oldRun, oldOut := runCmd, outputCmd
	bbrConfPath = filepath.Join(h.dir, "sysctl.d", "99-remnaforge-bbr.conf")
	bbrRuntimePath = filepath.Join(h.dir, "tcp_congestion_control")
	autoUpgradesPath = filepath.Join(h.dir, "apt.conf.d", "20auto-upgrades")
	rebootRequiredPath = filepath.Join(h.dir, "reboot-required")
	if err := os.WriteFile(bbrRuntimePath, []byte("cubic\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runCmd = func(name string, args ...string) error {
		line := strings.TrimSpace(name + " " + strings.Join(args, " "))
		h.calls = append(h.calls, line)
		if h.failOn != "" && strings.HasPrefix(line, h.failOn) {
			return errors.New("boom")
		}
		if h.bbrOnSysctl && strings.HasPrefix(line, "sysctl -p") {
			_ = os.WriteFile(bbrRuntimePath, []byte("bbr\n"), 0644)
		}
		if strings.HasPrefix(line, "sysctl -w net.ipv4.tcp_congestion_control=") {
			_ = os.WriteFile(bbrRuntimePath, []byte(strings.TrimPrefix(line, "sysctl -w net.ipv4.tcp_congestion_control=")+"\n"), 0644)
		}
		return nil
	}
	outputCmd = func(name string, args ...string) ([]byte, error) {
		line := strings.TrimSpace(name + " " + strings.Join(args, " "))
		h.calls = append(h.calls, line)
		for prefix, o := range h.outputs {
			if strings.HasPrefix(line, prefix) {
				return []byte(o), nil
			}
		}
		return nil, errors.New("no output configured for " + line)
	}
	t.Cleanup(func() {
		bbrConfPath, bbrRuntimePath, autoUpgradesPath, rebootRequiredPath = oldBBR, oldRT, oldAuto, oldReboot
		runCmd, outputCmd = oldRun, oldOut
	})
	return h
}

func (h *fakeHost) ran(prefix string) bool {
	for _, c := range h.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func TestWriteBBRConf(t *testing.T) {
	path := filepath.Join(t.TempDir(), "99-test-bbr.conf")
	if err := writeBBRConf(path); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != bbrConf {
		t.Fatalf("content = %q, err = %v; want %q", got, err, bbrConf)
	}
	// A second run must not rewrite an up-to-date file, even a read-only one.
	if err := os.Chmod(path, 0400); err != nil {
		t.Fatal(err)
	}
	if err := writeBBRConf(path); err != nil {
		t.Errorf("rewrite of up-to-date file failed: %v", err)
	}
	// A stale file is replaced.
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# stale\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeBBRConf(path); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != bbrConf {
		t.Errorf("stale file not replaced, got %q", got)
	}
}

func TestEnableBBR(t *testing.T) {
	h := newFakeHost(t)
	h.bbrOnSysctl = true
	if BBREnabled() {
		t.Fatal("BBR reported on before enabling")
	}
	if err := EnableBBR(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(bbrConfPath); string(got) != bbrConf {
		t.Errorf("drop-in = %q", got)
	}
	if !h.ran("modprobe tcp_bbr") || !h.ran("sysctl -p "+bbrConfPath) {
		t.Errorf("calls = %v", h.calls)
	}
	if !BBREnabled() {
		t.Error("BBR not reported on after enabling")
	}
	// Enabling twice is harmless.
	if err := EnableBBR(); err != nil {
		t.Errorf("second enable: %v", err)
	}
}

func TestEnableBBRFailsWhenKernelStaysOnCubic(t *testing.T) {
	newFakeHost(t) // sysctl succeeds but the runtime value does not change
	if err := EnableBBR(); !errors.Is(err, ErrBBRUnavailable) {
		t.Fatalf("want ErrBBRUnavailable, got %v", err)
	}
}

func TestEnableBBRSysctlError(t *testing.T) {
	h := newFakeHost(t)
	h.failOn = "sysctl -p"
	if err := EnableBBR(); err == nil || errors.Is(err, ErrBBRUnavailable) {
		t.Fatalf("want the sysctl error, got %v", err)
	}
}

func TestDisableBBR(t *testing.T) {
	h := newFakeHost(t)
	h.bbrOnSysctl = true
	if err := EnableBBR(); err != nil {
		t.Fatal(err)
	}
	if err := DisableBBR(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bbrConfPath); !os.IsNotExist(err) {
		t.Errorf("drop-in still there: %v", err)
	}
	if BBREnabled() {
		t.Error("BBR still on after disabling")
	}
	// Disabling when nothing is set is not an error.
	if err := DisableBBR(); err != nil {
		t.Errorf("second disable: %v", err)
	}
}

func TestUnattendedEnabledParsing(t *testing.T) {
	newFakeHost(t)
	if UnattendedEnabled() {
		t.Fatal("on without a file")
	}
	cases := map[string]bool{
		"APT::Periodic::Unattended-Upgrade \"1\";\n":                                             true,
		"APT::Periodic::Update-Package-Lists \"1\";\nAPT::Periodic::Unattended-Upgrade \"1\";\n": true,
		"APT::Periodic::Unattended-Upgrade \"0\";\n":                                             false,
		"// APT::Periodic::Unattended-Upgrade \"1\";\n":                                          false,
		"": false,
	}
	if err := os.MkdirAll(filepath.Dir(autoUpgradesPath), 0755); err != nil {
		t.Fatal(err)
	}
	for content, want := range cases {
		if err := os.WriteFile(autoUpgradesPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		if got := UnattendedEnabled(); got != want {
			t.Errorf("%q: got %v, want %v", content, got, want)
		}
	}
}

func TestEnableUnattendedInstallsMissingPackage(t *testing.T) {
	h := newFakeHost(t)
	h.outputs["dpkg-query"] = "unknown ok not-installed"
	if err := EnableUnattended(); err != nil {
		t.Fatal(err)
	}
	if !h.ran("apt-get install -y unattended-upgrades") {
		t.Errorf("package not installed, calls = %v", h.calls)
	}
	if !UnattendedEnabled() {
		t.Error("not enabled after EnableUnattended")
	}
}

func TestEnableUnattendedSkipsInstalledPackage(t *testing.T) {
	h := newFakeHost(t)
	h.outputs["dpkg-query"] = "install ok installed"
	if err := EnableUnattended(); err != nil {
		t.Fatal(err)
	}
	if h.ran("apt-get") {
		t.Errorf("apt-get ran for an installed package: %v", h.calls)
	}
}

func TestEnableUnattendedInstallFailure(t *testing.T) {
	h := newFakeHost(t)
	h.outputs["dpkg-query"] = ""
	h.failOn = "apt-get install"
	if err := EnableUnattended(); err == nil {
		t.Fatal("want the install error")
	}
	if _, err := os.Stat(autoUpgradesPath); !os.IsNotExist(err) {
		t.Error("config written although the install failed")
	}
}

func TestDisableUnattended(t *testing.T) {
	h := newFakeHost(t)
	h.outputs["dpkg-query"] = "install ok installed"
	if err := EnableUnattended(); err != nil {
		t.Fatal(err)
	}
	if err := DisableUnattended(); err != nil {
		t.Fatal(err)
	}
	if UnattendedEnabled() {
		t.Error("still enabled")
	}
	got, _ := os.ReadFile(autoUpgradesPath)
	if string(got) != autoUpgradesConf(false) {
		t.Errorf("config = %q", got)
	}
	if err := DisableUnattended(); err != nil {
		t.Errorf("second disable: %v", err)
	}
}

const simulation = `Reading package lists...
Building dependency tree...
The following packages will be upgraded:
  curl libcurl4
2 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.
Inst curl [8.5.0-2] (8.5.0-3 Ubuntu:24.04/noble-updates [amd64])
Inst libcurl4 [8.5.0-2] (8.5.0-3 Ubuntu:24.04/noble-updates [amd64])
Conf curl (8.5.0-3 Ubuntu:24.04/noble-updates [amd64])
Conf libcurl4 (8.5.0-3 Ubuntu:24.04/noble-updates [amd64])
`

func TestParsePending(t *testing.T) {
	if got, want := ParsePending(simulation), []string{"curl", "libcurl4"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := ParsePending("0 upgraded, 0 newly installed.\n"); len(got) != 0 {
		t.Errorf("got %v for an empty simulation", got)
	}
	if got := ParsePending(""); len(got) != 0 {
		t.Errorf("got %v for empty input", got)
	}
}

func TestPendingUpgrades(t *testing.T) {
	h := newFakeHost(t)
	h.outputs["apt-get -s upgrade"] = simulation
	got, err := PendingUpgrades()
	if err != nil || !reflect.DeepEqual(got, []string{"curl", "libcurl4"}) {
		t.Fatalf("got %v, %v", got, err)
	}
	if len(h.calls) < 2 || h.calls[0] != "apt-get update" {
		t.Errorf("the lists must be refreshed first: %v", h.calls)
	}
	h.failOn = "apt-get update"
	if _, err := PendingUpgrades(); err == nil {
		t.Error("want the update error")
	}
}

func TestApplyUpgradesKeepsLocalConfig(t *testing.T) {
	h := newFakeHost(t)
	if err := ApplyUpgrades(); err != nil {
		t.Fatal(err)
	}
	line := h.calls[0]
	for _, want := range []string{"apt-get upgrade -y", "--force-confold", "--force-confdef"} {
		if !strings.Contains(line, want) {
			t.Errorf("%q misses %q", line, want)
		}
	}
}

func TestRebootRequired(t *testing.T) {
	newFakeHost(t)
	if RebootRequired() {
		t.Fatal("reboot reported without the marker")
	}
	if err := os.WriteFile(rebootRequiredPath, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if !RebootRequired() {
		t.Error("marker not noticed")
	}
}

// script feeds answers to the menu and records what it printed.
type script struct {
	answers []string
	printed []string
	prompts []string
}

func (s *script) install(t *testing.T) {
	t.Helper()
	oldRead, oldClear, oldOut := readLine, clearScreen, out
	readLine = func(prompt string) string {
		s.prompts = append(s.prompts, prompt)
		if len(s.answers) == 0 {
			t.Fatalf("menu asked for more input: %q", prompt)
		}
		a := s.answers[0]
		s.answers = s.answers[1:]
		return a
	}
	clearScreen = func() error { return nil }
	out = func(_, msg string) { s.printed = append(s.printed, msg) }
	t.Cleanup(func() { readLine, clearScreen, out = oldRead, oldClear, oldOut })
}

func (s *script) said(sub string) bool {
	for _, p := range s.printed {
		if strings.Contains(p, sub) {
			return true
		}
	}
	return false
}

func TestMenuInvalidChoiceThenExit(t *testing.T) {
	newFakeHost(t)
	s := &script{answers: []string{"9", "", "0"}}
	s.install(t)
	Menu()
	if !s.said("Invalid choice") {
		t.Errorf("no invalid-choice message: %v", s.printed)
	}
}

func TestMenuShowsState(t *testing.T) {
	h := newFakeHost(t)
	h.bbrOnSysctl = true
	if err := EnableBBR(); err != nil {
		t.Fatal(err)
	}
	s := &script{answers: []string{"0"}}
	s.install(t)
	Menu()
	if !s.said("BBR congestion control: on") || !s.said("Automatic security updates: off") {
		t.Errorf("states not shown: %v", s.printed)
	}
}

func TestMenuTogglesBBRAfterConfirm(t *testing.T) {
	h := newFakeHost(t)
	h.bbrOnSysctl = true
	s := &script{answers: []string{"1", "y", "0"}}
	s.install(t)
	Menu()
	if !BBREnabled() || !s.said("is on") {
		t.Errorf("BBR not turned on: %v", s.printed)
	}
}

func TestMenuDeclinedConfirmChangesNothing(t *testing.T) {
	h := newFakeHost(t)
	for _, no := range []string{"n", "", "no", "x"} {
		s := &script{answers: []string{"1", no, "2", no, "0"}}
		s.install(t)
		Menu()
		if _, err := os.Stat(bbrConfPath); !os.IsNotExist(err) {
			t.Fatalf("answer %q wrote the BBR drop-in", no)
		}
		if _, err := os.Stat(autoUpgradesPath); !os.IsNotExist(err) {
			t.Fatalf("answer %q wrote the auto-upgrade config", no)
		}
	}
	if h.ran("apt-get") || h.ran("sysctl") || h.ran("modprobe") {
		t.Errorf("commands ran without a confirmation: %v", h.calls)
	}
}

func TestMenuTurnsOffAnOnSwitch(t *testing.T) {
	h := newFakeHost(t)
	h.outputs["dpkg-query"] = "install ok installed"
	if err := EnableUnattended(); err != nil {
		t.Fatal(err)
	}
	s := &script{answers: []string{"2", "Y", "0"}}
	s.install(t)
	Menu()
	if UnattendedEnabled() || !s.said("is off") {
		t.Errorf("not turned off: %v", s.printed)
	}
	if !strings.Contains(s.prompts[1], "Turn off") {
		t.Errorf("an on switch must ask to turn off, got %q", s.prompts[1])
	}
}

func TestMenuReportsFailure(t *testing.T) {
	newFakeHost(t) // the kernel stays on cubic, so enabling fails
	s := &script{answers: []string{"1", "y", "0"}}
	s.install(t)
	Menu()
	if !s.said("failed") {
		t.Errorf("failure not reported: %v", s.printed)
	}
}

func TestMenuUpgradeFlow(t *testing.T) {
	h := newFakeHost(t)
	h.outputs["apt-get -s upgrade"] = simulation
	s := &script{answers: []string{"3", "y", "0"}}
	s.install(t)
	Menu()
	if !h.ran("apt-get upgrade -y") {
		t.Errorf("upgrade did not run: %v", h.calls)
	}
	if !s.said("curl libcurl4") || !s.said("upgraded") {
		t.Errorf("output = %v", s.printed)
	}
}

func TestMenuUpgradeDeclinedAndNothingToDo(t *testing.T) {
	h := newFakeHost(t)
	h.outputs["apt-get -s upgrade"] = simulation
	s := &script{answers: []string{"3", "n", "0"}}
	s.install(t)
	Menu()
	if h.ran("apt-get upgrade -y") {
		t.Error("upgrade ran although the operator declined")
	}

	h2 := newFakeHost(t)
	h2.outputs["apt-get -s upgrade"] = "0 upgraded, 0 newly installed.\n"
	s2 := &script{answers: []string{"3", "0"}}
	s2.install(t)
	Menu()
	if h2.ran("apt-get upgrade -y") || !s2.said("up to date") {
		t.Errorf("calls %v, output %v", h2.calls, s2.printed)
	}
}

func TestMenuUpgradeReportsReboot(t *testing.T) {
	h := newFakeHost(t)
	h.outputs["apt-get -s upgrade"] = simulation
	if err := os.WriteFile(rebootRequiredPath, nil, 0644); err != nil {
		t.Fatal(err)
	}
	s := &script{answers: []string{"3", "y", "0"}}
	s.install(t)
	Menu()
	if !s.said("reboot") {
		t.Errorf("reboot notice missing: %v", s.printed)
	}
}

func TestMenuUpgradeFailure(t *testing.T) {
	h := newFakeHost(t)
	h.outputs["apt-get -s upgrade"] = simulation
	h.failOn = "apt-get upgrade -y"
	s := &script{answers: []string{"3", "y", "0"}}
	s.install(t)
	Menu()
	if !s.said("failed") || s.said("Packages upgraded") {
		t.Errorf("output = %v", s.printed)
	}
}
