// Command player-history prints a player's match record and recent games from a bcp-export-matches JSON file (or any compatible array).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"fortyk/eloevent/internal/bcp"
	"fortyk/eloevent/pkg/elodata"
)

func main() {
	matchesPath := flag.String("matches", "", "JSON from bcp-export-matches (or same shape)")
	playerQuery := flag.String("player", "", "player name (see -contains)")
	contains := flag.Bool("contains", false, "match if name contains -player (substring, case-insensitive)")
	lastN := flag.Int("last", 10, "show this many most recent games (0 = all, newest first)")
	flag.Parse()

	if strings.TrimSpace(*matchesPath) == "" || strings.TrimSpace(*playerQuery) == "" {
		fmt.Fprintln(os.Stderr, "Usage: player-history -matches games.json -player \"Player Name\" [-contains] [-last 10]")
		os.Exit(2)
	}

	body, err := os.ReadFile(*matchesPath)
	if err != nil {
		log.Fatal(err)
	}
	var rows []bcp.MatchFileRow
	if err := json.Unmarshal(body, &rows); err != nil {
		log.Fatalf("matches JSON: %v", err)
	}

	rep, err := elodata.PlayerLookup(rows, *playerQuery, *contains, *lastN)
	if err != nil {
		log.Fatal(err)
	}
	if rep == nil {
		if *contains {
			log.Fatalf("no games found where a name contains %q", *playerQuery)
		}
		log.Fatalf("no games found for exact name %q (try -contains)", *playerQuery)
	}

	if rep.MultiNameWarning {
		fmt.Fprintf(os.Stderr, "warning: -contains matched multiple roster names (combined record below)\n")
	}

	fmt.Printf("Player: %s\n", rep.DisplayName)
	fmt.Printf("Games:  %d  (record %d–%d–%d)\n", rep.Wins+rep.Losses+rep.Draws, rep.Wins, rep.Losses, rep.Draws)
	fmt.Printf("Win %%:  %.1f%%  (wins / games played)\n", rep.WinPct)
	fmt.Printf("Pts %%:  %.1f%%  ((wins + ½×draws) / games; chess-style)\n", rep.PointsPct)
	fmt.Fprintln(os.Stderr, "ΔElo: change for that game after inactivity decay at game time, same rules as local-elo (full match file replay).")

	if len(rep.RecentEvents) > 0 {
		fmt.Printf("\nLast %d events (by newest game played in each event):\n", len(rep.RecentEvents))
		fmt.Printf("%-12s %-40s %-12s %8s %12s %-12s\n", "Last day", "Event id", "Record W-L-D", "Games", "Σ ΔElo", "Rated Δs")
		fmt.Println(strings.Repeat("-", 106))
		for _, ev := range rep.RecentEvents {
			evID := strings.TrimSpace(ev.EventID)
			if evID == "" {
				evID = "—"
			}
			last := ev.LastPlayed.UTC().Format("2006-01-02")
			sumDE := "        —"
			if ev.DeltaGames > 0 {
				sumDE = fmt.Sprintf("%+11.1f", ev.TotalDeltaElo)
			}
			fmt.Printf("%-12s %-40s %3d-%2d-%2d %8d %12s %4d/%d\n",
				last, trunc(evID, 40),
				ev.Wins, ev.Losses, ev.Draws,
				ev.Games, sumDE, ev.DeltaGames, ev.Games)
		}
	}
	n := len(rep.Games)
	if *lastN > 0 {
		fmt.Printf("\nLast %d games (newest first):\n", n)
	} else {
		fmt.Printf("\nAll %d games (newest first):\n", n)
	}
	fmt.Printf("%-24s %6s %7s %-36s %-36s %s\n", "date", "res", "ΔElo", "opponent", "you were", "event_id")
	fmt.Println(strings.Repeat("-", 150))
	for _, g := range rep.Games {
		youWere := "B"
		if g.AsA {
			youWere = "A"
		}
		res := fmt.Sprintf("%c", g.Result)
		ts := g.Time.Format(time.RFC3339)
		if len(ts) > 24 {
			ts = ts[:24]
		}
		de := "    —"
		if g.DeltaElo != nil {
			d := *g.DeltaElo
			if d >= 0 {
				de = fmt.Sprintf("+%.1f", d)
			} else {
				de = fmt.Sprintf("%.1f", d)
			}
		}
		fmt.Printf("%-24s %6s %7s %-36s %-36s %s\n", ts, res, de, trunc(g.Opponent, 36), youWere, g.EventID)
	}
}

func trunc(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
