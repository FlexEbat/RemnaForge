package maintenance

import (
	"strings"
	"testing"

	"github.com/FlexEbat/RemnaForge/internal/dockercheck"
)

func find(checks []check, name string) *check {
	for i := range checks {
		if checks[i].name == name {
			return &checks[i]
		}
	}
	return nil
}

func TestDockerChecksHealthy(t *testing.T) {
	checks := dockerChecks(dockercheck.Status{
		Installed: true, Daemon: true, ServerVersion: "27.3.1",
		ComposeVersion: "2.29.7", ComposeV2: true, Context: "default",
		Projects: []dockercheck.Project{{Name: "remnawave", Dir: "/opt/remnawave"}},
	})
	for _, c := range checks {
		if !c.ok {
			t.Errorf("%s failed on a healthy host: %s", c.name, c.detail)
		}
	}
	if c := find(checks, "docker engine"); c == nil || c.detail != "27.3.1" {
		t.Errorf("engine check = %+v", c)
	}
	if c := find(checks, "docker compose"); c == nil || c.detail != "2.29.7" {
		t.Errorf("compose check = %+v", c)
	}
}

func TestDockerChecksProblems(t *testing.T) {
	if c := dockerChecks(dockercheck.Status{}); len(c) != 1 || c[0].ok {
		t.Errorf("a host without docker must fail one check: %+v", c)
	}
	checks := dockerChecks(dockercheck.Status{Installed: true, ComposeVersion: "1.29.2", Context: "prod"})
	for _, name := range []string{"docker engine", "docker compose", "docker context"} {
		if c := find(checks, name); c == nil || c.ok {
			t.Errorf("%s should fail: %+v", name, c)
		}
	}
	if c := find(dockerChecks(dockercheck.Status{Installed: true, Daemon: true}), "docker compose"); c == nil || c.ok || !strings.Contains(c.detail, "missing") {
		t.Errorf("a missing plugin: %+v", c)
	}
}

func TestDockerChecksProjectNameConflict(t *testing.T) {
	checks := dockerChecks(dockercheck.Status{
		Installed: true, Daemon: true, ComposeV2: true, ComposeVersion: "2.29.7",
		Projects: []dockercheck.Project{
			{Name: "remnawave", Dir: "/home/user/remnawave"},
			{Name: "remnanode", Dir: "/opt/remnanode"},
		},
	})
	c := find(checks, "compose project remnawave")
	if c == nil || c.ok || !strings.Contains(c.detail, "/home/user/remnawave") {
		t.Errorf("conflict check = %+v", c)
	}
	if find(checks, "compose project remnanode") != nil {
		t.Error("a project in its own directory was reported")
	}
}
