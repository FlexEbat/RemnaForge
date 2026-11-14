// Package dockercheck is the one place that knows about the Docker engine:
// whether it is installed and working, whether Compose v2 is there, which
// context the CLI talks to, whether the registry is reachable, what this
// tool already has running, and whether a compose project name is taken by
// another directory. It also installs Docker when it is missing.
package dockercheck

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/FlexEbat/RemnaForge/internal/execx"
	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/journal"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// ToolProjects are the compose project names this tool creates. Compose
// names a project after its directory, so they match /opt/remnawave and
// /opt/remnanode.
var ToolProjects = []string{"remnawave", "remnanode"}

// RegistryURL is the endpoint of the registry every image of the stack comes
// from. A reachable registry answers 200 or 401 here.
const RegistryURL = "https://registry-1.docker.io/v2/"

// installScriptURL is Docker's convenience install script.
const installScriptURL = "https://get.docker.com"

// Env holds every way to reach the host. DefaultEnv uses the real one.
type Env struct {
	LookPath   func(string) (string, error)
	Run        func(name string, args ...string) ([]byte, error) // stdout of a command
	Exec       func(name string, args ...string) error           // a command whose output goes to the terminal
	Fetch      func(url string) ([]byte, error)                  // downloads a script
	RunScript  func(script []byte) error                         // runs a script with sh
	HTTPStatus func(url string) (int, error)                     // status code of a GET
}

// DefaultEnv returns the Env of the running host.
func DefaultEnv() Env {
	return Env{
		LookPath: exec.LookPath,
		Run: func(name string, args ...string) ([]byte, error) {
			return execx.Command(name, args...).Output()
		},
		Exec: func(name string, args ...string) error {
			cmd := execx.Command(name, args...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		},
		Fetch: func(url string) ([]byte, error) {
			client := &http.Client{Timeout: 60 * time.Second}
			resp, err := client.Get(url)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
			}
			return io.ReadAll(resp.Body)
		},
		RunScript: func(script []byte) error {
			cmd := execx.CommandTimeout(execx.AptTimeout, "sh")
			cmd.Stdin = strings.NewReader(string(script))
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		},
		HTTPStatus: func(url string) (int, error) {
			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Get(url)
			if err != nil {
				return 0, err
			}
			resp.Body.Close()
			return resp.StatusCode, nil
		},
	}
}

// Project is one compose project known to the daemon.
type Project struct {
	Name string
	// Dir is the directory of the project's first compose file.
	Dir string
}

// Conflict says that several directories claim one compose project name.
type Conflict struct {
	Name string
	Dirs []string
}

// Object is a container, network or volume that belongs to a compose project.
type Object struct {
	Name    string
	Project string
}

// Status is what Inspect found.
type Status struct {
	Installed      bool
	ServerVersion  string // empty when the daemon does not answer
	Daemon         bool
	ComposeVersion string // empty when the Compose plugin is missing
	ComposeV2      bool
	Context        string   // the context the CLI uses
	Contexts       []string // every context
	Projects       []Project
	Conflicts      []Conflict
	Containers     []Object // of this tool's projects
	Networks       []Object
	Volumes        []Object
}

// Inspect reads the state of Docker. It only reads. Commands that need the
// daemon run only when the daemon answers.
func Inspect(env Env) Status {
	var st Status
	if _, err := env.LookPath("docker"); err != nil {
		return st
	}
	st.Installed = true
	if out, err := env.Run("docker", "info", "--format", "{{.ServerVersion}}"); err == nil {
		st.Daemon = true
		st.ServerVersion = strings.TrimSpace(string(out))
	}
	if out, err := env.Run("docker", "compose", "version", "--short"); err == nil {
		st.ComposeVersion = strings.TrimSpace(string(out))
		st.ComposeV2 = composeIsV2(st.ComposeVersion)
	}
	if out, err := env.Run("docker", "context", "ls", "--format", "{{json .}}"); err == nil {
		st.Context, st.Contexts = ParseContexts(string(out))
	}
	if !st.Daemon {
		return st
	}
	if out, err := env.Run("docker", "compose", "ls", "--all", "--format", "json"); err == nil {
		st.Projects = ParseComposeLs(string(out))
		st.Conflicts = FindConflicts(st.Projects)
	}
	const label = `{{.Name}}|{{.Label "com.docker.compose.project"}}`
	if out, err := env.Run("docker", "ps", "-a", "--format", label); err == nil {
		st.Containers = toolObjects(ParseObjects(string(out)))
	}
	if out, err := env.Run("docker", "network", "ls", "--format", label); err == nil {
		st.Networks = toolObjects(ParseObjects(string(out)))
	}
	if out, err := env.Run("docker", "volume", "ls", "--format", label); err == nil {
		st.Volumes = toolObjects(ParseObjects(string(out)))
	}
	return st
}

// Working reports whether Docker can run this tool's stacks: installed,
// daemon answering, Compose v2 present.
func Working(env Env) bool {
	if _, err := env.LookPath("docker"); err != nil {
		return false
	}
	if _, err := env.Run("docker", "info", "--format", "{{.ServerVersion}}"); err != nil {
		return false
	}
	out, err := env.Run("docker", "compose", "version", "--short")
	return err == nil && composeIsV2(strings.TrimSpace(string(out)))
}

