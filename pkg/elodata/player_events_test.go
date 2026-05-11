package elodata

import (
	"strings"
	"testing"

	"fortyk/eloevent/internal/bcp"
)

func TestPlayerLookup_RecentEventsOrdering(t *testing.T) {
	rows := []bcp.MatchFileRow{
		{Date: "2025-06-02T18:00:00Z", A: "Ada", B: "Bea", Winner: "a", EventID: "newest_evt"},
		{Date: "2025-06-01T18:00:00Z", A: "Ada", B: "Cee", Winner: "b", EventID: "middle_evt"},
		{Date: "2025-05-01T18:00:00Z", A: "Ada", B: "Dee", Winner: "draw", EventID: "oldest_evt"},
	}
	rep, err := PlayerLookup(rows, "Ada", false, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil {
		t.Fatal("nil report")
	}
	if len(rep.RecentEvents) != 3 {
		t.Fatalf("RecentEvents len got %d want 3", len(rep.RecentEvents))
	}
	if rep.RecentEvents[0].EventID != "newest_evt" {
		t.Fatalf("first event %q want newest_evt", rep.RecentEvents[0].EventID)
	}
	if rep.RecentEvents[0].Wins != 1 || rep.RecentEvents[0].Losses != 0 || rep.RecentEvents[0].Draws != 0 {
		t.Fatalf("newest record wrong %+v", rep.RecentEvents[0])
	}
	if rep.RecentEvents[2].EventID != "oldest_evt" {
		t.Fatalf("third event %q want oldest_evt", rep.RecentEvents[2].EventID)
	}
}

func TestPlayerLookup_RecentEventsEmptyIDMerged(t *testing.T) {
	rows := []bcp.MatchFileRow{
		{Date: "2025-06-01T18:00:00Z", A: "Ada", B: "Bea", Winner: "a", EventID: " "},
		{Date: "2025-05-02T18:00:00Z", A: "Ada", B: "Ce", Winner: "b"},
	}
	rep, err := PlayerLookup(rows, "Ada", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.RecentEvents) != 1 {
		t.Fatalf("want 1 bucket for blanks, got %d: %+v", len(rep.RecentEvents), rep.RecentEvents)
	}
	if strings.TrimSpace(rep.RecentEvents[0].EventID) != "" {
		t.Fatalf("want empty merged id got %q", rep.RecentEvents[0].EventID)
	}
	if rep.RecentEvents[0].Games != 2 {
		t.Fatalf("games %d want 2", rep.RecentEvents[0].Games)
	}
}
