package dockercheck

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

// fake is a scripted host: Run answers by the full command line.
type fake struct {
	have      map[string]bool   // binaries on PATH
	answers   map[string]string // command line -> stdout
	failing   map[string]bool   // command line -> error
	calls     []string
	scripts   int
	fetched   string
	status    int
	statusErr error
	fetchErr  error
	scriptErr error
}

func newFake() *fake {
	return &fake{have: map[string]bool{"docker": true}, answers: map[string]string{}, failing: map[string]bool{}, status: 401}
}

func (f *fake) env() Env {
	return Env{
		LookPath: func(n string) (string, error) {
			if f.have[n] {
				return "/usr/bin/" + n, nil
			}
			return "", errors.New("missing")
		},
		Run: func(name string, args ...string) ([]byte, error) {
			line := name + " " + strings.Join(args, " ")
			f.calls = append(f.calls, line)
			if f.failing[line] {
				return nil, errors.New("exit 1")
			}
			if out, ok := f.answers[line]; ok {
				return []byte(out), nil
			}
			return nil, errors.New("unscripted: " + line)
		},
		Exec: func(name string, args ...string) error {
			line := name + " " + strings.Join(args, " ")
			f.calls = append(f.calls, "EXEC "+line)
			if f.failing[line] {
				return errors.New("exit 1")
			}
			return nil
		},
		Fetch: func(url string) ([]byte, error) {
			f.fetched = url
			return []byte("#!/bin/sh\n"), f.fetchErr
		},
		RunScript: func([]byte) error {
			f.scripts++
			if f.scriptErr == nil {
				// installing makes docker work from now on
				f.have["docker"] = true
				f.answers["docker info --format {{.ServerVersion}}"] = "27.3.1\n"
				f.answers["docker compose version --short"] = "2.29.7\n"
			}
			return f.scriptErr
		},
		HTTPStatus: func(string) (int, error) { return f.status, f.statusErr },
	}
}

const (
	infoCmd    = "docker info --format {{.ServerVersion}}"
	composeCmd = "docker compose version --short"
)

func healthy() *fake {
	f := newFake()
	f.answers[infoCmd] = "27.3.1\n"
	f.answers[composeCmd] = "2.29.7\n"
	return f
}

func TestWorking(t *testing.T) {
	f := healthy()
	if !Working(f.env()) {
		t.Fatal("a healthy host is not working")
	}
	cases := map[string]func(*fake){
		"no docker binary": func(f *fake) { f.have["docker"] = false },
		"daemon down":      func(f *fake) { f.failing[infoCmd] = true },
		"no compose":       func(f *fake) { f.failing[composeCmd] = true },
		"compose v1":       func(f *fake) { f.answers[composeCmd] = "1.29.2\n" },
		"garbage compose":  func(f *fake) { f.answers[composeCmd] = "\n" },
	}
	for name, mod := range cases {
		f := healthy()
		mod(f)
		if Working(f.env()) {
			t.Errorf("%s: reported working", name)
		}
	}
}

func TestComposeIsV2(t *testing.T) {
	for v, want := range map[string]bool{"2.29.7": true, "v2.20.0": true, "v5.0.0": true, "1.29.2": false, "": false, "dev": false, "10.1": true} {
		if got := composeIsV2(v); got != want {
			t.Errorf("%q: got %v", v, got)
		}
	}
}

func TestInspectHealthyHost(t *testing.T) {
	f := healthy()
	f.answers["docker context ls --format {{json .}}"] = `{"Name":"default","Current":true}` + "\n" + `{"Name":"remote","Current":false}` + "\n"
	f.answers["docker compose ls --all --format json"] = `[{"Name":"remnawave","Status":"running(4)","ConfigFiles":"/opt/remnawave/docker-compose.yml"}]`
	const label = `{{.Name}}|{{.Label "com.docker.compose.project"}}`
	f.answers["docker ps -a --format "+label] = "remnawave-db|remnawave\nremnawave|remnawave\nother-app|blog\nloose|\n"
	f.answers["docker network ls --format "+label] = "bridge|\nremnawave-network|remnawave\n"
	f.answers["docker volume ls --format "+label] = "remnawave-db-data|remnawave\nblog-data|blog\n"
	st := Inspect(f.env())
	if !st.Installed || !st.Daemon || st.ServerVersion != "27.3.1" || !st.ComposeV2 || st.ComposeVersion != "2.29.7" {
		t.Errorf("engine fields: %+v", st)
	}
	if st.Context != "default" || !reflect.DeepEqual(st.Contexts, []string{"default", "remote"}) {
		t.Errorf("contexts: %q %v", st.Context, st.Contexts)
	}
	if len(st.Containers) != 2 || len(st.Networks) != 1 || len(st.Volumes) != 1 {
		t.Errorf("only this tool's objects belong in the status: %+v", st)
	}
	if len(st.Conflicts) != 0 || len(Evaluate(st)) != 0 {
		t.Errorf("findings on a healthy host: %v", Evaluate(st))
	}
}

