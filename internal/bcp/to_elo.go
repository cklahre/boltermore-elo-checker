package bcp

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"fortyk/eloevent/internal/elo40k"
)

// ErrUnscored means both players exist but MoV / game results are still 0–0.
var ErrUnscored = errors.New("unscored pairing")

// MatchExportOptions controls how BCP rows become local-elo matches.
type MatchExportOptions struct {
	EventStart        time.Time // parsed from Event.EventDate (UTC/layout tolerant)
	TieBreakSameRound int       // spreads same-round timestamps by table index
}

// PairingToMatch converts a pairing using name resolution and game points / gameResult.
// skipReason, if non-empty, tells why the row was dropped (bye, unscored, etc.).
func PairingToMatch(p Pairing, names map[string]string, opt MatchExportOptions) (_ elo40k.Match, skipReason string, err error) {
	a := sideName(p.Player1, names)
	b := sideName(p.Player2, names)
	if a == "" || b == "" {
		return elo40k.Match{}, "missing player (bye or unresolved id)", nil
	}
	t := opt.EventStart.Add(
		time.Duration(p.Round-1)*24*time.Hour +
			time.Duration(p.Table*opt.TieBreakSameRound)*time.Second,
	)
	if p.Round < 1 {
		t = opt.EventStart
	}

	var res elo40k.Outcome
	if p.P1Game != nil && p.P2Game != nil {
		res, err = outcomeFromGames(p.P1Game, p.P2Game)
		if err != nil {
			if errors.Is(err, ErrUnscored) {
				return elo40k.Match{}, "unscored or 0–0 without draw flags", nil
			}
			return elo40k.Match{}, "", err
		}
	} else {
		return elo40k.Match{}, "games not expanded or not reported yet", nil
	}

	return elo40k.Match{Time: t, A: a, B: b, Res: res}, "", nil
}

func sideName(side *PairingSide, names map[string]string) string {
	if side == nil {
		return ""
	}
	if n := pairingSideDirectName(side); n != "" {
		return n
	}
	if side.User != nil {
		n := strings.TrimSpace(side.User.FirstName + " " + side.User.LastName)
		if n != "" {
			return n
		}
	}
	if side.User != nil && side.User.ID != "" {
		if n := names[side.User.ID]; n != "" {
			return n
		}
	}
	if side.UserID != "" {
		if n := names[side.UserID]; n != "" {
			return n
		}
	}
	if side.ID != "" {
		if n := names[side.ID]; n != "" {
			return n
		}
	}
	return ""
}

func pairingSideDirectName(side *PairingSide) string {
	n := strings.TrimSpace(side.FirstName + " " + side.LastName)
	if n != "" {
		return n
	}
	return strings.TrimSpace(side.TeamName)
}

// outcomeFromGames uses gamePoints first, then gameResult integers (2=win,1=draw,0=loss).
func outcomeFromGames(g1, g2 *GameRecord) (elo40k.Outcome, error) {
	p1 := g1.GamePoints
	p2 := g2.GamePoints
	if p1 > p2 {
		return elo40k.AWin, nil
	}
	if p2 > p1 {
		return elo40k.BWin, nil
	}
	if p1 == p2 {
		if p1 == 0 && g1.GameResult == nil && g2.GameResult == nil {
			return 0, ErrUnscored
		}
		if g1.GameResult != nil && g2.GameResult != nil {
			if *g1.GameResult == 1 && *g2.GameResult == 1 {
				return elo40k.Draw, nil
			}
		}
		return elo40k.Draw, nil
	}
	return 0, fmt.Errorf("unexpected score state")
}

func parseBCPEventTimestamp(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05.000Z", s)
	}
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

// ParseEventStart parses Event.EventDate (RFC3339 / Zulu) into UTC.
func ParseEventStart(ev *Event) (time.Time, error) {
	if ev == nil {
		return time.Time{}, fmt.Errorf("nil event")
	}
	s := strings.TrimSpace(ev.EventDate)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty eventDate")
	}
	t, err := parseBCPEventTimestamp(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("eventDate %q: %w", s, err)
	}
	return t, nil
}

// ParseEventEnd parses Event.EventEndDate, or the start date if end is missing.
func ParseEventEnd(ev *Event) (time.Time, error) {
	if ev == nil {
		return time.Time{}, fmt.Errorf("nil event")
	}
	s := strings.TrimSpace(ev.EventEndDate)
	if s == "" {
		return ParseEventStart(ev)
	}
	t, err := parseBCPEventTimestamp(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("eventEndDate %q: %w", s, err)
	}
	return t, nil
}
