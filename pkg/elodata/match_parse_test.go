package elodata

import (
	"testing"
	"time"

	"fortyk/eloevent/internal/bcp"
)

func TestFilterRowsSince(t *testing.T) {
	rows := []bcp.MatchFileRow{
		{Date: "2026-06-19T23:59:59Z", A: "a", B: "b", Winner: "a"},
		{Date: "2026-06-20T00:00:00Z", A: "a", B: "c", Winner: "a"},
		{Date: "2026-11-01T12:00:00Z", A: "b", B: "c", Winner: "draw"},
	}
	got, err := FilterRowsSince(rows, time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].B != "c" {
		t.Fatalf("want the two rows from 2026-06-20 on, got %+v", got)
	}
}
