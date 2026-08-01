// Package oscheck is a port of check_os() (install_remnawave.sh:90-94):
//
//	check_os() {
//	    if ! grep -q "bullseye" /etc/os-release && ! grep -q "bookworm" /etc/os-release && \
//	       ! grep -q "jammy" /etc/os-release && ! grep -q "noble" /etc/os-release && \
//	       ! grep -q "trixie" /etc/os-release; then
//	        error "${LANG[ERROR_OS]}"
//	    fi
//	}
//
// Deliberate improvement over a literal port (per project decision to not
// force 1:1 where there's a better option): the original hardcodes five
// specific codenames (Debian 11 "bullseye", 12 "bookworm", 13 "trixie";
// Ubuntu 22.04 "jammy", 24.04 "noble") and has to be hand-edited every time
// a new Debian/Ubuntu release ships - which is exactly the bug being fixed
// here (the bundled LANG[ERROR_OS] message still said "Debian 11/12 and
// Ubuntu 22.04/24.04", already stale since "trixie" - Debian 13 - was in
// the check but not the message, and neither covered Ubuntu 26.04
// "resolute"/Debian 13.x point releases).
//
// Instead of matching codenames, this parses /etc/os-release's ID and
// VERSION_ID and compares version numbers: Debian >= 11, Ubuntu >= 22.04.
// That covers everything the original's codename list did (bullseye,
// bookworm, trixie, jammy, noble), plus Ubuntu 26.04 "resolute" and any
// Debian 13.x/future point or major release, without needing another edit
// every two years.
package oscheck

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
)

const osReleasePath = "/etc/os-release"

const (
	minDebianMajor = 11
	minUbuntuMajor = 22
	minUbuntuMinor = 4 // "22.04" -> major 22, minor 04
)

// parseOSRelease reads ID and VERSION_ID out of /etc/os-release (a simple
// KEY=VALUE file, values optionally double-quoted).
func parseOSRelease(path string) (id, versionID string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		val = strings.Trim(val, `"`)
		switch key {
		case "ID":
			id = val
		case "VERSION_ID":
			versionID = val
		}
	}
	return id, versionID, scanner.Err()
}

// CheckOS is the Go equivalent of check_os(). It returns an error (instead
// of bash's error()+exit 1) so the caller can decide how to present it;
// callers that want the original's "print in red and exit" behavior can do
// so themselves at the top level.
func CheckOS() error {
	id, versionID, err := parseOSRelease(osReleasePath)
	if err != nil {
		return fmt.Errorf("%s", i18n.T("ERROR_OS"))
	}

	if supported(id, versionID) {
		return nil
	}
	return fmt.Errorf("%s", i18n.T("ERROR_OS"))
}

func supported(id, versionID string) bool {
	switch strings.ToLower(id) {
	case "debian":
		major, _, ok := parseVersion(versionID)
		return ok && major >= minDebianMajor

	case "ubuntu":
		major, minor, ok := parseVersion(versionID)
		if !ok {
			return false
		}
		if major != minUbuntuMajor {
			return major > minUbuntuMajor
		}
		return minor >= minUbuntuMinor

	default:
		return false
	}
}

// parseVersion turns "24.04" into (24, 4, true), "13" into (13, 0, true).
// Extra segments beyond major.minor (e.g. a hypothetical "13.6.0") are
// ignored rather than causing a parse failure - only major.minor matters
// for the comparisons in supported().
func parseVersion(versionID string) (major, minor int, ok bool) {
	parts := strings.Split(versionID, ".")
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	if len(parts) > 1 {
		minor, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, false
		}
	}
	return major, minor, true
}

// Original bash (install_remnawave.sh:96-100):
//
//	check_root() {
//	    if [[ $EUID -ne 0 ]]; then
//	        error "${LANG[ERROR_ROOT]}"
//	    fi
//	}
func CheckRoot() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("%s", i18n.T("ERROR_ROOT"))
	}
	return nil
}
