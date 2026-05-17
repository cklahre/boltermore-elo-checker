package bcp

import (
	"testing"
)

func TestRosterPlayerFullName_fallbackToRootFields(t *testing.T) {
	p := RosterPlayer{
		FirstName: "Jack",
		LastName:  "Murphy",
	}
	if got := p.FullName(); got != "Jack Murphy" {
		t.Fatalf("FullName: got %q", got)
	}
}

func TestRosterPlayerFullName_nestedUserOverridesRoot(t *testing.T) {
	p := RosterPlayer{
		FirstName: "Ignore",
		LastName:  "Me",
		User:      &bcpUser{FirstName: "Jack", LastName: "Murphy"},
	}
	if got := p.FullName(); got != "Jack Murphy" {
		t.Fatalf("FullName: got %q want Jack Murphy", got)
	}
}
