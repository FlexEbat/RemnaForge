package execx

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTimeoutKillsCommand(t *testing.T) {
	start := time.Now()
	err := CommandTimeout(50*time.Millisecond, "sleep", "5").Run()
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("command was not killed in time: %s", time.Since(start))
	}
}

func TestTimeoutKillsChildren(t *testing.T) {
	start := time.Now()
	err := CommandTimeout(50*time.Millisecond, "sh", "-c", "sleep 5; sleep 5").Run()
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("process group was not killed: %s", time.Since(start))
	}
}

func TestOutputAndExitError(t *testing.T) {
	out, err := Output("echo", "hello")
	if err != nil || strings.TrimSpace(string(out)) != "hello" {
		t.Fatalf("got %q, %v", out, err)
	}
	if err := Run("false"); err == nil || errors.Is(err, ErrTimeout) {
		t.Fatalf("want a plain exit error, got %v", err)
	}
}

func TestCombinedOutput(t *testing.T) {
	out, err := Command("sh", "-c", "echo out; echo err 1>&2").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "out") || !strings.Contains(string(out), "err") {
		t.Fatalf("got %q", out)
	}
}

func TestStdinStreams(t *testing.T) {
	c := Command("cat")
	c.Stdin = strings.NewReader("data")
	out, err := c.Output()
	if err != nil || string(out) != "data" {
		t.Fatalf("got %q, %v", out, err)
	}
}

func TestTimeoutClasses(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want time.Duration
	}{
		{"apt-get", []string{"install", "-y", "curl"}, AptTimeout},
		{"dpkg", []string{"-l"}, AptTimeout},
		{"docker", []string{"compose", "up", "-d"}, DockerLongTimeout},
		{"docker", []string{"compose", "pull"}, DockerLongTimeout},
		{"docker", []string{"compose", "build"}, DockerLongTimeout},
		{"docker", []string{"info"}, DefaultTimeout},
		{"docker", []string{"compose", "down"}, DefaultTimeout},
		{"certbot", []string{"renew"}, CertbotTimeout},
		{"python3", []string{"-m", "pip", "install", "x"}, CertbotTimeout},
		{"ufw", []string{"status"}, DefaultTimeout},
	}
	for _, c := range cases {
		if got := TimeoutFor(c.name, c.args...); got != c.want {
			t.Errorf("%s %v: got %s, want %s", c.name, c.args, got, c.want)
		}
	}
}
