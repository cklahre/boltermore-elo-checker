package bcp

import (
	"fmt"
	"strings"
	"time"
)

// ExportEventStats summarizes one run of ExportEventMatches.
type ExportEventStats struct {
	EventName   string
	EventID     string
	RoundsTried int
	Skipped     int
}

// MetadataFilter applies the same optional gates in bcp-harvest-events, bcp-discover-events, and bcp-export-matches.
// Zero values disable that gate.
type MetadataFilter struct {
	MinPlayers            int
	MinRounds             int
	SinceUTC              *time.Time
	MaxCalendarSpanDays   int      // drop if (eventEnd − eventStart) in UTC calendar days exceeds this; prunes long leagues
	ExcludeNameSubstrings []string // case-insensitive; drop if event name contains any (after trim)
}

func (f MetadataFilter) inactive() bool {
	return f.MinPlayers <= 0 && f.MinRounds <= 0 && f.SinceUTC == nil && f.MaxCalendarSpanDays <= 0 && len(f.ExcludeNameSubstrings) == 0
}

// EventCalendarSpanDaysUTC is inclusive distance between start and end calendar days in UTC (0 = same day).
func EventCalendarSpanDaysUTC(ev *Event) (int, error) {
	if ev == nil {
		return 0, fmt.Errorf("nil event")
	}
	a, err := ParseEventStart(ev)
	if err != nil {
		return 0, err
	}
	b, err := ParseEventEnd(ev)
	if err != nil {
		return 0, err
	}
	d0 := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, time.UTC)
	d1 := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC)
	if d1.Before(d0) {
		return 0, fmt.Errorf("event ends before start")
	}
	return int(d1.Sub(d0).Hours() / 24), nil
}

