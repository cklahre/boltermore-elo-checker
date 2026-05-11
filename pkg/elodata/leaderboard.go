package elodata

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"fortyk/eloevent/internal/elo40k"
)

// LeaderboardFile is written by local-elo -out-json and read by the Discord bot (or anything else).
type LeaderboardFile struct {
	AsOfRFC3339 string           `json:"as_of"`
	Players     []LeaderboardRow `json:"players"`
}

// LeaderboardRow is one rated player after full replay + optional FinalizeDecay.
type LeaderboardRow struct {
	Rank  int     `json:"rank"`
	Name  string  `json:"name"`
	Key   string  `json:"key"`
	Elo   float64 `json:"elo"`
	Games int     `json:"games"`
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
		k := r.Key
		if k == "" {
			k = elo40k.PlayerKey(r.Name)
		}
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
		k := r.Key
		if k == "" {
			k = elo40k.PlayerKey(r.Name)
		}
		m[k] = r
	}
	return m
}
