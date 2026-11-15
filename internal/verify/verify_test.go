package verify

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// fakeDocker answers by the sub-command: the key is the arguments joined
// by spaces.
type fakeDocker map[string]struct {
	out string
	err error
}

func (f fakeDocker) run(_ string, args ...string) ([]byte, error) {
	r, ok := f[strings.Join(args, " ")]
	if !ok {
		return nil, errors.New("unexpected docker call: " + strings.Join(args, " "))
	}
	return []byte(r.out), r.err
}

const psAll = "compose ps -a --format json"
const cfgServices = "compose config --services"

func envWith(d fakeDocker) Env {
	e := DefaultEnv()
	e.Docker = d.run
	e.Now = func() time.Time { return time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC) }
	return e
}

const ndjson = `{"Service":"remnawave","State":"running","Health":"healthy"}
{"Service":"remnawave-db","State":"running","Health":"healthy"}
{"Service":"remnawave-redis","State":"running","Health":""}
`

func TestContainersExist(t *testing.T) {
	d := fakeDocker{
		cfgServices: {out: "remnawave\nremnawave-db\nremnawave-redis\n"},
		psAll:       {out: ndjson},
	}
	if r := ContainersExist(envWith(d), "/opt/remnawave"); !r.OK {
		t.Errorf("pass expected: %+v", r)
	}
	d[psAll] = struct {
		out string
		err error
	}{out: `{"Service":"remnawave","State":"running"}` + "\n"}
	r := ContainersExist(envWith(d), "/opt/remnawave")
	if r.OK || !strings.Contains(r.Detail, "remnawave-db") || r.Remedy == "" {
		t.Errorf("fail expected naming the missing service: %+v", r)
	}
}

func TestPsArrayFormat(t *testing.T) {
	d := fakeDocker{psAll: {out: `[{"Service":"a","State":"running","Health":""},{"Service":"b","State":"exited","Health":""}]`}}
	r := ContainersRunning(envWith(d), "/x")
	if r.OK || !strings.Contains(r.Detail, "b (exited)") {
		t.Errorf("got %+v", r)
	}
}

func TestContainersRunning(t *testing.T) {
	d := fakeDocker{psAll: {out: ndjson}}
	if r := ContainersRunning(envWith(d), "/x"); !r.OK {
		t.Errorf("pass expected: %+v", r)
	}
	d[psAll] = struct {
		out string
		err error
	}{out: `{"Service":"remnawave","State":"restarting"}`}
	r := ContainersRunning(envWith(d), "/x")
	if r.OK || !strings.Contains(r.Detail, "restarting") || !strings.Contains(r.Remedy, "docker compose logs") {
		t.Errorf("fail expected: %+v", r)
	}
	d[psAll] = struct {
		out string
		err error
	}{}
	if r := ContainersRunning(envWith(d), "/x"); r.OK {
		t.Errorf("no containers must fail: %+v", r)
	}
}

func TestHealthchecks(t *testing.T) {
	d := fakeDocker{psAll: {out: ndjson}}
	if r := HealthchecksPassed(envWith(d), "/x"); !r.OK || r.Skipped {
		t.Errorf("pass expected: %+v", r)
	}
	d[psAll] = struct {
		out string
		err error
	}{out: `{"Service":"remnawave","State":"running","Health":"starting"}`}
	r := HealthchecksPassed(envWith(d), "/x")
	if r.OK || !strings.Contains(r.Detail, "starting") {
		t.Errorf("fail expected: %+v", r)
	}
	d[psAll] = struct {
		out string
		err error
	}{out: `{"Service":"remnawave","State":"running","Health":""}`}
	if r := HealthchecksPassed(envWith(d), "/x"); !r.Skipped {
		t.Errorf("no healthcheck defined must skip: %+v", r)
	}
}

func TestPostgresReady(t *testing.T) {
	key := `compose exec -T remnawave-db sh -c pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"`
	d := fakeDocker{key: {out: "/var/run/postgresql:5432 - accepting connections\n"}}
	if r := PostgresReady(envWith(d), "/x"); !r.OK {
		t.Errorf("pass expected: %+v", r)
	}
	d[key] = struct {
		out string
		err error
	}{out: "/var/run/postgresql:5432 - no response\n", err: errors.New("exit status 2")}
	r := PostgresReady(envWith(d), "/x")
	if r.OK || !strings.Contains(r.Detail, "no response") {
		t.Errorf("fail expected: %+v", r)
	}
}

