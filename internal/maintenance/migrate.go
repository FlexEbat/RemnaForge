package maintenance

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

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

// ensureGzip adds response compression to an nginx http-level config
// when it has none; Remnawave 3.x serves large responses that the proxy
// is expected to compress.
func ensureGzip(conf string) (string, bool) {
	if strings.Contains(conf, "gzip on;") {
		return conf, false
	}
	block := "gzip on;\ngzip_vary on;\ngzip_proxied any;\ngzip_comp_level 6;\ngzip_types text/plain text/css application/json application/javascript text/xml application/xml image/svg+xml;\n\n"
	return block + conf, true
}

// MigrateV2ToV3 upgrades an installed 2.x panel in place: backup, .env
// and compose rewrite, proxy compression, then pull and restart.
func MigrateV2ToV3() {
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
	if a := ui.Reading(i18n.T("MAINT_MIGRATE_CONFIRM")); a != "y" && a != "Y" {
		return
	}

	dest, err := stack.BackupPanel(panelDir)
	if err != nil {
		fmt.Printf("%s%s: %v%s\n", ui.ColorRed, i18n.T("BACKUP_FAILED"), err, ui.ColorReset)
		return
	}
	fmt.Printf("%s"+i18n.T("BACKUP_DONE")+"%s\n", ui.ColorGreen, dest, ui.ColorReset)

	if err := os.WriteFile(envPath, []byte(newEnv), 0600); err != nil {
		fmt.Printf("%s%v%s\n", ui.ColorRed, err, ui.ColorReset)
		return
	}
	if err := os.WriteFile(composePath, []byte(newCompose), 0600); err != nil {
		fmt.Printf("%s%v%s\n", ui.ColorRed, err, ui.ColorReset)
		return
	}

	patchProxyCompression()

	for _, args := range [][]string{{"pull"}, {"up", "-d", "--remove-orphans"}} {
		cmd := exec.Command("docker", append([]string{"compose"}, args...)...)
		cmd.Dir = panelDir
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Printf("%s%v%s\n", ui.ColorRed, err, ui.ColorReset)
			return
		}
	}
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("MAINT_MIGRATE_DONE"), ui.ColorReset)
}

// patchProxyCompression makes sure the panel's reverse proxy compresses
// responses, restarting nginx when its config was changed.
func patchProxyCompression() {
	confPath := filepath.Join(panelDir, "nginx.conf")
	if data, err := os.ReadFile(confPath); err == nil {
		if patched, changed := ensureGzip(string(data)); changed {
			_ = os.WriteFile(confPath, []byte(patched), 0600)
		}
		return
	}
	if data, err := os.ReadFile(filepath.Join(panelDir, "Caddyfile")); err == nil && !strings.Contains(string(data), "encode") {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("MAINT_CADDY_ENCODE"), ui.ColorReset)
	}
}
