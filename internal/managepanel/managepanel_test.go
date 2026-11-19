package managepanel

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/FlexEbat/RemnaForge/internal/i18n"
)

func TestUniqueSortedLines(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"\n\n  \n", nil},
		{"b\na\nb\n", []string{"a", "b"}},
		{"  sha256:b \nsha256:a\nsha256:a", []string{"sha256:a", "sha256:b"}},
	}
	for _, c := range cases {
		if got := uniqueSortedLines(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: got %v, want %v", c.in, got, c.want)
		}
	}
}

func TestHasRemnawaveImageKnowsEveryTag(t *testing.T) {
	for _, images := range []string{
		"remnawave/backend:3\n",
		"postgres:17\nremnawave/backend:2.1.4\n",
		"remnawave/node:latest",
		"remnawave/backend",
	} {
		if !hasRemnawaveImage(images) {
			t.Errorf("%q was not recognised", images)
		}
	}
	for _, images := range []string{"", "postgres:17\nnginx:1.27\n", "remnawave/subscription-page:latest", "someone/remnawave/backend:3"} {
		if hasRemnawaveImage(images) {
			t.Errorf("%q was taken for a panel", images)
		}
	}
}

// dockerFailingOn puts a docker script first in PATH that fails when its
// arguments start with failOn, and returns the file that logs every call.
func dockerFailingOn(t *testing.T, failOn string) string {
	t.Helper()
	bin := t.TempDir()
	log := filepath.Join(bin, "calls")
	script := "#!/bin/sh\necho \"$@\" >> " + log + "\ncase \"$*\" in \"" + failOn + "\"*) echo 'network is unreachable' >&2; exit 1;; esac\n" +
		"case \"$1\" in inspect) exit 1;; esac\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	dir := t.TempDir()
	old := installDirs
	installDirs = []string{dir}
	t.Cleanup(func() { installDirs = old })
	return log
}

func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	real := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	f()
	w.Close()
	os.Stdout = real
	b, _ := io.ReadAll(r)
	return string(b)
}

// A failed pull leaves the images as they were; that was reported as
// "no update available".
func TestUpdateReportsAFailedPull(t *testing.T) {
	log := dockerFailingOn(t, "compose pull")
	out := captureStdout(t, updatePanelNode)
	if strings.Contains(out, i18n.T("NO_UPDATE")) || strings.Contains(out, i18n.T("UPDATE_SUCCESS1")) {
		t.Errorf("a failed pull was reported as an outcome of an update:\n%s", out)
	}
	if !strings.Contains(out, "docker compose pull failed") || !strings.Contains(out, "network is unreachable") {
		t.Errorf("the failure was not shown:\n%s", out)
	}
	if calls, _ := os.ReadFile(log); strings.Contains(string(calls), "compose up") {
		t.Errorf("the stack was recreated after a failed pull:\n%s", calls)
	}
}

func TestStartDoesNotReportSuccessWhenComposeFails(t *testing.T) {
	dockerFailingOn(t, "compose up")
	out := captureStdout(t, startPanelNode)
	if strings.Contains(out, i18n.T("PANEL_RUN")) {
		t.Errorf("Start reported success although docker compose up failed:\n%s", out)
	}
	if !strings.Contains(out, "docker compose up failed") {
		t.Errorf("the failure was not shown:\n%s", out)
	}
}

func TestLastLines(t *testing.T) {
	if got := lastLines("a\n\nb\nc\nd\n", 2); got != "c\nd" {
		t.Errorf("got %q", got)
	}
}
