package oscheck

import (
	"os"
	"strings"
	"testing"

	"github.com/FlexEbat/RemnaForge/internal/sysinfo"
)

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in           string
		major, minor int
		ok           bool
	}{
		{"24.04", 24, 4, true},
		{"22.04", 22, 4, true},
		{"13", 13, 0, true},
		{"13.6.0", 13, 6, true},
		{"", 0, 0, false},
		{"abc", 0, 0, false},
		{"24.x", 0, 0, false},
	}
	for _, c := range cases {
		major, minor, ok := parseVersion(c.in)
		if ok != c.ok || (ok && (major != c.major || minor != c.minor)) {
			t.Errorf("%q: got %d.%d ok=%v, want %d.%d ok=%v", c.in, major, minor, ok, c.major, c.minor, c.ok)
		}
	}
}

func TestSupported(t *testing.T) {
	cases := []struct {
		id, ver string
		want    bool
	}{
		{"debian", "10", false},
		{"debian", "11", true},
		{"debian", "12", true},
		{"debian", "13", true},
		{"Debian", "12", true},
		{"ubuntu", "20.04", false},
		{"ubuntu", "22.02", false},
		{"ubuntu", "22.04", true},
		{"ubuntu", "22.10", true},
		{"ubuntu", "24.04", true},
		{"ubuntu", "26.04", true},
		{"ubuntu", "", false},
		{"debian", "sid", false},
		{"fedora", "40", false},
		{"", "", false},
	}
	for _, c := range cases {
		if got := supported(c.id, c.ver); got != c.want {
			t.Errorf("%s %s: got %v, want %v", c.id, c.ver, got, c.want)
		}
	}
}

func TestCheckInfo(t *testing.T) {
	ok := []sysinfo.Info{
		{OSID: "ubuntu", OSVersion: "24.04", PackageManager: "apt"},
		{OSID: "debian", OSVersion: "12", PackageManager: "apt"},
	}
	for _, info := range ok {
		if err := checkInfo(info); err != nil {
			t.Errorf("%s %s refused: %v", info.OSID, info.OSVersion, err)
		}
	}
	refused := []struct {
		info     sysinfo.Info
		contains []string
	}{
		{sysinfo.Info{OSID: "fedora", OSVersion: "40", OSName: "Fedora Linux 40", PackageManager: "dnf"}, []string{"Fedora Linux 40", "dnf", "Debian 11+"}},
		{sysinfo.Info{OSID: "rocky", OSVersion: "9.3", PackageManager: "dnf"}, []string{"rocky 9.3", "dnf"}},
		{sysinfo.Info{OSID: "alpine", OSVersion: "3.19", PackageManager: "apk"}, []string{"alpine", "apk"}},
		{sysinfo.Info{OSID: "ubuntu", OSVersion: "20.04", OSName: "Ubuntu 20.04", PackageManager: "apt"}, []string{"Ubuntu 20.04", "apt"}},
		{sysinfo.Info{}, []string{"?", "-"}},
	}
	for _, c := range refused {
		err := checkInfo(c.info)
		if err == nil {
			t.Errorf("%+v accepted", c.info)
			continue
		}
		for _, want := range c.contains {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%+v: message %q misses %q", c.info, err, want)
			}
		}
	}
}

func TestCheckOSReadFailure(t *testing.T) {
	old := collect
	defer func() { collect = old }()
	collect = func() (sysinfo.Info, error) { return sysinfo.Info{}, os.ErrNotExist }
	if err := CheckOS(); err == nil {
		t.Fatal("want an error when os-release cannot be read")
	}
	collect = func() (sysinfo.Info, error) {
		return sysinfo.Info{OSID: "debian", OSVersion: "12"}, nil
	}
	if err := CheckOS(); err != nil {
		t.Fatalf("debian 12 refused: %v", err)
	}
}
