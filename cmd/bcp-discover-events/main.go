// Command bcp-discover-events lists BCP event ids using the same public geo search the web app uses
// (GET /v1/events with gameSystemId, latitude, longitude, distance, searchString, nextKey).
// There is no single global feed; repeat with different -search terms and/or map centers to widen coverage.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"fortyk/eloevent/internal/bcp"
)

func main() {
	preset := flag.String("preset", "", "if set: global (1 pin) | global-grid | us | eu | apac | minimal (see bcp-harvest-events)")
	lat := flag.Float64("lat", 0, "search center latitude (ignored if -preset)")
	lon := flag.Float64("lon", 0, "search center longitude (ignored if -preset)")
	distance := flag.Int("distance", 20000, "search radius (same units as BCP’s web search)")
	gameSys := flag.String("game-system-id", "WGMSzfKFYA", "BCP game system id (WGMSzfKFYA = Warhammer 40k)")
	searches := flag.String("searches", "", "comma-separated search strings; each must be ≥3 characters (BCP rule)")
	maxPages := flag.Int("max-pages-per-search", 50, "stop after this many nextKey pages per search term (0 = unlimited)")
	asJSON := flag.Bool("json", false, "print JSON array of {id,name,eventDate,ended} instead of ids")
	verbose := flag.Bool("v", false, "with default output: id<TAB>name<TAB>eventDate<TAB>ended")
	minPlayers := flag.Int("min-players", 0, "omit events with totalPlayers below this (0 = off); same rules as bcp-export-matches")
	minRounds := flag.Int("min-rounds", 0, "omit events with numberOfRounds below this (0 = off)")
	sinceStr := flag.String("since", "", "omit events before this UTC day (2006-01-02); same rules as bcp-export-matches")
	maxSpanDays := flag.Int("max-span-days", 0, "omit if event date span exceeds this many UTC days (0=off; see bcp-export-matches)")
	excludeName := flag.String("exclude-name", "", "comma-separated name substrings to drop (case-insensitive)")
	flag.Parse()

	var centers []bcp.GeoCenter
	if strings.TrimSpace(*preset) != "" {
		centers = bcp.PresetCenters(*preset)
		if len(centers) == 0 {
			log.Fatalf("unknown -preset %q (try global, global-grid, us, eu, apac, minimal)", *preset)
		}
	} else {
		if *lat == 0 && *lon == 0 {
			fmt.Fprintln(os.Stderr, "Usage: bcp-discover-events (-preset global|global-grid|us|eu|apac|minimal) | (-lat LAT -lon LON) -searches \"...\"")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "Example (US east coarse pass):")
			fmt.Fprintln(os.Stderr, `  bcp-discover-events -lat 39.5 -lon -77.0 -searches "war,tournament,open,grand,gt,day,itc"`)
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "Wide crawl + ids file: go run ./cmd/bcp-harvest-events")
			fmt.Fprintln(os.Stderr, "Then: go run ./cmd/bcp-export-matches -events-file bcp-event-ids.txt -out matches.json")
			os.Exit(2)
		}
		centers = []bcp.GeoCenter{{Name: "cli", Lat: *lat, Lon: *lon}}
	}

	terms := splitTerms(*searches)
	if len(terms) == 0 {
		log.Fatal("-searches is required (comma-separated, each term ≥3 characters)")
	}

	c := &bcp.Client{}
	seen := make(map[string]bcp.EventListHit)
	for _, pin := range centers {
		for _, term := range terms {
			if len([]rune(term)) < 3 {
				log.Fatalf("search term %q is shorter than 3 characters", term)
			}
			p := bcp.EventSearchParams{
				Limit:        100,
				GameSystemID: *gameSys,
				Latitude:     pin.Lat,
				Longitude:    pin.Lon,
				Distance:     *distance,
				SearchString: term,
			}
			maxP := *maxPages
			if maxP == 0 {
				maxP = 0
			}
			hits, err := bcp.SearchEventsAll(c, p, maxP)
			if err != nil {
				log.Fatalf("center %s term %q: %v", pin.Name, term, err)
			}
			for _, h := range hits {
				seen[h.ID] = h
			}
			fmt.Fprintf(os.Stderr, "center %s search %q → %d hits (unique total %d)\n", pin.Name, term, len(hits), len(seen))
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

	if *asJSON {
		enc, err := json.MarshalIndent(list, "", "  ")
		if err != nil {
			log.Fatal(err)
		}
		os.Stdout.Write(enc)
		os.Stdout.Write([]byte("\n"))
		return
	}

	for _, h := range list {
		if *verbose {
			fmt.Printf("%s\t%s\t%s\t%v\n", h.ID, h.Name, h.EventDate, h.Ended)
		} else {
			fmt.Println(h.ID)
		}
	}
}

func splitTerms(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