func TestInspectWithoutDocker(t *testing.T) {
	f := newFake()
	f.have["docker"] = false
	st := Inspect(f.env())
	if st.Installed || len(f.calls) != 0 {
		t.Errorf("status %+v, calls %v", st, f.calls)
	}
	if len(Evaluate(st)) != 0 {
		t.Error("a host without Docker must not warn: the installer adds it")
	}
}

func TestInspectDaemonDownSkipsObjectQueries(t *testing.T) {
	f := newFake()
	f.failing[infoCmd] = true
	f.answers[composeCmd] = "2.29.7\n"
	st := Inspect(f.env())
	if st.Daemon || !st.Installed {
		t.Fatalf("status %+v", st)
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "docker ps") || strings.HasPrefix(c, "docker compose ls") {
			t.Errorf("%q ran without a daemon", c)
		}
	}
	if fs := Evaluate(st); len(fs) != 1 || !strings.Contains(fs[0].Message, "daemon") {
		t.Errorf("findings = %v", fs)
	}
}

func TestEvaluateFindings(t *testing.T) {
	st := Status{Installed: true, Daemon: true, ComposeVersion: "1.29.2", Context: "prod-server",
		Conflicts: []Conflict{{Name: "remnawave", Dirs: []string{"/opt/a/remnawave", "/opt/remnawave"}}}}
	var msgs []string
	for _, f := range Evaluate(st) {
		msgs = append(msgs, f.Message)
	}
	joined := strings.Join(msgs, "\n")
	for _, want := range []string{"1.29.2", "prod-server", `"remnawave"`, "/opt/a/remnawave, /opt/remnawave"} {
		if !strings.Contains(joined, want) {
			t.Errorf("findings miss %q:\n%s", want, joined)
		}
	}
	st = Status{Installed: true, Daemon: true, Context: "default"}
	if fs := Evaluate(st); len(fs) != 1 || !strings.Contains(fs[0].Message, "missing") {
		t.Errorf("a missing plugin must be reported once: %v", fs)
	}
}

