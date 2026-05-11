// Command elo-roster prints a BCP event roster with Elo from local-elo -out-json (same idea as the Discord /elo-roster).
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"fortyk/eloevent/internal/bcp"
	"fortyk/eloevent/internal/elo40k"
	"fortyk/eloevent/pkg/elodata"
)

func main() {
	eventID := flag.String("event", "", "BCP event id")
	leaderPath := flag.String("leaderboard", "leaderboard.json", "JSON from local-elo -out-json")
	limit := flag.Int("limit", 0, "max rows to print (0 = all)")
	interval := flag.Uint("min-interval-ms", 400, "min ms between BCP HTTP requests")
	flag.Parse()

	id := strings.TrimSpace(*eventID)
	if id == "" {
		fmt.Fprintln(os.Stderr, "Usage: elo-roster -event EVENT_ID [-leaderboard leaderboard.json] [-limit N]")
		os.Exit(2)
	}

	lb, err := elodata.ReadLeaderboardJSON(*leaderPath)
	if err != nil {
		log.Fatalf("leaderboard: %v", err)
	}

	c := &bcp.Client{MinInterval: time.Duration(*interval) * time.Millisecond}
	roster, err := bcp.FetchRoster(c, id)
	if err != nil {
		log.Fatalf("bcp roster: %v", err)
	}

	byRow := lb.RowByKey()
	type row struct {
		name string
		elo  float64
		drop bool
	}
	var lines []row
	for _, p := range roster {
		n := p.FullName()
		if n == "" {
			n = "(unnamed)"
		}
		k := elo40k.PlayerKey(n)
		elo := elo40k.Baseline
		if r, ok := byRow[k]; ok {
			elo = r.Elo
		}
		lines = append(lines, row{name: n, elo: elo, drop: p.Dropped})
	}
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].elo != lines[j].elo {
			return lines[i].elo > lines[j].elo
		}
		return lines[i].name < lines[j].name
	})

	fmt.Printf("Roster Elo · %s · %d players (not in %s → %.0f baseline)\n", id, len(lines), *leaderPath, elo40k.Baseline)
	fmt.Printf("Leaderboard as_of: %s\n\n", lb.AsOfRFC3339)

	max := len(lines)
	if *limit > 0 && *limit < max {
		max = *limit
	}
	for i := 0; i < max; i++ {
		ln := lines[i]
		tag := ""
		if ln.drop {
			tag = "  [dropped]"
		}
		fmt.Printf("%5.1f  %-40s%s\n", ln.elo, trunc(ln.name, 40), tag)
	}
	if len(lines) > max {
		fmt.Printf("\n… +%d more (use -limit 0 for all)\n", len(lines)-max)
	}
}

func trunc(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
