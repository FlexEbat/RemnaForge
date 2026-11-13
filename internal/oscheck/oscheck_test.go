package oscheck

import (
	"os"
	"path/filepath"
	"testing"
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

func TestParseOSRelease(t *testing.T) {
	write := func(t *testing.T, content string) string {
		p := filepath.Join(t.TempDir(), "os-release")
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	t.Run("quoted and plain values", func(t *testing.T) {
		p := write(t, "PRETTY_NAME=\"Ubuntu 24.04 LTS\"\nNAME=\"Ubuntu\"\nVERSION_ID=\"24.04\"\nID=ubuntu\nID_LIKE=debian\n")
		id, ver, err := parseOSRelease(p)
		if err != nil || id != "ubuntu" || ver != "24.04" {
			t.Fatalf("got %q %q %v", id, ver, err)
		}
	})
	t.Run("ID_LIKE does not override ID", func(t *testing.T) {
		p := write(t, "ID=debian\nID_LIKE=ubuntu\nVERSION_ID=\"12\"\n")
		if id, _, _ := parseOSRelease(p); id != "debian" {
			t.Fatalf("id = %q", id)
		}
	})
	t.Run("lines without a value are skipped", func(t *testing.T) {
		p := write(t, "# comment\n\nID=debian\nbroken line\nVERSION_ID=\"12\"\n")
		id, ver, err := parseOSRelease(p)
		if err != nil || id != "debian" || ver != "12" {
			t.Fatalf("got %q %q %v", id, ver, err)
		}
	})
	t.Run("missing file", func(t *testing.T) {
		if _, _, err := parseOSRelease(filepath.Join(t.TempDir(), "nope")); err == nil {
			t.Fatal("want an error")
		}
	})
}
