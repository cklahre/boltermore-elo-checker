// Command statcheck-elo-leaderboard prints the global Stat-Check Elo ranking:
// every player in the feed, ordered by Elo (same basis as https://stat-check.com/elo).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"fortyk/eloevent/internal/statcheckelo"
)

func main() {
	topN := flag.Int("n", 0, "if > 0, only print the top N players")
	outJSON := flag.String("out-json", "", "if set, write full sorted JSON snapshot here (for static sites; ignores -n)")
	quiet := flag.Bool("quiet", false, "do not print the ASCII table to stdout")
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

	if p := strings.TrimSpace(*outJSON); p != "" {
		if err := writeStatCheckJSON(p, players, eloFile, statURL); err != nil {
			log.Fatalf("out-json: %v", err)
		}
		fmt.Fprintf(os.Stderr, "wrote %d players → %s\n", len(players), p)
	}

	if !*quiet {
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
}

func trunc(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

type statCheckJSONFile struct {
	Source           string                         `json:"source"`
	LoadedFrom       string                         `json:"loaded_from"`
	FetchedAtRFC3339 string                         `json:"fetched_at_rfc3339"`
	Players          []statcheckelo.StatCheckPlayer `json:"players"`
}

func writeStatCheckJSON(path string, players []statcheckelo.StatCheckPlayer, eloFile, statURL string) error {
	src := strings.TrimSpace(eloFile)
	if src == "" {
		src = strings.TrimSpace(statURL)
	}
	f := statCheckJSONFile{
		Source:           "stat-check",
		LoadedFrom:       src,
		FetchedAtRFC3339: time.Now().UTC().Format(time.RFC3339),
		Players:          players,
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}
