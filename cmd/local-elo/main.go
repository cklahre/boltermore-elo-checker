// Command local-elo builds a Stat-Check–style Elo leaderboard from your own match history
// (JSON file). It does not call Stat-Check. Rules: baseline 1500, K=32, 13-week × 20% decay
// toward 1500 (see internal/elo40k).
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"fortyk/eloevent/internal/elo40k"
	"fortyk/eloevent/pkg/elodata"
)

func main() {
	matchesPath := flag.String("matches", "", "path to JSON array of games (see internal/elo40k/matches_json.go)")
	asOf := flag.String("as-of", "", "apply final inactivity decay through this date (2006-01-02 or RFC3339); default: now")
	topN := flag.Int("n", 0, "if > 0, print only top N rows")
	outJSON := flag.String("out-json", "", "if set, write full leaderboard snapshot JSON here (for bots; ignores -n)")
	flag.Parse()

	if strings.TrimSpace(*matchesPath) == "" {
		fmt.Fprintln(os.Stderr, "Usage: local-elo -matches games.json [-as-of DATE] [-n 50] [-out-json leaderboard.json]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "JSON rows look like:")
		fmt.Fprintln(os.Stderr, `  { "date": "2025-03-01", "a": "Alice Example", "b": "Bob Example", "winner": "a" }`)
		fmt.Fprintln(os.Stderr, "Optional: -out-json leaderboard.json for Discord bot / APIs.")
		os.Exit(2)
	}

	ms, err := elo40k.LoadMatchesJSON(*matchesPath)
	if err != nil {
		log.Fatalf("matches: %v", err)
	}

	e := elo40k.NewEngine()
	e.PlayAll(ms)

	var cutoff time.Time
	if strings.TrimSpace(*asOf) == "" {
		cutoff = time.Now()
	} else {
		var perr error
		for _, lay := range []string{time.RFC3339, "2006-01-02"} {
			cutoff, perr = time.ParseInLocation(lay, strings.TrimSpace(*asOf), time.Local)
			if perr == nil {
				break
			}
		}
		if perr != nil {
			log.Fatalf("as-of: %v", perr)
		}
	}
	e.FinalizeDecay(cutoff)

	rows := e.Snapshot()
	if p := strings.TrimSpace(*outJSON); p != "" {
		if err := elodata.WriteLeaderboardJSON(p, cutoff, rows); err != nil {
			log.Fatalf("out-json: %v", err)
		}
		fmt.Fprintf(os.Stderr, "wrote %d players → %s\n", len(rows), p)
	}

	limit := len(rows)
	if *topN > 0 && *topN < limit {
		limit = *topN
	}

	fmt.Printf("%-6s | %-36s | %8s | %s\n", "Rank", "Player", "Elo", "Games")
	fmt.Println(strings.Repeat("-", 62))
	for i := 0; i < limit; i++ {
		p := rows[i]
		fmt.Printf("%-6d | %-36s | %8.1f | %d\n", i+1, trunc(p.DisplayName, 36), p.Rating, p.Games)
	}
	if *topN > 0 && len(rows) > limit {
		fmt.Fprintf(os.Stderr, "\n(showing top %d of %d players)\n", limit, len(rows))
	}
}

func trunc(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
