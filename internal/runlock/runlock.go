// Package runlock keeps two RemnaForge instances from changing the same
// server at once, for example from two SSH sessions.
//
// The lock is an exclusive flock on a file. The kernel drops a flock when
// the holding process dies, so a crash never leaves a stale lock behind
// and no PID liveness check is needed. The file descriptor is opened with
// O_CLOEXEC (Go's default), so child processes such as docker or apt do
// not inherit it and cannot keep the lock alive after the parent exits.
package runlock

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// DefaultPath is the lock file of the tool, next to its other state.
const DefaultPath = "/usr/local/remnawave_reverse/remnaforge.lock"

// BusyError reports that another instance holds the lock. PID and Since
// come from the note the holder wrote into the file, and are zero when
// that note could not be read.
type BusyError struct {
	PID   int
	Since time.Time
}

func (e *BusyError) Error() string {
	return fmt.Sprintf("another instance holds the run lock (pid %d, since %s)", e.PID, e.Since.Format(time.RFC3339))
}

// Held is an acquired lock.
type Held struct{ f *os.File }

// Acquire takes the lock at path without waiting. It returns a
// *BusyError when another instance holds it.
func Acquire(path string) (*Held, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		defer f.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, readHolder(f)
		}
		return nil, err
	}
	if err := f.Truncate(0); err == nil {
		_, _ = f.WriteAt([]byte(fmt.Sprintf("%d %d\n", os.Getpid(), time.Now().Unix())), 0)
	}
	return &Held{f: f}, nil
}

// readHolder parses the "<pid> <unix time>" note of the current holder.
func readHolder(f *os.File) *BusyError {
	buf := make([]byte, 64)
	n, _ := f.ReadAt(buf, 0)
	fields := strings.Fields(string(buf[:n]))
	busy := &BusyError{}
	if len(fields) == 2 {
		if pid, err := strconv.Atoi(fields[0]); err == nil {
			busy.PID = pid
		}
		if ts, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
			busy.Since = time.Unix(ts, 0)
		}
	}
	return busy
}

// Release drops the lock. The note stays in the file and is overwritten
// by the next holder.
func (h *Held) Release() {
	if h == nil || h.f == nil {
		return
	}
	_ = syscall.Flock(int(h.f.Fd()), syscall.LOCK_UN)
	_ = h.f.Close()
	h.f = nil
}
