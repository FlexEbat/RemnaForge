// Package cfgfile owns every write of the configuration files this tool
// generates (.env, docker-compose.yml, nginx.conf, Caddyfile) and of the
// credential files it creates. A manifest records which file the installer
// wrote and what it wrote, so a later run can tell an untouched installer
// file, which it may replace, from a file the operator edited, which it must
// not overwrite without asking. Every change is preceded by a backup and
// made atomically.
package cfgfile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Owner says who is responsible for a file.
type Owner string

const (
	// OwnerInstaller: the installer wrote the file and it is unchanged.
	OwnerInstaller Owner = "installer"
	// OwnerUser: the operator changed the file or chose to keep their version.
	OwnerUser Owner = "user"
	// OwnerThirdParty: another program created the file.
	OwnerThirdParty Owner = "third-party"
)

// Paths and the clock are variables so tests can use a temporary directory.
var (
	// ManifestPath is the manifest file.
	ManifestPath = "/usr/local/remnawave_reverse/manifest.json"
	// BackupRoot holds one directory per change, named by timestamp.
	BackupRoot = "/usr/local/remnawave_reverse/backups"
	// Now is the clock.
	Now = time.Now
)

// Entry is what the manifest knows about one file.
type Entry struct {
	Owner Owner `json:"owner"`
	// SHA256 is the hash of the content the file had when this entry was
	// written. The file is unchanged since then while its hash still matches.
	SHA256 string `json:"sha256"`
	// Proposed is the hash of the content the installer last offered, so a
	// "keep" decision is not asked again for the same offer.
	Proposed string `json:"proposed,omitempty"`
}

// Manifest is the content of the manifest file.
type Manifest struct {
	Version int               `json:"version"`
	Files   map[string]Entry  `json:"files"`
	Values  map[string]string `json:"values,omitempty"`
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func loadManifest() (Manifest, error) {
	m := Manifest{Version: 1, Files: map[string]Entry{}, Values: map[string]string{}}
	b, err := os.ReadFile(ManifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, err
	}
	if m.Files == nil {
		m.Files = map[string]Entry{}
	}
	if m.Values == nil {
		m.Values = map[string]string{}
	}
	return m, nil
}

func saveManifest(m Manifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(ManifestPath), 0700); err != nil {
		return err
	}
	return atomicWrite(ManifestPath, append(b, '\n'), 0600)
}

// Stable returns the value stored under name, generating and storing it on
// first use. Install flows draw their random values (cookie names, secrets)
// through it, so running a flow again produces the same files.
func Stable(name string, generate func() string) (string, error) {
	m, err := loadManifest()
	if err != nil {
		return "", err
	}
	if v, ok := m.Values[name]; ok && v != "" {
		return v, nil
	}
	v := generate()
	m.Values[name] = v
	return v, saveManifest(m)
}

// Forget drops the manifest entries of every file under dir and every stable
// value whose name starts with valuePrefix. A removed installation starts
// fresh the next time.
func Forget(dir, valuePrefix string) error {
	m, err := loadManifest()
	if err != nil {
		return err
	}
	prefix := strings.TrimRight(dir, "/") + "/"
	for p := range m.Files {
		if strings.HasPrefix(p, prefix) {
			delete(m.Files, p)
		}
	}
	for k := range m.Values {
		if valuePrefix != "" && strings.HasPrefix(k, valuePrefix) {
			delete(m.Values, k)
		}
	}
	return saveManifest(m)
}

// Lookup returns the manifest entry of path.
func Lookup(path string) (Entry, bool) {
	m, err := loadManifest()
	if err != nil {
		return Entry{}, false
	}
	e, ok := m.Files[path]
	return e, ok
}

// StableOr is Stable for call sites that cannot do anything about a manifest
// that does not work: it falls back to a fresh value, and the write that
// follows reports the broken manifest.
func StableOr(name string, generate func() string) string {
	v, err := Stable(name, generate)
	if err != nil || v == "" {
		return generate()
	}
	return v
}
