package rotate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/RemnaForge/internal/ui"
)

const envBefore = `APP_PORT=3000
DATABASE_URL="postgresql://postgres:oldpass1234@remnawave-db:5432/panel"
JWT_AUTH_SECRET=keepme-keepme-keepme
WEBHOOK_ENABLED=false
WEBHOOK_SECRET_HEADER=oldhook-oldhook-oldhook
POSTGRES_USER=postgres
POSTGRES_PASSWORD=oldpass1234
POSTGRES_DB=panel
`

// host scripts docker. Every command line is recorded in order; input of a
// command is recorded beside it.
type host struct {
	ops      []string
	inputs   []string
	fail     map[string]error // command line prefix -> error
	inspects []string         // answers of successive `docker inspect` calls, the last repeats
	sleeps   int
	n        int
	dir      string
	// envAtRecreate is the content of .env at the moment of each
	// `docker compose up`: the backend reads it when it starts.
	envAtRecreate []string
}

func (h *host) env(t *testing.T, dir string) Env {
	t.Helper()
	h.dir = dir
	return Env{
		Dir: dir,
		Run: func(_, name string, args ...string) ([]byte, error) {
			line := name + " " + strings.Join(args, " ")
			h.ops = append(h.ops, line)
			for p, err := range h.fail {
				if strings.HasPrefix(line, p) {
					return nil, err
				}
			}
			if strings.HasPrefix(line, "docker compose up") {
				b, _ := os.ReadFile(filepath.Join(h.dir, ".env"))
				h.envAtRecreate = append(h.envAtRecreate, string(b))
			}
			if strings.HasPrefix(line, "docker inspect") {
				i := h.n
				if i >= len(h.inspects) {
					i = len(h.inspects) - 1
				}
				h.n++
				return []byte(h.inspects[i]), nil
			}
			return nil, nil
		},
		RunInput: func(_, input, name string, args ...string) error {
			line := name + " " + strings.Join(args, " ")
			h.ops = append(h.ops, line)
			h.inputs = append(h.inputs, input)
			for p, err := range h.fail {
				if strings.HasPrefix(line, p) {
					return err
				}
			}
			return nil
		},
		Random: func(n int) string {
			h.n += 0
			return strings.Repeat("N", n)
		},
		Sleep:    func(time.Duration) { h.sleeps++ },
		Attempts: 3,
		Interval: time.Millisecond,
		Docker:   noDocker{},
	}
}

type noDocker struct{}

func (noDocker) Objects() ([]string, []string, error) { return nil, nil, nil }
func (noDocker) RemoveContainer(string) error         { return nil }
func (noDocker) RemoveNetwork(string) error           { return nil }

func panelDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envBefore), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func readEnv(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const healthy = "true healthy\n"

func TestRotateRunsTheStepsInOrder(t *testing.T) {
	dir := panelDir(t)
	h := &host{inspects: []string{healthy}}
	res, err := Rotate(h.env(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"docker exec remnawave-db pg_isready -U postgres -d panel",                                           // 1. the database answers
		"docker inspect -f {{.State.Running}} {{if .State.Health}}{{.State.Health.Status}}{{end}} remnawave", // 1. the backend is healthy
		`docker exec -i remnawave-db sh -c psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"`,    // 2. new password in the database
		"docker compose up -d --force-recreate remnawave",                                                    // 4. the backend picks up .env
		"docker inspect -f {{.State.Running}} {{if .State.Health}}{{.State.Health.Status}}{{end}} remnawave", // 5. healthy again
	}
	if len(h.ops) != len(want) {
		t.Fatalf("ops:\n%s", strings.Join(h.ops, "\n"))
	}
	for i := range want {
		if h.ops[i] != want[i] {
			t.Errorf("step %d:\n got  %s\n want %s", i+1, h.ops[i], want[i])
		}
	}
	got := readEnv(t, dir)
	newPass, newHook := strings.Repeat("N", 40), strings.Repeat("N", 64)
	for _, line := range []string{
		"POSTGRES_PASSWORD=" + newPass,
		`DATABASE_URL="postgresql://postgres:` + newPass + `@remnawave-db:5432/panel"`,
		"WEBHOOK_SECRET_HEADER=" + newHook,
	} {
		if !strings.Contains(got, line+"\n") {
			t.Errorf(".env misses %q:\n%s", line, got)
		}
	}
	for _, kept := range []string{"APP_PORT=3000", "JWT_AUTH_SECRET=keepme-keepme-keepme", "WEBHOOK_ENABLED=false", "POSTGRES_USER=postgres", "POSTGRES_DB=panel"} {
		if !strings.Contains(got, kept+"\n") {
			t.Errorf("an unrelated line was lost: %s", kept)
		}
	}
	// The backend reads .env when it starts, so the new values must be on
	// disk before it is recreated.
	if len(h.envAtRecreate) != 1 || !strings.Contains(h.envAtRecreate[0], "POSTGRES_PASSWORD="+newPass) || !strings.Contains(h.envAtRecreate[0], "WEBHOOK_SECRET_HEADER="+newHook) {
		t.Errorf(".env at the recreate: %q", h.envAtRecreate)
	}
	if res.WebhookToken != newHook {
		t.Errorf("result %q", res.WebhookToken)
	}
	if len(h.inputs) != 1 || h.inputs[0] != `ALTER USER "postgres" WITH PASSWORD '`+newPass+"';\n" {
		t.Errorf("SQL on stdin: %q", h.inputs)
	}
	if fi, _ := os.Stat(filepath.Join(dir, ".env")); fi.Mode().Perm() != 0600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
}

// No secret, old or new, is ever an argument of a command.
func TestNoSecretInAnyCommandLine(t *testing.T) {
	dir := panelDir(t)
	h := &host{inspects: []string{healthy}}
	if _, err := Rotate(h.env(t, dir)); err != nil {
		t.Fatal(err)
	}
	for _, op := range h.ops {
		for _, secret := range []string{"oldpass1234", "oldhook-oldhook", "NNNNNNNN", "keepme"} {
			if strings.Contains(op, secret) {
				t.Errorf("%q carries a secret in %q", secret, op)
			}
		}
	}
}

func TestRotateRevertsEverythingWhenTheBackendStaysUnhealthy(t *testing.T) {
	dir := panelDir(t)
	// healthy before, unhealthy after the recreate, healthy again after the revert
	h := &host{inspects: []string{healthy, "true unhealthy\n", "true unhealthy\n", "true unhealthy\n", healthy}}
	_, err := Rotate(h.env(t, dir))
	if err == nil || !strings.Contains(err.Error(), "did not become healthy") {
		t.Fatalf("err = %v", err)
	}
	if got := readEnv(t, dir); got != envBefore {
		t.Errorf(".env was not restored byte for byte:\n%s", got)
	}
	if h.sleeps != 3 {
		t.Errorf("waited %d times, want 3", h.sleeps)
	}
	// Two SQL statements: the new password, then the old one back.
	if len(h.inputs) != 2 || !strings.Contains(h.inputs[0], strings.Repeat("N", 40)) || h.inputs[1] != `ALTER USER "postgres" WITH PASSWORD 'oldpass1234';`+"\n" {
		t.Errorf("SQL: %q", h.inputs)
	}
	// The order of the whole run: the revert comes after the failed check.
	var recreate []int
	for i, op := range h.ops {
		if strings.HasPrefix(op, "docker compose up") {
			recreate = append(recreate, i)
		}
	}
	if len(recreate) != 2 || recreate[1] <= recreate[0] {
		t.Fatalf("the backend must be recreated twice, ops:\n%s", strings.Join(h.ops, "\n"))
	}
	// The first recreate saw the new values, the one of the revert the old
	// ones: the files are restored before the backend is started again.
	if len(h.envAtRecreate) != 2 || !strings.Contains(h.envAtRecreate[0], strings.Repeat("N", 40)) || h.envAtRecreate[1] != envBefore {
		t.Errorf(".env at the two recreates: %q", h.envAtRecreate)
	}
	last := h.ops[len(h.ops)-1]
	if !strings.HasPrefix(last, "docker inspect") {
		t.Errorf("the revert must end with a health check, last op %q", last)
	}
	for _, op := range h.ops {
		if strings.Contains(op, "oldpass1234") || strings.Contains(op, "NNNN") {
			t.Errorf("a secret reached a command line: %s", op)
		}
	}
}

func TestRotateStopsBeforeChangingAnythingWhenTheDatabaseIsDown(t *testing.T) {
	dir := panelDir(t)
	h := &host{inspects: []string{healthy}, fail: map[string]error{"docker exec remnawave-db pg_isready": errors.New("no response")}}
	if _, err := Rotate(h.env(t, dir)); err == nil {
		t.Fatal("expected an error")
	}
	if len(h.inputs) != 0 || readEnv(t, dir) != envBefore {
		t.Error("something was changed although the database was down")
	}
	if len(h.ops) != 1 {
		t.Errorf("ops: %v", h.ops)
	}
}

func TestRotateStopsBeforeChangingAnythingWhenTheBackendIsUnhealthy(t *testing.T) {
	dir := panelDir(t)
	h := &host{inspects: []string{"true unhealthy\n"}}
	if _, err := Rotate(h.env(t, dir)); err == nil {
		t.Fatal("expected an error")
	}
	if len(h.inputs) != 0 || readEnv(t, dir) != envBefore {
		t.Error("a panel that was already unhealthy was changed")
	}
}

func TestRotateFailureOfTheSQLLeavesNothingToRevert(t *testing.T) {
	dir := panelDir(t)
	h := &host{inspects: []string{healthy}, fail: map[string]error{"docker exec -i remnawave-db": errors.New("psql: auth")}}
	if _, err := Rotate(h.env(t, dir)); err == nil {
		t.Fatal("expected an error")
	}
	if readEnv(t, dir) != envBefore {
		t.Error(".env changed")
	}
	if len(h.inputs) != 1 {
		t.Errorf("the old password was written back although the new one never took effect: %q", h.inputs)
	}
	for _, op := range h.ops {
		if strings.HasPrefix(op, "docker compose up") {
			t.Error("the backend was recreated")
		}
	}
}

func TestRotateReportsARevertThatFails(t *testing.T) {
	dir := panelDir(t)
	h := &host{inspects: []string{healthy, "false \n"}}
	calls := 0
	env := h.env(t, dir)
	inner := env.RunInput
	env.RunInput = func(d, in, name string, args ...string) error {
		calls++
		if calls == 2 { // the revert
			return errors.New("connection refused")
		}
		return inner(d, in, name, args...)
	}
	if _, err := Rotate(env); err == nil {
		t.Fatal("expected an error")
	}
	if readEnv(t, dir) != envBefore {
		t.Error(".env not restored although the database revert failed")
	}
}

func TestRotateFailsOnAnEnvWithoutTheNeededKeys(t *testing.T) {
	for name, env := range map[string]string{
		"no user":     "POSTGRES_PASSWORD=x\nPOSTGRES_DB=d\nDATABASE_URL=postgresql://u:x@h/d\n",
		"no db":       "POSTGRES_USER=u\nPOSTGRES_PASSWORD=x\nDATABASE_URL=postgresql://u:x@h/d\n",
		"no password": "POSTGRES_USER=u\nPOSTGRES_DB=d\nDATABASE_URL=postgresql://u:x@h/d\n",
		"no url":      "POSTGRES_USER=u\nPOSTGRES_PASSWORD=x\nPOSTGRES_DB=d\n",
	} {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, ".env"), []byte(env), 0600)
		h := &host{inspects: []string{healthy}}
		if _, err := Rotate(h.env(t, dir)); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if len(h.ops) != 0 {
			t.Errorf("%s: commands ran: %v", name, h.ops)
		}
	}
	h := &host{}
	if _, err := Rotate(h.env(t, t.TempDir())); err == nil {
		t.Error("a missing .env was accepted")
	}
}

