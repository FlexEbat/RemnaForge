package verify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWaitStopsOnFirstPass(t *testing.T) {
	var slept []time.Duration
	defer func(old func(time.Duration)) { sleep = old }(sleep)
	sleep = func(d time.Duration) { slept = append(slept, d) }
	calls := 0
	var failed []int
	r := Wait(func() Result {
		calls++
		return Result{Name: "x", OK: calls == 3}
	}, 5, time.Second, func(a int, _ Result) { failed = append(failed, a) })
	if !r.OK || calls != 3 || len(slept) != 2 || len(failed) != 2 || failed[1] != 2 {
		t.Errorf("ok=%v calls=%d slept=%v failed=%v", r.OK, calls, slept, failed)
	}
}

func TestWaitGivesUpAfterAttempts(t *testing.T) {
	defer func(old func(time.Duration)) { sleep = old }(sleep)
	n := 0
	sleep = func(time.Duration) { n++ }
	calls := 0
	r := Wait(func() Result { calls++; return Result{Name: "x", Detail: "down"} }, 4, time.Second, nil)
	if r.OK || calls != 4 || n != 3 || r.Detail != "down" {
		t.Errorf("ok=%v calls=%d sleeps=%d detail=%q", r.OK, calls, n, r.Detail)
	}
}

// Every install flow waits through verify and ends with verify.Finish. A
// flow that keeps its own loop would drift from what doctor reports.
func TestInstallFlowsUseVerify(t *testing.T) {
	flows := map[string][]string{
		"panelonly/install_panel.go":      {"verify.WaitForApp(", "verify.Finish("},
		"panelfull/install_panel.go":      {"verify.WaitForApp(", "verify.Finish("},
		"caddypanelonly/install_panel.go": {"verify.WaitForApp(", "verify.Finish("},
		"caddypanelfull/install_panel.go": {"verify.WaitForApp(", "verify.Finish("},
		"nginxnode/install_node.go":       {"verify.WaitForNode(", "verify.Finish("},
		"caddynode/install_node.go":       {"verify.WaitForNode(", "verify.Finish("},
	}
	for file, wants := range flows {
		b, err := os.ReadFile(filepath.Join("..", file))
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		for _, w := range wants {
			if !strings.Contains(src, w) {
				t.Errorf("%s does not call %s", file, w)
			}
		}
		for _, banned := range []string{"maxAttempts", "NODE_ATTEMPT", "CONTAINERS_NOT_READY_ATTEMPT"} {
			if strings.Contains(src, banned) {
				t.Errorf("%s still has its own wait loop (%s)", file, banned)
			}
		}
	}
}
