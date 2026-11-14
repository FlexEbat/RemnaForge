package txn

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/FlexEbat/RemnaForge/internal/cfgfile"
	"github.com/FlexEbat/RemnaForge/internal/journal"
)

// fakeDocker keeps the objects of this tool by name. foreign belongs to other
// stacks and is never listed, so a rollback cannot touch it. volumes exist
// only to prove nothing removes them: the Docker interface cannot.
type fakeDocker struct {
	containers map[string]bool
	networks   map[string]bool
	volumes    map[string]bool
	foreign    map[string]bool
	listErr    error
	failRemove map[string]bool
	removed    []string
}

func newDocker() *fakeDocker {
	return &fakeDocker{containers: map[string]bool{}, networks: map[string]bool{}, volumes: map[string]bool{},
		foreign: map[string]bool{}, failRemove: map[string]bool{}}
}

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (d *fakeDocker) Objects() ([]string, []string, error) {
	if d.listErr != nil {
		return nil, nil, d.listErr
	}
	return keys(d.containers), keys(d.networks), nil
}

func (d *fakeDocker) RemoveContainer(n string) error {
	d.removed = append(d.removed, "container "+n)
	if d.failRemove[n] {
		return errors.New("busy")
	}
	delete(d.containers, n)
	return nil
}

func (d *fakeDocker) RemoveNetwork(n string) error {
	d.removed = append(d.removed, "network "+n)
	if d.failRemove[n] {
		return errors.New("in use")
	}
	delete(d.networks, n)
	return nil
}

// world is everything a flow touches in these tests.
type world struct {
	dir    string
	env    string
	comp   string
	nginx  string
	secret string
	d      *fakeDocker
}

type fileState struct {
	exists bool
	data   string
	mode   os.FileMode
	entry  cfgfile.Entry
	has    bool
}

