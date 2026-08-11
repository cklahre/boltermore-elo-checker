package elodata

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"time"

	"fortyk/eloevent/internal/bcp"
	"fortyk/eloevent/internal/elo40k"
)

// buildLeaderboardWebRows attaches career W-L-D + recent games/events for each snapshot player.
// Single pass over matches (plus one delta replay) — not per-player full scans, which OOM small droplets.
func buildLeaderboardWebRows(snap []elo40k.Player, matchRows []bcp.MatchFileRow, recentN int) ([]LeaderboardRow, error) {
	if recentN < 0 {
		recentN = 0
	}

	byPairing, byLine, err := ComputeMatchDeltas(matchRows)
	if err != nil {
		return nil, err
	}

	type evAgg struct {
		w, l, d, games, deltaGames int
		sumDelta                   float64
		lastPlayed                 time.Time
	}
	type pAgg struct {
		w, l, d int
		events  map[string]*evAgg
		recent  []RecentGame // newest first, capped at recentN
	}

	indexByKey := make(map[string]int, len(snap))
	aggs := make([]*pAgg, len(snap))
	for i, p := range snap {
		k := elo40k.PlayerKey(p.DisplayName)
		indexByKey[k] = i
		aggs[i] = &pAgg{events: make(map[string]*evAgg)}
	}

	aggFor := func(name string) *pAgg {
		i, ok := indexByKey[elo40k.PlayerKey(name)]
		if !ok {
			return nil
		}
		return aggs[i]
	}

	applySide := func(asA bool, myName, opponent, win, eventID, pairingID string, t time.Time) error {
		a := aggFor(myName)
		if a == nil {
			return nil
		}
		oc, err := outcomeForSide(asA, win)
		if err != nil {
			return err
		}
		switch oc {
		case 'W':
			a.w++
		case 'L':
			a.l++
		case 'D':
			a.d++
		}
		g := playedGame{
			t:         t,
			asA:       asA,
			myName:    myName,
			opponent:  opponent,
			outcome:   oc,
			eventID:   strings.TrimSpace(eventID),
			pairingID: strings.TrimSpace(pairingID),
		}
		ek := strings.TrimSpace(eventID)
		ev := a.events[ek]
		if ev == nil {
			ev = &evAgg{}
			a.events[ek] = ev
		}
		ev.games++
		switch oc {
		case 'W':
			ev.w++
		case 'L':
			ev.l++
		case 'D':
			ev.d++
		}
		if de, ok := deltaForPlayed(g, byPairing, byLine); ok {
			ev.sumDelta += de
			ev.deltaGames++
		}
		if t.After(ev.lastPlayed) {
			ev.lastPlayed = t
		}
		return nil
	}

	type rowMeta struct {
		t         time.Time
		a, b      string
		win       string
		eventID   string
		pairingID string
	}
	metas := make([]rowMeta, 0, len(matchRows))
	for _, r := range matchRows {
		a := strings.TrimSpace(r.A)
		b := strings.TrimSpace(r.B)
		if a == "" || b == "" {
			continue
		}
		t, err := ParseRowTime(r.Date)
		if err != nil {
			return nil, err
		}
		win := strings.TrimSpace(strings.ToLower(r.Winner))
		metas = append(metas, rowMeta{
			t: t, a: a, b: b, win: win,
			eventID: r.EventID, pairingID: strings.TrimSpace(r.PairingID),
		})
		if err := applySide(true, a, b, win, r.EventID, r.PairingID, t); err != nil {
			return nil, err
		}
		if err := applySide(false, b, a, win, r.EventID, r.PairingID, t); err != nil {
			return nil, err
		}
	}

	if recentN > 0 && len(metas) > 0 {
		order := make([]int, len(metas))
		for i := range order {
			order[i] = i
		}
		sort.Slice(order, func(i, j int) bool {
			ii, jj := order[i], order[j]
			if metas[ii].t.Equal(metas[jj].t) {
				if metas[ii].a == metas[jj].a {
					return metas[ii].b > metas[jj].b
				}
				return metas[ii].a > metas[jj].a
			}
			return metas[ii].t.After(metas[jj].t)
		})

		appendRecent := func(asA bool, myName, opponent, win, eventID, pairingID string, t time.Time) error {
			a := aggFor(myName)
			if a == nil || len(a.recent) >= recentN {
				return nil
			}
			oc, err := outcomeForSide(asA, win)
			if err != nil {
				return err
			}
			g := playedGame{
				t: t, asA: asA, myName: myName, opponent: opponent,
				outcome: oc, eventID: strings.TrimSpace(eventID), pairingID: strings.TrimSpace(pairingID),
			}
			rg := RecentGame{
				Time: t, Result: oc, Opponent: opponent, AsA: asA, EventID: strings.TrimSpace(eventID),
			}
			if de, ok := deltaForPlayed(g, byPairing, byLine); ok {
				rg.DeltaElo = &de
			}
			a.recent = append(a.recent, rg)
			return nil
		}

		for _, mi := range order {
			m := metas[mi]
			if err := appendRecent(true, m.a, m.b, m.win, m.eventID, m.pairingID, m.t); err != nil {
				return nil, err
			}
			if err := appendRecent(false, m.b, m.a, m.win, m.eventID, m.pairingID, m.t); err != nil {
				return nil, err
			}
		}
	}

	rows := make([]LeaderboardRow, 0, len(snap))
	for i, p := range snap {
		a := aggs[i]
		total := a.w + a.l + a.d
		var winPct, ptsPct float64
		if total > 0 {
			winPct = 100.0 * float64(a.w) / float64(total)
			ptsPct = 100.0 * (float64(a.w) + 0.5*float64(a.d)) / float64(total)
		}

		recent := make([]RecentGameWire, len(a.recent))
		for j, g := range a.recent {
			recent[j] = RecentGameWire{
				Time:     g.Time.UTC().Format(time.RFC3339),
				Result:   string([]byte{g.Result}),
				Opponent: g.Opponent,
				AsA:      g.AsA,
				EventID:  g.EventID,
				DeltaElo: g.DeltaElo,
			}
		}

		type evSort struct {
			id string
			e  *evAgg
		}
		evs := make([]evSort, 0, len(a.events))
		for id, e := range a.events {
			evs = append(evs, evSort{id: id, e: e})
		}
		sort.Slice(evs, func(i, j int) bool {
			return evs[i].e.lastPlayed.After(evs[j].e.lastPlayed)
		})
		if len(evs) > RecentEventSummaryCap {
			evs = evs[:RecentEventSummaryCap]
		}
		recentEv := make([]RecentEventWire, len(evs))
		for j, item := range evs {
			recentEv[j] = RecentEventWire{
				EventID:       item.id,
				LastPlayed:    item.e.lastPlayed.UTC().Format(time.RFC3339),
				Wins:          item.e.w,
				Losses:        item.e.l,
				Draws:         item.e.d,
				Games:         item.e.games,
				TotalDeltaElo: item.e.sumDelta,
				DeltaGames:    item.e.deltaGames,
			}
		}

		rows = append(rows, LeaderboardRow{
			Rank:         i + 1,
			Name:         p.DisplayName,
			Key:          elo40k.PlayerKey(p.DisplayName),
			Elo:          p.Rating,
			Games:        p.Games,
			Wins:         a.w,
			Losses:       a.l,
			Draws:        a.d,
			WinPct:       winPct,
			PointsPct:    ptsPct,
			RecentGames:  recent,
			RecentEvents: recentEv,
		})
	}
	return rows, nil
}

// WriteLeaderboardWebJSON writes the same leaderboard as WriteLeaderboardJSON plus recent_games per player,
// using the same delta rules as player-history.
func WriteLeaderboardWebJSON(path string, asOf time.Time, snap []elo40k.Player, matchRows []bcp.MatchFileRow, recentN int) error {
	rows, err := buildLeaderboardWebRows(snap, matchRows, recentN)
	if err != nil {
		return err
	}
	f := LeaderboardFile{
		AsOfRFC3339: asOf.UTC().Format(time.RFC3339),
		Players:     rows,
	}
	// Compact JSON — indented blobs of ~36k players OOMs small droplets at marshal time.
	raw, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}
