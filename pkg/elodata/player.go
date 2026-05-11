package elodata

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"fortyk/eloevent/internal/bcp"
)

// RecentGame is one row in a player’s history (newest-first when returned from PlayerLookup).
type RecentGame struct {
	Time     time.Time
	Result   byte // 'W' 'L' 'D'
	Opponent string
	AsA      bool
	EventID  string
	DeltaElo *float64 // nil if unavailable
}

// EventRollup is per-event stats for one player across their full history in the match pool.
type EventRollup struct {
	EventID    string // from pairings rows; empty if source rows omit event_id
	LastPlayed time.Time
	Wins       int
	Losses     int
	Draws      int
	Games      int // Wins+Losses+Draws when data is consistent
	// TotalDeltaElo sums this player’s ΔElo for games where a pairing delta was resolved.
	TotalDeltaElo float64
	DeltaGames    int // games included in TotalDeltaElo
}

// PlayerReport is aggregate stats plus recent games for Discord/CLI.
type PlayerReport struct {
	DisplayName         string
	Wins, Losses, Draws int
	WinPct, PointsPct   float64
	MultiNameWarning    bool
	Games               []RecentGame  // newest first, truncated to LastN (0 = all)
	RecentEvents        []EventRollup // newest activity first; at most RecentEventSummaryCap buckets
}

// RecentEventSummaryCap is how many distinct events get attached to each PlayerReport.
const RecentEventSummaryCap = 3

// PlayerLookup filters match rows for one player, recomputes Elo deltas from the full pool, and returns stats.
// lastN caps how many games are returned in Games (0 = all). Games are newest-first.
func PlayerLookup(rows []bcp.MatchFileRow, query string, contains bool, lastN int) (*PlayerReport, error) {
	byPairing, byLine, err := ComputeMatchDeltas(rows)
	if err != nil {
		return nil, err
	}
	return playerReportWithDeltas(rows, query, contains, lastN, byPairing, byLine)
}

// PlayerLookupWithDeltas is like PlayerLookup but reuses delta maps computed once via ComputeMatchDeltas.
func PlayerLookupWithDeltas(rows []bcp.MatchFileRow, query string, contains bool, lastN int, byPairing map[string]PairDeltas, byLine map[string]PairDeltas) (*PlayerReport, error) {
	if byPairing == nil || byLine == nil {
		return nil, fmt.Errorf("nil delta maps")
	}
	return playerReportWithDeltas(rows, query, contains, lastN, byPairing, byLine)
}

func playerReportWithDeltas(rows []bcp.MatchFileRow, query string, contains bool, lastN int, byPairing map[string]PairDeltas, byLine map[string]PairDeltas) (*PlayerReport, error) {
	games, displayName, err := collectPlayedGames(rows, query, contains)
	if err != nil {
		return nil, err
	}
	if len(games) == 0 {
		return nil, nil
	}

	multi := false
	if contains {
		seen := make(map[string]struct{})
		for _, g := range games {
			seen[g.myName] = struct{}{}
		}
		multi = len(seen) > 1
	}

	var w, l, d int
	for _, g := range games {
		switch g.outcome {
		case 'W':
			w++
		case 'L':
			l++
		case 'D':
			d++
		}
	}
	total := w + l + d
	var winPct, ptsPct float64
	if total > 0 {
		winPct = 100.0 * float64(w) / float64(total)
		ptsPct = 100.0 * (float64(w) + 0.5*float64(d)) / float64(total)
	}

	sort.Slice(games, func(i, j int) bool { return games[i].t.After(games[j].t) })

	recentEv := rollupRecentEvents(games, byPairing, byLine, RecentEventSummaryCap)

	limit := len(games)
	if lastN > 0 && lastN < limit {
		limit = lastN
	}
	outGames := make([]RecentGame, 0, limit)
	for i := 0; i < limit; i++ {
		g := games[i]
		rg := RecentGame{
			Time:     g.t,
			Result:   g.outcome,
			Opponent: g.opponent,
			AsA:      g.asA,
			EventID:  g.eventID,
		}
		if de, ok := deltaForPlayed(g, byPairing, byLine); ok {
			rg.DeltaElo = &de
		}
		outGames = append(outGames, rg)
	}

	return &PlayerReport{
		DisplayName:      displayName,
		Wins:             w,
		Losses:           l,
		Draws:            d,
		WinPct:           winPct,
		PointsPct:        ptsPct,
		MultiNameWarning: multi,
		Games:            outGames,
		RecentEvents:     recentEv,
	}, nil
}

