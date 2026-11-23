package bcp

import (
	"strings"
	"time"
)

// legacyRoundSlack bounds the per-table tie-break seconds of the old export, which placed
// round N at EventStart + (N-1)*24h + table seconds. Rows from the current export carry
// at least a 15-minute round offset past the day boundary from round 2 onward.
const legacyRoundSlack = 15 * time.Minute

// RedateStats summarizes a RedateLegacyShards pass.
type RedateStats struct {
	Rows, Changed, Skipped int
	EventsChanged          int
	EventsMissingMeta      int
	NamesFilled            int
}

// FillEventNames sets EventName on rows that lack one, from meta. Returns rows filled.
func FillEventNames(shards [][]MatchFileRow, meta map[string]*Event) int {
	n := 0
	for _, rows := range shards {
		for i := range rows {
			if strings.TrimSpace(rows[i].EventName) != "" {
				continue
			}
			ev := meta[strings.TrimSpace(rows[i].EventID)]
			if ev == nil || strings.TrimSpace(ev.Name) == "" {
				continue
			}
			rows[i].EventName = strings.TrimSpace(ev.Name)
			n++
		}
	}
	return n
}

// RedateLegacyShards rewrites dates on rows produced by the old one-day-per-round export so
// rounds fall inside the event's real calendar span (MatchExportOptions.RoundTime).
// meta maps event id → BCP event metadata. Rows already in the current layout, rows without
// an event id or metadata, and unparsable dates are left unchanged. Shards are edited in place.
func RedateLegacyShards(shards [][]MatchFileRow, meta map[string]*Event) RedateStats {
	var st RedateStats

	type ref struct{ shard, row int }
	byEvent := map[string][]ref{}
	for si, rows := range shards {
		for ri, r := range rows {
			st.Rows++
			if eid := strings.TrimSpace(r.EventID); eid != "" {
				byEvent[eid] = append(byEvent[eid], ref{si, ri})
			}
		}
	}

	for eid, refs := range byEvent {
		ev := meta[eid]
		if ev == nil {
			st.EventsMissingMeta++
			st.Skipped += len(refs)
			continue
		}

		times := make([]time.Time, len(refs))
		var earliest time.Time
		ok := true
		for i, rf := range refs {
			t, err := time.Parse(time.RFC3339, strings.TrimSpace(shards[rf.shard][rf.row].Date))
			if err != nil {
				ok = false
				break
			}
			times[i] = t.UTC()
			if earliest.IsZero() || times[i].Before(earliest) {
				earliest = times[i]
			}
		}
		if !ok {
			st.Skipped += len(refs)
			continue
		}

		start, err := ParseEventStart(ev)
		if err != nil || earliest.Before(start) {
			// BCP's eventDate moved since export; the earliest row is round 1.
			start = earliest
		}
		end, err := ParseEventEnd(ev)
		if err != nil || end.Before(start) {
			end = start
		}

		type legacy struct {
			idx, round int
			table      time.Duration
		}
		var rows []legacy
		maxRound := 0
		for i, t := range times {
			off := t.Sub(start)
			rem := off % (24 * time.Hour)
			if rem >= legacyRoundSlack {
				continue
			}
			round := int(off/(24*time.Hour)) + 1
			rows = append(rows, legacy{idx: i, round: round, table: rem})
			if round > maxRound {
				maxRound = round
			}
		}

		total := ev.NumberOfRounds
		if total < maxRound {
			total = maxRound
		}
		opt := MatchExportOptions{EventStart: start, EventEnd: end, TotalRounds: total}

		changed := false
		for _, l := range rows {
			rf := refs[l.idx]
			nt := opt.RoundTime(l.round).Add(l.table).Format(time.RFC3339)
			if shards[rf.shard][rf.row].Date != nt {
				shards[rf.shard][rf.row].Date = nt
				st.Changed++
				changed = true
			}
		}
		if changed {
			st.EventsChanged++
		}
	}
	st.NamesFilled = FillEventNames(shards, meta)
	return st
}
