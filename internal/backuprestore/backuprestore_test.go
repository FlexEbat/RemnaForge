package backuprestore

import (
	"crypto/sha256"
	"encoding/hex"
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
