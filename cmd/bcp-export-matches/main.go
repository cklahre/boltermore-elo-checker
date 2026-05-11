// Command bcp-export-matches downloads Swiss pairings + scores from Best Coast Pairings
// for one or many events and writes a JSON match list usable with local-elo (internal/elo40k).
//
// Requires pairings to exist (pairingType=Pairing on the API).
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"fortyk/eloevent/internal/bcp"
)

func main() {
	eventID := flag.String("event", "", "single BCP event id")
	eventsFile := flag.String("events-file", "", "newline-separated event ids (or use -event for one)")
	outPath := flag.String("out", "", "write JSON here (default: stdout)")
	maxRound := flag.Int("max-round", 0, "if > 0, stop after this round for each event")
	continueOnError := flag.Bool("continue-on-error", false, "when exporting many events: log and skip failures")
	dedupe := flag.Bool("dedupe", true, "drop duplicate pairing_id (or fallback fingerprint) when merging")
	sleepMs := flag.Int("sleep-ms", 0, "optional pause between events when exporting many (reduces rate limits)")
	minPlayers := flag.Int("min-players", 0, "skip events with totalPlayers below this (0 = off; GTs: try 30 or 31)")
	minRounds := flag.Int("min-rounds", 0, "skip events with numberOfRounds below this (0 = off; GTs: try 5)")
	sinceStr := flag.String("since", "", "skip events before this calendar day in UTC (layout 2006-01-02); improves yield vs legacy BCP data")
	maxSpanDays := flag.Int("max-span-days", 0, "skip if (eventEndDate−eventDate) exceeds this many UTC calendar days (0=off; try 4–7 to drop long leagues)")
	excludeName := flag.String("exclude-name", "", "comma-separated case-insensitive substrings; skip if event name contains any (e.g. league,season,ladder)")
	flag.Parse()

	ids, err := collectEventIDs(*eventID, *eventsFile)
	if err != nil {
		log.Fatal(err)
	}
	if len(ids) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: bcp-export-matches -event ID | -events-file ids.txt [-out matches.json]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Build ids.txt with bcp-discover-events, your own list, or pasting ids from BCP.")
		os.Exit(2)
	}

	sinceUTC, err := bcp.ParseSinceDay(*sinceStr)
	if err != nil {
		log.Fatalf("-since: %v", err)
	}
	meta := bcp.MetadataFilterFromFlags(*minPlayers, *minRounds, *maxSpanDays, sinceUTC, *excludeName)

	c := &bcp.Client{}
	multi := len(ids) > 1
	cont := *continueOnError || multi

	var all []bcp.MatchFileRow
	for i, id := range ids {
		if i > 0 && *sleepMs > 0 {
			time.Sleep(time.Duration(*sleepMs) * time.Millisecond)
		}
		ev, err := bcp.FetchEvent(c, id)
		if err != nil {
			if cont {
				log.Printf("skip event %s: fetch metadata: %v", id, err)
				continue
			}
			log.Fatalf("event %s: %v", id, err)
		}
		if r := bcp.EventSkippedByFilter(ev, meta); r != "" {
			if cont {
				log.Printf("skip event %s (%s): %s", id, ev.Name, r)
				continue
			}
			log.Fatalf("event %s (%s): %s", id, ev.Name, r)
		}
		rows, st, err := bcp.ExportMatchesForEvent(c, ev, *maxRound)
		if err != nil {
			if cont {
				log.Printf("skip event %s (%s): %v", id, st.EventName, err)
				continue
			}
			log.Fatalf("event %s: %v", id, err)
		}
		if len(rows) == 0 {
			if cont {
				log.Printf("no games exported for %s (%s); skipped=%d", id, st.EventName, st.Skipped)
				continue
			}
			log.Fatalf("no games exported for %s (%s); skipped=%d", id, st.EventName, st.Skipped)
		}
		fmt.Fprintf(os.Stderr, "event %s (%s): %d games (rounds≤%d, skipped rows=%d)\n", id, st.EventName, len(rows), st.RoundsTried, st.Skipped)
		all = append(all, rows...)
	}

	if len(all) == 0 {
		log.Fatal("no games exported in total")
	}

	if *dedupe {
		before := len(all)
		all = bcp.DedupeMatchRows(all)
		if before != len(all) {
			fmt.Fprintf(os.Stderr, "dedupe: %d → %d rows\n", before, len(all))
		}
	}

	raw, err := bcp.MarshalMatchFileJSON(all)
	if err != nil {
		log.Fatalf("json: %v", err)
	}

	if strings.TrimSpace(*outPath) != "" {
		if err := os.WriteFile(*outPath, raw, 0o644); err != nil {
			log.Fatalf("write: %v", err)
		}
		fmt.Fprintf(os.Stderr, "Wrote %d matches → %s (for local-elo: -matches %s)\n", len(all), *outPath, *outPath)
		return
	}
	os.Stdout.Write(raw)
	os.Stdout.Write([]byte("\n"))
}

func collectEventIDs(single, path string) ([]string, error) {
	var out []string
	if strings.TrimSpace(single) != "" {
		out = append(out, strings.TrimSpace(single))
	}
	if strings.TrimSpace(path) == "" {
		return out, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
