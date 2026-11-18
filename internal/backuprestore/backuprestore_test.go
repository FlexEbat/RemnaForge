package backuprestore

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyScript(t *testing.T) {
	data := []byte("#!/bin/bash\necho ok\n")
	sum := sha256.Sum256(data)
	good := hex.EncodeToString(sum[:])

	if err := verifyScript(data, good); err != nil {
		t.Errorf("matching digest rejected: %v", err)
	}
	if err := verifyScript(append(data, '#'), good); err == nil {
		t.Error("tampered script accepted")
	}
	if err := verifyScript(data, strings.Repeat("0", 64)); err == nil {
		t.Error("wrong digest accepted")
	}
}

func TestScriptPinFormat(t *testing.T) {
	if len(scriptRef) != 40 {
		t.Errorf("scriptRef %q is not a full commit hash", scriptRef)
	}
	if len(scriptSHA256) != 64 {
		t.Errorf("scriptSHA256 has length %d, want 64", len(scriptSHA256))
	}
	if strings.Contains(scriptURL, "/main/") {
		t.Error("scriptURL must not follow a moving branch")
	}
}

func TestExistingScriptIsRunOnlyWhenPinned(t *testing.T) {
	dir := t.TempDir()
	if present, pinned := existingScript(filepath.Join(dir, "none.sh")); present || pinned {
		t.Error("a missing file was reported as present")
	}
	other := filepath.Join(dir, "backup-restore.sh")
	if err := os.WriteFile(other, []byte("#!/bin/sh\necho not the pinned script\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if present, pinned := existingScript(other); !present || pinned {
		t.Errorf("a foreign script was trusted: present=%v pinned=%v", present, pinned)
	}
}
