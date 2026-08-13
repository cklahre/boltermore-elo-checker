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
	eventID := flag.String("event", "", "BCP event id (or full event URL)")
	interval := flag.Uint("min-interval-ms", 400, "min ms between BCP HTTP requests")
	limit := flag.Int("limit", 0, "max rows per table (0 = no cap)")
	bcpTok := flag.String("bcp-token", "", "BCP bearer JWT for GET /v1/armylists (or env BCP_BEARER_TOKEN)")
	noDetachment := flag.Bool("no-detachment", false, "skip one request per list for detachment text")
	flag.Parse()

	id := strings.TrimSpace(*eventID)
	if id == "" {
		fmt.Fprintln(os.Stderr, "Usage: elo-factions -event EVENT_ID [-bcp-token JWT] [-no-detachment] [-min-interval-ms 400] [-limit N]")
		os.Exit(2)
	}
	// Allow pasting a full BCP URL
	if i := strings.Index(strings.ToLower(id), "/event/"); i >= 0 {
		rest := id[i+len("/event/"):]
		if j := strings.IndexAny(rest, "?#/"); j >= 0 {
			rest = rest[:j]
		}
		id = strings.TrimSpace(rest)
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
	active := bcp.ActiveRoster(roster)

	title := id
	if ev != nil && strings.TrimSpace(ev.Name) != "" {
		title = strings.TrimSpace(ev.Name)
	}

	byArmy := bcp.FactionCounts(active, func(p bcp.RosterPlayer) string { return p.ArmyFactionName() })
	byDisp := bcp.FactionCounts(active, func(p bcp.RosterPlayer) string { return p.DispositionName() })

	fmt.Printf("Faction / disposition · %s · %s\n", title, id)
	fmt.Printf("Players: %d active", len(active))
	if dropped := len(roster) - len(active); dropped > 0 {
		fmt.Printf(" · %d dropped (ignored)", dropped)
	}
	fmt.Println()
	fmt.Println()

	fmt.Println("By faction")
	printTable(byArmy, *limit)
	fmt.Println()
	fmt.Println("By disposition")
	printTable(byDisp, *limit)

	wantDetachment := c.BearerToken != "" && !*noDetachment
	if wantDetachment {
		ids := bcp.UniqueListIDs(active)
		if len(ids) == 0 {
			fmt.Println("\nNo list ids on active roster — cannot load detachments.")
		} else {
			det, failed := bcp.ListDetachmentIndex(c, ids)
			tree := bcp.ArmyDetachmentTree(active, det, failed)
			fmt.Printf("\nArmy → detachment (lists loaded: %d)\n\n", len(ids))
			fmt.Print(bcp.FormatArmyDetachmentTree(tree, false))
			fmt.Println()
		}
	} else if c.BearerToken == "" {
		fmt.Println("\nTip: set BCP_BEARER_TOKEN for optional army → detachment detail (disposition already comes from the public roster).")
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
