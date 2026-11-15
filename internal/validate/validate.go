// Package validate checks the configuration an install flow has just
// written before anything is started: the compose file with
// `docker compose config`, and the proxy configuration (nginx or Caddy) in a
// throwaway container that has the mounts and the image the real service
// will have. A failure stops the flow with the tool's own output, before
// `docker compose up` changes anything.
package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/FlexEbat/RemnaForge/internal/execx"
	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// Env is how validation reaches Docker and the file system.
type Env struct {
	// Run runs a command in dir and returns its combined output.
	Run func(dir, name string, args ...string) (string, error)
	// Stream runs a command whose output goes to the terminal (a build).
	Stream   func(dir, name string, args ...string) error
	ReadFile func(path string) ([]byte, error)
	Exists   func(path string) bool
}

// DefaultEnv is the real host.
func DefaultEnv() Env {
	return Env{
		Run: func(dir, name string, args ...string) (string, error) {
			cmd := execx.Command(name, args...)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			return string(out), err
		},
		Stream: func(dir, name string, args ...string) error {
			cmd := execx.Command(name, args...)
			cmd.Dir = dir
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			return cmd.Run()
		},
		ReadFile: os.ReadFile,
		Exists: func(p string) bool {
			_, err := os.Stat(p)
			return err == nil
		},
	}
}

// Error is a failed validation: which step failed and what the tool said.
type Error struct {
	Step   string
	Output string
	Err    error
}

func (e *Error) Error() string {
	out := strings.TrimSpace(e.Output)
	if out == "" {
		return fmt.Sprintf("%s: %v", e.Step, e.Err)
	}
	return fmt.Sprintf("%s: %v\n%s", e.Step, e.Err, out)
}

func (e *Error) Unwrap() error { return e.Err }

type composeFile struct {
	Name     string                    `json:"name"`
	Services map[string]composeService `json:"services"`
}

type composeService struct {
	Image       string            `json:"image"`
	Build       json.RawMessage   `json:"build"`
	Volumes     []composeVolume   `json:"volumes"`
	Environment map[string]string `json:"environment"`
}

type composeVolume struct {
	Type   string `json:"type"`
	Source string `json:"source"`
	Target string `json:"target"`
}

func (s composeService) builds() bool { return len(s.Build) > 0 && string(s.Build) != "null" }

// Stack validates the compose project in dir. Call it before every
// `docker compose up`.
func Stack(env Env, dir string) error {
	fmt.Printf("%s%s%s\n", ui.ColorGray, i18n.T("VALIDATE_RUNNING"), ui.ColorReset)
	err := stack(env, dir)
	if err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, fmt.Sprintf(i18n.T("VALIDATE_FAILED"), err), ui.ColorReset)
	}
	return err
}

// CheckStack is Stack with the real host.
func CheckStack(dir string) error { return Stack(DefaultEnv(), dir) }

func stack(env Env, dir string) error {
	if out, err := env.Run(dir, "docker", "compose", "config", "-q"); err != nil {
		return &Error{Step: "docker compose config", Output: out, Err: err}
	}
	out, err := env.Run(dir, "docker", "compose", "config", "--format", "json")
	if err != nil {
		return &Error{Step: "docker compose config --format json", Output: out, Err: err}
	}
	var cf composeFile
	if err := json.Unmarshal([]byte(out), &cf); err != nil {
		return &Error{Step: "reading the resolved compose file", Output: out, Err: err}
	}
	names := make([]string, 0, len(cf.Services))
	for n := range cf.Services {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		svc := cf.Services[n]
		switch kind(n, svc) {
		case "nginx":
			if err := nginx(env, dir, n, svc); err != nil {
				return err
			}
		case "caddy":
			if err := caddy(env, dir, cf.Name, n, svc); err != nil {
				return err
			}
		}
	}
	return nil
}

func kind(name string, s composeService) string {
	img := strings.ToLower(s.Image)
	switch {
	case strings.Contains(img, "nginx") || strings.Contains(strings.ToLower(name), "nginx"):
		return "nginx"
	case strings.Contains(img, "caddy") || strings.Contains(strings.ToLower(name), "caddy"):
		return "caddy"
	}
	return ""
}

// bindMounts turns the bind volumes of a service into `-v` arguments. A
// source that does not exist is skipped, so Docker does not create it, and
// the proxy reports the missing file itself. Device paths are skipped.
func bindMounts(env Env, s composeService) (args []string, sources []string) {
	for _, v := range s.Volumes {
		if v.Type != "bind" || v.Source == "" || v.Target == "" {
			continue
		}
		if strings.HasPrefix(v.Source, "/dev") || strings.HasPrefix(v.Source, "/proc") || strings.HasPrefix(v.Source, "/sys") {
			continue
		}
		if !env.Exists(v.Source) {
			continue
		}
		args = append(args, "-v", v.Source+":"+v.Target+":ro")
		sources = append(sources, v.Source)
	}
	return args, sources
}

