// Package elo40k implements Warhammer 40k–style Elo to match Stat-Check’s published rules:
//   - baseline (starting) rating 1500
//   - K-factor 32 for every game
//   - inactivity decay: after each full 13-week period with no games, rating moves 20%
//     toward 1500 (equivalent to rating = 1500 + 0.8*(rating-1500) once per period).
//
// Pairwise expected score uses the standard chess Elo formula (400‑scale).
package elo40k

import (
	"math"
	"time"
)

const (
	// Baseline is the rating new players start at and decay trends toward.
	Baseline = 1500.0
	// K is the rating sensitivity (Stat-Check uses 32 for all matches).
	K = 32.0
	// InactivityWeeks is the length of one inactivity bucket before decay applies.
	InactivityWeeks = 13
	// DecayRetention is the multiplier on (rating - Baseline) per decay period (20% pull toward baseline).
	DecayRetention = 0.8
)

// ExpectedScore returns the expected score (0..1) for the player with rating ra when
// facing an opponent with rating rb.
func ExpectedScore(ra, rb float64) float64 {
	return 1.0 / (1.0 + math.Pow(10, (rb-ra)/400.0))
}

// Update returns new ratings for two players after one game.
// sa and sb are actual scores (1 win, 0.5 draw, 0 loss) and must satisfy sa+sb == 1.
func Update(ra, rb, sa, sb float64) (newA, newB float64) {
	ea := ExpectedScore(ra, rb)
	eb := ExpectedScore(rb, ra)
	return ra + K*(sa-ea), rb + K*(sb-eb)
}

// InactivityDecayPeriods returns how many full 13-week decay steps apply between lastPlay and t.
func InactivityDecayPeriods(lastPlay, t time.Time) int {
	if lastPlay.IsZero() || !t.After(lastPlay) {
		return 0
	}
	weeks := int(t.Sub(lastPlay).Hours() / (24 * 7))
	return weeks / InactivityWeeks
}

// ApplyDecay applies n full inactivity periods at once (same as n sequential Stat-Check steps).
func ApplyDecay(rating float64, periods int) float64 {
	if periods <= 0 {
		return rating
	}
	f := math.Pow(DecayRetention, float64(periods))
	return Baseline + f*(rating-Baseline)
}
