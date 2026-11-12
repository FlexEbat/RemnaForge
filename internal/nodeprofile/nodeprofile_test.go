package nodeprofile

import "testing"

func TestToggleOptionDeprecated(t *testing.T) {
	off, on := false, true

	if toggleOption(inboundOption{checked: &off, deprecated: true}) || off {
		t.Fatal("a deprecated option that is off must stay off")
	}
	if !toggleOption(inboundOption{checked: &on, deprecated: true}) || on {
		t.Fatal("a deprecated option that is on must be switchable off")
	}
}

func TestToggleOptionRegular(t *testing.T) {
	v := false
	if !toggleOption(inboundOption{checked: &v}) || !v {
		t.Fatal("a regular option must switch on")
	}
	if !toggleOption(inboundOption{checked: &v}) || v {
		t.Fatal("a regular option must switch off")
	}
}