// upstreamHostRE finds the host of proxy_pass and its relatives, and of a
// `server host:port;` line of an upstream block, wherever they stand in the
// line: a one-line `location / { proxy_pass http://app:3000; }` counts.
var upstreamHostRE = regexp.MustCompile(`(?:\b(?:proxy_pass|grpc_pass|fastcgi_pass|uwsgi_pass)\s+(?:[a-z]+://)?([A-Za-z0-9_.-]+)(?::\d+)?|(?:^|[\s;{])server\s+([A-Za-z0-9_.-]+):\d+)`)

// upstreamHosts lists the host names a proxy configuration refers to, so
// the throwaway container can resolve them. nginx refuses to load a
// configuration whose upstream host does not resolve, and the real container
// resolves them in the compose network, which does not exist yet.
func upstreamHosts(conf string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range upstreamHostRE.FindAllStringSubmatch(conf, -1) {
		h := m[1]
		if h == "" {
			h = m[2]
		}
		if h == "" || h == "unix" || strings.EqualFold(h, "localhost") || seen[h] || looksLikeIP(h) {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

var ipRE = regexp.MustCompile(`^[0-9.]+$`)

func looksLikeIP(h string) bool { return ipRE.MatchString(h) }

func nginx(env Env, dir, name string, s composeService) error {
	if s.Image == "" {
		return nil
	}
	mounts, sources := bindMounts(env, s)
	args := []string{"run", "--rm", "--entrypoint", "nginx"}
	args = append(args, mounts...)
	hosts := map[string]bool{}
	for _, src := range sources {
		if !strings.HasSuffix(src, ".conf") {
			continue
		}
		if b, err := env.ReadFile(src); err == nil {
			for _, h := range upstreamHosts(string(b)) {
				hosts[h] = true
			}
		}
	}
	var hs []string
	for h := range hosts {
		hs = append(hs, h)
	}
	sort.Strings(hs)
	for _, h := range hs {
		args = append(args, "--add-host", h+":127.0.0.1")
	}
	args = append(args, s.Image, "-t")
	if out, err := env.Run(dir, "docker", args...); err != nil {
		return &Error{Step: "nginx -t (" + name + ")", Output: out, Err: err}
	}
	return nil
}

func caddy(env Env, dir, project, name string, s composeService) error {
	image := s.Image
	if s.builds() {
		// The image carries modules the stock one lacks, so it has to be
		// built before its Caddyfile can be checked. `up --build` reuses it.
		if err := env.Stream(dir, "docker", "compose", "build", name); err != nil {
			return &Error{Step: "docker compose build " + name, Err: err}
		}
		if image == "" {
			image = project + "-" + name
		}
	}
	if image == "" {
		return nil
	}
	mounts, sources := bindMounts(env, s)
	target := "/etc/caddy/Caddyfile"
	for i, src := range sources {
		if filepath.Base(src) == "Caddyfile" {
			target = strings.Split(mounts[2*i+1], ":")[1]
		}
	}
	// The Caddyfile reads placeholders such as {$SELF_STEAL_DOMAIN} from the
	// environment of the service. They go in through a file, never as
	// arguments of the command.
	envFile, cleanup, err := writeEnvFile(s.Environment)
	if err != nil {
		return &Error{Step: "preparing the Caddy environment", Err: err}
	}
	defer cleanup()
	args := []string{"run", "--rm", "--entrypoint", "caddy", "--env-file", envFile}
	args = append(args, mounts...)
	args = append(args, image, "validate", "--config", target, "--adapter", "caddyfile")
	if out, err := env.Run(dir, "docker", args...); err != nil {
		return &Error{Step: "caddy validate (" + name + ")", Output: out, Err: err}
	}
	return nil
}

func writeEnvFile(env map[string]string) (string, func(), error) {
	f, err := os.CreateTemp("", "caddy-env-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	if err := f.Chmod(0600); err != nil {
		f.Close()
		cleanup()
		return "", func() {}, err
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, err := fmt.Fprintf(f, "%s=%s\n", k, strings.ReplaceAll(env[k], "\n", " ")); err != nil {
			f.Close()
			cleanup()
			return "", func() {}, err
		}
	}
	return f.Name(), cleanup, f.Close()
}
