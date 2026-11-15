package validate

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// Messages recorded from the tools.
const (
	nginxBroken = `2026/11/14 10:00:01 [emerg] 1#1: unexpected end of file, expecting ";" or "}" in /etc/nginx/conf.d/default.conf:12
nginx: [emerg] unexpected end of file, expecting ";" or "}" in /etc/nginx/conf.d/default.conf:12
nginx: configuration file /etc/nginx/nginx.conf test failed
`
	composeBroken = "validating /opt/remnawave/docker-compose.yml: services.remnawave-nginx.volumes must be a list\n"
	caddyBroken   = "Error: adapting config using caddyfile: /etc/caddy/Caddyfile:7: unrecognized directive: reverse_prox\n"
)

const nginxConf = `server {
    listen unix:/dev/shm/nginx.sock;
    location / { proxy_pass http://remnawave:3000; }
    location /sub { proxy_pass http://remnawave-subscription-page:3010/; }
    location /local { proxy_pass http://127.0.0.1:9000; }
    location /sock { proxy_pass http://unix:/dev/shm/x.sock; }
    location /lh { proxy_pass http://localhost:8080; }
}
upstream api {
    server remnawave-api:8081;
    server 10.0.0.5:8081;
}
`

// host scripts docker. Each command is recorded; output and error are chosen
// by the command line prefix.
type host struct {
	files      map[string]string // path -> content
	out        map[string]string
	fail       map[string]bool
	runs       []string
	built      []string
	envSeen    map[string]string // env-file path -> content at the time of the run
	buildFails bool
}

func newHost() *host {
	return &host{files: map[string]string{}, out: map[string]string{}, fail: map[string]bool{}, envSeen: map[string]string{}}
}

func (h *host) env() Env {
	return Env{
		Run: func(dir, name string, args ...string) (string, error) {
			line := name + " " + strings.Join(args, " ")
			h.runs = append(h.runs, line)
			for i, a := range args {
				if a == "--env-file" {
					b, _ := os.ReadFile(args[i+1])
					h.envSeen[args[i+1]] = string(b)
					if fi, err := os.Stat(args[i+1]); err == nil && fi.Mode().Perm() != 0600 {
						h.envSeen["mode"] = fi.Mode().Perm().String()
					}
				}
			}
			for p, o := range h.out {
				if strings.HasPrefix(line, p) {
					if h.fail[p] {
						return o, errors.New("exit status 1")
					}
					return o, nil
				}
			}
			return "", nil
		},
		Stream: func(dir, name string, args ...string) error {
			h.built = append(h.built, name+" "+strings.Join(args, " "))
			if h.buildFails {
				return errors.New("build failed")
			}
			return nil
		},
		ReadFile: func(p string) ([]byte, error) {
			if c, ok := h.files[p]; ok {
				return []byte(c), nil
			}
			return nil, os.ErrNotExist
		},
		Exists: func(p string) bool { _, ok := h.files[p]; return ok },
	}
}

const nginxJSON = `{"name":"remnawave","services":{
 "remnawave-nginx":{"image":"nginx:1.28","volumes":[
   {"type":"bind","source":"/opt/remnawave/nginx.conf","target":"/etc/nginx/conf.d/default.conf","read_only":true},
   {"type":"bind","source":"/etc/letsencrypt/live/missing","target":"/etc/ssl/missing"},
   {"type":"bind","source":"/dev/shm","target":"/dev/shm"},
   {"type":"volume","source":"caddy_data","target":"/data"}]},
 "remnawave-db":{"image":"postgres:17"}}}`

