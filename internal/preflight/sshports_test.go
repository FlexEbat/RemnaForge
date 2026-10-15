package preflight

import (
	"reflect"
	"testing"
)

func TestPortFromSSHConnection(t *testing.T) {
	tests := []struct {
		in   string
		want int
		ok   bool
	}{
		{"203.0.113.5 51234 198.51.100.7 2222", 2222, true},
		{"2001:db8::1 51234 2001:db8::2 22", 22, true},
		{"", 0, false},
		{"only three fields here", 0, false},
		{"a 1 b 70000", 0, false},
		{"a 1 b notaport", 0, false},
	}
	for _, tc := range tests {
		got, ok := portFromSSHConnection(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("portFromSSHConnection(%q) = %d, %v; want %d, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestParseSSHDPorts(t *testing.T) {
	out := "port 2222\nport 22\nlistenaddress 0.0.0.0\nportable 5\nport 0\nport abc\n"
	got := parseSSHDPorts(out)
	if want := []int{2222, 22}; !reflect.DeepEqual(got, want) {
		t.Errorf("parseSSHDPorts = %v, want %v", got, want)
	}
}

func TestUniquePorts(t *testing.T) {
	if got := uniquePorts(nil); !reflect.DeepEqual(got, []int{22}) {
		t.Errorf("empty input = %v, want [22]", got)
	}
	if got := uniquePorts([]int{2222, 22, 2222}); !reflect.DeepEqual(got, []int{2222, 22}) {
		t.Errorf("dedup = %v, want [2222 22]", got)
	}
}
