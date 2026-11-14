// Package rotate replaces the secrets of an installed panel: the database
// password and WEBHOOK_SECRET_HEADER. The run is a transaction: the new
// values are applied in a fixed order, the panel is checked, and when the
// check fails the files and the database go back to the old values. No
// secret is ever an argument of a command: SQL goes through stdin.
package rotate

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/FlexEbat/RemnaForge/internal/cfgfile"
	"github.com/FlexEbat/RemnaForge/internal/execx"
	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/txn"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// Names of the compose services and containers involved.
const (
	dbContainer = "remnawave-db"
	backendName = "remnawave"
)

// Env holds everything the rotation reaches outside itself.
type Env struct {
	// Dir is the panel directory with the .env and docker-compose.yml.
	Dir string
	// Run runs a command in dir and returns its standard output.
	Run func(dir, name string, args ...string) ([]byte, error)
	// RunInput runs a command with input on standard input.
	RunInput func(dir, input, name string, args ...string) error
	Random   func(n int) string
	Sleep    func(time.Duration)
	// Attempts and Interval bound the wait for the backend to be healthy.
	Attempts int
	Interval time.Duration
	Docker   txn.Docker
}

// DefaultEnv returns the Env of the running host for the panel in dir.
func DefaultEnv(dir string) Env {
	return Env{
		Dir: dir,
		Run: func(d, name string, args ...string) ([]byte, error) {
			cmd := execx.Command(name, args...)
			cmd.Dir = d
			return cmd.Output()
		},
		RunInput: func(d, input, name string, args ...string) error {
			cmd := execx.Command(name, args...)
			cmd.Dir = d
			cmd.Stdin = strings.NewReader(input)
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			return cmd.Run()
		},
		Random:   randomAlnum,
		Sleep:    time.Sleep,
		Attempts: 30,
		Interval: 2 * time.Second,
		Docker:   txn.DefaultDocker(),
	}
}

func randomAlnum(n int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	out := make([]byte, n)
	for i := range out {
		v, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		out[i] = alphabet[v.Int64()]
	}
	return string(out)
}

// Creds are the values a rotation changes.
type Creds struct {
	User, DB     string
	Password     string
	WebhookToken string
}

var kvRE = func(key string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `=(.*)$`)
}

func envValue(env, key string) (string, bool) {
	m := kvRE(key).FindStringSubmatch(env)
	if m == nil {
		return "", false
	}
	return strings.Trim(strings.TrimSpace(m[1]), `"'`), true
}

// ReadCreds extracts the current credentials from the content of a .env.
func ReadCreds(env string) (Creds, error) {
	var c Creds
	var ok bool
	if c.User, ok = envValue(env, "POSTGRES_USER"); !ok {
		return c, errors.New("POSTGRES_USER is missing from .env")
	}
	if c.DB, ok = envValue(env, "POSTGRES_DB"); !ok {
		return c, errors.New("POSTGRES_DB is missing from .env")
	}
	if c.Password, ok = envValue(env, "POSTGRES_PASSWORD"); !ok {
		return c, errors.New("POSTGRES_PASSWORD is missing from .env")
	}
	if _, ok = envValue(env, "DATABASE_URL"); !ok {
		return c, errors.New("DATABASE_URL is missing from .env")
	}
	c.WebhookToken, _ = envValue(env, "WEBHOOK_SECRET_HEADER")
	return c, nil
}

var urlPassRE = regexp.MustCompile(`(://[^:/@\s"']+:)[^@\s"']*(@)`)

// UpdateEnv returns the .env with the new password and webhook secret in
// POSTGRES_PASSWORD, DATABASE_URL and WEBHOOK_SECRET_HEADER. Every other line
// stays as it is.
func UpdateEnv(env string, c Creds) (string, error) {
	if !kvRE("POSTGRES_PASSWORD").MatchString(env) || !kvRE("DATABASE_URL").MatchString(env) {
		return "", errors.New("POSTGRES_PASSWORD or DATABASE_URL is missing from .env")
	}
	env = kvRE("POSTGRES_PASSWORD").ReplaceAllLiteralString(env, "POSTGRES_PASSWORD="+c.Password)
	env = kvRE("DATABASE_URL").ReplaceAllStringFunc(env, func(line string) string {
		return urlPassRE.ReplaceAllString(line, "${1}"+c.Password+"${2}")
	})
	if kvRE("WEBHOOK_SECRET_HEADER").MatchString(env) {
		env = kvRE("WEBHOOK_SECRET_HEADER").ReplaceAllLiteralString(env, "WEBHOOK_SECRET_HEADER="+c.WebhookToken)
	} else {
		if !strings.HasSuffix(env, "\n") {
			env += "\n"
		}
		env += "WEBHOOK_SECRET_HEADER=" + c.WebhookToken + "\n"
	}
	return env, nil
}

// alterPassword is the SQL that sets a role's password. It goes through
// stdin, never an argument.
func alterPassword(user, password string) string {
	return fmt.Sprintf("ALTER USER \"%s\" WITH PASSWORD '%s';\n", strings.ReplaceAll(user, `"`, `""`), strings.ReplaceAll(password, "'", "''"))
}

// Result is what a successful rotation produced.
type Result struct {
	WebhookToken string
}

