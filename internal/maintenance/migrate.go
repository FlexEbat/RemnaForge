package maintenance

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/FlexEbat/RemnaForge/internal/cfgfile"
	"github.com/FlexEbat/RemnaForge/internal/execx"
	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/stack"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// removedEnvKeys are panel variables that no longer exist in 3.x.
var removedEnvKeys = []string{"JWT_API_TOKENS_SECRET", "SWAGGER_PATH", "SCALAR_PATH", "IS_DOCS_ENABLED"}

var backendTagRE = regexp.MustCompile(`remnawave/backend:2[^\s"']*`)

// migrateEnv converts a 2.x panel .env to the 3.x variable set. It
// reports whether anything changed.
func migrateEnv(env string) (string, bool) {
	lines := strings.Split(env, "\n")
	hasApp := false
	for _, l := range lines {
		if strings.HasPrefix(l, "APP_SECRET=") {
			hasApp = true
		}
	}

	changed := false
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "JWT_AUTH_SECRET="):
			changed = true
			if !hasApp {
				out = append(out, "APP_SECRET="+strings.TrimPrefix(l, "JWT_AUTH_SECRET="))
				hasApp = true
			}
			continue
		case hasPrefixAny(l, removedEnvKeys):
			changed = true
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n"), changed
}

func hasPrefixAny(line string, keys []string) bool {
	for _, k := range keys {
		if strings.HasPrefix(line, k+"=") {
			return true
		}
	}
	return false
}

// migrateCompose moves the panel image from the 2.x line to 3.x.
func migrateCompose(compose string) (string, bool) {
	out := backendTagRE.ReplaceAllString(compose, "remnawave/backend:3")
	return out, out != compose
}

var gzipOnRE = regexp.MustCompile(`(?m)^\s*gzip\s+on\s*;`)

// ensureGzip adds response compression to an nginx http-level config
// when it has none; Remnawave 3.x serves large responses that the proxy
// is expected to compress. A commented-out "gzip on;" does not count.
func ensureGzip(conf string) (string, bool) {
	if gzipOnRE.MatchString(conf) {
		return conf, false
	}
	block := "gzip on;\ngzip_vary on;\ngzip_proxied any;\ngzip_comp_level 6;\ngzip_types text/plain text/css application/json application/javascript text/xml application/xml image/svg+xml;\n\n"
	return block + conf, true
}

// file is a config file with the content to put back on a rollback.
type file struct {
	path, content string
	secret        bool
}

// MigrateV2ToV3 upgrades an installed 2.x panel in place: backup, .env
// and compose rewrite, proxy compression, then pull and restart.
func MigrateV2ToV3() { migrateV2ToV3(ui.Reading) }

func migrateV2ToV3(ask func(string) string) {
	envPath := filepath.Join(panelDir, ".env")
	composePath := filepath.Join(panelDir, "docker-compose.yml")
	envData, envErr := os.ReadFile(envPath)
	composeData, composeErr := os.ReadFile(composePath)
	if envErr != nil || composeErr != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("MAINT_NO_PANEL"), ui.ColorReset)
		return
	}

	newEnv, envChanged := migrateEnv(string(envData))
	newCompose, composeChanged := migrateCompose(string(composeData))
	if !envChanged && !composeChanged {
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("MAINT_ALREADY_V3"), ui.ColorReset)
		return
	}

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("MAINT_MIGRATE_WARNING"), ui.ColorReset)
	if a := ask(i18n.T("MAINT_MIGRATE_CONFIRM")); a != "y" && a != "Y" {
		return
	}

	dest, err := stack.BackupPanel(panelDir)
	if err != nil {
		fmt.Printf("%s%s: %v%s\n", ui.ColorRed, i18n.T("BACKUP_FAILED"), err, ui.ColorReset)
		return
	}
	fmt.Printf("%s"+i18n.T("BACKUP_DONE")+"%s\n", ui.ColorGreen, dest, ui.ColorReset)

	// Every file the migration changes goes here with its old content, so a
	// failed pull puts the 2.x files back and the stack keeps running as it
	// did before.
	originals := []file{{envPath, string(envData), true}, {composePath, string(composeData), false}}
	restore := func() {
		for _, f := range originals {
			if err := cfgfile.Replace(f.path, f.content, cfgfile.Opts{Secret: f.secret}); err != nil {
				fmt.Printf("%s%v%s\n", ui.ColorRed, err, ui.ColorReset)
			}
		}
	}

	if err := cfgfile.Replace(envPath, newEnv, cfgfile.Opts{Secret: true}); err != nil {
		fmt.Printf("%s%v%s\n", ui.ColorRed, err, ui.ColorReset)
		return
	}
	if err := cfgfile.Replace(composePath, newCompose, cfgfile.Opts{}); err != nil {
		fmt.Printf("%s%v%s\n", ui.ColorRed, err, ui.ColorReset)
		restore()
		return
	}
	patched, restartProxy := patchProxyCompression()
	if patched != nil {
		originals = append(originals, *patched)
	}

	run := func(args ...string) error {
		cmd := execx.Command("docker", append([]string{"compose"}, args...)...)
		cmd.Dir = panelDir
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		return cmd.Run()
	}
	if err := run("pull"); err != nil {
		restore()
		fmt.Printf("%s%s%s\n", ui.ColorRed, fmt.Sprintf(i18n.T("MAINT_MIGRATE_PULL_FAILED"), err, dest), ui.ColorReset)
		return
	}
	if err := run("up", "-d", "--remove-orphans"); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, fmt.Sprintf(i18n.T("MAINT_MIGRATE_UP_FAILED"), err, dest), ui.ColorReset)
		return
	}
	// "up -d" does not recreate a container because a bind-mounted file
	// changed, so nginx would keep the old configuration.
	if restartProxy {
		if err := run("restart", "remnawave-nginx"); err != nil {
			fmt.Printf("%s%v%s\n", ui.ColorRed, err, ui.ColorReset)
		}
	}
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("MAINT_MIGRATE_DONE"), ui.ColorReset)
}

// patchProxyCompression makes sure the panel's reverse proxy compresses
// responses. It returns the original nginx.conf when it changed the file,
// and whether nginx has to restart to read the change.
func patchProxyCompression() (original *file, restartProxy bool) {
	confPath := filepath.Join(panelDir, "nginx.conf")
	if data, err := os.ReadFile(confPath); err == nil {
		patched, changed := ensureGzip(string(data))
		if !changed {
			return nil, false
		}
		if err := cfgfile.Replace(confPath, patched, cfgfile.Opts{}); err != nil {
			fmt.Printf("%s%v%s\n", ui.ColorRed, err, ui.ColorReset)
			return nil, false
		}
		return &file{confPath, string(data), false}, true
	}
	if data, err := os.ReadFile(filepath.Join(panelDir, "Caddyfile")); err == nil && !strings.Contains(string(data), "encode") {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("MAINT_CADDY_ENCODE"), ui.ColorReset)
	}
	return nil, false
}
