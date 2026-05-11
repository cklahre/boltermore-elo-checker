// Command bcp-harvest-events runs a wide geo × search-string crawl of BCP’s public /v1/events API,
// merges unique event ids, writes them to disk, and optionally exports all match results.
//
// Default -preset global uses a single map pin: with Warhammer 40k + a large -distance, BCP’s search
// already returns effectively catalog-wide rows, so a 70+ pin grid mostly duplicates the same ids.
// Use -preset global-grid for the full grid, or -no-stop-on-diminish to always run every pin.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"fortyk/eloevent/internal/bcp"
)

func main() {
	preset := flag.String("preset", "global", "map grid: global (1 pin) | global-grid (us+eu+apac) | us | eu | apac | minimal")
	searches := flag.String("searches", "", "optional comma-separated extra search terms (each ≥3 chars); merged with built-ins unless -searches-only")
	searchesOnly := flag.Bool("searches-only", false, "use only -searches terms (no built-in list)")
	distance := flag.Int("distance", 30000, "search radius per center (same field as BCP’s web search)")
	gameSys := flag.String("game-system-id", "WGMSzfKFYA", "Warhammer 40k game system id on BCP")
	maxPages := flag.Int("max-pages-per-search", 100, "max nextKey pages per (center × term); 0 = unlimited")
	outIDs := flag.String("out-ids", "bcp-event-ids.txt", "write newline-separated event ids here (empty = skip)")
	outJSON := flag.String("out-events-json", "", "optional: write merged event list JSON for auditing")
	minInterval := flag.Uint("min-interval-ms", 350, "minimum ms between HTTP GETs (client-wide throttle)")
	exportMatches := flag.String("export-matches", "", "optional: after discovery, export pairings to this JSON path")
	exportDedupe := flag.Bool("export-dedupe", true, "with -export-matches: dedupe pairing ids")
	exportMaxRound := flag.Int("export-max-round", 0, "with -export-matches: cap rounds per event (0 = heuristic)")
	exportSleepMs := flag.Int("export-sleep-ms", 300, "with -export-matches: pause between each event")
	stopDiminish := flag.Bool("stop-on-diminish", true, "with multi-pin presets: stop after a full center if it adds almost no new ids (BCP overlap)")
	dimMinNew := flag.Int("diminish-min-new", 25, "with -stop-on-diminish: minimum new ids a center must add to continue")
	dimMinPct := flag.Float64("diminish-min-pct", 0.02, "with -stop-on-diminish: also require this %% of current total (e.g. 0.02 = 0.02%%)")
	minPlayers := flag.Int("min-players", 0, "omit events from out files when totalPlayers below this (0 = off); same as bcp-export-matches")
	minRounds := flag.Int("min-rounds", 0, "omit events when numberOfRounds below this (0 = off)")
	sinceStr := flag.String("since", "", "omit events before this UTC day (2006-01-02)")
	maxSpanDays := flag.Int("max-span-days", 0, "omit if event date span exceeds this many UTC days (0=off)")
	excludeName := flag.String("exclude-name", "", "comma-separated name substrings to drop (case-insensitive)")
	flag.Parse()

	centers := bcp.PresetCenters(*preset)
	if len(centers) == 0 {
		log.Fatalf("unknown -preset %q (try global, global-grid, us, eu, apac, minimal)", *preset)
	}

	terms := termList(*searchesOnly, *searches)
	if len(terms) == 0 {
		log.Fatal("no search terms (built-ins missing or pass -searches)")
	}

	client := &bcp.Client{MinInterval: time.Duration(*minInterval) * time.Millisecond}

	seen := make(map[string]bcp.EventListHit)
	passes := 0
	start := time.Now()

	for i, pin := range centers {
		nBefore := len(seen)
		for _, term := range terms {
			if len([]rune(term)) < 3 {
				log.Printf("skip term %q (< 3 chars)", term)
				continue
			}
			p := bcp.EventSearchParams{
				Limit:        100,
				GameSystemID: *gameSys,
				Latitude:     pin.Lat,
				Longitude:    pin.Lon,
				Distance:     *distance,
				SearchString: term,
			}
			hits, err := bcp.SearchEventsAll(client, p, *maxPages)
			if err != nil {
				log.Fatalf("center %s (%f,%f) term %q: %v", pin.Name, pin.Lat, pin.Lon, term, err)
			}
			passes++
			nNew := 0
			for _, h := range hits {
				if h.ID == "" {
					continue
				}
				if _, ok := seen[h.ID]; !ok {
					nNew++
				}
				seen[h.ID] = h
			}
			fmt.Fprintf(os.Stderr, "[%s + %q] +%d new (unique total %d)\n", pin.Name, term, nNew, len(seen))
		}
		newForPin := len(seen) - nBefore
		if *stopDiminish && i > 0 && len(centers) > 1 {
			th := *dimMinNew
			if pct := int(float64(len(seen)) * (*dimMinPct / 100.0)); pct > th {
				th = pct
			}
			if newForPin < th {
				fmt.Fprintf(os.Stderr, "stop-on-diminish: center %q added only %d new ids (threshold %d with %d total). Skipping remaining %d pin(s). At large -distance, BCP’s 40k search overlaps heavily across pins. Use -preset global-grid -stop-on-diminish=false to force every pin.\n",
					pin.Name, newForPin, th, len(seen), len(centers)-i-1)
				break
			}
		}
	}

	list := make([]bcp.EventListHit, 0, len(seen))
	for _, h := range seen {
		list = append(list, h)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].EventDate != list[j].EventDate {
			return list[i].EventDate < list[j].EventDate
		}
		return list[i].ID < list[j].ID
	})

	sinceUTC, err := bcp.ParseSinceDay(*sinceStr)
	if err != nil {
		log.Fatalf("-since: %v", err)
	}
	nBeforeFilter := len(list)
	meta := bcp.MetadataFilterFromFlags(*minPlayers, *minRounds, *maxSpanDays, sinceUTC, *excludeName)
	list = bcp.FilterEventListHits(list, meta)
	if nBeforeFilter != len(list) {
		fmt.Fprintf(os.Stderr, "metadata filter: %d → %d events (min-players=%d min-rounds=%d since=%q max-span-days=%d exclude-name=%q)\n",
			nBeforeFilter, len(list), *minPlayers, *minRounds, strings.TrimSpace(*sinceStr), *maxSpanDays, strings.TrimSpace(*excludeName))
	}

	if strings.TrimSpace(*outIDs) != "" {
		var b strings.Builder
		for _, h := range list {
			b.WriteString(h.ID)
			b.WriteByte('\n')
		}
		if err := os.WriteFile(*outIDs, []byte(b.String()), 0o644); err != nil {
			log.Fatal(err)
		}
		fmt.Fprintf(os.Stderr, "Wrote %d ids → %s\n", len(list), *outIDs)
	}

	if strings.TrimSpace(*outJSON) != "" {
		raw, err := json.MarshalIndent(list, "", "  ")
		if err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(*outJSON, raw, 0o644); err != nil {
			log.Fatal(err)
		}
		fmt.Fprintf(os.Stderr, "Wrote events JSON → %s\n", *outJSON)
	}

	fmt.Fprintf(os.Stderr, "Harvest done: %d unique events, %d (center×term) passes, %s\n",
		len(list), passes, time.Since(start).Round(time.Second))

	if strings.TrimSpace(*exportMatches) == "" {
		return
	}

	ids := make([]string, len(list))
	for i := range list {
		ids[i] = list[i].ID
	}
	if err := exportAllMatches(client, ids, *exportMatches, *exportDedupe, *exportMaxRound, *exportSleepMs); err != nil {
		log.Fatal(err)
	}
}