func TestStackValidatesComposeThenNginx(t *testing.T) {
	h := newHost()
	h.files["/opt/remnawave/nginx.conf"] = nginxConf
	h.out["docker compose config --format json"] = nginxJSON
	if err := stack(h.env(), "/opt/remnawave"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"docker compose config -q",
		"docker compose config --format json",
		"docker run --rm --entrypoint nginx -v /opt/remnawave/nginx.conf:/etc/nginx/conf.d/default.conf:ro " +
			"--add-host remnawave:127.0.0.1 --add-host remnawave-api:127.0.0.1 --add-host remnawave-subscription-page:127.0.0.1 nginx:1.28 -t",
	}
	if !reflect.DeepEqual(h.runs, want) {
		t.Errorf("commands:\n%s\nwant:\n%s", strings.Join(h.runs, "\n"), strings.Join(want, "\n"))
	}
}

func TestBrokenComposeStopsBeforeAnyProxyCheck(t *testing.T) {
	h := newHost()
	h.out["docker compose config -q"] = composeBroken
	h.fail["docker compose config -q"] = true
	err := stack(h.env(), "/opt/remnawave")
	var ve *Error
	if !errors.As(err, &ve) || ve.Step != "docker compose config" {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "services.remnawave-nginx.volumes must be a list") {
		t.Errorf("the tool output is missing from the error:\n%v", err)
	}
	if len(h.runs) != 1 {
		t.Errorf("commands after a failed compose check: %v", h.runs)
	}
}

func TestBrokenNginxConfIsReportedWithItsOutput(t *testing.T) {
	h := newHost()
	h.files["/opt/remnawave/nginx.conf"] = "server { listen 80 location / {"
	h.out["docker compose config --format json"] = nginxJSON
	h.out["docker run"] = nginxBroken
	h.fail["docker run"] = true
	err := stack(h.env(), "/opt/remnawave")
	var ve *Error
	if !errors.As(err, &ve) || !strings.HasPrefix(ve.Step, "nginx -t") {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{`unexpected end of file, expecting ";" or "}"`, "default.conf:12", "test failed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error misses %q:\n%v", want, err)
		}
	}
}

func TestOnlyExistingBindMountsAreMountedReadOnly(t *testing.T) {
	h := newHost()
	h.files["/opt/remnawave/nginx.conf"] = "server {}"
	h.files["/etc/letsencrypt/live/x/fullchain.pem"] = "pem"
	h.out["docker compose config --format json"] = `{"services":{"remnawave-nginx":{"image":"nginx:1.28","volumes":[
	 {"type":"bind","source":"/opt/remnawave/nginx.conf","target":"/etc/nginx/conf.d/default.conf"},
	 {"type":"bind","source":"/etc/letsencrypt/live/x/fullchain.pem","target":"/etc/ssl/fullchain.pem"},
	 {"type":"bind","source":"/etc/letsencrypt/live/y/fullchain.pem","target":"/etc/ssl/y.pem"},
	 {"type":"bind","source":"/proc/stat","target":"/x"},
	 {"type":"tmpfs","target":"/tmp"}]}}}`
	if err := stack(h.env(), "/d"); err != nil {
		t.Fatal(err)
	}
	run := h.runs[len(h.runs)-1]
	if strings.Count(run, " -v ") != 2 || !strings.Contains(run, "/opt/remnawave/nginx.conf:/etc/nginx/conf.d/default.conf:ro") ||
		!strings.Contains(run, "fullchain.pem:/etc/ssl/fullchain.pem:ro") {
		t.Errorf("run: %s", run)
	}
	if strings.Contains(run, "/y/") || strings.Contains(run, "/proc") {
		t.Errorf("a missing or device path was mounted: %s", run)
	}
}

