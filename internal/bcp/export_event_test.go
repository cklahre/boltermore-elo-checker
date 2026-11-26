package bcp

import "testing"

func TestExportRoundLimit(t *testing.T) {
	cases := []struct {
		name      string
		ev        Event
		maxRounds int
		want      int
	}{
		{
			name: "mid event uses scheduled rounds so later reexport can fill",
			ev:   Event{NumberOfRounds: 9, CurrentRound: 3},
			want: 9,
		},
		{
			name: "top cut past scheduled swiss",
			ev:   Event{NumberOfRounds: 6, CurrentRound: 9},
			want: 9,
		},
		{
			name: "complete event",
			ev:   Event{NumberOfRounds: 5, CurrentRound: 5},
			want: 5,
		},
		{
			name:      "explicit max-round cap",
			ev:        Event{NumberOfRounds: 9, CurrentRound: 9},
			maxRounds: 4,
			want:      4,
		},
		{
			name: "only currentRound set",
			ev:   Event{CurrentRound: 3},
			want: 3,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExportRoundLimit(&tc.ev, tc.maxRounds)
			if got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}
