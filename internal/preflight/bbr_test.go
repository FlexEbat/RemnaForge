package preflight

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteBBRConf(t *testing.T) {
	path := filepath.Join(t.TempDir(), "99-test-bbr.conf")

	if err := writeBBRConf(path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != bbrConf {
		t.Fatalf("content = %q, err = %v; want %q", got, err, bbrConf)
	}

	// A second run must not rewrite an up-to-date file.
	old := os.FileMode(0400)
	if err := os.Chmod(path, old); err != nil {
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
