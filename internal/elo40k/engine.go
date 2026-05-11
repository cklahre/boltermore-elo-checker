package elo40k

import (
	"sort"
	"strings"
	"time"
)

// Outcome is the result from the first player’s perspective (PlayerA).
type Outcome int

const (
	AWin Outcome = iota
	BWin
	Draw
)

// Match is one ranked game between two players at a point in time.
type Match struct {
	Time time.Time
	A    string
	B    string
	Res  Outcome
}

// Player is current rating state for one person.
type Player struct {
	DisplayName string
	Rating      float64
	LastGame    time.Time
	Games       int
}

// Engine processes matches in time order and applies decay when a player returns after gaps.
type Engine struct {
	players map[string]*Player
}

// NewEngine builds an empty rating pool.
func NewEngine() *Engine {
	return &Engine{players: make(map[string]*Player)}
}

func key(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// PlayerKey is the canonical key used to merge player names (same as the Elo engine).
func PlayerKey(name string) string { return key(name) }

func (e *Engine) ensure(name string) *Player {
	k := key(name)
	p, ok := e.players[k]
	if !ok {
		p = &Player{DisplayName: strings.TrimSpace(name), Rating: Baseline}
		e.players[k] = p
	} else if p.DisplayName == "" {
		p.DisplayName = strings.TrimSpace(name)
	}
	return p
}

// applyDecay updates p’s rating as of t based on time since LastGame.
func (e *Engine) applyDecay(p *Player, t time.Time) {
	if p.LastGame.IsZero() {
		return
	}
	n := InactivityDecayPeriods(p.LastGame, t)
	if n <= 0 {
		return
	}
	p.Rating = ApplyDecay(p.Rating, n)
}

// Play applies decay for both sides up to m.Time, then applies the K-factor update.
func (e *Engine) Play(m Match) {
	_, _ = e.PlayWithDeltas(m)
}

// PlayWithDeltas is like Play but returns each side’s rating change this match after
// inactivity decay at m.Time and before/after the K update.
func (e *Engine) PlayWithDeltas(m Match) (deltaA, deltaB float64) {
	pa, pb := e.ensure(m.A), e.ensure(m.B)

	e.applyDecay(pa, m.Time)
	e.applyDecay(pb, m.Time)

	beforeA, beforeB := pa.Rating, pb.Rating

	var sa, sb float64
	switch m.Res {
	case AWin:
		sa, sb = 1, 0
	case BWin:
		sa, sb = 0, 1
	case Draw:
		sa, sb = 0.5, 0.5
	default:
		sa, sb = 0.5, 0.5
	}

	afterA, afterB := Update(beforeA, beforeB, sa, sb)
	pa.Rating, pb.Rating = afterA, afterB
	now := m.Time
	pa.LastGame, pb.LastGame = now, now
	pa.Games++
	pb.Games++
	return afterA - beforeA, afterB - beforeB
}

// FinalizeDecay applies inactivity decay from each player’s last game to asOf (no new games).
func (e *Engine) FinalizeDecay(asOf time.Time) {
	for _, p := range e.players {
		e.applyDecay(p, asOf)
	}
}

// Snapshot returns a sorted slice: highest rating first, then name.
func (e *Engine) Snapshot() []Player {
	out := make([]Player, 0, len(e.players))
	for _, p := range e.players {
		if p == nil {
			continue
		}
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Rating != out[j].Rating {
			return out[i].Rating > out[j].Rating
		}
		return out[i].DisplayName < out[j].DisplayName
	})
	return out
}

// PlayAll sorts matches by time and plays them.
func (e *Engine) PlayAll(ms []Match) {
	sort.Slice(ms, func(i, j int) bool {
		if ms[i].Time.Equal(ms[j].Time) {
			return ms[i].A < ms[j].A
		}
		return ms[i].Time.Before(ms[j].Time)
	})
	for i := range ms {
		e.Play(ms[i])
	}
}
