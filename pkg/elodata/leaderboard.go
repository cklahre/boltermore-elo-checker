package elodata

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"fortyk/eloevent/internal/bcp"
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

// LeaderboardRow is one rated player after full replay + optional FinalizeDecay.
type LeaderboardRow struct {
	Rank        int              `json:"rank"`
	Name        string           `json:"name"`
	Key         string           `json:"key"`
	Elo         float64          `json:"elo"`
	Games       int              `json:"games"`
	RecentGames []RecentGameWire `json:"recent_games,omitempty"`
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

// WriteLeaderboardWebJSON writes the same leaderboard as WriteLeaderboardJSON plus recent_games per player,
// using the same delta rules as player-history.
func WriteLeaderboardWebJSON(path string, asOf time.Time, snap []elo40k.Player, matchRows []bcp.MatchFileRow, recentN int) error {
	byPairing, byLine, err := ComputeMatchDeltas(matchRows)
	if err != nil {
		return err
	}
	rows := make([]LeaderboardRow, 0, len(snap))
	for i, p := range snap {
		rep, err := PlayerLookupWithDeltas(matchRows, p.DisplayName, false, recentN, byPairing, byLine)
		if err != nil {
			return fmt.Errorf("%s: %w", p.DisplayName, err)
		}
		var recent []RecentGameWire
		if rep != nil {
			recent = make([]RecentGameWire, len(rep.Games))
			for j, g := range rep.Games {
				recent[j] = RecentGameWire{
					Time:     g.Time.UTC().Format(time.RFC3339),
					Result:   string([]byte{g.Result}),
					Opponent: g.Opponent,
					AsA:      g.AsA,
					EventID:  g.EventID,
					DeltaElo: g.DeltaElo,
				}
			}
		}
		rows = append(rows, LeaderboardRow{
			Rank:        i + 1,
			Name:        p.DisplayName,
			Key:         elo40k.PlayerKey(p.DisplayName),
			Elo:         p.Rating,
			Games:       p.Games,
			RecentGames: recent,
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
