package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/FlexEbat/RemnaForge/internal/runctx"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// ReportDir holds the saved reports.
const ReportDir = "/usr/local/remnawave_reverse/reports"

// Plan says what is installed and so which checks apply.
type Plan struct {
	PanelDir  string   // set when a panel is installed
	NodeDir   string   // set when a node is installed
	Domains   []string // names served by the reverse proxy, checked for routing and TLS
	Selfsteal []string // selfsteal names of a node, checked for the HTML page and TLS
}

// Run executes every check that applies to plan, in a fixed order: the
// stacks first, then the database, the application, the proxy, TLS and the
// integrations.
func Run(env Env, p Plan) []Result {
	var res []Result
	for _, dir := range []string{p.PanelDir, p.NodeDir} {
		if dir == "" {
			continue
		}
		res = append(res, ContainersExist(env, dir), ContainersRunning(env, dir), HealthchecksPassed(env, dir))
	}
	if p.PanelDir != "" {
		res = append(res, PostgresReady(env, p.PanelDir), AppAnswers(env))
	}
	for _, d := range p.Domains {
		res = append(res, ProxyRoutes(env, d), TLSValid(env, d))
	}
	for _, d := range p.Selfsteal {
		res = append(res, SelfstealServed(env, d), TLSValid(env, d))
	}
	if p.PanelDir != "" {
		res = append(res, Telegram(env, filepath.Join(p.PanelDir, ".env")))
	}
	return res
}

// Report renders results as plain text for the report file.
func Report(results []Result, at time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "RemnaForge report %s\n\n", at.Format("2006-01-02 15:04:05 MST"))
	for _, r := range results {
		b.WriteString(r.Line(false) + "\n")
	}
	if n := Failed(results); n == 0 {
		b.WriteString("\nAll checks passed.\n")
	} else {
		fmt.Fprintf(&b, "\nFailed checks: %d\n", n)
	}
	return b.String()
}

// Save writes the report to dir as report-<timestamp>.txt at mode 0600 in a
// directory at 0700, with secrets masked, and returns the path.
func Save(dir string, results []Result, at time.Time) (string, error) {
	if runctx.DryRun() {
		runctx.Say("save the report into %s", dir)
		return "", nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "report-"+at.UTC().Format("20060102-150405")+".txt")
	if err := os.WriteFile(path, []byte(ui.Redact(Report(results, at))), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