func termList(searchesOnly bool, extraCSV string) []string {
	var base []string
	if !searchesOnly {
		base = append(base, bcp.DefaultSearchTerms()...)
	}
	for _, p := range splitComma(extraCSV) {
		p = strings.TrimSpace(p)
		if len([]rune(p)) < 3 {
			continue
		}
		base = append(base, p)
	}
	seen := make(map[string]struct{})
	out := make([]string, 0, len(base))
	for _, t := range base {
		k := strings.ToLower(strings.TrimSpace(t))
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, strings.TrimSpace(t))
	}
	return out
}

func splitComma(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func exportAllMatches(c *bcp.Client, ids []string, outPath string, dedupe bool, maxRound, sleepMs int) error {
	var all []bcp.MatchFileRow
	for i, id := range ids {
		if i > 0 && sleepMs > 0 {
			time.Sleep(time.Duration(sleepMs) * time.Millisecond)
		}
		rows, st, err := bcp.ExportEventMatches(c, id, maxRound)
		if err != nil {
			log.Printf("export skip %s (%s): %v", id, st.EventName, err)
			continue
		}
		if len(rows) == 0 {
			log.Printf("export empty %s (%s) skipped=%d", id, st.EventName, st.Skipped)
			continue
		}
		fmt.Fprintf(os.Stderr, "export [%d/%d] %s (%s): %d games\n", i+1, len(ids), id, st.EventName, len(rows))
		all = append(all, rows...)
	}
	if len(all) == 0 {
		return fmt.Errorf("export: no games from any event")
	}
	if dedupe {
		before := len(all)
		all = bcp.DedupeMatchRows(all)
		fmt.Fprintf(os.Stderr, "export dedupe: %d → %d games\n", before, len(all))
	}
	raw, err := bcp.MarshalMatchFileJSON(all)
	if err != nil {
		return err
	}
	if err := os.WriteFile(outPath, raw, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Wrote %d matches → %s (local-elo: -matches %s)\n", len(all), outPath, outPath)
	return nil
}
