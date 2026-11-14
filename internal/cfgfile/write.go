package cfgfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// Kind says how a file is merged.
type Kind int

const (
	// KindRaw has no merge: the operator keeps or replaces the whole file.
	KindRaw Kind = iota
	// KindEnv is a .env file, merged by key.
	KindEnv
	// KindNginx is an nginx.conf, merged by top-level block.
	KindNginx
	// KindCaddy is a Caddyfile, merged by top-level block.
	KindCaddy
	// KindCompose is a docker-compose.yml, merged by service, network and volume.
	KindCompose
	// KindLines is a file of independent lines (.gitignore), merged by line.
	KindLines
)

// Opts tune a write.
type Opts struct {
	Kind Kind
	// Mode is the permission of a new file; it defaults to 0600. A file that
	// already exists keeps its permission.
	Mode os.FileMode
	// Secret marks a file that holds credentials. Its directory is closed to
	// everyone but the owner, its values are registered for redaction in the
	// log, and the manifest remembers it. A .env file is always secret.
	Secret bool
}

func (o Opts) secret() bool { return o.Secret || o.Kind == KindEnv }

// Outcome says what a write did.
type Outcome int

const (
	// Created: the file did not exist.
	Created Outcome = iota
	// Unchanged: nothing was written.
	Unchanged
	// Replaced: the installer's version replaced the file.
	Replaced
	// Merged: the installer's blocks went into the operator's file.
	Merged
	// Kept: the operator's file stayed as it was.
	Kept
)

// Decision is the operator's answer when a file they changed would be
// overwritten.
type Decision int

const (
	Keep Decision = iota
	MergeIt
	ReplaceIt
)

// Decide asks the operator. diff is the difference between the file and the
// installer's version; canMerge says whether a merge is offered. Tests
// replace it.
var Decide = defaultDecide

// beforeRename runs after the temporary file is complete and before it
// replaces the target. Tests use it to stop the process at that point.
var beforeRename = func() {}

// atomicWrite writes data to a temporary file in the same directory, flushes
// it to disk and renames it over path, so a reader sees either the old file
// or the new one, never half of one.
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	beforeRename()
	if err := os.Rename(name, path); err != nil {
		return err
	}
	ok = true
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// backup copies the current file into BackupRoot/<timestamp>/<path>, mode
// 0600. A missing file has nothing to back up.
func backup(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	stamp := Now().UTC().Format("20060102-150405")
	rel := strings.TrimLeft(filepath.ToSlash(path), "/")
	for n := 0; ; n++ {
		dir := filepath.Join(BackupRoot, stamp)
		if n > 0 {
			dir = filepath.Join(BackupRoot, fmt.Sprintf("%s-%d", stamp, n))
		}
		dest := filepath.Join(dir, rel)
		if _, err := os.Stat(dest); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0600)
	}
}

func modeFor(path string, o Opts) os.FileMode {
	if fi, err := os.Stat(path); err == nil {
		return fi.Mode().Perm()
	}
	if o.Mode != 0 {
		return o.Mode
	}
	return 0600
}

func record(m *Manifest, path string, owner Owner, content, proposed string, secret bool) {
	m.Files[path] = Entry{Owner: owner, SHA256: hash(content), Proposed: hash(proposed), Secret: secret}
}

// closeSecret takes group and other permissions from a secret file that
// already exists, and from its directory.
func closeSecret(path string) {
	if fi, err := os.Stat(path); err == nil && fi.Mode().Perm()&0077 != 0 {
		_ = os.Chmod(path, fi.Mode().Perm()&^0077)
	}
	tightenDir(filepath.Dir(path))
}

// tightenDir closes a directory to everyone but its owner. It only ever
// removes permissions.
func tightenDir(dir string) {
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() && fi.Mode().Perm()&0077 != 0 {
		_ = os.Chmod(dir, fi.Mode().Perm()&^0077)
	}
}

func commit(path, content string, o Opts, m *Manifest, owner Owner, proposed string) error {
	if o.secret() {
		ui.RegisterSecretsFromText(content)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		tightenDir(filepath.Dir(path))
	}
	if Observe != nil {
		if snap, err := takeSnapshot(path, *m); err == nil {
			Observe(snap)
		}
	}
	if err := backup(path); err != nil {
		return err
	}
	mode := modeFor(path, o)
	if o.secret() {
		mode &^= 0077
	}
	if err := atomicWrite(path, []byte(content), mode); err != nil {
		return err
	}
	record(m, path, owner, content, proposed, o.secret())
	return saveManifest(*m)
}

func merged(kind Kind, existing, proposed string) (string, bool) {
	switch kind {
	case KindEnv:
		return MergeEnv(existing, proposed), true
	case KindNginx:
		return MergeNginx(existing, proposed), true
	case KindCaddy:
		return MergeCaddy(existing, proposed), true
	case KindCompose:
		return MergeCompose(existing, proposed), true
	case KindLines:
		return MergeLines(existing, proposed), true
	}
	return "", false
}

