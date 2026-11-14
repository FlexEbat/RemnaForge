// Package journal collects notes about changes an install run made that
// cannot be undone: packages installed, certificates issued, cron entries
// added. It has no dependencies so any package can add a note. The install
// transaction (internal/txn) prints the notes when a run fails.
package journal

import "sync"

// Note is one irreversible change.
type Note struct {
	Kind string // "packages", "docker", "certificate", "cron"
	Text string
}

var (
	mu    sync.Mutex
	notes []Note
)

// Add records a note.
func Add(kind, text string) {
	mu.Lock()
	defer mu.Unlock()
	notes = append(notes, Note{Kind: kind, Text: text})
}

// Take returns the notes recorded so far and clears them.
func Take() []Note {
	mu.Lock()
	defer mu.Unlock()
	out := notes
	notes = nil
	return out
}

// Reset drops the notes.
func Reset() { _ = Take() }
