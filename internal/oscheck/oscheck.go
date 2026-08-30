// Package oscheck checks that the host OS is a supported Debian or
// Ubuntu release.
//
// Rather than matching a hardcoded list of codenames (which needs a
// hand-edit every time a new Debian/Ubuntu release ships), this parses
// /etc/os-release's ID and VERSION_ID and compares version numbers:
// Debian >= 11, Ubuntu >= 22.04. That covers every release in the
// classic codename set (bullseye, bookworm, trixie, jammy, noble) plus
// anything newer, without needing another edit every two years.
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

// CheckOS checks that the host OS is a supported release. It returns an
// error rather than printing and exiting itself, so the caller can
// decide how to present it; a caller that wants a "print in red and
// exit" behavior can do so at the top level.
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
// It ignores extra segments beyond major.minor (a hypothetical "13.6.0")
// instead of failing to parse them, since supported() only compares
// major.minor.
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

// CheckRoot returns an error unless the process is running as root
// (euid 0).
func CheckRoot() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("%s", i18n.T("ERROR_ROOT"))
	}
	return nil
}
