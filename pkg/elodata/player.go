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

// PlayerReport is aggregate stats plus recent games for Discord/CLI.
type PlayerReport struct {
	DisplayName         string
	Wins, Losses, Draws int
	WinPct, PointsPct   float64
	MultiNameWarning    bool
	Games               []RecentGame // newest first, truncated to LastN (0 = all)
}

// PlayerLookup filters match rows for one player, recomputes Elo deltas from the full pool, and returns stats.
// lastN caps how many games are returned in Games (0 = all). Games are newest-first.
func PlayerLookup(rows []bcp.MatchFileRow, query string, contains bool, lastN int) (*PlayerReport, error) {
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

	byPairing, byLine, err := ComputeMatchDeltas(rows)
	if err != nil {
		return nil, err
	}

	sort.Slice(games, func(i, j int) bool { return games[i].t.After(games[j].t) })

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
