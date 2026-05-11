// Command statcheck-elo-leaderboard prints the global Stat-Check Elo ranking:
// every player in the feed, ordered by Elo (same basis as https://stat-check.com/elo).
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"fortyk/eloevent/internal/statcheckelo"
)

func main() {
	topN := flag.Int("n", 0, "if > 0, only print the top N players")
	flag.Parse()

	eloFile := os.Getenv("STATCHECK_ELO_FILE")
	statURL := os.Getenv("STATCHECK_ELO_URL")
	if statURL == "" {
		statURL = statcheckelo.DefaultEloJSONURL
	}

	var players []statcheckelo.StatCheckPlayer
	var err error
	if eloFile != "" {
		fmt.Fprintf(os.Stderr, "Loading Elo data from file %s...\n", eloFile)
		players, err = statcheckelo.LoadEloPlayersFromFile(eloFile)
	} else {
		fmt.Fprintf(os.Stderr, "Fetching Elo data from %s...\n", statURL)
		players, err = statcheckelo.FetchEloPlayers(statURL)
	}
	if err != nil {
		log.Fatalf("load Elo data: %v", err)
	}

	statcheckelo.SortByEloDesc(players)

	limit := len(players)
	if *topN > 0 && *topN < limit {
		limit = *topN
	}

	fmt.Printf("%-6s | %-40s | %s\n", "Rank", "Player", "Elo")
	fmt.Println(strings.Repeat("-", 58))
	for i := 0; i < limit; i++ {
		p := players[i]
		fmt.Printf("%-6d | %-40s | %.1f\n", i+1, trunc(p.PlayerName, 40), p.Elo)
	}
	if *topN > 0 && len(players) > limit {
		fmt.Fprintf(os.Stderr, "\n(showing top %d of %d players)\n", limit, len(players))
	}
}

func trunc(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
