// Package execx runs external commands with a timeout. Every call site in the
// project goes through it, so a hung apt, docker or certbot process cannot
// block the installer forever.
package execx

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Timeouts per command class. They are variables so tests can shorten them.
var (
	// AptTimeout covers apt, apt-get and dpkg: package installs on a slow
	// mirror take minutes.
	AptTimeout = 15 * time.Minute
	// DockerLongTimeout covers image pulls and builds, which download and
	// compile for a long time on small servers.
	DockerLongTimeout = 30 * time.Minute
	// CertbotTimeout covers certificate issue and renewal. A DNS-01 challenge
	// waits for record propagation, which regularly exceeds the default.
	CertbotTimeout = 10 * time.Minute
	// DefaultTimeout covers every other command.
	DefaultTimeout = 2 * time.Minute
)

// ErrTimeout is wrapped in the error of a command that was killed because it
// ran longer than its timeout.
var ErrTimeout = errors.New("command timed out")

// TimeoutFor returns the timeout of the command class that name and args
// belong to.
func TimeoutFor(name string, args ...string) time.Duration {
	switch name {
	case "apt", "apt-get", "dpkg", "add-apt-repository":
		return AptTimeout
	case "certbot":
		return CertbotTimeout
	case "python3":
		if hasArg(args, "pip") {
			return CertbotTimeout
		}
	case "docker":
		if hasArg(args, "pull", "build", "--build", "up", "run") {
			return DockerLongTimeout
		}
	}
	return DefaultTimeout
}

func hasArg(args []string, want ...string) bool {
	for _, a := range args {
		for _, w := range want {
			if a == w {
				return true
			}
		}
	}
	return false
}

// Cmd is an exec.Cmd with a timeout. The exported fields and methods of
// exec.Cmd (Stdin, Stdout, Stderr, Env, Dir, StdinPipe) work as usual.
type Cmd struct {
	*exec.Cmd
	timeout  time.Duration
	group    bool
	timer    *time.Timer
	timedOut atomic.Bool
}

// Command returns a Cmd that is killed after the timeout of its class.
// The timer starts when the command starts, not when it is built.
func Command(name string, args ...string) *Cmd {
	return &Cmd{
		Cmd:     exec.Command(name, args...),
		timeout: TimeoutFor(name, args...),
		group:   true,
	}
}

// CommandTimeout is Command with an explicit timeout.
func CommandTimeout(timeout time.Duration, name string, args ...string) *Cmd {
	c := Command(name, args...)
	c.timeout = timeout
	return c
}

// Interactive returns a Cmd without a timeout, for commands the operator
// drives by hand (a shell in a container, a followed log). It keeps the
// process in the foreground group of the terminal.
func Interactive(name string, args ...string) *Cmd {
	return &Cmd{Cmd: exec.Command(name, args...)}
}

// Start starts the command and arms the timer.
func (c *Cmd) Start() error {
	if c.group {
		if c.SysProcAttr == nil {
			c.SysProcAttr = &syscall.SysProcAttr{}
		}
		c.SysProcAttr.Setpgid = true
	}
	if err := c.Cmd.Start(); err != nil {
		return err
	}
	if c.timeout > 0 {
		c.timer = time.AfterFunc(c.timeout, c.kill)
	}
	return nil
}

// kill ends the whole process group, so children such as the docker CLI
// plugin processes do not outlive the command.
func (c *Cmd) kill() {
	c.timedOut.Store(true)
	if c.Process == nil {
		return
	}
	if c.group {
		_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		return
	}
	_ = c.Process.Kill()
}

// Wait waits for the command and reports a timeout as ErrTimeout.
func (c *Cmd) Wait() error {
	err := c.Cmd.Wait()
	if c.timer != nil {
		c.timer.Stop()
	}
	if c.timedOut.Load() {
		return fmt.Errorf("%s after %s: %w", c.Path, c.timeout, ErrTimeout)
	}
	return err
}

// Run starts the command and waits for it.
func (c *Cmd) Run() error {
	if err := c.Start(); err != nil {
		return err
	}
	return c.Wait()
}

// Output runs the command and returns its standard output.
func (c *Cmd) Output() ([]byte, error) {
	if c.Stdout != nil {
		return nil, errors.New("execx: Stdout already set")
	}
	var out bytes.Buffer
	c.Stdout = &out
	var errBuf bytes.Buffer
	captureErr := c.Stderr == nil
	if captureErr {
		c.Stderr = &errBuf
	}
	err := c.Run()
	var ee *exec.ExitError
	if captureErr && errors.As(err, &ee) {
		ee.Stderr = errBuf.Bytes()
	}
	return out.Bytes(), err
}

// CombinedOutput runs the command and returns standard output and standard
// error together.
func (c *Cmd) CombinedOutput() ([]byte, error) {
	if c.Stdout != nil || c.Stderr != nil {
		return nil, errors.New("execx: Stdout or Stderr already set")
	}
	var b bytes.Buffer
	c.Stdout = &b
	c.Stderr = &b
	err := c.Run()
	return b.Bytes(), err
}

// Run runs a command with the timeout of its class and discards its output.
func Run(name string, args ...string) error {
	return Command(name, args...).Run()
}

// Output runs a command with the timeout of its class and returns its
// standard output.
func Output(name string, args ...string) ([]byte, error) {
	return Command(name, args...).Output()
}

// String returns the command line for messages.
func (c *Cmd) String() string {
	return strings.Join(c.Args, " ")
}
