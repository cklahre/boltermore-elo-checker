package elodata

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"fortyk/eloevent/internal/elo40k"
)

// LeaderboardFile is written by local-elo -out-json and read by the Discord bot (or anything else).
type LeaderboardFile struct {
	AsOfRFC3339 string           `json:"as_of"`
	Players     []LeaderboardRow `json:"players"`
}

// RecentGameWire is one game for JSON consumers (GitHub Pages, APIs).
type RecentGameWire struct {
	Time     string   `json:"time"`
	Result   string   `json:"result"`
	Opponent string   `json:"opponent"`
	AsA      bool     `json:"as_a"`
	EventID  string   `json:"event_id,omitempty"`
	DeltaElo *float64 `json:"delta_elo,omitempty"`
}

// RecentEventWire is per-event rollup for GitHub Pages / JSON APIs.
type RecentEventWire struct {
	EventID       string  `json:"event_id"` // empty if source had no event_id for that bucket
	LastPlayed    string  `json:"last_played_rfc3339"`
	Wins          int     `json:"wins"`
	Losses        int     `json:"losses"`
	Draws         int     `json:"draws"`
	Games         int     `json:"games"`
	TotalDeltaElo float64 `json:"total_delta_elo"`
	DeltaGames    int     `json:"delta_games"`
}

// LeaderboardRow is one rated player after full replay + optional FinalizeDecay.
type LeaderboardRow struct {
	Rank         int               `json:"rank"`
	Name         string            `json:"name"`
	Key          string            `json:"key"`
	Elo          float64           `json:"elo"`
	Games        int               `json:"games"`
	Wins         int               `json:"wins,omitempty"`
	Losses       int               `json:"losses,omitempty"`
	Draws        int               `json:"draws,omitempty"`
	WinPct       float64           `json:"win_pct,omitempty"`
	PointsPct    float64           `json:"points_pct,omitempty"`
	RecentGames  []RecentGameWire  `json:"recent_games,omitempty"`
	RecentEvents []RecentEventWire `json:"recent_events,omitempty"`
}

// WriteLeaderboardJSON writes snapshot from the Elo engine (caller runs FinalizeDecay first as desired).
func WriteLeaderboardJSON(path string, asOf time.Time, players []elo40k.Player) error {
	rows := make([]LeaderboardRow, 0, len(players))
	for i, p := range players {
		rows = append(rows, LeaderboardRow{
			Rank:  i + 1,
			Name:  p.DisplayName,
			Key:   elo40k.PlayerKey(p.DisplayName),
			Elo:   p.Rating,
			Games: p.Games,
		})
	}
	f := LeaderboardFile{
		AsOfRFC3339: asOf.UTC().Format(time.RFC3339),
		Players:     rows,
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

// ReadLeaderboardJSON loads a file produced by WriteLeaderboardJSON.
func ReadLeaderboardJSON(path string) (*LeaderboardFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f LeaderboardFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("leaderboard json: %w", err)
	}
	return &f, nil
}

// EloByKey returns a map from PlayerKey → elo for roster / lookups.
func (f *LeaderboardFile) EloByKey() map[string]float64 {
	if f == nil {
		return nil
	}
	m := make(map[string]float64, len(f.Players))
	for _, r := range f.Players {
		k := rowStorageKey(r)
		m[k] = r.Elo
	}
	return m
}

// RowByKey returns full leaderboard rows keyed by PlayerKey (latest wins if duplicate keys).
func (f *LeaderboardFile) RowByKey() map[string]LeaderboardRow {
	if f == nil {
		return nil
	}
	m := make(map[string]LeaderboardRow, len(f.Players))
	for _, r := range f.Players {
		k := rowStorageKey(r)
		m[k] = r
	}
	return m
}

func rowStorageKey(r LeaderboardRow) string {
	if strings.TrimSpace(r.Key) != "" {
		return r.Key
	}
	return elo40k.PlayerKey(r.Name)
}

// LookupPlayerRow finds a leaderboard row for a roster or pairing display name.
// It tries exact PlayerKey first, then matches on whitespace-normalized name/key
// so "Jack  Murphy", "jack murphy", and "Jack Murphy" align across BCP payloads and JSON.
func (f *LeaderboardFile) LookupPlayerRow(name string) (LeaderboardRow, bool) {
	if f == nil {
		return LeaderboardRow{}, false
	}
	if strings.TrimSpace(name) == "" || name == "(unnamed)" {
		return LeaderboardRow{}, false
	}
	exact := elo40k.PlayerKey(name)
	for _, r := range f.Players {
		if rowStorageKey(r) == exact {
			return r, true
		}
	}
	want := elo40k.PlayerMatchKey(name)
	if want == "" {
		return LeaderboardRow{}, false
	}
	for _, r := range f.Players {
		if elo40k.PlayerMatchKey(r.Name) == want {
			return r, true
		}
		if strings.TrimSpace(r.Key) != "" && elo40k.PlayerMatchKey(r.Key) == want {
			return r, true
		}
	}
	return LeaderboardRow{}, false
}
