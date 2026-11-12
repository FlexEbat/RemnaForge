package runlock

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSecondAcquireIsBusy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "x.lock")

	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()

	_, err = Acquire(path)
	var busy *BusyError
	if !errors.As(err, &busy) {
		t.Fatalf("want *BusyError, got %v", err)
	}
	if busy.PID != os.Getpid() {
		t.Fatalf("holder pid: want %d, got %d", os.Getpid(), busy.PID)
	}
	if busy.Since.IsZero() {
		t.Fatal("holder start time must be set")
	}
}

func TestReleaseAllowsNextAcquire(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")

	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	first.Release()

	second, err := Acquire(path)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	second.Release()
}

func TestLockFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	h, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Release()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode: want 0600, got %v", info.Mode().Perm())
	}
}
