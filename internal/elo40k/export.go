package elo40k

import (
	"encoding/json"
	"time"
)

// MatchesToExportJSON marshals matches in the same schema as LoadMatchesJSON / ParseMatchesJSON.
func MatchesToExportJSON(ms []Match) ([]byte, error) {
	type row struct {
		Date   string `json:"date"`
		A      string `json:"a"`
		B      string `json:"b"`
		Winner string `json:"winner"`
	}
	out := make([]row, 0, len(ms))
	for _, m := range ms {
		var w string
		switch m.Res {
		case AWin:
			w = "a"
		case BWin:
			w = "b"
		case Draw:
			w = "draw"
		}
		out = append(out, row{
			Date:   m.Time.Format(time.RFC3339),
			A:      m.A,
			B:      m.B,
			Winner: w,
		})
	}
	return json.MarshalIndent(out, "", "  ")
}
