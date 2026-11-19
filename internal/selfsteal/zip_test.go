package selfsteal

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func buildZip(t *testing.T, files map[string]string) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	// Newer Go versions return ErrInsecurePath together with a usable
	// reader when GODEBUG=zipinsecurepath=0; extractZip must still refuse.
	if err != nil && zr == nil {
		t.Fatal(err)
	}
	return zr
}

func TestExtractZipRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "site")
	if err := os.MkdirAll(dest, 0755); err != nil {
		t.Fatal(err)
	}

	err := extractZip(buildZip(t, map[string]string{"../evil.txt": "x"}), dest)
	if err == nil {
		t.Fatal("expected an error for an entry outside the destination")
	}
	if _, statErr := os.Stat(filepath.Join(root, "evil.txt")); !os.IsNotExist(statErr) {
		t.Errorf("file was written outside the destination (stat err: %v)", statErr)
	}
}

func TestExtractZipWritesNestedFiles(t *testing.T) {
	dest := t.TempDir()
	err := extractZip(buildZip(t, map[string]string{
		"index.html":       "<html></html>",
		"assets/css/a.css": "body{}",
	}), dest)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"index.html": "<html></html>", "assets/css/a.css": "body{}"} {
		got, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, err = %v; want %q", name, got, err, want)
		}
	}
}

func TestExtractZipRefusesTooManyEntries(t *testing.T) {
	old := maxZipEntries
	maxZipEntries = 2
	defer func() { maxZipEntries = old }()
	zr := buildZip(t, map[string]string{"a": "1", "b": "2", "c": "3"})
	if err := extractZip(zr, t.TempDir()); err == nil {
		t.Error("an archive over the entry limit was unpacked")
	}
}

func TestExtractZipRefusesAnArchiveThatUnpacksTooLarge(t *testing.T) {
	old := maxExtractedSize
	maxExtractedSize = 10
	defer func() { maxExtractedSize = old }()
	zr := buildZip(t, map[string]string{"big.txt": "0123456789abcdefghij"})
	dest := t.TempDir()
	if err := extractZip(zr, dest); err == nil {
		t.Error("an archive over the size limit was unpacked")
	}
}
