package managepanel

import (
	"reflect"
	"testing"
)

func TestUniqueSortedLines(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"\n\n  \n", nil},
		{"b\na\nb\n", []string{"a", "b"}},
		{"  sha256:b \nsha256:a\nsha256:a", []string{"sha256:a", "sha256:b"}},
	}
	for _, c := range cases {
		if got := uniqueSortedLines(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: got %v, want %v", c.in, got, c.want)
		}
	}
}
