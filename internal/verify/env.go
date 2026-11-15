package verify

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/FlexEbat/RemnaForge/internal/execx"
)

// Env holds what the checks touch outside the process, so tests replace
// each piece: the docker CLI, the HTTP client, the TLS dialer and the file
// system.
type Env struct {
	// Docker runs `docker args...` in dir and returns standard output. On
	// failure the error carries the tool's standard error.
	Docker func(dir string, args ...string) ([]byte, error)
	HTTP   *http.Client
	// TLSDial connects to host:443 with full verification (chain, host name,
	// validity) and returns the leaf certificate's expiry.
	TLSDial     func(host string) (time.Time, error)
	ReadFile    func(path string) ([]byte, error)
	Now         func() time.Time
	AppURL      string // the panel backend, answers on the loopback
	TelegramAPI string // base URL of the Telegram Bot API
}

// DefaultEnv is the environment of a real server.
func DefaultEnv() Env {
	return Env{
		Docker:      dockerRun,
		HTTP:        &http.Client{Timeout: 10 * time.Second},
		TLSDial:     tlsDial,
		ReadFile:    os.ReadFile,
		Now:         time.Now,
		AppURL:      "http://127.0.0.1:3000/api/auth/status",
		TelegramAPI: "https://api.telegram.org",
	}
}

func dockerRun(dir string, args ...string) ([]byte, error) {
	cmd := execx.Command("docker", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return out, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
	}
	return out, err
}

func tlsDial(host string) (time.Time, error) {
	d := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := tls.DialWithDialer(d, "tcp", net.JoinHostPort(host, "443"), &tls.Config{ServerName: host})
	if err != nil {
		return time.Time{}, err
	}
	defer conn.Close()
	return conn.ConnectionState().PeerCertificates[0].NotAfter, nil
}
