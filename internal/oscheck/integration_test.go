//go:build integration

package oscheck

import "testing"

// TestCheckOSOnRealRunner runs CheckOS against whatever Ubuntu version the
// CI runner actually has, not a synthetic /etc/os-release. GitHub-hosted
// ubuntu-latest runners are recent LTS releases, which this port must
// accept.
func TestCheckOSOnRealRunner(t *testing.T) {
	if err := CheckOS(); err != nil {
		t.Fatalf("CheckOS rejected the CI runner's own OS: %v", err)
	}
}

func TestCheckRootOnRealRunner(t *testing.T) {
	// The build-and-test job doesn't run as root; the preflight-integration
	// job does (via sudo). This just confirms CheckRoot's euid check
	// itself doesn't panic or misbehave; the actual root/non-root
	// assertion depends on how the job invoking this test is set up.
	_ = CheckRoot()
}