func TestParseComposeLs(t *testing.T) {
	out := `[{"Name":"remnawave","Status":"running(4)","ConfigFiles":"/opt/remnawave/docker-compose.yml,/opt/remnawave/override.yml"},
	{"Name":"blog","Status":"exited(1)","ConfigFiles":"/srv/blog/compose.yaml"}]`
	got := ParseComposeLs(out)
	want := []Project{{"remnawave", "/opt/remnawave"}, {"blog", "/srv/blog"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	for _, bad := range []string{"", "not json", "{}", "null"} {
		if got := ParseComposeLs(bad); len(got) != 0 {
			t.Errorf("%q: got %v", bad, got)
		}
	}
}

// Two compose projects that share a name but live in different directories.
func TestFindConflictsTwoProjectsSameName(t *testing.T) {
	projects := []Project{
		{"remnawave", "/opt/remnawave"},
		{"remnawave", "/home/user/test/remnawave"},
		{"remnanode", "/opt/remnanode"},
		{"blog", "/srv/blog"},
		{"blog", "/srv/blog"}, // the same directory twice is not a conflict
	}
	got := FindConflicts(projects)
	want := []Conflict{{Name: "remnawave", Dirs: []string{"/home/user/test/remnawave", "/opt/remnawave"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if got := FindConflicts(nil); len(got) != 0 {
		t.Errorf("nil gave %v", got)
	}
}

func TestConflictFor(t *testing.T) {
	existing := []Project{{"remnawave", "/home/user/remnawave"}, {"remnanode", "/opt/remnanode"}}
	c := ConflictFor(existing, "/opt/remnawave")
	if c == nil || c.Name != "remnawave" || !reflect.DeepEqual(c.Dirs, []string{"/home/user/remnawave", "/opt/remnawave"}) {
		t.Errorf("got %+v", c)
	}
	if c := ConflictFor(existing, "/opt/remnanode"); c != nil {
		t.Errorf("the project's own directory is not a conflict: %+v", c)
	}
	if c := ConflictFor(existing, "/opt/remnanode/"); c != nil {
		t.Errorf("a trailing slash changed the result: %+v", c)
	}
	if c := ConflictFor(existing, "/opt/unrelated"); c != nil {
		t.Errorf("got %+v", c)
	}
}

func TestParseContexts(t *testing.T) {
	cur, names := ParseContexts("{\"Name\":\"a\",\"Current\":false}\n\nbroken\n{\"Name\":\"b\",\"Current\":true}\n{\"Current\":true}\n")
	if cur != "b" || !reflect.DeepEqual(names, []string{"a", "b"}) {
		t.Errorf("got %q %v", cur, names)
	}
	if cur, names := ParseContexts(""); cur != "" || names != nil {
		t.Errorf("empty gave %q %v", cur, names)
	}
}

func TestParseObjects(t *testing.T) {
	got := ParseObjects("a|p\nb|\nno-separator\n|orphan\n  c|q  \n")
	want := []Object{{"a", "p"}, {"b", ""}, {"c", "q"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestRegistryReachable(t *testing.T) {
	for _, code := range []int{200, 401} {
		f := newFake()
		f.status = code
		if err := RegistryReachable(f.env()); err != nil {
			t.Errorf("status %d: %v", code, err)
		}
	}
	for _, code := range []int{403, 429, 500, 503} {
		f := newFake()
		f.status = code
		if err := RegistryReachable(f.env()); err == nil {
			t.Errorf("status %d accepted", code)
		}
	}
	f := newFake()
	f.statusErr = errors.New("dial tcp: i/o timeout")
	if err := RegistryReachable(f.env()); err == nil {
		t.Error("a network error was accepted")
	}
}

func TestVolumeExistsEnv(t *testing.T) {
	f := newFake()
	f.answers["docker volume ls -q --filter name=remnawave-db-data"] = "remnawave_remnawave-db-data\n"
	f.answers["docker volume ls -q --filter name=other"] = "\n"
	if !VolumeExistsEnv(f.env(), "remnawave-db-data") {
		t.Error("existing volume not found")
	}
	if VolumeExistsEnv(f.env(), "other") {
		t.Error("an empty answer counted as a volume")
	}
	if VolumeExistsEnv(f.env(), "unscripted") {
		t.Error("a failing command counted as a volume")
	}
}

func TestEnsureInstallsWhenMissing(t *testing.T) {
	f := newFake()
	f.have["docker"] = false
	f.answers["systemctl is-active --quiet docker"] = ""
	f.answers["systemctl is-enabled --quiet docker"] = ""
	if err := Ensure(f.env()); err != nil {
		t.Fatal(err)
	}
	if f.scripts != 1 || f.fetched != "https://get.docker.com" {
		t.Errorf("scripts = %d, fetched %q", f.scripts, f.fetched)
	}
}

func TestEnsureDoesNothingWhenWorking(t *testing.T) {
	f := healthy()
	f.answers["systemctl is-active --quiet docker"] = ""
	f.answers["systemctl is-enabled --quiet docker"] = ""
	if err := Ensure(f.env()); err != nil {
		t.Fatal(err)
	}
	if f.scripts != 0 || f.fetched != "" {
		t.Error("the install script ran on a working host")
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "EXEC") {
			t.Errorf("%s ran although the service is active and enabled", c)
		}
	}
}

func TestEnsureInstallsWhenComposeMissing(t *testing.T) {
	f := newFake()
	f.answers[infoCmd] = "27.3.1\n"
	f.failing[composeCmd] = true
	f.answers["systemctl is-active --quiet docker"] = ""
	f.answers["systemctl is-enabled --quiet docker"] = ""
	_ = Ensure(f.env())
	if f.scripts != 1 {
		t.Errorf("an engine without Compose v2 must trigger the install, scripts = %d", f.scripts)
	}
}

func TestEnsureStartsAndEnablesService(t *testing.T) {
	f := healthy()
	f.failing["systemctl is-active --quiet docker"] = true
	f.failing["systemctl is-enabled --quiet docker"] = true
	if err := Ensure(f.env()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(f.calls, "\n")
	if !strings.Contains(joined, "EXEC systemctl start docker") || !strings.Contains(joined, "EXEC systemctl enable docker") {
		t.Errorf("calls:\n%s", joined)
	}
}

func TestEnsureErrors(t *testing.T) {
	t.Run("download fails", func(t *testing.T) {
		f := newFake()
		f.have["docker"] = false
		f.fetchErr = errors.New("offline")
		if err := Ensure(f.env()); err == nil || f.scripts != 0 {
			t.Errorf("err = %v, scripts = %d", err, f.scripts)
		}
	})
	t.Run("script fails", func(t *testing.T) {
		f := newFake()
		f.have["docker"] = false
		f.scriptErr = errors.New("apt broke")
		if err := Ensure(f.env()); err == nil {
			t.Error("want the script error")
		}
	})
	t.Run("start fails", func(t *testing.T) {
		f := healthy()
		f.failing["systemctl is-active --quiet docker"] = true
		f.failing["systemctl start docker"] = true
		if err := Ensure(f.env()); err == nil {
			t.Error("want the start error")
		}
	})
	t.Run("enable fails", func(t *testing.T) {
		f := healthy()
		f.answers["systemctl is-active --quiet docker"] = ""
		f.failing["systemctl is-enabled --quiet docker"] = true
		f.failing["systemctl enable docker"] = true
		if err := Ensure(f.env()); err == nil {
			t.Error("want the enable error")
		}
	})
	t.Run("still not working after the script", func(t *testing.T) {
		f := newFake() // docker present, daemon never answers, script succeeds
		f.answers["systemctl is-active --quiet docker"] = ""
		f.answers["systemctl is-enabled --quiet docker"] = ""
		f.failing[infoCmd] = true
		env := f.env()
		env.RunScript = func([]byte) error { return nil }
		if err := Ensure(env); err == nil || !strings.Contains(err.Error(), "still not working") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestLinesAndRegistryLine(t *testing.T) {
	got := strings.Join(Lines(Status{Installed: true, ServerVersion: "27.3.1", ComposeVersion: "2.29.7",
		Containers: []Object{{"a", "remnawave"}}, Volumes: []Object{{"v", "remnawave"}}}), "\n")
	for _, want := range []string{"engine 27.3.1", "compose 2.29.7", "1 containers, 0 networks, 1 volumes"} {
		if !strings.Contains(got, want) {
			t.Errorf("block misses %q:\n%s", want, got)
		}
	}
	if got := Lines(Status{}); len(got) != 1 || !strings.Contains(got[0], "not installed") {
		t.Errorf("got %v", got)
	}
	if got := RegistryLine(nil); !strings.Contains(got, "reachable") {
		t.Errorf("got %q", got)
	}
	if got := RegistryLine(errors.New("timeout")); !strings.Contains(got, "timeout") {
		t.Errorf("got %q", got)
	}
}

func TestParseOSRelease(t *testing.T) {
	cases := []struct {
		name, in, distro, codename string
		ok                         bool
	}{
		{"debian", "ID=debian\nVERSION_CODENAME=bookworm\n", "debian", "bookworm", true},
		{"ubuntu", "ID=ubuntu\nVERSION_CODENAME=noble\nUBUNTU_CODENAME=noble\n", "ubuntu", "noble", true},
		{"ubuntu derivative", "ID=linuxmint\nID_LIKE=\"ubuntu debian\"\nVERSION_CODENAME=wilma\nUBUNTU_CODENAME=noble\n", "ubuntu", "noble", true},
		{"debian derivative has no Docker suite", "ID=kali\nID_LIKE=debian\nVERSION_CODENAME=kali-rolling\n", "", "", false},
		{"other", "ID=fedora\nVERSION_ID=40\n", "", "", false},
		{"no codename", "ID=debian\n", "", "", false},
	}
	for _, c := range cases {
		distro, codename, ok := parseOSRelease(c.in)
		if distro != c.distro || codename != c.codename || ok != c.ok {
			t.Errorf("%s: got %q %q %v", c.name, distro, codename, ok)
		}
	}
}

func TestDockerSources(t *testing.T) {
	got := dockerSources("ubuntu", "noble", "amd64")
	want := "Types: deb\nURIs: https://download.docker.com/linux/ubuntu\nSuites: noble\nComponents: stable\nArchitectures: amd64\nSigned-By: /etc/apt/keyrings/docker.asc\n"
	if got != want {
		t.Errorf("got:\n%s", got)
	}
}

func aptHost(t *testing.T, osRelease string) (*fake, Env, *[]string, map[string]string) {
	t.Helper()
	f := newFake()
	f.have["docker"] = false
	f.answers["systemctl is-active --quiet docker"] = ""
	f.answers["systemctl is-enabled --quiet docker"] = ""
	f.answers["dpkg --print-architecture"] = "arm64\n"
	env := f.env()
	var apt []string
	written := map[string]string{}
	env.ReadFile = func(string) ([]byte, error) { return []byte(osRelease), nil }
	env.WriteFile = func(path string, data []byte, _ os.FileMode) error { written[path] = string(data); return nil }
	env.Apt = func(args ...string) error { apt = append(apt, strings.Join(args, " ")); return nil }
	return f, env, &apt, written
}

func TestEnsureInstallsFromTheAptRepositoryOnUbuntu(t *testing.T) {
	f, env, apt, written := aptHost(t, "ID=ubuntu\nUBUNTU_CODENAME=noble\nVERSION_CODENAME=noble\n")
	// Docker is "installed" once apt has run.
	env.Apt = func(args ...string) error {
		*apt = append(*apt, strings.Join(args, " "))
		if len(args) > 1 && args[1] == "-y" && strings.Contains(strings.Join(args, " "), "docker-ce") {
			f.have["docker"] = true
			f.answers["docker info --format {{.ServerVersion}}"] = "27.3.1\n"
			f.answers["docker compose version --short"] = "2.29.7\n"
		}
		return nil
	}
	if err := Ensure(env); err != nil {
		t.Fatal(err)
	}
	if f.scripts != 0 {
		t.Error("the convenience script ran on Ubuntu")
	}
	if f.fetched != "https://download.docker.com/linux/ubuntu/gpg" {
		t.Errorf("the key came from %q", f.fetched)
	}
	if !strings.Contains(written[dockerSourcesPath], "Suites: noble") || !strings.Contains(written[dockerSourcesPath], "Architectures: arm64") {
		t.Errorf("sources file:\n%s", written[dockerSourcesPath])
	}
	if _, ok := written[dockerKeyPath]; !ok {
		t.Error("the key was not written")
	}
	joined := strings.Join(*apt, "\n")
	for _, want := range []string{"update", "install -y ca-certificates curl", "install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin"} {
		if !strings.Contains(joined, want) {
			t.Errorf("apt calls lack %q:\n%s", want, joined)
		}
	}
}

func TestEnsureFallsBackToTheScriptOnAnotherSystem(t *testing.T) {
	f, env, apt, _ := aptHost(t, "ID=fedora\n")
	_ = Ensure(env)
	if f.scripts != 1 || f.fetched != "https://get.docker.com" || len(*apt) != 0 {
		t.Errorf("scripts=%d fetched=%q apt=%q", f.scripts, f.fetched, *apt)
	}
}

func TestEnsureStopsWhenTheDockerKeyCannotBeDownloaded(t *testing.T) {
	f, env, _, written := aptHost(t, "ID=debian\nVERSION_CODENAME=bookworm\n")
	f.fetchErr = errors.New("offline")
	if err := Ensure(env); err == nil {
		t.Error("no error for a missing key")
	}
	if len(written) != 0 {
		t.Errorf("files were written without a key: %v", written)
	}
}
