package certs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func withBundleDir(t *testing.T) string {
	t.Helper()
	oldDir, oldHook := HAProxyBundleDir, HAProxyHookPath
	root := t.TempDir()
	HAProxyBundleDir = filepath.Join(root, "haproxy")
	HAProxyHookPath = filepath.Join(root, "hook.sh")
	t.Cleanup(func() { HAProxyBundleDir, HAProxyHookPath = oldDir, oldHook })
	return root
}

func writeLive(t *testing.T, live, name, chain, key string) {
	t.Helper()
	dir := filepath.Join(live, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fullchain.pem"), []byte(chain), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "privkey.pem"), []byte(key), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestWriteHAProxyBundle(t *testing.T) {
	root := withBundleDir(t)
	live := filepath.Join(root, "live")
	writeLive(t, live, "example.org", "CHAIN-NO-NEWLINE", "KEY\n")
	if err := WriteHAProxyBundle(live, "example.org"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(HAProxyBundlePath("example.org"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "CHAIN-NO-NEWLINE\nKEY\n" {
		t.Errorf("bundle %q: the chain comes first, the key second, on separate lines", got)
	}
	st, _ := os.Stat(HAProxyBundlePath("example.org"))
	dst, _ := os.Stat(HAProxyBundleDir)
	if st.Mode().Perm() != 0o600 || dst.Mode().Perm() != 0o700 {
		t.Errorf("modes: file %v dir %v", st.Mode().Perm(), dst.Mode().Perm())
	}
	if _, err := os.Stat(HAProxyBundlePath("example.org") + ".tmp"); err == nil {
		t.Error("the temporary file was left behind")
	}
	if err := WriteHAProxyBundle(live, "missing.org"); err == nil {
		t.Error("a name without certificate files must fail")
	}
}

func TestRenewHookFollowsTheBundle(t *testing.T) {
	root := withBundleDir(t)
	if got := renewHookFor("example.org"); got != renewHookCommand {
		t.Errorf("without a bundle the nginx hook stays: %q", got)
	}
	live := filepath.Join(root, "live")
	writeLive(t, live, "example.org", "C\n", "K\n")
	if err := WriteHAProxyBundle(live, "example.org"); err != nil {
		t.Fatal(err)
	}
	if got := renewHookFor("example.org"); got != "renew_hook = "+HAProxyHookPath {
		t.Errorf("with a bundle the HAProxy hook is used: %q", got)
	}
	if renewHookFor("other.org") != renewHookCommand {
		t.Error("another name without a bundle keeps the nginx hook")
	}
}

func TestValidateBundleName(t *testing.T) {
	for _, bad := range []string{"", ".", "..", "../x", "a/b"} {
		if validateBundleName(bad) == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
	if validateBundleName("example.org") != nil || validateBundleName("*.example.org") != nil {
		t.Error("ordinary names must pass")
	}
}

// The hook script runs for real: it rebuilds an existing bundle from the
// renewed files, leaves a name without a bundle alone, and signals HAProxy
// in every stack directory that has a compose file.
func TestHAProxyHookScript(t *testing.T) {
	root := withBundleDir(t)
	live, stack, bin := filepath.Join(root, "live"), filepath.Join(root, "stack"), filepath.Join(root, "bin")
	for _, d := range []string{stack, bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(stack, "docker-compose.yml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(root, "docker.log")
	fake := "#!/bin/sh\necho \"$PWD: $*\" >> " + record + "\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	writeLive(t, live, "example.org", "OLD-CHAIN\n", "OLD-KEY\n")
	writeLive(t, live, "unbundled.org", "U-CHAIN\n", "U-KEY\n")
	if err := WriteHAProxyBundle(live, "example.org"); err != nil {
		t.Fatal(err)
	}
	if err := InstallHAProxyHook(); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(HAProxyHookPath); st.Mode().Perm() != 0o700 {
		t.Errorf("the hook must be executable by root only, got %v", st.Mode().Perm())
	}

	writeLive(t, live, "example.org", "NEW-CHAIN\n", "NEW-KEY\n") // certbot renewed it
	cmd := exec.Command("sh", HAProxyHookPath)
	cmd.Env = append(os.Environ(), "LIVE_DIR="+live, "BUNDLE_DIR="+HAProxyBundleDir,
		"STACK_DIRS="+stack+" "+filepath.Join(root, "absent"), "PATH="+bin+":"+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hook failed: %v\n%s", err, out)
	}
	got, _ := os.ReadFile(HAProxyBundlePath("example.org"))
	if string(got) != "NEW-CHAIN\nNEW-KEY\n" {
		t.Errorf("bundle after renewal: %q", got)
	}
	if _, err := os.Stat(HAProxyBundlePath("unbundled.org")); err == nil {
		t.Error("a name without a bundle must not get one")
	}
	log, _ := os.ReadFile(record)
	if !strings.Contains(string(log), stack+": compose kill -s USR2 remnawave-haproxy") || strings.Count(string(log), "\n") != 1 {
		t.Errorf("HAProxy must be signalled once, in the stack that exists:\n%s", log)
	}
}
