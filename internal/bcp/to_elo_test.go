package bcp

import (
	"testing"
	"time"
)

func TestRoundTimeSpreadsRoundsAcrossEventDays(t *testing.T) {
	start := time.Date(2026, 11, 20, 14, 0, 0, 0, time.UTC)
	opt := MatchExportOptions{
		EventStart:  start,
		EventEnd:    time.Date(2026, 11, 21, 23, 0, 0, 0, time.UTC),
		TotalRounds: 8,
	}
	wantDays := []int{20, 20, 20, 20, 21, 21, 21, 21}
	var prev time.Time
	for r := 1; r <= 8; r++ {
		got := opt.RoundTime(r)
		if got.Day() != wantDays[r-1] {
			t.Fatalf("round %d: day %d, want %d (%s)", r, got.Day(), wantDays[r-1], got)
		}
		if !got.After(prev) {
			t.Fatalf("round %d not after previous round: %s <= %s", r, got, prev)
		}
		prev = got
	}
}

func TestRoundTimeSingleDayAndMissingEnd(t *testing.T) {
	start := time.Date(2026, 4, 13, 15, 0, 0, 0, time.UTC)
	opt := MatchExportOptions{EventStart: start, TotalRounds: 5}
	for r := 1; r <= 5; r++ {
		if got := opt.RoundTime(r); got.Day() != 13 {
			t.Fatalf("round %d landed on day %d, want 13", r, got.Day())
		}
	}
}

func TestPairingTableTieBreakKeepsRoundOrder(t *testing.T) {
	opt := MatchExportOptions{
		EventStart:        time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
		TotalRounds:       5,
		TieBreakSameRound: 1,
	}
	lateTable := opt.RoundTime(1).Add(500 * time.Second)
	if !opt.RoundTime(2).After(lateTable) {
		t.Fatalf("round 2 should sort after round 1 table 500")
	}
}