func TestAppAnswers(t *testing.T) {
	var gotXFF string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotXFF = r.Header.Get("X-Forwarded-For")
		w.WriteHeader(status)
	}))
	defer srv.Close()
	e := DefaultEnv()
	e.AppURL = srv.URL
	if r := AppAnswers(e); !r.OK || gotXFF != "127.0.0.1" {
		t.Errorf("pass expected with the proxy header: %+v, xff=%q", r, gotXFF)
	}
	status = http.StatusBadGateway
	if r := AppAnswers(e); r.OK || r.Remedy == "" {
		t.Errorf("502 must fail: %+v", r)
	}
	status = http.StatusForbidden
	if r := AppAnswers(e); r.OK {
		t.Errorf("403 must fail: the installer waited for a status below 400: %+v", r)
	}
	srv.Close()
	if r := AppAnswers(e); r.OK {
		t.Errorf("closed port must fail: %+v", r)
	}
}

func TestProxyRoutes(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
	defer srv.Close()
	e := DefaultEnv()
	e.HTTP = srv.Client()
	host := strings.TrimPrefix(srv.URL, "https://")
	if r := ProxyRoutes(e, host); !r.OK {
		t.Errorf("pass expected: %+v", r)
	}
	status = http.StatusServiceUnavailable
	if r := ProxyRoutes(e, host); r.OK {
		t.Errorf("503 must fail: %+v", r)
	}
	if r := ProxyRoutes(e, "127.0.0.1:1"); r.OK {
		t.Errorf("unreachable must fail: %+v", r)
	}
}

func TestTLSValid(t *testing.T) {
	e := envWith(nil)
	e.TLSDial = func(string) (time.Time, error) { return e.Now().Add(30 * 24 * time.Hour), nil }
	if r := TLSValid(e, "stats.example.com"); !r.OK || !strings.Contains(r.Detail, "30 days") {
		t.Errorf("pass expected: %+v", r)
	}
	e.TLSDial = func(string) (time.Time, error) {
		return time.Time{}, errors.New("x509: certificate signed by unknown authority")
	}
	if r := TLSValid(e, "stats.example.com"); r.OK || !strings.Contains(r.Detail, "unknown authority") || r.Remedy == "" {
		t.Errorf("chain error must fail: %+v", r)
	}
}