type playedGame struct {
	t         time.Time
	asA       bool
	myName    string
	opponent  string
	outcome   byte
	eventID   string
	pairingID string
}

func collectPlayedGames(rows []bcp.MatchFileRow, query string, contains bool) ([]playedGame, string, error) {
	q := strings.TrimSpace(query)
	var out []playedGame
	var displayName string

	for _, r := range rows {
		a := strings.TrimSpace(r.A)
		b := strings.TrimSpace(r.B)
		matchA := nameMatches(a, q, contains)
		matchB := nameMatches(b, q, contains)
		if matchA && matchB {
			continue
		}
		if !matchA && !matchB {
			continue
		}

		t, err := ParseRowTime(r.Date)
		if err != nil {
			return nil, "", fmt.Errorf("row date %q: %w", r.Date, err)
		}

		win := strings.TrimSpace(strings.ToLower(r.Winner))
		if matchA {
			oc, err := outcomeForSide(true, win)
			if err != nil {
				return nil, "", err
			}
			if displayName == "" {
				displayName = a
			}
			out = append(out, playedGame{t: t, asA: true, myName: a, opponent: b, outcome: oc, eventID: r.EventID, pairingID: strings.TrimSpace(r.PairingID)})
		} else {
			oc, err := outcomeForSide(false, win)
			if err != nil {
				return nil, "", err
			}
			if displayName == "" {
				displayName = b
			}
			out = append(out, playedGame{t: t, asA: false, myName: b, opponent: a, outcome: oc, eventID: r.EventID, pairingID: strings.TrimSpace(r.PairingID)})
		}
	}

	if displayName == "" {
		displayName = q
	}
	return out, displayName, nil
}

func outcomeForSide(isPlayerA bool, win string) (byte, error) {
	switch win {
	case "draw":
		return 'D', nil
	case "a":
		if isPlayerA {
			return 'W', nil
		}
		return 'L', nil
	case "b":
		if isPlayerA {
			return 'L', nil
		}
		return 'W', nil
	default:
		return 0, fmt.Errorf("unknown winner %q (expected a, b, or draw)", win)
	}
}

func nameMatches(field, query string, contains bool) bool {
	if contains {
		return strings.Contains(strings.ToLower(field), strings.ToLower(strings.TrimSpace(query)))
	}
	return strings.EqualFold(strings.TrimSpace(field), strings.TrimSpace(query))
}

// rollupRecentEvents returns up to limit distinct events: bucket key is trimmed EventID (empty merges into one bucket).
// Events ordered by newest LastPlayed descending.
func rollupRecentEvents(all []playedGame, byPairing map[string]PairDeltas, byLine map[string]PairDeltas, limit int) []EventRollup {
	if limit <= 0 || len(all) == 0 {
		return nil
	}
	type agg struct {
		w, l, d    int
		games      int
		sumDelta   float64
		deltaCt    int
		lastPlayed time.Time
	}
	m := make(map[string]*agg)
	for _, g := range all {
		key := strings.TrimSpace(g.eventID)
		a := m[key]
		if a == nil {
			a = &agg{}
			m[key] = a
		}
		a.games++
		switch g.outcome {
		case 'W':
			a.w++
		case 'L':
			a.l++
		case 'D':
			a.d++
		}
		if de, ok := deltaForPlayed(g, byPairing, byLine); ok {
			a.sumDelta += de
			a.deltaCt++
		}
		if g.t.After(a.lastPlayed) {
			a.lastPlayed = g.t
		}
	}
	type pair struct {
		key string
		a   *agg
	}
	pairs := make([]pair, 0, len(m))
	for k, v := range m {
		pairs = append(pairs, pair{key: k, a: v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].a.lastPlayed.After(pairs[j].a.lastPlayed)
	})
	if len(pairs) > limit {
		pairs = pairs[:limit]
	}
	out := make([]EventRollup, 0, len(pairs))
	for _, it := range pairs {
		out = append(out, EventRollup{
			EventID:       it.key,
			LastPlayed:    it.a.lastPlayed,
			Wins:          it.a.w,
			Losses:        it.a.l,
			Draws:         it.a.d,
			Games:         it.a.games,
			TotalDeltaElo: it.a.sumDelta,
			DeltaGames:    it.a.deltaCt,
		})
	}
	return out
}
