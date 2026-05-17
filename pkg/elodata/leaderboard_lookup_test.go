package elodata

import (
	"testing"
)

func TestLookupPlayerRow_WhitespaceVariants(t *testing.T) {
	f := &LeaderboardFile{
		Players: []LeaderboardRow{
			{Rank: 3, Name: "Jack Murphy", Key: "jack murphy", Elo: 1620.5, Games: 12},
		},
	}

	for _, nm := range []string{"Jack Murphy", "Jack  Murphy", "  jack murphy "} {
		r, ok := f.LookupPlayerRow(nm)
		if !ok {
			t.Fatalf("%q: expected match", nm)
		}
		if r.Elo != 1620.5 {
			t.Fatalf("%q Elo=%v want 1620.5", nm, r.Elo)
		}
	}
}
