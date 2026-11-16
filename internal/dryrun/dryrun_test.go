package dryrun

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/FlexEbat/RemnaForge/internal/cfgfile"
	"github.com/FlexEbat/RemnaForge/internal/execx"
	"github.com/FlexEbat/RemnaForge/internal/fsx"
	"github.com/FlexEbat/RemnaForge/internal/runctx"
	"github.com/FlexEbat/RemnaForge/internal/selfsteal"
	"github.com/FlexEbat/RemnaForge/internal/validate"
)

// tree describes every file under root: path, mode and content hash.
func tree(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, _ := d.Info()
		h := ""
		if d.Type().IsRegular() {
			b, _ := os.ReadFile(p)
			h = fmt.Sprintf("%x", sha256.Sum256(b))
		}
		lines = append(lines, fmt.Sprintf("%s %v %s", p, info.Mode(), h))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// capture runs fn and returns what it printed to standard output.
func capture(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	done := make(chan string)
	go func() {
		var b bytes.Buffer
		_, _ = io.Copy(&b, r)
		done <- b.String()
	}()
	fn()
	w.Close()
	os.Stdout = old
	return <-done
}

func setup(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cfgfile.ManifestPath = filepath.Join(root, "state", "manifest.json")
	cfgfile.BackupRoot = filepath.Join(root, "state", "backups")
	t.Cleanup(func() { runctx.SetDryRun(false) })
	return root
}

func TestDryRunChangesNothing(t *testing.T) {
	root := setup(t)
	existing := filepath.Join(root, "stack", "nginx.conf")
	secret := filepath.Join(root, "stack", ".env")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Dir(existing), 0o755))
	must(os.WriteFile(existing, []byte("server {\n  listen 80;\n}\n"), 0o644))
	must(os.WriteFile(secret, []byte("A=1\n"), 0o600))
	must(os.WriteFile(filepath.Join(root, "victim"), []byte("keep"), 0o644))
	before := tree(t, root)

	runctx.SetDryRun(true)
	marker := filepath.Join(root, "marker")
	var catOut []byte
	var catErr error
	out := capture(t, func() {
		// cfgfile: a new file, a changed file, a secret file, the helpers.
		_, err := cfgfile.Write(filepath.Join(root, "stack", "new.conf"), "a\nb\n", cfgfile.Opts{})
		must(err)
		must(cfgfile.Replace(existing, "server {\n  listen 443;\n}\n", cfgfile.Opts{}))
		must(cfgfile.Replace(secret, "TOKEN=hunter2-very-secret\n", cfgfile.Opts{Kind: cfgfile.KindEnv}))
		must(cfgfile.WriteSecret(filepath.Join(root, "stack", ".secrets", "cf.ini"), "key=hunter2-very-secret\n"))
		must(cfgfile.EnsureGitignore(filepath.Join(root, "stack")))
		// fsx
		must(fsx.WriteFile(filepath.Join(root, "w"), []byte("x"), 0o644))
		must(fsx.MkdirAll(filepath.Join(root, "d", "e"), 0o755))
		must(fsx.Remove(filepath.Join(root, "victim")))
		must(fsx.RemoveAll(filepath.Join(root, "stack")))
		must(fsx.Rename(filepath.Join(root, "victim"), filepath.Join(root, "moved")))
		must(fsx.Chmod(filepath.Join(root, "victim"), 0o000))
		must(fsx.Symlink("victim", filepath.Join(root, "link")))
		// execx: mutating commands are announced, a read-only one runs.
		must(execx.Command("sh", "-c", "touch "+marker).Run())
		must(execx.Command("touch", marker).Run())
		skipped, err := execx.Command("sh", "-c", "echo changed; touch "+marker).Output()
		must(err)
		if len(skipped) != 0 {
			t.Errorf("a skipped command returned output %q", skipped)
		}
		catOut, catErr = execx.Command("cat", filepath.Join(root, "victim")).Output()
		// packages with their own guards
		must(validate.CheckStack(filepath.Join(root, "nowhere")))
		must(selfsteal.RandomHTML(""))
	})

	if after := tree(t, root); after != before {
		t.Errorf("the file system changed in a dry run:\n--- before\n%s\n--- after\n%s", before, after)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a mutating command ran")
	}
	if catErr != nil || string(catOut) != "keep" {
		t.Errorf("a read-only command must still run: %q %v", catOut, catErr)
	}
	if _, err := os.Stat(cfgfile.ManifestPath); err == nil {
		t.Error("the manifest was written")
	}
	if strings.Contains(out, "hunter2-very-secret") {
		t.Errorf("a secret reached the plan:\n%s", out)
	}
	for _, want := range []string{"[dry-run]", "create " + filepath.Join(root, "stack", "new.conf"), "listen 443", "content not shown", "run sh -c touch"} {
		if !strings.Contains(out, want) {
			t.Errorf("the plan lacks %q:\n%s", want, out)
		}
	}
}