func TestUpdateEnv(t *testing.T) {
	got, err := UpdateEnv(envBefore, Creds{Password: "NEWPASS", WebhookToken: "NEWHOOK"})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.NewReplacer("oldpass1234", "NEWPASS", "oldhook-oldhook-oldhook", "NEWHOOK").Replace(envBefore)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	// A missing webhook header is added, an unquoted URL works.
	got, err = UpdateEnv("POSTGRES_PASSWORD=a\nDATABASE_URL=postgresql://u:a@h:5432/d", Creds{Password: "b", WebhookToken: "w"})
	if err != nil || got != "POSTGRES_PASSWORD=b\nDATABASE_URL=postgresql://u:b@h:5432/d\nWEBHOOK_SECRET_HEADER=w\n" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := UpdateEnv("APP=1\n", Creds{}); err == nil {
		t.Error("an .env without the keys was accepted")
	}
}

func TestReadCreds(t *testing.T) {
	c, err := ReadCreds(envBefore)
	if err != nil || c.User != "postgres" || c.DB != "panel" || c.Password != "oldpass1234" || c.WebhookToken != "oldhook-oldhook-oldhook" {
		t.Errorf("%+v %v", c, err)
	}
	c, err = ReadCreds("POSTGRES_USER='u'\nPOSTGRES_DB=\"d\"\nPOSTGRES_PASSWORD=\"p\"\nDATABASE_URL=x\n")
	if err != nil || c.User != "u" || c.DB != "d" || c.Password != "p" || c.WebhookToken != "" {
		t.Errorf("%+v %v", c, err)
	}
}

func TestAlterPasswordQuoting(t *testing.T) {
	if got := alterPassword(`we"ird`, `pa'ss`); got != `ALTER USER "we""ird" WITH PASSWORD 'pa''ss';`+"\n" {
		t.Errorf("got %q", got)
	}
}

func TestRandomAlnum(t *testing.T) {
	a, b := randomAlnum(40), randomAlnum(40)
	if len(a) != 40 || a == b {
		t.Errorf("%q %q", a, b)
	}
	for _, r := range a {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			t.Errorf("non-alphanumeric %q: URLs and SQL stay safe only with letters and digits", r)
		}
	}
}

func TestRunNeedsConfirmation(t *testing.T) {
	dir := panelDir(t)
	h := &host{inspects: []string{healthy}}
	Run(h.env(t, dir), func(string) string { return "n" })
	if len(h.ops) != 0 || readEnv(t, dir) != envBefore {
		t.Error("rotated without a yes")
	}
	ui.ForgetSecrets()
	Run(h.env(t, dir), func(string) string { return "y" })
	if len(h.ops) == 0 || readEnv(t, dir) == envBefore {
		t.Error("did not rotate after a yes")
	}
}

func TestRotatedSecretsAreMaskedInTheLog(t *testing.T) {
	ui.ForgetSecrets()
	dir := panelDir(t)
	h := &host{inspects: []string{healthy}}
	if _, err := Rotate(h.env(t, dir)); err != nil {
		t.Fatal(err)
	}
	out := ui.Redact("old oldpass1234 new " + strings.Repeat("N", 40) + " hook " + strings.Repeat("N", 64))
	if strings.Contains(out, "oldpass1234") || strings.Contains(out, "NNNNNN") {
		t.Errorf("got %q", out)
	}
}
