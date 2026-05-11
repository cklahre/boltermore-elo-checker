package elodata

import (
	"fmt"
	"strings"
	"time"

	"fortyk/eloevent/internal/bcp"
	"fortyk/eloevent/internal/elo40k"
)

// ParseRowTime parses timestamps used in bcp-export-matches JSON.
func ParseRowTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	var last error
	for _, lay := range layouts {
		t, err := time.ParseInLocation(lay, s, time.UTC)
		if err == nil {
			return t.UTC(), nil
		}
		last = err
	}
	for _, lay := range layouts {
		t, err := time.ParseInLocation(lay, s, time.Local)
		if err == nil {
			return t.UTC(), nil
		}
		last = err
	}
	return time.Time{}, fmt.Errorf("parse date: %v", last)
}

// RowToMatch converts a BCP export row to an elo40k.Match.
func RowToMatch(r *bcp.MatchFileRow) (elo40k.Match, error) {
	t, err := ParseRowTime(r.Date)
	if err != nil {
		return elo40k.Match{}, err
	}
	a := strings.TrimSpace(r.A)
	b := strings.TrimSpace(r.B)
	var res elo40k.Outcome
	switch strings.ToLower(strings.TrimSpace(r.Winner)) {
	case "a":
		res = elo40k.AWin
	case "b":
		res = elo40k.BWin
	case "draw":
		res = elo40k.Draw
	default:
		return elo40k.Match{}, fmt.Errorf("unknown winner %q", r.Winner)
	}
	return elo40k.Match{Time: t, A: a, B: b, Res: res}, nil
}
