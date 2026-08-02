package certs

import (
	"regexp"
	"sort"
	"strconv"
)

var chunkRE = regexp.MustCompile(`\d+|\D+`)

// natLess replicates `sort -V` well enough for our purposes: split each
// string into runs of digits vs. non-digits, compare digit runs
// numerically and non-digit runs lexically. Used everywhere the original
// piped `find ... | sort -V | tail -n 1` to pick the "latest" matching
// directory/version (e.g. "example.com", "example.com-0001", ...).
func natLess(a, b string) bool {
	ac := chunkRE.FindAllString(a, -1)
	bc := chunkRE.FindAllString(b, -1)

	for i := 0; i < len(ac) && i < len(bc); i++ {
		if ac[i] == bc[i] {
			continue
		}
		an, aErr := strconv.Atoi(ac[i])
		bn, bErr := strconv.Atoi(bc[i])
		if aErr == nil && bErr == nil {
			return an < bn
		}
		return ac[i] < bc[i]
	}
	return len(ac) < len(bc)
}

// sortNatural sorts a slice of strings in place using natLess.
func sortNatural(items []string) {
	sort.Slice(items, func(i, j int) bool { return natLess(items[i], items[j]) })
}
