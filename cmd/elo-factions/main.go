// Command elo-factions prints BCP faction / army → detachments (same idea as Discord /elo-factions).
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"fortyk/eloevent/internal/bcp"
)

func main() {
	eventID := flag.String("event", "", "BCP event id")
	interval := flag.Uint("min-interval-ms", 400, "min ms between BCP HTTP requests")
	limit := flag.Int("limit", 0, "max rows for faction-only table (0 = no cap; ignored when detachments load)")
	bcpTok := flag.String("bcp-token", "", "BCP bearer JWT for GET /v1/armylists (or env BCP_BEARER_TOKEN)")
	noDetachment := flag.Bool("no-detachment", false, "skip one request per list for detachment text")
	flag.Parse()

	id := strings.TrimSpace(*eventID)
	if id == "" {
		fmt.Fprintln(os.Stderr, "Usage: elo-factions -event EVENT_ID [-bcp-token JWT] [-no-detachment] [-min-interval-ms 400] [-limit N]")
		os.Exit(2)
	}

	c := &bcp.Client{MinInterval: time.Duration(*interval) * time.Millisecond}
	tok := strings.TrimSpace(*bcpTok)
	if tok == "" {
		tok = strings.TrimSpace(os.Getenv("BCP_BEARER_TOKEN"))
	}
	c.BearerToken = tok

	ev, err := bcp.FetchEvent(c, id)
	if err != nil {
		log.Fatalf("bcp event: %v", err)
	}
	roster, err := bcp.FetchRoster(c, id)
	if err != nil {
		log.Fatalf("bcp roster: %v", err)
	}

	title := id
	if ev != nil && strings.TrimSpace(ev.Name) != "" {
		title = strings.TrimSpace(ev.Name)
	}

	byArmy := bcp.FactionCounts(roster, func(p bcp.RosterPlayer) string { return p.ArmyFactionName() })

	fmt.Printf("Faction breakdown · %s · %s\n", title, id)
	fmt.Printf("Players: %d (BCP roster; includes dropped)\n\n", len(roster))

	wantDetachment := c.BearerToken != "" && !*noDetachment
	if wantDetachment {
		ids := bcp.UniqueListIDs(roster)
		if len(ids) == 0 {
			fmt.Println("No list ids on roster — cannot load lists.")
			fmt.Println("By faction (army)")
			printTable(byArmy, *limit)
		} else {
			det, failed := bcp.ListDetachmentIndex(c, ids)
			tree := bcp.ArmyDetachmentTree(roster, det, failed)
			fmt.Printf("Lists loaded: %d (GET /v1/armylists)\n\n", len(ids))
			fmt.Print(bcp.FormatArmyDetachmentTree(tree, false))
			fmt.Println()
		}
	} else {
		fmt.Println("By faction (army)")
		printTable(byArmy, *limit)
		if c.BearerToken == "" {
			fmt.Println("\nTip: set BCP_BEARER_TOKEN or -bcp-token to a BCP JWT (DevTools → Network on bestcoastpairings.com while logged in) for army → detachment breakdown.")
		} else {
			fmt.Println("\nDetachment fetch skipped (-no-detachment).")
		}
	}
}

func printTable(rows []bcp.CountRow, limit int) {
	if len(rows) == 0 {
		fmt.Println("  (none)")
		return
	}
	n := len(rows)
	if limit > 0 && limit < n {
		n = limit
	}
	for i := 0; i < n; i++ {
		r := rows[i]
		fmt.Printf("  %4d  %s\n", r.Count, r.Label)
	}
	if limit > 0 && len(rows) > limit {
		fmt.Printf("  … +%d more rows\n", len(rows)-limit)
	}
}
