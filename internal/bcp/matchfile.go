package bcp

import (
	"encoding/json"
	"sort"
	"time"

	"fortyk/eloevent/internal/elo40k"
)

// MatchFileRow is one game line on disk: local-elo fields plus optional BCP lineage.
// Unknown JSON keys are ignored when loading into elo40k; event_id / pairing_id are for dedup and audits.
type MatchFileRow struct {
	Date      string `json:"date"`
	A         string `json:"a"`
	B         string `json:"b"`
	Winner    string `json:"winner"`
	EventID   string `json:"event_id,omitempty"`
	PairingID string `json:"pairing_id,omitempty"`
}

func matchFileRowFromPairing(eventID string, p Pairing, m elo40k.Match) MatchFileRow {
	var w string
	switch m.Res {
	case elo40k.AWin:
		w = "a"
	case elo40k.BWin:
		w = "b"
	case elo40k.Draw:
		w = "draw"
	}
	return MatchFileRow{
		Date:      m.Time.Format(time.RFC3339),
		A:         m.A,
		B:         m.B,
		Winner:    w,
		EventID:   eventID,
		PairingID: p.ID,
	}
}

// MarshalMatchFileJSON encodes rows sorted by date then A,B.
func MarshalMatchFileJSON(rows []MatchFileRow) ([]byte, error) {
	cp := append([]MatchFileRow(nil), rows...)
	sort.Slice(cp, func(i, j int) bool {
		if cp[i].Date != cp[j].Date {
			return cp[i].Date < cp[j].Date
		}
		if cp[i].A != cp[j].A {
			return cp[i].A < cp[j].A
		}
		return cp[i].B < cp[j].B
	})
	return json.MarshalIndent(cp, "", "  ")
}

// DedupeMatchRows keeps the first row for each pairing_id, otherwise event|round|table|a|b|date key.
func DedupeMatchRows(rows []MatchFileRow) []MatchFileRow {
	seen := make(map[string]struct{})
	out := make([]MatchFileRow, 0, len(rows))
	for _, r := range rows {
		k := r.PairingID
		if k == "" {
			k = r.EventID + "|" + r.Date + "|" + r.A + "|" + r.B
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, r)
	}
	return out
}
