package main

import (
	"reflect"
	"testing"
)

// A flag that takes a value keeps the word after it, so "--port 3000" is never
// read as a positional request ID or profile.
func TestSplitArgsKeepsValuesWithTheirFlags(t *testing.T) {
	flags, positional := splitArgs([]string{"req-1", "--port", "3000", "--all", "--local", "9000", "--profile", "m2"})
	if want := []string{"--port", "3000", "--all", "--local", "9000", "--profile", "m2"}; !reflect.DeepEqual(flags, want) {
		t.Fatalf("flags = %v, want %v", flags, want)
	}
	if want := []string{"req-1"}; !reflect.DeepEqual(positional, want) {
		t.Fatalf("positional = %v, want %v", positional, want)
	}
}

func TestSplitArgsAcceptsPositionalsAfterFlags(t *testing.T) {
	flags, positional := splitArgs([]string{"--all", "--profile", "m2", "3000", "5173"})
	if !reflect.DeepEqual(flags, []string{"--all", "--profile", "m2"}) || !reflect.DeepEqual(positional, []string{"3000", "5173"}) {
		t.Fatalf("flags %v positional %v", flags, positional)
	}
}

func TestApproveNeedsExactlyOneSelector(t *testing.T) {
	if err := runApprove([]string{"req-1"}); err == nil {
		t.Fatal("approve without --all or --port was accepted")
	}
	if err := runApprove([]string{"req-1", "--all", "--port", "3000"}); err == nil {
		t.Fatal("approve with both --all and --port was accepted")
	}
}
