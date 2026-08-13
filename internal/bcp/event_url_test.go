package bcp

import "testing"

func TestParseEventID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"FevLNTNDipqC", "FevLNTNDipqC"},
		{"https://www.bestcoastpairings.com/event/MveGm9kfjwiQ", "MveGm9kfjwiQ"},
		{"https://www.bestcoastpairings.com/event/MveGm9kfjwiQ?active_tab=roster", "MveGm9kfjwiQ"},
		{"  MveGm9kfjwiQ  ", "MveGm9kfjwiQ"},
	}
	for _, tc := range cases {
		if got := ParseEventID(tc.in); got != tc.want {
			t.Fatalf("%q → %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestEventPageURL(t *testing.T) {
	got := EventPageURL("FevLNTNDipqC")
	want := "https://www.bestcoastpairings.com/event/FevLNTNDipqC"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if EventPageURL("  ") != "" {
		t.Fatal("empty id should yield empty URL")
	}
}

func TestEventMarkdownLink(t *testing.T) {
	got := EventMarkdownLink("7e2r7AbBqeR8")
	want := "[7e2r7AbBqeR8](https://www.bestcoastpairings.com/event/7e2r7AbBqeR8)"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if EventMarkdownLink("") != "—" {
		t.Fatal("empty id should be em dash")
	}
}
