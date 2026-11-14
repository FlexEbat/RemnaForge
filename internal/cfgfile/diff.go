package cfgfile

import (
	"fmt"
	"strings"
)

const maxDiffLines = 80

// Diff returns a line diff from old to new: "-" lines only the file has,
// "+" lines only the installer's version has, two lines of context around
// each change. The output is cut after maxDiffLines lines.
func Diff(old, new string) string {
	a := strings.Split(strings.TrimSuffix(old, "\n"), "\n")
	b := strings.Split(strings.TrimSuffix(new, "\n"), "\n")
	if len(a)*len(b) > 4_000_000 {
		return fmt.Sprintf("(files differ: %d and %d lines)", len(a), len(b))
	}
	// longest common subsequence table
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	type line struct {
		op   byte
		text string
	}
	var all []line
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			all = append(all, line{' ', a[i]})
			i++
			j++
		case i < len(a) && (j == len(b) || lcs[i+1][j] >= lcs[i][j+1]):
			all = append(all, line{'-', a[i]})
			i++
		default:
			all = append(all, line{'+', b[j]})
			j++
		}
	}
	near := make([]bool, len(all))
	for k, l := range all {
		if l.op != ' ' {
			for d := -2; d <= 2; d++ {
				if k+d >= 0 && k+d < len(all) {
					near[k+d] = true
				}
			}
		}
	}
	var out []string
	skipped := false
	for k, l := range all {
		if !near[k] {
			skipped = true
			continue
		}
		if skipped && len(out) > 0 {
			out = append(out, "...")
		}
		skipped = false
		out = append(out, string(l.op)+" "+l.text)
	}
	if len(out) > maxDiffLines {
		out = append(out[:maxDiffLines], fmt.Sprintf("... (%d more lines)", len(out)-maxDiffLines))
	}
	return strings.Join(out, "\n")
}
