package certs

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/FlexEbat/RemnaForge/internal/fsx"
)

// HAProxy reads one file per name with the certificate chain and the
// private key together. The bundles live apart from certbot's live
// directory, which certbot owns, and the container mounts only them, so it
// never sees the private keys of other names.
var (
	HAProxyBundleDir = "/etc/letsencrypt/haproxy"
	// HAProxyHookPath is the script certbot runs after a renewal.
	HAProxyHookPath = "/usr/local/remnawave_reverse/haproxy-renew-hook.sh"
)

// HAProxyBundlePath is the bundle of a certificate name.
func HAProxyBundlePath(name string) string {
	return filepath.Join(HAProxyBundleDir, name+".pem")
}

// WriteHAProxyBundle joins the fullchain and the private key of name from
// liveDir into its bundle, at mode 0600 in a 0700 directory. The write goes
// through a temporary file and a rename, so HAProxy never reads half a
// bundle.
func WriteHAProxyBundle(liveDir, name string) error {
	chain, err := os.ReadFile(filepath.Join(liveDir, name, "fullchain.pem"))
	if err != nil {
		return err
	}
	key, err := os.ReadFile(filepath.Join(liveDir, name, "privkey.pem"))
	if err != nil {
		return err
	}
	if err := fsx.MkdirAll(HAProxyBundleDir, 0o700); err != nil {
		return err
	}
	if len(chain) > 0 && chain[len(chain)-1] != '\n' {
		chain = append(chain, '\n')
	}
	tmp := HAProxyBundlePath(name) + ".tmp"
	if err := fsx.WriteFile(tmp, append(chain, key...), 0o600); err != nil {
		return err
	}
	return fsx.Rename(tmp, HAProxyBundlePath(name))
}

// haproxyHookScript rebuilds every bundle that exists from certbot's live
// directory, then asks each HAProxy container to reload with SIGUSR2 (the
// master process starts a new worker and retires the old one, so no
// connection is cut). LIVE_DIR, BUNDLE_DIR and STACK_DIRS are overridable
// for the tests.
const haproxyHookScript = `#!/bin/sh
# Written by RemnaForge. certbot runs it after each renewal.
set -eu
LIVE_DIR="${LIVE_DIR:-/etc/letsencrypt/live}"
BUNDLE_DIR="${BUNDLE_DIR:-/etc/letsencrypt/haproxy}"
STACK_DIRS="${STACK_DIRS:-/opt/remnawave /opt/remnanode}"

for bundle in "$BUNDLE_DIR"/*.pem; do
    [ -e "$bundle" ] || continue
    name="$(basename "$bundle" .pem)"
    chain="$LIVE_DIR/$name/fullchain.pem"
    key="$LIVE_DIR/$name/privkey.pem"
    [ -f "$chain" ] && [ -f "$key" ] || continue
    tmp="$bundle.tmp"
    umask 077
    cat "$chain" "$key" > "$tmp"
    mv "$tmp" "$bundle"
done

for dir in $STACK_DIRS; do
    [ -f "$dir/docker-compose.yml" ] || continue
    (cd "$dir" && docker compose kill -s USR2 remnawave-haproxy >/dev/null 2>&1) || true
done
`

// InstallHAProxyHook writes the renew script, executable by root only.
func InstallHAProxyHook() error {
	if err := fsx.MkdirAll(filepath.Dir(HAProxyHookPath), 0o755); err != nil {
		return err
	}
	return fsx.WriteFile(HAProxyHookPath, []byte(haproxyHookScript), 0o700)
}

// haproxyRenewHook is the renew_hook line for a name that has a bundle.
func haproxyRenewHook() string { return "renew_hook = " + HAProxyHookPath }

// renewHookFor returns the renew_hook line of a certificate name: the
// HAProxy script when the name has a bundle, the nginx restart otherwise.
// Both fixRenewHook and FixLetsencryptStructure ask here, so repairing a
// certificate's structure cannot swap an HAProxy hook for the nginx one.
func renewHookFor(name string) string {
	if _, err := os.Stat(HAProxyBundlePath(name)); err == nil {
		return haproxyRenewHook()
	}
	return renewHookCommand
}

func validateBundleName(name string) error {
	if name == "" || filepath.Base(name) != name || name == "." || name == ".." {
		return fmt.Errorf("invalid certificate name %q", name)
	}
	return nil
}
