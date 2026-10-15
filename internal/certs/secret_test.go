package certs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteSecretFileModes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets", "certbot", "cloudflare.ini")
	if err := writeSecretFile(path, "dns_cloudflare_api_token = x\n"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("file mode = %o, want 0600", fi.Mode().Perm())
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0700 {
		t.Errorf("dir mode = %o, want 0700", di.Mode().Perm())
	}
}

func TestWriteSecretFileReportsError(t *testing.T) {
	// A regular file where the directory should be makes MkdirAll fail.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeSecretFile(filepath.Join(blocker, "x.ini"), "x"); err == nil {
		t.Error("expected an error when the directory cannot be created")
	}
}
