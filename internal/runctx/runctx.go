// Package runctx carries the settings of one run of the tool that every
// mutating package must honor. It has no dependencies, so execx, cfgfile and
// the rest can all import it.
package runctx

import (
	"fmt"
	"sync/atomic"
)

var dryRun atomic.Bool

// SetDryRun turns the dry run on or off. main calls it once, before any flow.
func SetDryRun(on bool) { dryRun.Store(on) }

// DryRun reports whether mutating calls must only be announced.
func DryRun() bool { return dryRun.Load() }

// Say prints one planned action. The caller passes text that is already
// free of secrets.
func Say(format string, args ...any) {
	fmt.Printf("\x1b[36m[dry-run]\x1b[0m "+format+"\n", args...)
}