// The same calls outside a dry run do change things, so the test above
// tests the switch and not an inert setup.
func TestSameCallsWriteOutsideDryRun(t *testing.T) {
	root := setup(t)
	runctx.SetDryRun(false)
	marker := filepath.Join(root, "marker")
	if err := execx.Command("touch", marker).Run(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Error("the command did not run")
	}
	if _, err := cfgfile.Write(filepath.Join(root, "x.conf"), "a\n", cfgfile.Opts{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "x.conf")); err != nil {
		t.Error("the file was not written")
	}
	if _, err := os.Stat(cfgfile.ManifestPath); err != nil {
		t.Error("the manifest was not written")
	}
}

func TestReadOnlyCommands(t *testing.T) {
	yes := [][]string{
		{"docker", "info"}, {"docker", "ps", "-a"}, {"docker", "compose", "ps", "-a", "--format", "json"},
		{"docker", "compose", "config", "--services"}, {"docker", "run", "--rm", "nginx", "nginx", "-t"},
		{"systemctl", "is-active", "docker"}, {"ufw", "status"}, {"dpkg", "-s", "curl"},
		{"apt-get", "-s", "install", "curl"}, {"certbot", "certificates"}, {"certbot", "certonly", "--dry-run"},
		{"timedatectl", "show"}, {"ip", "-4", "addr", "show"}, {"cat", "/etc/os-release"},
		{"/usr/sbin/sshd", "-T"}, {"curl", "-sS", "https://example.com"}, {"sysctl", "net.ipv4.ip_forward"},
	}
	no := [][]string{
		{"docker", "compose", "up", "-d"}, {"docker", "compose", "down"}, {"docker", "compose", "exec", "-T", "remnawave-db", "psql"},
		{"docker", "pull", "nginx"}, {"docker", "rm", "-f", "x"}, {"systemctl", "restart", "docker"},
		{"systemctl", "enable", "--now", "docker"}, {"ufw", "allow", "443"}, {"ufw", "reload"},
		{"apt-get", "install", "-y", "curl"}, {"apt-get", "update"}, {"certbot", "certonly", "--standalone"},
		{"timedatectl", "set-ntp", "true"}, {"ip", "addr", "add", "10.0.0.1/24", "dev", "eth0"},
		{"sh", "-c", "echo hi"}, {"rm", "-rf", "/tmp/x"}, {"curl", "-o", "/tmp/x", "https://example.com"},
		{"curl", "-X", "POST", "https://example.com"}, {"sysctl", "-w", "net.ipv4.ip_forward=1"},
		{"sysctl", "net.ipv4.ip_forward=1"}, {"iptables", "-A", "INPUT"}, {"unknown-tool"},
	}
	for _, c := range yes {
		if !execx.ReadOnly(c[0], c[1:]...) {
			t.Errorf("%v must be read-only", c)
		}
	}
	for _, c := range no {
		if execx.ReadOnly(c[0], c[1:]...) {
			t.Errorf("%v must count as changing the system", c)
		}
	}
}

// Every file system change outside the guarded packages goes through fsx.
// A new direct os call would escape the dry run.
func TestNoUnguardedFileSystemChanges(t *testing.T) {
	re := regexp.MustCompile(`\bos\.(WriteFile|MkdirAll|Mkdir|RemoveAll|Remove|Rename|Chmod|Chown|Symlink|Create|OpenFile|CreateTemp|MkdirTemp|Truncate)\(`)
	// Packages that handle the dry run themselves, with the reason.
	allowed := map[string]string{
		"fsx":           "is the wrapper",
		"cfgfile":       "announces in commit and guards atomicWrite and saveManifest",
		"runlock":       "main takes no lock in a dry run",
		"ui":            "main keeps no log in a dry run",
		"validate":      "stack returns before it creates its temporary file",
		"verify":        "Save returns before it writes",
		"stack":         "backupPanelTo returns before it creates the dump",
		"selfsteal":     "RandomHTML and InteractiveInstall return before the copy",
		"firewall":      "AppendConsent returns before it opens the log",
		"certs":         "the log file and the compose append are guarded",
		"backuprestore": "Run returns before the download",
		"dryrun":        "this test",
	}
	root := filepath.Join("..")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		pkg := strings.Split(rel, string(filepath.Separator))[0]
		if _, ok := allowed[pkg]; ok {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if loc := re.FindIndex(b); loc != nil {
			t.Errorf("%s calls %s directly, use fsx or add a dry-run guard", rel, b[loc[0]:loc[1]])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A flow must stop before it starts the stack in a dry run: what follows
// needs the running services.
func TestFlowsStopBeforeStartingTheStack(t *testing.T) {
	for _, f := range []string{
		"panelonly/install_panel.go", "panelfull/install_panel.go", "caddypanelonly/install_panel.go",
		"caddypanelfull/install_panel.go", "nginxnode/install_node.go", "caddynode/install_node.go",
	} {
		b, err := os.ReadFile(filepath.Join("..", f))
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		guard := strings.Index(src, "if runctx.DryRun() {")
		start := strings.Index(src, `"compose", "up", "-d"`)
		if guard < 0 || start < 0 || guard > start {
			t.Errorf("%s: the dry-run stop must come before docker compose up (guard %d, up %d)", f, guard, start)
		}
	}
}