// Rotate replaces the database password and the webhook secret. Order:
//  1. check that the database answers and the backend is healthy,
//  2. set the new password in the database,
//  3. write the new values to .env,
//  4. recreate the backend,
//  5. wait until the backend is healthy again.
//
// A failure after step 1 puts .env back through the transaction, sets the old
// password in the database, recreates the backend and checks it again.
func Rotate(env Env) (Result, error) {
	envPath := filepath.Join(env.Dir, ".env")
	raw, err := os.ReadFile(envPath)
	if err != nil {
		return Result{}, err
	}
	old, err := ReadCreds(string(raw))
	if err != nil {
		return Result{}, err
	}
	fresh := old
	fresh.Password = env.Random(40)
	fresh.WebhookToken = env.Random(64)
	ui.RegisterSecret(old.Password)
	ui.RegisterSecret(fresh.Password)
	ui.RegisterSecret(fresh.WebhookToken)
	newEnv, err := UpdateEnv(string(raw), fresh)
	if err != nil {
		return Result{}, err
	}

	dbChanged := false
	err = txn.RunWith(env.Docker, "rotate", func() error {
		if err := env.precheck(old); err != nil {
			return err
		}
		if err := env.RunInput(env.Dir, alterPassword(old.User, fresh.Password), "docker", "exec", "-i", dbContainer, "sh", "-c",
			`psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"`); err != nil {
			return fmt.Errorf("setting the new database password: %w", err)
		}
		dbChanged = true
		if err := cfgfile.Replace(envPath, newEnv, cfgfile.Opts{Secret: true}); err != nil {
			return err
		}
		if err := env.recreateBackend(); err != nil {
			return err
		}
		return env.waitHealthy()
	})
	if err != nil {
		// The transaction has put .env back. Put the database and the
		// backend back to match.
		if dbChanged {
			env.revert(old)
		}
		return Result{}, err
	}
	return Result{WebhookToken: fresh.WebhookToken}, nil
}

func (e Env) precheck(c Creds) error {
	if _, err := e.Run(e.Dir, "docker", "exec", dbContainer, "pg_isready", "-U", c.User, "-d", c.DB); err != nil {
		return fmt.Errorf("the database does not answer: %w", err)
	}
	return e.healthy()
}

func (e Env) recreateBackend() error {
	_, err := e.Run(e.Dir, "docker", "compose", "up", "-d", "--force-recreate", backendName)
	if err != nil {
		return fmt.Errorf("recreating %s: %w", backendName, err)
	}
	return nil
}

// healthy checks the backend container once: running, and healthy when it
// has a health check.
func (e Env) healthy() error {
	out, err := e.Run(e.Dir, "docker", "inspect", "-f",
		"{{.State.Running}} {{if .State.Health}}{{.State.Health.Status}}{{end}}", backendName)
	if err != nil {
		return fmt.Errorf("%s is not running: %w", backendName, err)
	}
	f := strings.Fields(string(out))
	if len(f) == 0 || f[0] != "true" {
		return fmt.Errorf("%s is not running", backendName)
	}
	if len(f) > 1 && f[1] != "healthy" {
		return fmt.Errorf("%s is %s", backendName, f[1])
	}
	return nil
}

func (e Env) waitHealthy() error {
	var last error
	for i := 0; i < e.Attempts; i++ {
		if last = e.healthy(); last == nil {
			return nil
		}
		e.Sleep(e.Interval)
	}
	return fmt.Errorf("%s did not become healthy: %w", backendName, last)
}

// revert returns the database password and the backend to the old values
// after .env has been restored. It reports what it cannot do.
func (e Env) revert(old Creds) {
	say := func(color, msg string) { fmt.Printf("%s%s%s\n", color, msg, ui.ColorReset) }
	if err := e.RunInput(e.Dir, alterPassword(old.User, old.Password), "docker", "exec", "-i", dbContainer, "sh", "-c",
		`psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"`); err != nil {
		say(ui.ColorRed, fmt.Sprintf(i18n.T("ROTATE_REVERT_DB_FAILED"), err))
		return
	}
	if err := e.recreateBackend(); err != nil {
		say(ui.ColorRed, fmt.Sprintf(i18n.T("ROTATE_REVERT_BACKEND_FAILED"), err))
		return
	}
	if err := e.waitHealthy(); err != nil {
		say(ui.ColorRed, fmt.Sprintf(i18n.T("ROTATE_REVERT_UNHEALTHY"), err))
		return
	}
	say(ui.ColorGreen, i18n.T("ROTATE_REVERTED"))
}

// Run asks for confirmation, rotates and prints the outcome.
func Run(env Env, ask func(string) string) {
	if a := strings.ToLower(strings.TrimSpace(ask(i18n.T("ROTATE_CONFIRM")))); a != "y" && a != "yes" {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("ROTATE_CANCELLED"), ui.ColorReset)
		return
	}
	res, err := Rotate(env)
	if err != nil {
		fmt.Printf("%s%s: %v%s\n", ui.ColorRed, i18n.T("ROTATE_FAILED"), err, ui.ColorReset)
		return
	}
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("ROTATE_DONE"), ui.ColorReset)
	fmt.Printf("%s"+i18n.T("ROTATE_WEBHOOK")+"%s\n", ui.ColorYellow, res.WebhookToken, ui.ColorReset)
}
