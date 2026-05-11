package elodata

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"fortyk/eloevent/internal/bcp"
	"fortyk/eloevent/internal/elo40k"
)

// PairDeltas is rating change for A and B for one pairing line replay.
type PairDeltas struct {
	DeltaA, DeltaB float64
}

// MatchLineKey is a fallback key when pairing_id is missing.
func MatchLineKey(t time.Time, a, b string) string {
	return fmt.Sprintf("%d|%s|%s", t.UnixNano(), a, b)
}

// ComputeMatchDeltas replays all rows like local-elo / player-history and indexes deltas by pairing id and line key.
func ComputeMatchDeltas(rows []bcp.MatchFileRow) (byPairing, byLine map[string]PairDeltas, err error) {
	type tagged struct {
		m   elo40k.Match
		pid string
	}
	var list []tagged
	for _, r := range rows {
		m, err := RowToMatch(&r)
		if err != nil {
			return nil, nil, err
		}
		list = append(list, tagged{m: m, pid: strings.TrimSpace(r.PairingID)})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].m.Time.Equal(list[j].m.Time) {
			if list[i].m.A == list[j].m.A {
				return list[i].m.B < list[j].m.B
			}
			return list[i].m.A < list[j].m.A
		}
		return list[i].m.Time.Before(list[j].m.Time)
	})

	byPairing = make(map[string]PairDeltas)
	byLine = make(map[string]PairDeltas)
	e := elo40k.NewEngine()
	for _, item := range list {
		dA, dB := e.PlayWithDeltas(item.m)
		pd := PairDeltas{DeltaA: dA, DeltaB: dB}
		lk := MatchLineKey(item.m.Time, item.m.A, item.m.B)
		byLine[lk] = pd
		if item.pid != "" {
			byPairing[item.pid] = pd
		}
	}
	return byPairing, byLine, nil
}

func deltaForPlayed(g playedGame, byPairing map[string]PairDeltas, byLine map[string]PairDeltas) (float64, bool) {
	var pd PairDeltas
	var ok bool
	if g.pairingID != "" {
		pd, ok = byPairing[g.pairingID]
	}
	if !ok {
		a, b := g.myName, g.opponent
		if !g.asA {
			a, b = g.opponent, g.myName
		}
		pd, ok = byLine[MatchLineKey(g.t, a, b)]
	}
	if !ok {
		return 0, false
	}
	if g.asA {
		return pd.DeltaA, true
	}
	return pd.DeltaB, true
}
