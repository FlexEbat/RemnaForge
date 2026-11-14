// Package txn makes an install run a transaction. While a flow runs, every
// file cfgfile changes is snapshotted and the containers and networks of
// this tool are listed before the run. When the flow fails, the run is
// reverted in reverse order: containers and networks created by it are
// removed, then files are put back byte for byte. Volumes and databases are
// never touched, and packages, certificates and other system changes are
// reported, not reverted.
package txn

import (
	"fmt"
	"sort"

	"github.com/FlexEbat/RemnaForge/internal/cfgfile"
	"github.com/FlexEbat/RemnaForge/internal/dockercheck"
	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/journal"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// Docker is what the transaction needs from Docker. It has no way to remove
// a volume, on purpose.
type Docker interface {
	// Objects lists the containers and networks of this tool's compose
	// projects. A host without docker has none; a docker that does not
	// answer is an error.
	Objects() (containers, networks []string, err error)
	RemoveContainer(name string) error
	RemoveNetwork(name string) error
}

type realDocker struct{ env dockercheck.Env }

func (d realDocker) Objects() ([]string, []string, error) {
	if _, err := d.env.LookPath("docker"); err != nil {
		return nil, nil, nil
	}
	st := dockercheck.Inspect(d.env)
	if !st.Daemon {
		return nil, nil, fmt.Errorf("the docker daemon does not answer")
	}
	return names(st.Containers), names(st.Networks), nil
}

func (d realDocker) RemoveContainer(name string) error {
	_, err := d.env.Run("docker", "rm", "-f", name)
	return err
}

func (d realDocker) RemoveNetwork(name string) error {
	_, err := d.env.Run("docker", "network", "rm", name)
	return err
}

func names(objs []dockercheck.Object) []string {
	var out []string
	for _, o := range objs {
		out = append(out, o.Name)
	}
	sort.Strings(out)
	return out
}

// DefaultDocker talks to the real docker.
func DefaultDocker() Docker {
	return realDocker{env: dockercheck.DefaultEnv()}
}

// Report says what a rollback did.
type Report struct {
	RestoredFiles     []string
	RemovedFiles      []string // files the run created
	RemovedContainers []string
	RemovedNetworks   []string
	// LostContainers existed before the run and are gone now; the run (or a
	// teardown inside it) removed them and they are not restored.
	LostContainers []string
	Skipped        string // why the docker part was skipped, if it was
	Failures       []error
	NotReverted    []journal.Note
}

// Tx is a running transaction.
type Tx struct {
	name        string
	docker      Docker
	snaps       []cfgfile.Snapshot
	seen        map[string]bool
	beforeC     []string
	beforeN     []string
	beforeKnown bool
	beforeErr   error
}

func begin(name string, d Docker) *Tx {
	t := &Tx{name: name, docker: d, seen: map[string]bool{}}
	t.beforeC, t.beforeN, t.beforeErr = d.Objects()
	t.beforeKnown = t.beforeErr == nil
	journal.Reset()
	cfgfile.Observe = t.observe
	return t
}

func (t *Tx) observe(s cfgfile.Snapshot) {
	if t.seen[s.Path] {
		return // the first snapshot is the state before the run
	}
	t.seen[s.Path] = true
	t.snaps = append(t.snaps, s)
}

func (t *Tx) end() { cfgfile.Observe = nil }

func diff(after, before []string) []string {
	had := map[string]bool{}
	for _, b := range before {
		had[b] = true
	}
	var out []string
	for _, a := range after {
		if !had[a] {
			out = append(out, a)
		}
	}
	return out
}

// Rollback reverts the run in reverse order. It does not stop at a failure:
// every step is tried and failures are reported.
func (t *Tx) Rollback() Report {
	var r Report
	fail := func(err error) { r.Failures = append(r.Failures, err) }

	// 1. containers, then networks, created by this run
	if !t.beforeKnown {
		r.Skipped = fmt.Sprint(t.beforeErr)
	} else if c, n, err := t.docker.Objects(); err != nil {
		r.Skipped = err.Error()
	} else {
		for _, name := range diff(c, t.beforeC) {
			if err := t.docker.RemoveContainer(name); err != nil {
				fail(fmt.Errorf("container %s: %w", name, err))
			} else {
				r.RemovedContainers = append(r.RemovedContainers, name)
			}
		}
		for _, name := range diff(n, t.beforeN) {
			if err := t.docker.RemoveNetwork(name); err != nil {
				fail(fmt.Errorf("network %s: %w", name, err))
			} else {
				r.RemovedNetworks = append(r.RemovedNetworks, name)
			}
		}
		r.LostContainers = diff(t.beforeC, c)
	}

	// 2. files, last change first
	for i := len(t.snaps) - 1; i >= 0; i-- {
		s := t.snaps[i]
		if err := s.Restore(); err != nil {
			fail(fmt.Errorf("file %s: %w", s.Path, err))
			continue
		}
		if s.Existed {
			r.RestoredFiles = append(r.RestoredFiles, s.Path)
		} else {
			r.RemovedFiles = append(r.RemovedFiles, s.Path)
		}
	}

	// 3. what cannot be undone is only reported
	r.NotReverted = journal.Take()
	return r
}

// Run runs fn as a transaction. When fn fails or panics, the run is
// reverted and the report is printed; the error of fn is returned.
func Run(name string, fn func() error) error {
	return RunWith(DefaultDocker(), name, fn)
}

// RunWith is Run over a given Docker.
func RunWith(d Docker, name string, fn func() error) (err error) {
	t := begin(name, d)
	defer t.end()
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("%v", p)
		}
		if err != nil {
			Print(t.Rollback())
		} else {
			journal.Reset()
		}
	}()
	return fn()
}

// line prints one report line; tests silence it.
var line = func(color, msg string) { fmt.Printf("%s%s%s\n", color, msg, ui.ColorReset) }

var tr = func(key string, args ...any) string {
	if len(args) == 0 {
		return i18n.T(key)
	}
	return fmt.Sprintf(i18n.T(key), args...)
}

// Print writes a report for the operator.
func Print(r Report) {
	line(ui.ColorYellow, tr("TXN_FAILED"))
	for _, f := range r.RestoredFiles {
		line(ui.ColorGray, tr("TXN_RESTORED", f))
	}
	for _, f := range r.RemovedFiles {
		line(ui.ColorGray, tr("TXN_REMOVED_FILE", f))
	}
	for _, c := range r.RemovedContainers {
		line(ui.ColorGray, tr("TXN_REMOVED_CONTAINER", c))
	}
	for _, n := range r.RemovedNetworks {
		line(ui.ColorGray, tr("TXN_REMOVED_NETWORK", n))
	}
	if r.Skipped != "" {
		line(ui.ColorYellow, tr("TXN_DOCKER_SKIPPED", r.Skipped))
	}
	for _, c := range r.LostContainers {
		line(ui.ColorYellow, tr("TXN_LOST_CONTAINER", c))
	}
	line(ui.ColorGreen, tr("TXN_VOLUMES_KEPT"))
	for _, n := range r.NotReverted {
		line(ui.ColorYellow, tr("TXN_NOT_REVERTED", n.Text))
	}
	for _, err := range r.Failures {
		line(ui.ColorRed, tr("TXN_ROLLBACK_FAILED", err))
	}
}