// RegistryReachable asks the registry for its API root. Any HTTP answer
// below 500 shows the registry can be reached; 401 is the normal reply of an
// open registry to an anonymous request.
func RegistryReachable(env Env) error {
	code, err := env.HTTPStatus(RegistryURL)
	if err != nil {
		return err
	}
	if code != http.StatusOK && code != http.StatusUnauthorized {
		return fmt.Errorf("registry answered status %d", code)
	}
	return nil
}

// VolumeExists reports whether a docker volume whose name contains the given
// text exists (compose prefixes volume names with the project).
func VolumeExists(name string) bool {
	return VolumeExistsEnv(DefaultEnv(), name)
}

// VolumeExistsEnv is VolumeExists over an Env.
func VolumeExistsEnv(env Env, name string) bool {
	out, err := env.Run("docker", "volume", "ls", "-q", "--filter", "name="+name)
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func composeIsV2(version string) bool {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	major, _, _ := strings.Cut(v, ".")
	n, err := strconv.Atoi(major)
	return err == nil && n >= 2
}

// ParseContexts reads `docker context ls --format {{json .}}` (one JSON
// object per line) and returns the current context and all names.
func ParseContexts(output string) (current string, names []string) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var c struct {
			Name    string
			Current bool
		}
		if json.Unmarshal([]byte(line), &c) != nil || c.Name == "" {
			continue
		}
		names = append(names, c.Name)
		if c.Current {
			current = c.Name
		}
	}
	return current, names
}

// ParseComposeLs reads `docker compose ls --format json`: a JSON array whose
// items carry Name and ConfigFiles (comma separated).
func ParseComposeLs(output string) []Project {
	var raw []struct {
		Name        string
		ConfigFiles string
	}
	if json.Unmarshal([]byte(strings.TrimSpace(output)), &raw) != nil {
		return nil
	}
	var out []Project
	for _, r := range raw {
		first, _, _ := strings.Cut(r.ConfigFiles, ",")
		out = append(out, Project{Name: r.Name, Dir: dirOf(strings.TrimSpace(first))})
	}
	return out
}

func dirOf(file string) string {
	i := strings.LastIndex(file, "/")
	if i <= 0 {
		return file
	}
	return file[:i]
}

// FindConflicts groups projects by name and reports every name that more
// than one directory uses.
func FindConflicts(projects []Project) []Conflict {
	dirs := map[string]map[string]bool{}
	for _, p := range projects {
		if dirs[p.Name] == nil {
			dirs[p.Name] = map[string]bool{}
		}
		dirs[p.Name][p.Dir] = true
	}
	var out []Conflict
	for name, set := range dirs {
		if len(set) < 2 {
			continue
		}
		c := Conflict{Name: name}
		for d := range set {
			c.Dirs = append(c.Dirs, d)
		}
		sort.Strings(c.Dirs)
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ConflictFor reports whether the compose project that dir would create
// (named after its last path element) already exists from another directory.
func ConflictFor(projects []Project, dir string) *Conflict {
	dir = strings.TrimRight(dir, "/")
	name := dir[strings.LastIndex(dir, "/")+1:]
	var others []string
	for _, p := range projects {
		if p.Name == name && p.Dir != dir {
			others = append(others, p.Dir)
		}
	}
	if len(others) == 0 {
		return nil
	}
	sort.Strings(others)
	return &Conflict{Name: name, Dirs: append(others, dir)}
}

// ParseObjects reads lines of "name|project".
func ParseObjects(output string) []Object {
	var out []Object
	for _, line := range strings.Split(output, "\n") {
		name, project, ok := strings.Cut(strings.TrimSpace(line), "|")
		if ok && name != "" {
			out = append(out, Object{Name: name, Project: project})
		}
	}
	return out
}

func toolObjects(all []Object) []Object {
	var out []Object
	for _, o := range all {
		for _, p := range ToolProjects {
			if o.Project == p {
				out = append(out, o)
			}
		}
	}
	return out
}

// Ensure makes Docker work: it installs the engine with Docker's script when
// the engine or Compose v2 is missing, starts and enables the service, and
// checks the result.
func Ensure(env Env) error {
	if !Working(env) {
		fmt.Printf("%sInstalling Docker via get.docker.com...%s\n", ui.ColorYellow, ui.ColorReset)
		journal.Add("docker", i18n.T("JOURNAL_DOCKER"))
		script, err := env.Fetch(installScriptURL)
		if err != nil {
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_DOWNLOAD_DOCKER_KEY"), ui.ColorReset)
			return fmt.Errorf("failed to download get-docker.sh: %w", err)
		}
		if err := env.RunScript(script); err != nil {
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_INSTALL_DOCKER"), ui.ColorReset)
			return err
		}
	}
	if _, err := env.LookPath("docker"); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_DOCKER_NOT_INSTALLED"), ui.ColorReset)
		return err
	}
	if _, err := env.Run("systemctl", "is-active", "--quiet", "docker"); err != nil {
		if err := env.Exec("systemctl", "start", "docker"); err != nil {
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_START_DOCKER"), ui.ColorReset)
			return err
		}
	}
	if _, err := env.Run("systemctl", "is-enabled", "--quiet", "docker"); err != nil {
		if err := env.Exec("systemctl", "enable", "docker"); err != nil {
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_ENABLE_DOCKER"), ui.ColorReset)
			return err
		}
	}
	if !Working(env) {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_DOCKER_NOT_WORKING"), ui.ColorReset)
		return fmt.Errorf("docker still not working after install")
	}
	return nil
}
