package bcp

import (
	"testing"
	"time"
)

func legacyRow(start time.Time, eventID string, round, table int) MatchFileRow {
	t := start.Add(time.Duration(round-1)*24*time.Hour + time.Duration(table)*time.Second)
	return MatchFileRow{Date: t.Format(time.RFC3339), A: "a", B: "b", Winner: "a", EventID: eventID}
}

func TestRedateLegacyShardsCompressesRoundsIntoEventSpan(t *testing.T) {
	start := time.Date(2026, 11, 20, 14, 0, 0, 0, time.UTC)
	ev := &Event{
		ID:             "ev1",
		EventDate:      start.Format(time.RFC3339),
		EventEndDate:   time.Date(2026, 11, 21, 23, 0, 0, 0, time.UTC).Format(time.RFC3339),
		NumberOfRounds: 8,
	}
	var rows []MatchFileRow
	for r := 1; r <= 8; r++ {
		rows = append(rows, legacyRow(start, "ev1", r, 3))
	}
	shards := [][]MatchFileRow{rows}

	st := RedateLegacyShards(shards, map[string]*Event{"ev1": ev})
	if st.Changed != 7 || st.EventsChanged != 1 {
		t.Fatalf("stats: %+v", st)
	}
	wantDays := []int{20, 20, 20, 20, 21, 21, 21, 21}
	for i, r := range shards[0] {
		got, _ := time.Parse(time.RFC3339, r.Date)
		if got.Day() != wantDays[i] {
			t.Fatalf("round %d: got %s, want day %d", i+1, r.Date, wantDays[i])
		}
	}

	again := RedateLegacyShards(shards, map[string]*Event{"ev1": ev})
	if again.Changed != 0 {
		t.Fatalf("second pass should be a no-op, changed %d", again.Changed)
	}
}

func TestRedateLegacyShardsFillsEventNames(t *testing.T) {
	start := time.Date(2026, 4, 13, 15, 0, 0, 0, time.UTC)
	ev := &Event{ID: "ev3", Name: "Spring Open", EventDate: start.Format(time.RFC3339), NumberOfRounds: 5}
	kept := legacyRow(start, "ev3", 2, 1)
	kept.EventName = "Already Set"
	shards := [][]MatchFileRow{{legacyRow(start, "ev3", 1, 1), kept}}
	st := RedateLegacyShards(shards, map[string]*Event{"ev3": ev})
	if st.NamesFilled != 1 || shards[0][0].EventName != "Spring Open" || shards[0][1].EventName != "Already Set" {
		t.Fatalf("names: filled=%d %q %q", st.NamesFilled, shards[0][0].EventName, shards[0][1].EventName)
	}
}

func TestRedateLegacyShardsSkipsEventsWithoutMeta(t *testing.T) {
	start := time.Date(2026, 4, 13, 15, 0, 0, 0, time.UTC)
	shards := [][]MatchFileRow{{legacyRow(start, "unknown", 3, 1)}}
	before := shards[0][0].Date
	st := RedateLegacyShards(shards, map[string]*Event{})
	if st.EventsMissingMeta != 1 || shards[0][0].Date != before {
		t.Fatalf("expected untouched row, stats %+v date %s", st, shards[0][0].Date)
	}
}

func TestRedateLegacyShardsConsistentAcrossShards(t *testing.T) {
	start := time.Date(2026, 4, 13, 15, 0, 0, 0, time.UTC)
	ev := &Event{ID: "ev2", EventDate: start.Format(time.RFC3339), NumberOfRounds: 5}
	shards := [][]MatchFileRow{
		{legacyRow(start, "ev2", 1, 1), legacyRow(start, "ev2", 4, 2)},
		{legacyRow(start, "ev2", 4, 2)},
	}
	RedateLegacyShards(shards, map[string]*Event{"ev2": ev})
	if shards[0][1].Date != shards[1][0].Date {
		t.Fatalf("duplicate rows diverged: %s vs %s", shards[0][1].Date, shards[1][0].Date)
	}
	got, _ := time.Parse(time.RFC3339, shards[0][1].Date)
	if got.Day() != 13 {
		t.Fatalf("single-day event round 4 landed on %s", shards[0][1].Date)
	}
}
