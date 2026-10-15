package certs

import (
	"reflect"
	"testing"
)

func TestSortNatural(t *testing.T) {
	got := []string{"example.com-0010", "example.com-0002", "example.com", "example.com-0001"}
	sortNatural(got)
	want := []string{"example.com", "example.com-0001", "example.com-0002", "example.com-0010"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sortNatural = %v, want %v", got, want)
	}
}

func TestNatLessComparesNumbersNumerically(t *testing.T) {
	if !natLess("a2", "a10") {
		t.Error("a2 should sort before a10")
	}
	if natLess("a10", "a2") {
		t.Error("a10 should not sort before a2")
	}
}