func TestUpstreamHosts(t *testing.T) {
	got := upstreamHosts(nginxConf)
	want := []string{"remnawave", "remnawave-api", "remnawave-subscription-page"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := upstreamHosts("server { return 200; }"); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

const caddyJSON = `{"name":"remnawave","services":{"remnawave-caddy":{
 "image":"remnaforge-caddy-proxyprotocol:2.11.2","build":{"context":"/opt/remnawave"},
 "volumes":[{"type":"bind","source":"/opt/remnawave/Caddyfile","target":"/etc/caddy/Caddyfile"},
            {"type":"bind","source":"/var/www/html","target":"/var/www/html"},
            {"type":"volume","source":"caddy_data","target":"/data"}],
 "environment":{"SELF_STEAL_DOMAIN":"steal.example.com","PANEL_DOMAIN":"panel.example.com","CADDY_SOCKET_PATH":"/dev/shm/nginx.sock"}}}}`

func TestCaddyIsBuiltThenValidatedWithItsEnvironmentInAFile(t *testing.T) {
	h := newHost()
	h.files["/opt/remnawave/Caddyfile"] = "x"
	h.files["/var/www/html"] = "dir"
	h.out["docker compose config --format json"] = caddyJSON
	if err := stack(h.env(), "/opt/remnawave"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(h.built, []string{"docker compose build remnawave-caddy"}) {
		t.Errorf("builds: %v", h.built)
	}
	run := h.runs[len(h.runs)-1]
	for _, want := range []string{"docker run --rm --entrypoint caddy --env-file ", "-v /opt/remnawave/Caddyfile:/etc/caddy/Caddyfile:ro",
		"remnaforge-caddy-proxyprotocol:2.11.2 validate --config /etc/caddy/Caddyfile --adapter caddyfile"} {
		if !strings.Contains(run, want) {
			t.Errorf("run misses %q:\n%s", want, run)
		}
	}
	// The environment travels in a file of mode 0600 that is gone afterwards,
	// and no value of it is on the command line.
	m := regexp.MustCompile(`--env-file (\S+)`).FindStringSubmatch(run)
	if m == nil {
		t.Fatal("no env file")
	}
	seen := h.envSeen[m[1]]
	for _, line := range []string{"SELF_STEAL_DOMAIN=steal.example.com", "PANEL_DOMAIN=panel.example.com", "CADDY_SOCKET_PATH=/dev/shm/nginx.sock"} {
		if !strings.Contains(seen, line+"\n") {
			t.Errorf("env file misses %q:\n%s", line, seen)
		}
	}
	if h.envSeen["mode"] != "" {
		t.Errorf("the env file mode was %s", h.envSeen["mode"])
	}
	if _, err := os.Stat(m[1]); !os.IsNotExist(err) {
		t.Error("the env file was left behind")
	}
	for _, v := range []string{"steal.example.com", "panel.example.com"} {
		if strings.Contains(run, v) {
			t.Errorf("an environment value is on the command line: %s", run)
		}
	}
}

func TestCaddyDefaultImageNameAndCaddyfileTarget(t *testing.T) {
	h := newHost()
	h.files["/c/Caddyfile"] = "x"
	h.out["docker compose config --format json"] = `{"name":"remnanode","services":{"proxy-caddy":{"build":{"context":"."},
	  "volumes":[{"type":"bind","source":"/c/Caddyfile","target":"/srv/Caddyfile"}]}}}`
	if err := stack(h.env(), "/c"); err != nil {
		t.Fatal(err)
	}
	run := h.runs[len(h.runs)-1]
	if !strings.Contains(run, "remnanode-proxy-caddy validate --config /srv/Caddyfile") {
		t.Errorf("run: %s", run)
	}
}

func TestBrokenCaddyfileAndFailedBuildStopTheFlow(t *testing.T) {
	h := newHost()
	h.files["/opt/remnawave/Caddyfile"] = "x"
	h.out["docker compose config --format json"] = caddyJSON
	h.out["docker run"] = caddyBroken
	h.fail["docker run"] = true
	err := stack(h.env(), "/opt/remnawave")
	if err == nil || !strings.Contains(err.Error(), "unrecognized directive: reverse_prox") || !strings.Contains(err.Error(), "caddy validate") {
		t.Errorf("err = %v", err)
	}

	h = newHost()
	h.out["docker compose config --format json"] = caddyJSON
	h.buildFails = true
	err = stack(h.env(), "/opt/remnawave")
	if err == nil || !strings.Contains(err.Error(), "docker compose build remnawave-caddy") {
		t.Errorf("err = %v", err)
	}
	for _, r := range h.runs {
		if strings.HasPrefix(r, "docker run") {
			t.Error("the proxy was validated although the build failed")
		}
	}
}

func TestStackWithoutAProxyOnlyChecksCompose(t *testing.T) {
	h := newHost()
	h.out["docker compose config --format json"] = `{"services":{"remnawave":{"image":"remnawave/backend:2"},"remnawave-db":{"image":"postgres:17"}}}`
	if err := stack(h.env(), "/d"); err != nil || len(h.runs) != 2 {
		t.Errorf("err %v, runs %v", err, h.runs)
	}
}

func TestUnreadableResolvedComposeIsAnError(t *testing.T) {
	h := newHost()
	h.out["docker compose config --format json"] = "not json at all"
	if err := stack(h.env(), "/d"); err == nil {
		t.Error("garbage was accepted")
	}
	h = newHost()
	h.out["docker compose config --format json"] = ""
	h.fail["docker compose config --format json"] = true
	if err := stack(h.env(), "/d"); err == nil {
		t.Error("a failing json step was accepted")
	}
}

func TestErrorMessage(t *testing.T) {
	e := &Error{Step: "x", Output: "  line one\nline two\n", Err: errors.New("exit status 1")}
	if got := e.Error(); got != "x: exit status 1\nline one\nline two" {
		t.Errorf("got %q", got)
	}
	if got := (&Error{Step: "y", Err: errors.New("boom")}).Error(); got != "y: boom" {
		t.Errorf("got %q", got)
	}
	if !errors.Is(e, e.Err) {
		t.Error("Unwrap is missing")
	}
}

func TestPublicStackPrintsAndReturnsTheError(t *testing.T) {
	h := newHost()
	h.out["docker compose config -q"] = composeBroken
	h.fail["docker compose config -q"] = true
	if err := Stack(h.env(), "/d"); err == nil {
		t.Error("a broken compose file passed")
	}
}

// Every `docker compose up` of an install flow is preceded, in the same
// block, by a validation of the same directory that returns the error, so a
// failed validation ends the flow before `up` runs.
func TestEveryFlowValidatesBeforeComposeUp(t *testing.T) {
	flows := []string{"panelfull/install_panel.go", "panelonly/install_panel.go", "caddypanelfull/install_panel.go",
		"caddypanelonly/install_panel.go", "nginxnode/install_node.go", "caddynode/install_node.go"}
	upRE := regexp.MustCompile(`^\t(\w+) := execx\.Command\("docker", "compose", "up"`)
	checkRE := regexp.MustCompile(`^\tif err := validate\.CheckStack\((\w+)\); err != nil \{$`)
	dirRE := regexp.MustCompile(`^\t(\w+)\.Dir = (\w+)$`)
	total := 0
	for _, f := range flows {
		b, err := os.ReadFile(filepath.Join("..", f))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(b), "\n")
		for i, l := range lines {
			m := upRE.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			total++
			if i < 3 {
				t.Fatalf("%s:%d", f, i+1)
			}
			c := checkRE.FindStringSubmatch(lines[i-3])
			if c == nil || strings.TrimSpace(lines[i-2]) != "return err" || strings.TrimSpace(lines[i-1]) != "}" {
				t.Errorf("%s:%d: compose up is not preceded by `if err := validate.CheckStack(dir); err != nil { return err }`", f, i+1)
				continue
			}
			d := dirRE.FindStringSubmatch(lines[i+1])
			if d == nil || d[1] != m[1] || d[2] != c[1] {
				t.Errorf("%s:%d: the validated directory %q is not the directory of the up command", f, i+1, c[1])
			}
		}
	}
	if total != 10 {
		t.Errorf("found %d compose up commands in the flows, expected 10", total)
	}
}