func newWorld(t *testing.T) *world {
	t.Helper()
	dir := t.TempDir()
	w := &world{dir: dir, env: filepath.Join(dir, "opt", ".env"), comp: filepath.Join(dir, "opt", "docker-compose.yml"),
		nginx: filepath.Join(dir, "opt", "nginx.conf"), secret: filepath.Join(dir, "certs", ".secrets", "cloudflare.ini"), d: newDocker()}
	// The operator's own nginx.conf exists before the run, with its own mode.
	if err := os.MkdirAll(filepath.Dir(w.nginx), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(w.nginx, []byte("# my tuned config\nserver { listen 8080; }\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if _, err := cfgfile.Write(w.nginx, "# my tuned config\nserver { listen 8080; }\n", cfgfile.Opts{Kind: cfgfile.KindNginx}); err != nil {
		t.Fatal(err)
	}
	// Objects that exist before the run.
	w.d.containers["remnawave-db"] = true
	w.d.networks["remnawave-old-network"] = true
	w.d.volumes["remnawave-db-data-old"] = true
	w.d.foreign["other-app"] = true
	journal.Reset()
	return w
}

func (w *world) paths() []string { return []string{w.env, w.comp, w.nginx, w.secret} }

func (w *world) files() map[string]fileState {
	out := map[string]fileState{}
	for _, p := range w.paths() {
		st := fileState{}
		if b, err := os.ReadFile(p); err == nil {
			st.exists, st.data = true, string(b)
			fi, _ := os.Stat(p)
			st.mode = fi.Mode().Perm()
		}
		st.entry, st.has = cfgfile.Lookup(p)
		out[p] = st
	}
	return out
}

func (w *world) docker() map[string][]string {
	return map[string][]string{
		"containers": keys(w.d.containers), "networks": keys(w.d.networks),
		"volumes": keys(w.d.volumes), "foreign": keys(w.d.foreign),
	}
}

var errInjected = errors.New("injected failure")

const steps = 7

// flow is an install run of seven steps. It fails right after step failAfter
// (0 fails before the first); steps+1 lets it succeed.
func (w *world) flow(failAfter int) func() error {
	return func() error {
		step := 0
		next := func(do func() error) error {
			if step >= failAfter {
				return errInjected
			}
			if err := do(); err != nil {
				return err
			}
			step++
			return nil
		}
		fns := []func() error{
			func() error {
				_, err := cfgfile.Write(w.env, "APP_PORT=3000\nJWT=abc\n", cfgfile.Opts{Kind: cfgfile.KindEnv})
				return err
			},
			func() error {
				_, err := cfgfile.Write(w.comp, "services:\n  remnawave:\n    environment:\n      - REMNAWAVE_API_TOKEN=$api_token\n", cfgfile.Opts{Kind: cfgfile.KindCompose})
				return err
			},
			func() error { return cfgfile.Replace(w.nginx, "# generated\nserver { listen 443; }\n", cfgfile.Opts{}) },
			func() error {
				w.d.containers["remnawave"], w.d.containers["remnawave-subscription-page"] = true, true
				w.d.networks["remnawave-network"] = true
				w.d.volumes["remnawave-db-data"] = true // a volume this run created: it must stay
				journal.Add("packages", "system packages")
				return nil
			},
			func() error { return cfgfile.SetComposeEnv(w.comp, "REMNAWAVE_API_TOKEN", "tok-123") },
			func() error { return cfgfile.WriteSecret(w.secret, "dns_cloudflare_api_token = x\n") },
			func() error { w.d.containers["remnawave-extra"] = true; return nil },
		}
		for _, f := range fns {
			if err := next(f); err != nil {
				return err
			}
		}
		if failAfter == steps { // every step ran, then the run fails
			return errInjected
		}
		return nil
	}
}

// The fault-injection test of the slice: the flow fails after every step and
// the state before the run comes back, byte for byte for files and by name
// for containers and networks. Volumes are never removed.
func TestFailureAfterEveryStepRestoresThePreRunState(t *testing.T) {
	for failAfter := 0; failAfter <= steps; failAfter++ {
		t.Run(fmt.Sprintf("after step %d", failAfter), func(t *testing.T) {
			w := newWorld(t)
			beforeFiles, beforeDocker := w.files(), w.docker()
			err := RunWith(w.d, "test", w.flow(failAfter))
			if !errors.Is(err, errInjected) {
				t.Fatalf("err = %v", err)
			}
			if got := w.files(); !reflect.DeepEqual(got, beforeFiles) {
				t.Errorf("files differ from the pre-run state:\n got  %+v\n want %+v", got, beforeFiles)
			}
			after := w.docker()
			volumesAfter := after["volumes"]
			after["volumes"] = nil
			want := beforeDocker
			volumesBefore := want["volumes"]
			want["volumes"] = nil
			if !reflect.DeepEqual(after, want) {
				t.Errorf("containers or networks differ:\n got  %v\n want %v", after, want)
			}
			// Every volume that existed stays; one created by the run stays too.
			for _, v := range volumesBefore {
				found := false
				for _, a := range volumesAfter {
					found = found || a == v
				}
				if !found {
					t.Errorf("volume %s was removed", v)
				}
			}
			if failAfter >= 4 && !w.d.volumes["remnawave-db-data"] {
				t.Error("a volume created by the run was removed")
			}
			if cfgfile.Observe != nil {
				t.Error("the observer was left installed")
			}
		})
	}
}

func TestSuccessKeepsEverythingAndClearsNotes(t *testing.T) {
	w := newWorld(t)
	if err := RunWith(w.d, "test", w.flow(steps+1)); err != nil {
		t.Fatal(err)
	}
	f := w.files()
	if !f[w.env].exists || !f[w.secret].exists || !strings.Contains(f[w.nginx].data, "listen 443") {
		t.Errorf("files after a successful run: %+v", f)
	}
	if !strings.Contains(f[w.comp].data, "tok-123") {
		t.Error("compose lost the token")
	}
	for _, c := range []string{"remnawave", "remnawave-subscription-page", "remnawave-extra", "remnawave-db"} {
		if !w.d.containers[c] {
			t.Errorf("container %s is gone", c)
		}
	}
	if len(w.d.removed) != 0 {
		t.Errorf("removed on success: %v", w.d.removed)
	}
	if n := journal.Take(); len(n) != 0 {
		t.Errorf("notes left after a successful run: %v", n)
	}
}

func TestOnlyObjectsCreatedByTheRunAreRemoved(t *testing.T) {
	w := newWorld(t)
	_ = RunWith(w.d, "test", w.flow(7))
	want := []string{"container remnawave", "container remnawave-extra", "container remnawave-subscription-page", "network remnawave-network"}
	got := append([]string(nil), w.d.removed...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("removed %v, want %v", got, want)
	}
	if !w.d.containers["remnawave-db"] || !w.d.networks["remnawave-old-network"] || !w.d.foreign["other-app"] {
		t.Error("something that existed before the run was touched")
	}
	// Containers go before networks.
	last := -1
	for i, r := range w.d.removed {
		if strings.HasPrefix(r, "network") {
			last = i
		} else if last >= 0 {
			t.Errorf("a container was removed after a network: %v", w.d.removed)
		}
	}
}

func TestAFileChangedTwiceGoesBackToItsFirstState(t *testing.T) {
	w := newWorld(t)
	before := w.files()
	_ = RunWith(w.d, "test", func() error {
		_ = cfgfile.Replace(w.nginx, "first\n", cfgfile.Opts{})
		_ = cfgfile.Replace(w.nginx, "second\n", cfgfile.Opts{})
		_ = cfgfile.Replace(w.nginx, "third\n", cfgfile.Opts{})
		return errInjected
	})
	if got := w.files(); !reflect.DeepEqual(got, before) {
		t.Errorf("got %+v\nwant %+v", got, before)
	}
}

func TestPanicIsRolledBackAndReturnedAsAnError(t *testing.T) {
	w := newWorld(t)
	before := w.files()
	err := RunWith(w.d, "test", func() error {
		_ = cfgfile.Replace(w.nginx, "changed\n", cfgfile.Opts{})
		w.d.containers["remnawave"] = true
		panic("boom")
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(w.files(), before) || w.d.containers["remnawave"] {
		t.Error("a panic left changes behind")
	}
}

func TestRollbackContinuesPastAFailedStepAndReportsIt(t *testing.T) {
	w := newWorld(t)
	var report Report
	err := RunWith(w.d, "test", func() error {
		_ = cfgfile.Replace(w.nginx, "changed\n", cfgfile.Opts{})
		w.d.containers["stuck"], w.d.containers["fine"] = true, true
		w.d.failRemove["stuck"] = true
		return errInjected
	})
	_ = err
	// Run the same thing by hand to read the report.
	w2 := newWorld(t)
	tx := begin("t", w2.d)
	defer tx.end()
	_ = cfgfile.Replace(w2.nginx, "changed\n", cfgfile.Opts{})
	w2.d.containers["stuck"], w2.d.containers["fine"] = true, true
	w2.d.failRemove["stuck"] = true
	report = tx.Rollback()
	if len(report.Failures) != 1 || !strings.Contains(report.Failures[0].Error(), "stuck") {
		t.Errorf("failures %v", report.Failures)
	}
	if !reflect.DeepEqual(report.RemovedContainers, []string{"fine"}) {
		t.Errorf("removed %v", report.RemovedContainers)
	}
	if b, _ := os.ReadFile(w2.nginx); !strings.Contains(string(b), "my tuned config") {
		t.Error("the file was not restored although a container could not be removed")
	}
}

func TestAContainerTheRunRemovedIsReportedNotRestored(t *testing.T) {
	w := newWorld(t)
	tx := begin("t", w.d)
	defer tx.end()
	delete(w.d.containers, "remnawave-db") // a teardown inside the run
	r := tx.Rollback()
	if !reflect.DeepEqual(r.LostContainers, []string{"remnawave-db"}) {
		t.Errorf("lost %v", r.LostContainers)
	}
	if w.d.containers["remnawave-db"] {
		t.Error("a container was invented")
	}
}

func TestDockerThatDoesNotAnswerSkipsContainersButRestoresFiles(t *testing.T) {
	w := newWorld(t)
	w.d.listErr = errors.New("daemon down")
	before := w.files()
	_ = RunWith(w.d, "test", func() error {
		_ = cfgfile.Replace(w.nginx, "changed\n", cfgfile.Opts{})
		return errInjected
	})
	if !reflect.DeepEqual(w.files(), before) {
		t.Error("files not restored")
	}
	if len(w.d.removed) != 0 {
		t.Errorf("containers touched although the state before was unknown: %v", w.d.removed)
	}
	w2 := newWorld(t)
	w2.d.listErr = errors.New("daemon down")
	tx := begin("t", w2.d)
	defer tx.end()
	if r := tx.Rollback(); r.Skipped == "" {
		t.Error("the report does not say containers were skipped")
	}
}

func TestIrreversibleChangesAreReportedNotReverted(t *testing.T) {
	w := newWorld(t)
	tx := begin("t", w.d)
	defer tx.end()
	journal.Add("packages", "system packages installed with apt-get")
	journal.Add("certificate", "the certificate request for a.example.com")
	r := tx.Rollback()
	if len(r.NotReverted) != 2 || r.NotReverted[1].Kind != "certificate" {
		t.Errorf("not reverted: %+v", r.NotReverted)
	}
	if left := journal.Take(); len(left) != 0 {
		t.Errorf("notes were not cleared: %v", left)
	}
}

func TestFilesCreatedByTheRunAreRemovedAndTheManifestForgetsThem(t *testing.T) {
	w := newWorld(t)
	_ = RunWith(w.d, "test", w.flow(6))
	for _, p := range []string{w.env, w.comp, w.secret} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s survived the rollback", p)
		}
		if _, ok := cfgfile.Lookup(p); ok {
			t.Errorf("the manifest still lists %s", p)
		}
	}
}

// Volumes and databases are never removed: the interface the rollback uses
// has no way to do it, and no source of the package names a volume command.
func TestRollbackCannotRemoveVolumes(t *testing.T) {
	typ := reflect.TypeOf((*Docker)(nil)).Elem()
	for i := 0; i < typ.NumMethod(); i++ {
		if strings.Contains(strings.ToLower(typ.Method(i).Name), "volume") {
			t.Errorf("Docker has the method %s", typ.Method(i).Name)
		}
	}
	src, err := os.ReadFile("txn.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{`"volume"`, `"prune"`, `"-v"`, `"down"`, `"--volumes"`} {
		if strings.Contains(string(src), banned) {
			t.Errorf("txn.go contains %s", banned)
		}
	}
}

func TestEverySixFlowRunsInsideATransaction(t *testing.T) {
	for _, f := range []string{"panelfull/install_panel.go", "panelonly/install_panel.go", "caddypanelfull/install_panel.go",
		"caddypanelonly/install_panel.go", "nginxnode/install_node.go", "caddynode/install_node.go"} {
		src, err := os.ReadFile(filepath.Join("..", f))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(src), "return txn.Run(") {
			t.Errorf("%s does not run its flow in a transaction", f)
		}
	}
}
