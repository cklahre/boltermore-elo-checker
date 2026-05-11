package elo40k

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// LoadMatchesJSON reads a JSON array of games. Each object supports:
//
//	{ "date": "2006-01-02" | RFC3339, "a": "…", "b": "…", "winner": "a"|"b"|"draw" }
//
// Aliases: player_a / player_b instead of a / b; "result" may be "win_a", "win_b", "draw".
func LoadMatchesJSON(path string) ([]Match, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseMatchesJSON(body)
}

type wireMatch struct {
	Date string `json:"date"`
	Time string `json:"time"`
	A    string `json:"a"`
	B    string `json:"b"`
	PA   string `json:"player_a"`
	PB   string `json:"player_b"`
	// Winner: a | b | draw (or win_a / win_b aliases via Result)
	Winner string `json:"winner"`
	Result string `json:"result"`
}

// ParseMatchesJSON decodes match records from JSON bytes.
func ParseMatchesJSON(body []byte) ([]Match, error) {
	var wires []wireMatch
	if err := json.Unmarshal(body, &wires); err != nil {
		return nil, fmt.Errorf("matches JSON: %w", err)
	}
	out := make([]Match, 0, len(wires))
	for i, w := range wires {
		a := strings.TrimSpace(firstNonEmpty(w.A, w.PA))
		b := strings.TrimSpace(firstNonEmpty(w.B, w.PB))
		if a == "" || b == "" {
			return nil, fmt.Errorf("match %d: need player a/b (or player_a/player_b)", i)
		}
		t, err := parseWhen(w.Date, w.Time)
		if err != nil {
			return nil, fmt.Errorf("match %d: %w", i, err)
		}
		res, err := parseOutcome(w.Winner, w.Result)
		if err != nil {
			return nil, fmt.Errorf("match %d: %w", i, err)
		}
		out = append(out, Match{Time: t, A: a, B: b, Res: res})
	}
	return out, nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func parseWhen(date, clock string) (time.Time, error) {
	date = strings.TrimSpace(date)
	clock = strings.TrimSpace(clock)

	if date == "" {
		return time.Time{}, fmt.Errorf("missing date")
	}

	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	var t time.Time
	var err error
	for _, lay := range layouts {
		t, err = time.ParseInLocation(lay, date, time.Local)
		if err == nil {
			return t, nil
		}
	}
	// date field might be date-only; optional separate time field (not commonly used)
	if clock != "" {
		combo := date + " " + clock
		for _, lay := range []string{"2006-01-02 15:04:05", "2006-01-02 3:04PM"} {
			t, err = time.ParseInLocation(lay, combo, time.Local)
			if err == nil {
				return t, nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("could not parse date %q", date)
}

func parseOutcome(winner, result string) (Outcome, error) {
	s := strings.TrimSpace(strings.ToLower(firstNonEmpty(result, winner)))
	switch s {
	case "a", "win_a", "1-0", "player_a", "pa":
		return AWin, nil
	case "b", "win_b", "0-1", "player_b", "pb":
		return BWin, nil
	case "draw", "tie", "0.5-0.5", "split", "draws":
		return Draw, nil
	case "":
		return Draw, fmt.Errorf(`need "winner" (a|b|draw) or result`)
	default:
		return Draw, fmt.Errorf("unknown winner/result %q (use a, b, or draw)", s)
	}
}
