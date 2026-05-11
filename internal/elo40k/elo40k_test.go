package elo40k

import (
	"math"
	"testing"
	"time"
)

func TestExpectedAndUpdateSymmetric(t *testing.T) {
	ra, rb := 1500.0, 1500.0
	if e := ExpectedScore(ra, rb); math.Abs(e-0.5) > 1e-9 {
		t.Fatalf("expected 0.5 at equal, got %v", e)
	}
	a, b := Update(ra, rb, 1, 0)
	if math.Abs(a-1516) > 0.01 || math.Abs(b-1484) > 0.01 {
		t.Fatalf("got %v %v want ~1516 1484", a, b)
	}
}

func TestDecayOnePeriod(t *testing.T) {
	r := ApplyDecay(2000, 1)
	if math.Abs(r-1900) > 1e-9 {
		t.Fatalf("one period from 2000: got %v want 1900", r)
	}
	r2 := ApplyDecay(2000, 2)
	if math.Abs(r2-1820) > 1e-9 {
		t.Fatalf("two periods from 2000: got %v want 1820", r2)
	}
}

func TestInactivityDecayPeriods(t *testing.T) {
	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if n := InactivityDecayPeriods(t0, t0.AddDate(0, 0, 12*7)); n != 0 {
		t.Fatalf("12 weeks: got %d want 0", n)
	}
	if n := InactivityDecayPeriods(t0, t0.AddDate(0, 0, 13*7)); n != 1 {
		t.Fatalf("13 weeks: got %d want 1", n)
	}
}

func TestEngineDecayBeforeReturn(t *testing.T) {
	e := NewEngine()
	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	e.Play(Match{Time: t0, A: "Amy", B: "Bob", Res: AWin})

	// 26 weeks later: two decay periods for both before they play again.
	t1 := t0.AddDate(0, 0, 26*7)
	e.Play(Match{Time: t1, A: "Amy", B: "Bob", Res: BWin})

	a := e.ensure("Amy")
	// After decay from 1516: 1500 + 0.8^2 * 16 = 1500 + 10.24 = 1510.24, then loss update
	if a.Rating >= 1516 {
		t.Fatalf("Amy should have decayed from first win peak before second game, got rating %v", a.Rating)
	}
}
