package reinstall

import "testing"

func TestEveryReinstallTypeHasAHAProxyFlow(t *testing.T) {
	for _, option := range []string{"1", "2", "3"} {
		if installHAProxy(option) == nil {
			t.Errorf("reinstall type %s has no HAProxy flow", option)
		}
	}
	if installHAProxy("0") != nil || installHAProxy("") != nil {
		t.Error("an unknown reinstall type has a flow")
	}
}
