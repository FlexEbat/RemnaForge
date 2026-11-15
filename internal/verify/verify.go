// Package verify holds the named post-install checks and the report built
// from them. Install flows call Run at the end and maintenance.Doctor calls
// it over the whole installation, so the same state prints the same lines
// in both places.
package verify

import (
	"fmt"
	"strings"

	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// Result is the outcome of one named check.
type Result struct {
	Name    string
	OK      bool
	Skipped bool   // the check does not apply here, and counts as neither pass nor fail
	Detail  string // what was found
	Remedy  string // what to do, set on a failure
}

// Line renders r as the line printed to the terminal and written to the
// report. A failure with a remedy puts the remedy on the next line.
func (r Result) Line(color bool) string {
	mark, c := "[ ok ]", ui.ColorGreen
	switch {
	case r.Skipped:
		mark, c = "[skip]", ui.ColorGray
	case !r.OK:
		mark, c = "[fail]", ui.ColorRed
	}
	var b strings.Builder
	if color {
		b.WriteString(c + mark + ui.ColorReset)
	} else {
		b.WriteString(mark)
	}
	b.WriteString(" " + r.Name)
	if r.Detail != "" {
		if color {
			b.WriteString(" " + ui.ColorGray + "- " + r.Detail + ui.ColorReset)
		} else {
			b.WriteString(" - " + r.Detail)
		}
	}
	if !r.OK && !r.Skipped && r.Remedy != "" {
		b.WriteString("\n       " + r.Remedy)
	}
	return b.String()
}

// Print writes r to the terminal.
func (r Result) Print() { fmt.Println(r.Line(true)) }

// Failed counts the results that failed. Skipped results do not count.
func Failed(results []Result) int {
	n := 0
	for _, r := range results {
		if !r.OK && !r.Skipped {
			n++
		}
	}
	return n
}

func pass(name, detail string) Result { return Result{Name: name, OK: true, Detail: detail} }

func fail(name, detail, remedy string) Result {
	return Result{Name: name, Detail: detail, Remedy: remedy}
}

func skip(name, detail string) Result {
	return Result{Name: name, Skipped: true, OK: true, Detail: detail}
}
