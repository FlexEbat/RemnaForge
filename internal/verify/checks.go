package verify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

type composeContainer struct {
	Service string `json:"Service"`
	State   string `json:"State"`
	Health  string `json:"Health"`
}

// containers lists the compose services of dir that have a container.
// `docker compose ps --format json` prints one JSON object per line since
// Compose 2.21 and one JSON array before, so both are read.
func containers(env Env, dir string) ([]composeContainer, error) {
	out, err := env.Docker(dir, "compose", "ps", "-a", "--format", "json")
	if err != nil {
		return nil, err
	}
	out = bytes.TrimSpace(out)
	if len(out) == 0 {
		return nil, nil
	}
	if out[0] == '[' {
		var list []composeContainer
		return list, json.Unmarshal(out, &list)
	}
	var list []composeContainer
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var c composeContainer
		if err := dec.Decode(&c); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, nil
}

func services(env Env, dir string) ([]string, error) {
	out, err := env.Docker(dir, "compose", "config", "--services")
	if err != nil {
		return nil, err
	}
	var res []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			res = append(res, l)
		}
	}
	sort.Strings(res)
	return res, nil
}

func remedyLogs(dir string) string {
	return fmt.Sprintf("cd %s && docker compose logs --tail 100", dir)
}

// ContainersExist checks that every service of the compose file has a
// container.
func ContainersExist(env Env, dir string) Result {
	name := "containers exist " + dir
	want, err := services(env, dir)
	if err != nil {
		return fail(name, err.Error(), "check the compose file: cd "+dir+" && docker compose config")
	}
	if len(want) == 0 {
		return fail(name, "no services in the compose file", "check the compose file: cd "+dir+" && docker compose config")
	}
	have, err := containers(env, dir)
	if err != nil {
		return fail(name, err.Error(), "check that the docker daemon answers: docker info")
	}
	present := map[string]bool{}
	for _, c := range have {
		present[c.Service] = true
	}
	var missing []string
	for _, s := range want {
		if !present[s] {
			missing = append(missing, s)
		}
	}
	if len(missing) > 0 {
		return fail(name, "no container for: "+strings.Join(missing, ", "), "create them: cd "+dir+" && docker compose up -d")
	}
	return pass(name, fmt.Sprintf("%d services have a container", len(want)))
}

// ContainersRunning checks that every container of dir is in the running
// state.
func ContainersRunning(env Env, dir string) Result {
	name := "containers running " + dir
	have, err := containers(env, dir)
	if err != nil {
		return fail(name, err.Error(), "check that the docker daemon answers: docker info")
	}
	if len(have) == 0 {
		return fail(name, "no containers", "start them: cd "+dir+" && docker compose up -d")
	}
	var down []string
	for _, c := range have {
		if c.State != "running" {
			down = append(down, c.Service+" ("+c.State+")")
		}
	}
	if len(down) > 0 {
		return fail(name, "not running: "+strings.Join(down, ", "), remedyLogs(dir))
	}
	return pass(name, fmt.Sprintf("%d containers running", len(have)))
}

// HealthchecksPassed checks the healthcheck state of every container that
// defines one. A container without a healthcheck is not judged.
func HealthchecksPassed(env Env, dir string) Result {
	name := "healthchecks " + dir
	have, err := containers(env, dir)
	if err != nil {
		return fail(name, err.Error(), "check that the docker daemon answers: docker info")
	}
	var bad []string
	checked := 0
	for _, c := range have {
		if c.Health == "" {
			continue
		}
		checked++
		if c.Health != "healthy" {
			bad = append(bad, c.Service+" ("+c.Health+")")
		}
	}
	if len(bad) > 0 {
		return fail(name, "not healthy: "+strings.Join(bad, ", "), remedyLogs(dir))
	}
	if checked == 0 {
		return skip(name, "no container defines a healthcheck")
	}
	return pass(name, fmt.Sprintf("%d healthy", checked))
}

// PostgresReady runs pg_isready inside the database container. The role and
// database come from the container's own environment, so no credential
// appears in an argument.
func PostgresReady(env Env, dir string) Result {
	name := "PostgreSQL accepts connections"
	out, err := env.Docker(dir, "compose", "exec", "-T", "remnawave-db",
		"sh", "-c", `pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"`)
	if err != nil {
		return fail(name, firstLine(string(out), err), "cd "+dir+" && docker compose logs --tail 100 remnawave-db")
	}
	return pass(name, firstLine(string(out), nil))
}

func firstLine(out string, err error) string {
	out = strings.TrimSpace(out)
	if i := strings.IndexByte(out, '\n'); i >= 0 {
		out = out[:i]
	}
	if out == "" && err != nil {
		return err.Error()
	}
	return out
}