// ParseCommaTerms splits on comma, trims spaces, drops empties (for -exclude-name flags).
func ParseCommaTerms(csv string) []string {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(csv, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// EventSkippedByFilter returns a non-empty human-readable reason if the event should be skipped
// before downloading pairings (min totals are inclusive: -min-players 30 keeps events with totalPlayers >= 30).
func EventSkippedByFilter(ev *Event, f MetadataFilter) string {
	if ev == nil {
		return "nil event"
	}
	if f.MinPlayers > 0 && ev.TotalPlayers < f.MinPlayers {
		return fmt.Sprintf("totalPlayers=%d need >=%d", ev.TotalPlayers, f.MinPlayers)
	}
	if f.MinRounds > 0 && ev.NumberOfRounds < f.MinRounds {
		return fmt.Sprintf("numberOfRounds=%d need >=%d", ev.NumberOfRounds, f.MinRounds)
	}
	if f.SinceUTC != nil {
		t, err := ParseEventStart(ev)
		if err != nil {
			return fmt.Sprintf("event date: %v", err)
		}
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		sinceDay := time.Date(f.SinceUTC.Year(), f.SinceUTC.Month(), f.SinceUTC.Day(), 0, 0, 0, 0, time.UTC)
		if day.Before(sinceDay) {
			return fmt.Sprintf("event before -since %s", sinceDay.Format("2006-01-02"))
		}
	}
	if f.MaxCalendarSpanDays > 0 {
		span, err := EventCalendarSpanDaysUTC(ev)
		if err != nil {
			return fmt.Sprintf("calendar span: %v", err)
		}
		if span > f.MaxCalendarSpanDays {
			return fmt.Sprintf("calendar span %d days > max %d", span, f.MaxCalendarSpanDays)
		}
	}
	nameLower := strings.ToLower(ev.Name)
	for _, sub := range f.ExcludeNameSubstrings {
		sub = strings.ToLower(strings.TrimSpace(sub))
		if sub == "" {
			continue
		}
		if strings.Contains(nameLower, sub) {
			return fmt.Sprintf("name contains excluded %q", sub)
		}
	}
	return ""
}

// FilterEventListHits keeps search hits that pass EventSkippedByFilter (same rules as bcp-export-matches).
func FilterEventListHits(list []EventListHit, f MetadataFilter) []EventListHit {
	if f.inactive() {
		return list
	}
	out := make([]EventListHit, 0, len(list))
	for _, h := range list {
		if EventSkippedByFilter(ListHitAsEvent(h), f) != "" {
			continue
		}
		out = append(out, h)
	}
	return out
}

// MetadataFilterFromFlags builds MetadataFilter from CLI-style values (-exclude-name is comma-separated).
func MetadataFilterFromFlags(minPlayers, minRounds, maxCalendarSpanDays int, sinceUTC *time.Time, excludeNameCSV string) MetadataFilter {
	return MetadataFilter{
		MinPlayers:            minPlayers,
		MinRounds:             minRounds,
		SinceUTC:              sinceUTC,
		MaxCalendarSpanDays:   maxCalendarSpanDays,
		ExcludeNameSubstrings: ParseCommaTerms(excludeNameCSV),
	}
}

// ParseSinceDay parses a -since value as a UTC calendar day (layout 2006-01-02). Empty string returns (nil, nil).
func ParseSinceDay(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, err
	}
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return &day, nil
}

// ExportEventMatches pulls all scored Swiss Pairing rows for one event into MatchFileRows.
// maxRounds 0 means use heuristics from event metadata (currentRound vs numberOfRounds).
func ExportEventMatches(c *Client, eventID string, maxRounds int) ([]MatchFileRow, ExportEventStats, error) {
	var st ExportEventStats
	st.EventID = eventID

	ev, err := FetchEvent(c, eventID)
	if err != nil {
		return nil, st, err
	}
	return ExportMatchesForEvent(c, ev, maxRounds)
}

// ExportMatchesForEvent exports pairings for an event already loaded via FetchEvent (avoids a second GET).
func ExportMatchesForEvent(c *Client, ev *Event, maxRounds int) ([]MatchFileRow, ExportEventStats, error) {
	var st ExportEventStats
	if ev == nil {
		return nil, st, fmt.Errorf("nil event")
	}
	eventID := ev.ID
	st.EventID = eventID
	st.EventName = ev.Name

	start, err := ParseEventStart(ev)
	if err != nil {
		return nil, st, err
	}

	roster, err := FetchRoster(c, eventID)
	if err != nil {
		return nil, st, err
	}
	names := NameLookup(roster)

	opt := MatchExportOptions{
		EventStart:        start,
		TieBreakSameRound: 1,
	}

	rounds := ev.NumberOfRounds
	if ev.CurrentRound > 0 && ev.CurrentRound < rounds {
		rounds = ev.CurrentRound
	}
	if maxRounds > 0 && maxRounds < rounds {
		rounds = maxRounds
	}
	if rounds <= 0 {
		return nil, st, fmt.Errorf("event %s: numberOfRounds=%d currentRound=%d", eventID, ev.NumberOfRounds, ev.CurrentRound)
	}

	var rows []MatchFileRow
	var emptyStreak int
	st.RoundsTried = rounds

	for r := 1; r <= rounds; r++ {
		pairs, err := FetchPairings(c, eventID, r)
		if err != nil {
			return rows, st, fmt.Errorf("round %d: %w", r, err)
		}
		if len(pairs) == 0 {
			emptyStreak++
			if emptyStreak >= 3 && len(rows) == 0 {
				return rows, st, fmt.Errorf("no pairings for first rounds — event may not have started")
			}
			continue
		}
		emptyStreak = 0

		for _, p := range pairs {
			m, reason, err := PairingToMatch(p, names, opt)
			if err != nil {
				return rows, st, fmt.Errorf("round %d table %d: %w", p.Round, p.Table, err)
			}
			if reason != "" {
				st.Skipped++
				continue
			}
			rows = append(rows, matchFileRowFromPairing(eventID, p, m))
		}
	}

	return rows, st, nil
}