// Write puts the installer's version of a file on disk without destroying
// the operator's work:
//
//   - a missing file is created;
//   - an identical file is left alone;
//   - a .env file is merged by key whatever its history: existing values stay,
//     missing keys are added;
//   - a file the installer wrote and nobody changed is replaced;
//   - any other file (changed, or never written by the installer) is shown to
//     the operator as a diff, who keeps it, merges the installer's blocks in
//     or replaces it.
//
// Every change is preceded by a backup and written atomically.
func Write(path, proposed string, o Opts) (Outcome, error) {
	m, err := loadManifest()
	if err != nil {
		return Unchanged, err
	}
	existingBytes, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := commit(path, proposed, o, &m, OwnerInstaller, proposed); err != nil {
			return Unchanged, err
		}
		return Created, nil
	}
	if err != nil {
		return Unchanged, err
	}
	existing := string(existingBytes)
	entry, known := m.Files[path]
	if o.secret() {
		ui.RegisterSecretsFromText(existing)
		closeSecret(path)
	}

	if existing == proposed {
		// The file already is the installer's version, whatever its history.
		if !known || entry.Owner != OwnerInstaller || entry.SHA256 != hash(existing) {
			record(&m, path, OwnerInstaller, existing, proposed, o.secret())
			return Unchanged, saveManifest(m)
		}
		return Unchanged, nil
	}

	if o.Kind == KindEnv || o.Kind == KindLines {
		// Files of independent entries are merged whatever their history:
		// the operator's entries stay and missing ones are added.
		out, _ := merged(o.Kind, existing, proposed)
		if out == existing {
			return Unchanged, nil
		}
		if err := commit(path, out, o, &m, OwnerInstaller, proposed); err != nil {
			return Unchanged, err
		}
		return Merged, nil
	}

	if known && entry.Owner == OwnerInstaller && entry.SHA256 == hash(existing) {
		if err := commit(path, proposed, o, &m, OwnerInstaller, proposed); err != nil {
			return Unchanged, err
		}
		return Replaced, nil
	}

	// The operator's file: a decision made for this very offer stands.
	if known && entry.Owner == OwnerUser && entry.SHA256 == hash(existing) && entry.Proposed == hash(proposed) {
		return Kept, nil
	}
	_, canMerge := merged(o.Kind, "", "")
	switch Decide(path, Diff(existing, proposed), canMerge) {
	case MergeIt:
		if canMerge {
			out, _ := merged(o.Kind, existing, proposed)
			if err := commit(path, out, o, &m, OwnerUser, proposed); err != nil {
				return Unchanged, err
			}
			return Merged, nil
		}
		fallthrough
	case Keep:
		record(&m, path, OwnerUser, existing, proposed, o.secret())
		return Kept, saveManifest(m)
	default:
		if err := commit(path, proposed, o, &m, OwnerInstaller, proposed); err != nil {
			return Unchanged, err
		}
		return Replaced, nil
	}
}

// Replace writes content over the file without asking: a backup first, an
// atomic write, a manifest entry. It is for changes the operator asked for
// themselves (opening a port, migrating) and for credentials the operator
// just typed.
func Replace(path, content string, o Opts) error {
	m, err := loadManifest()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil && o.secret() {
		closeSecret(path)
	}
	if cur, err := os.ReadFile(path); err == nil && string(cur) == content {
		if e, ok := m.Files[path]; !ok || e.SHA256 != hash(content) {
			record(&m, path, OwnerInstaller, content, content, o.secret())
			return saveManifest(m)
		}
		return nil
	}
	return commit(path, content, o, &m, OwnerInstaller, content)
}

// WriteSecret writes a credentials file: directory 0700, file 0600. Tools
// like certbot refuse credentials that other users can read.
func WriteSecret(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return Replace(path, content, Opts{Mode: 0600, Secret: true})
}

// SetComposeEnv sets KEY=value in an environment list entry of a compose
// file ("      - KEY=..."), keeping the indentation and the list marker.
func SetComposeEnv(path, key, value string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	re := regexp.MustCompile(`(?m)^(\s*-\s*` + regexp.QuoteMeta(key) + `=).*$`)
	if !re.Match(data) {
		return fmt.Errorf("%s not found in %s", key, path)
	}
	out := re.ReplaceAllString(string(data), "${1}"+strings.ReplaceAll(value, "$", "$$"))
	return Replace(path, out, Opts{})
}

// Observe, when set, receives a Snapshot of a file just before cfgfile
// changes or creates it. The install transaction uses it to put every file
// back if a run fails.
var Observe func(Snapshot)

// Snapshot is a file as it was before a change: its bytes, permission and
// manifest entry, or the fact that it did not exist.
type Snapshot struct {
	Path     string
	Existed  bool
	Data     []byte
	Mode     os.FileMode
	Entry    Entry
	HadEntry bool
}

func takeSnapshot(path string, m Manifest) (Snapshot, error) {
	s := Snapshot{Path: path}
	s.Entry, s.HadEntry = m.Files[path]
	fi, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	s.Existed, s.Data, s.Mode = true, data, fi.Mode().Perm()
	return s, nil
}

// Restore puts the file and its manifest entry back as the snapshot has
// them, byte for byte. A file that did not exist is removed.
func (s Snapshot) Restore() error {
	m, err := loadManifest()
	if err != nil {
		return err
	}
	if s.Existed {
		if err := atomicWrite(s.Path, s.Data, s.Mode); err != nil {
			return err
		}
	} else if err := os.Remove(s.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if s.HadEntry {
		m.Files[s.Path] = s.Entry
	} else {
		delete(m.Files, s.Path)
	}
	return saveManifest(m)
}

// gitignoreLines keeps secrets out of version control when an operator puts
// a stack directory under git.
const gitignoreText = "# Written by RemnaForge: keep secrets out of version control.\n.env\n*.env\n.secrets/\n*.ini\n*.key\n*.pem\n"

// EnsureGitignore writes a .gitignore into a stack directory that excludes
// the .env file and secret files. An existing .gitignore keeps its lines.
func EnsureGitignore(dir string) error {
	_, err := Write(filepath.Join(dir, ".gitignore"), gitignoreText, Opts{Kind: KindLines, Mode: 0644})
	return err
}
