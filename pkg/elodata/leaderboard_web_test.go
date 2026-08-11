package elodata

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"fortyk/eloevent/internal/bcp"
	"fortyk/eloevent/internal/elo40k"
)

func TestBuildLeaderboardWebRows_SinglePass(t *testing.T) {
	rows := []bcp.MatchFileRow{
		{Date: "2026-01-01T12:00:00Z", A: "Ada", B: "Bob", Winner: "a", EventID: "e1", PairingID: "p1"},
		{Date: "2026-01-02T12:00:00Z", A: "Ada", B: "Cara", Winner: "b", EventID: "e2", PairingID: "p2"},
		{Date: "2026-01-03T12:00:00Z", A: "Bob", B: "Cara", Winner: "draw", EventID: "e2", PairingID: "p3"},
	}
	e := elo40k.NewEngine()
	var ms []elo40k.Match
	for i := range rows {
		m, err := RowToMatch(&rows[i])
		if err != nil {
			t.Fatal(err)
		}
		ms = append(ms, m)
	}
	e.PlayAll(ms)
	e.FinalizeDecay(time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC))
	snap := e.Snapshot()

	out, err := buildLeaderboardWebRows(snap, rows, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(snap) {
		t.Fatalf("rows %d snap %d", len(out), len(snap))
	}

	var ada *LeaderboardRow
	for i := range out {
		if out[i].Name == "Ada" {
			ada = &out[i]
			break
		}
	}
	if ada == nil {
		t.Fatal("Ada missing")
	}
	if ada.Wins != 1 || ada.Losses != 1 || ada.Draws != 0 {
		t.Fatalf("Ada record %d-%d-%d", ada.Wins, ada.Losses, ada.Draws)
	}
	if len(ada.RecentGames) != 2 {
		t.Fatalf("Ada recent games %d", len(ada.RecentGames))
	}
	if ada.RecentGames[0].Opponent != "Cara" {
		t.Fatalf("newest opponent %q", ada.RecentGames[0].Opponent)
	}
}

func TestWriteLeaderboardWebDir_Smoke(t *testing.T) {
	dir := t.TempDir()
	rows := []bcp.MatchFileRow{
		{Date: "2026-01-01T12:00:00Z", A: "Ada", B: "Bob", Winner: "a", EventID: "e1", PairingID: "p1"},
	}
	e := elo40k.NewEngine()
	m, err := RowToMatch(&rows[0])
	if err != nil {
		t.Fatal(err)
	}
	e.PlayAll([]elo40k.Match{m})
	snap := e.Snapshot()
	if err := WriteLeaderboardWebDir(dir, time.Now().UTC(), snap, rows, 5, 50); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "index.json")); err != nil {
		t.Fatal(err)
	}
}