// AppAnswers asks the panel backend on the loopback for its auth status with
// the proxy headers it insists on. A status below 400 means the application
// is up and accepts the proxy headers.
func AppAnswers(env Env) Result {
	name := "application answers HTTP " + hostOf(env.AppURL)
	req, err := http.NewRequest("GET", env.AppURL, nil)
	if err != nil {
		return fail(name, err.Error(), "")
	}
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, err := env.HTTP.Do(req)
	if err != nil {
		return fail(name, unwrap(err), "the backend does not listen: cd /opt/remnawave && docker compose logs --tail 100 remnawave")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fail(name, resp.Status, "the backend answers with an error: cd /opt/remnawave && docker compose logs --tail 100 remnawave")
	}
	return pass(name, resp.Status)
}

// ProxyRoutes requests https://domain/ through the reverse proxy. A status
// below 500 means the proxy found a route for the name.
func ProxyRoutes(env Env, domain string) Result {
	name := "reverse proxy routes " + domain
	resp, err := env.HTTP.Get("https://" + domain + "/")
	if err != nil {
		// The proxy answers a request without the secret cookie by closing the
		// connection (nginx 444, Caddy abort, HAProxy silent-drop), so a
		// healthy panel domain ends here in EOF or a reset. The TLS check
		// that follows proves the handshake works. A refused or timed-out
		// connection is still a failure.
		if closedWithoutAnswer(err) {
			return pass(name, "the proxy closed the connection without an answer, as it does without the secret cookie")
		}
		return fail(name, unwrap(err), "check that "+domain+" points at this server and that port 443 is open")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 500 {
		return fail(name, resp.Status, "the proxy has no working upstream for this name: check its logs")
	}
	return pass(name, resp.Status)
}

// closedWithoutAnswer reports whether err is the other side closing an
// established connection before it sent a response.
func closedWithoutAnswer(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET)
}

// TLSValid completes a verified TLS handshake with domain:443: chain, host
// name and validity period are all checked by the dialer.
func TLSValid(env Env, domain string) Result {
	name := "TLS handshake and chain " + domain
	notAfter, err := env.TLSDial(domain)
	if err != nil {
		return fail(name, err.Error(), "reissue the certificate from the certificate menu and check that the proxy serves it")
	}
	days := int(notAfter.Sub(env.Now()).Hours() / 24)
	return pass(name, fmt.Sprintf("valid, %d days left", days))
}

// Telegram calls getMe with the bot token from the panel's .env. It runs
// only when a token is set. A failure to reach the host is reported apart
// from a rejected token, and the token never appears in the result.
func Telegram(env Env, envFile string) Result {
	name := "Telegram bot"
	data, err := env.ReadFile(envFile)
	if err != nil {
		return skip(name, "no "+filepath.Base(envFile))
	}
	token := dotenvValue(string(data), "TELEGRAM_BOT_TOKEN")
	if token == "" || token == "change_me" {
		return skip(name, "no bot token set")
	}
	resp, err := env.HTTP.Get(env.TelegramAPI + "/bot" + url.PathEscape(token) + "/getMe")
	if err != nil {
		return fail(name, "cannot reach "+hostOf(env.TelegramAPI)+": "+unwrap(err),
			"check that the server can open https://"+hostOf(env.TelegramAPI)+" (firewall, DNS)")
	}
	defer resp.Body.Close()
	var body struct {
		OK bool `json:"ok"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&body)
	switch {
	case resp.StatusCode == http.StatusOK && body.OK:
		return pass(name, "token accepted")
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusNotFound:
		return fail(name, "Telegram rejected the bot token", "set a valid TELEGRAM_BOT_TOKEN in the panel .env")
	}
	return fail(name, "Telegram answered "+resp.Status, "try again later or check the bot with @BotFather")
}

// dotenvValue returns the value of key in a .env text, without quotes.
func dotenvValue(text, key string) string {
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "#") || !strings.HasPrefix(l, key+"=") {
			continue
		}
		return strings.Trim(strings.TrimSpace(l[len(key)+1:]), `"'`)
	}
	return ""
}

// unwrap returns the cause of a *url.Error, which would otherwise repeat
// the request URL and with it a bot token.
func unwrap(err error) string {
	if ue, ok := err.(*url.Error); ok {
		return ue.Err.Error()
	}
	return err.Error()
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

// SelfstealServed fetches https://domain and expects an HTML page below
// status 400: the selfsteal site a node serves to anyone but its clients.
func SelfstealServed(env Env, domain string) Result {
	name := "selfsteal site serves HTML " + domain
	resp, err := env.HTTP.Get("https://" + domain)
	if err != nil {
		return fail(name, unwrap(err), "check that "+domain+" points at this server, port 443 is open and the node containers run")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode >= 400:
		return fail(name, resp.Status, "the proxy answers with an error: check its logs")
	case !strings.Contains(strings.ToLower(string(body)), "html"):
		return fail(name, "the answer is not an HTML page", "install a selfsteal template from the selfsteal menu")
	}
	return pass(name, resp.Status)
}
