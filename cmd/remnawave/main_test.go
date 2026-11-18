package main

import "testing"

func TestCheckArgs(t *testing.T) {
	ok := [][]string{nil, {"doctor"}, {"versions"}, {"--version"}, {"-v"}, {"version"}, {"help"}, {"--help"}, {"-h"}}
	for _, a := range ok {
		if err := checkArgs(a); err != nil {
			t.Errorf("checkArgs(%q) = %v, want nil", a, err)
		}
	}
	bad := [][]string{{"--dryrun"}, {"--dry_run"}, {"-dry-run"}, {"install"}, {"doctor", "extra"}, {"--version", "x"}}
	for _, a := range bad {
		if err := checkArgs(a); err == nil {
			t.Errorf("checkArgs(%q) = nil, want an error", a)
		}
	}
}

func TestSplitDryRunLeavesOtherArguments(t *testing.T) {
	rest, dry := splitDryRun([]string{"--dry-run", "doctor"})
	if !dry || len(rest) != 1 || rest[0] != "doctor" {
		t.Errorf("got rest=%q dry=%v", rest, dry)
	}
}
