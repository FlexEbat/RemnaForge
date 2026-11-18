package ipv6

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FlexEbat/RemnaForge/internal/cfgfile/cfgfiletest"
)

func TestMain(m *testing.M) { cfgfiletest.Main(m) }

var keys = []string{"net.ipv6.conf.all.disable_ipv6", "net.ipv6.conf.eth0.disable_ipv6"}

func TestRewriteSysctlReplacesOnlyTheKeysAndKeepsTheRest(t *testing.T) {
	in := "# my settings\nvm.swappiness = 10\nnet.ipv6.conf.all.disable_ipv6 = 1\nnet.core.somaxconn = 1024\n"
	got := rewriteSysctl(in, keys, 0)
	want := "# my settings\nvm.swappiness = 10\nnet.core.somaxconn = 1024\nnet.ipv6.conf.all.disable_ipv6 = 0\nnet.ipv6.conf.eth0.disable_ipv6 = 0\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRewriteSysctlOfAnEmptyFile(t *testing.T) {
	got := rewriteSysctl("", keys, 1)
	if got != "net.ipv6.conf.all.disable_ipv6 = 1\nnet.ipv6.conf.eth0.disable_ipv6 = 1\n" {
		t.Errorf("got %q", got)
	}
}

func TestRewriteSysctlIsIdempotent(t *testing.T) {
	once := rewriteSysctl("vm.swappiness = 10\n", keys, 1)
	if twice := rewriteSysctl(once, keys, 1); twice != once {
		t.Errorf("a second run changed the file:\n%s\n--\n%s", once, twice)
	}
}

// The file is replaced atomically and its old content is backed up, so a
// crash cannot leave a truncated /etc/sysctl.conf.
func TestSetSysctlIPv6LinesWritesThroughCfgfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sysctl.conf")
	if err := os.WriteFile(path, []byte("vm.swappiness = 10\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := sysctlConf
	sysctlConf = path
	defer func() { sysctlConf = old }()
	if err := setSysctlIPv6Lines("eth0", 1); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "vm.swappiness = 10\n") || !strings.Contains(string(b), "net.ipv6.conf.eth0.disable_ipv6 = 1\n") {
		t.Errorf("got %q", b)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", info.Mode().Perm())
	}
}