func tgEnv(t *testing.T, token string, h http.HandlerFunc) (Env, string, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	dir := t.TempDir()
	f := filepath.Join(dir, ".env")
	if err := os.WriteFile(f, []byte("A=1\nTELEGRAM_BOT_TOKEN="+token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := DefaultEnv()
	e.TelegramAPI = srv.URL
	return e, f, srv
}

func TestTelegram(t *testing.T) {
	const token = "123456:ABC-secret-token"
	var path string
	e, f, srv := tgEnv(t, token, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if !strings.Contains(r.URL.Path, token) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	})
	defer srv.Close()
	if r := Telegram(e, f); !r.OK || r.Skipped || path != "/bot"+token+"/getMe" {
		t.Errorf("pass expected: %+v path=%q", r, path)
	}
	// A rejected token is reported apart from an unreachable host.
	e2, f2, srv2 := tgEnv(t, token, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	defer srv2.Close()
	if r := Telegram(e2, f2); r.OK || !strings.Contains(r.Detail, "rejected") {
		t.Errorf("rejected token: %+v", r)
	}
	srv2.Close()
	r := Telegram(e2, f2)
	if r.OK || !strings.Contains(r.Detail, "cannot reach") {
		t.Errorf("unreachable host: %+v", r)
	}
	if strings.Contains(r.Detail, token) || strings.Contains(r.Remedy, token) {
		t.Errorf("the token leaked into the result: %+v", r)
	}
}

func TestTelegramSkipsWithoutToken(t *testing.T) {
	for _, tok := range []string{"", "change_me"} {
		e, f, srv := tgEnv(t, tok, func(w http.ResponseWriter, r *http.Request) { t.Error("no request expected") })
		if r := Telegram(e, f); !r.Skipped {
			t.Errorf("token %q must skip: %+v", tok, r)
		}
		srv.Close()
	}
	e := DefaultEnv()
	if r := Telegram(e, filepath.Join(t.TempDir(), "missing.env")); !r.Skipped {
		t.Errorf("missing .env must skip: %+v", r)
	}
}

func TestRunOrderAndPlan(t *testing.T) {
	d := fakeDocker{
		cfgServices: {out: "remnawave\n"},
		psAll:       {out: `{"Service":"remnawave","State":"running","Health":"healthy"}`},
		`compose exec -T remnawave-db sh -c pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"`: {out: "accepting connections"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	e := envWith(d)
	e.AppURL = srv.URL
	e.TLSDial = func(string) (time.Time, error) { return e.Now().Add(48 * time.Hour), nil }
	e.HTTP = srv.Client()
	e.ReadFile = func(string) ([]byte, error) { return nil, os.ErrNotExist }
	got := Run(e, Plan{PanelDir: "/opt/remnawave"})
	var names []string
	for _, r := range got {
		names = append(names, strings.SplitN(r.Name, " /", 2)[0])
	}
	want := []string{"containers exist", "containers running", "healthchecks", "PostgreSQL accepts connections", "application answers HTTP " + hostOf(srv.URL), "Telegram bot"}
	if strings.Join(names, "|") != strings.Join(want, "|") {
		t.Errorf("order:\n got %v\nwant %v", names, want)
	}
	if Failed(got) != 0 {
		t.Errorf("failures: %+v", got)
	}
}

func TestReportIsRedactedAndPrivate(t *testing.T) {
	ui.ForgetSecrets()
	defer ui.ForgetSecrets()
	ui.RegisterSecret("s3cr3t-value-123")
	res := []Result{
		{Name: "a", OK: true, Detail: "fine"},
		{Name: "b", Detail: "token s3cr3t-value-123 failed", Remedy: "do X"},
		{Name: "c", Skipped: true, OK: true, Detail: "n/a"},
	}
	dir := filepath.Join(t.TempDir(), "reports")
	path, err := Save(dir, res, time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	dst, _ := os.Stat(dir)
	if st.Mode().Perm() != 0o600 || dst.Mode().Perm() != 0o700 {
		t.Errorf("modes: file %v dir %v", st.Mode().Perm(), dst.Mode().Perm())
	}
	b, _ := os.ReadFile(path)
	text := string(b)
	if strings.Contains(text, "s3cr3t-value-123") {
		t.Errorf("secret in the report:\n%s", text)
	}
	for _, want := range []string{"[ ok ] a - fine", "[fail] b", "       do X", "[skip] c", "Failed checks: 1"} {
		if !strings.Contains(text, want) {
			t.Errorf("report lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "\x1b[") {
		t.Errorf("color codes in the report")
	}
}

func TestLineMatchesBetweenTerminalAndReport(t *testing.T) {
	r := Result{Name: "x", Detail: "d", Remedy: "r"}
	plain := r.Line(false)
	colored := r.Line(true)
	stripped := strings.NewReplacer(ui.ColorRed, "", ui.ColorReset, "", ui.ColorGray, "", ui.ColorGreen, "").Replace(colored)
	if plain != stripped {
		t.Errorf("terminal and report differ:\n%q\n%q", plain, stripped)
	}
}

func TestSelfstealServed(t *testing.T) {
	status, body := http.StatusOK, "<!DOCTYPE HTML><html></html>"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	defer srv.Close()
	e := DefaultEnv()
	e.HTTP = srv.Client()
	host := strings.TrimPrefix(srv.URL, "https://")
	if r := SelfstealServed(e, host); !r.OK {
		t.Errorf("pass expected: %+v", r)
	}
	body = "plain text"
	if r := SelfstealServed(e, host); r.OK || !strings.Contains(r.Detail, "not an HTML") {
		t.Errorf("non-HTML must fail: %+v", r)
	}
	body, status = "<html>", http.StatusNotFound
	if r := SelfstealServed(e, host); r.OK {
		t.Errorf("404 must fail: %+v", r)
	}
}
