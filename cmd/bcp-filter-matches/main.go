// Command bcp-filter-matches drops rows from a bcp-export-matches JSON file whose event_id
// is not listed in an ids file, so you can shrink an existing export without re-crawling BCP.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"fortyk/eloevent/internal/bcp"
)

func main() {
	idsPath := flag.String("ids", "", "newline-separated event ids (same as bcp-export-matches -events-file)")
	matchesPath := flag.String("matches", "", "input JSON produced by bcp-export-matches")
	outPath := flag.String("out", "", "write filtered JSON here")
	keepNoEventID := flag.Bool("keep-no-event-id", false, "keep rows with empty event_id (default: drop them)")
	flag.Parse()

	if strings.TrimSpace(*idsPath) == "" || strings.TrimSpace(*matchesPath) == "" || strings.TrimSpace(*outPath) == "" {
		fmt.Fprintln(os.Stderr, "Usage: bcp-filter-matches -ids ids.txt -matches bcp-matches.json -out filtered.json")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Optional: -keep-no-event-id keeps rows missing event_id (normally discarded).")
		os.Exit(2)
	}

	allowed, err := readEventIDs(*idsPath)
	if err != nil {
		log.Fatal(err)
	}

	body, err := os.ReadFile(*matchesPath)
	if err != nil {
		log.Fatal(err)
	}
	var rows []bcp.MatchFileRow
	if err := json.Unmarshal(body, &rows); err != nil {
		log.Fatalf("matches JSON: %v", err)
	}

	var out []bcp.MatchFileRow
	var droppedNoID, droppedNotListed int
	for _, r := range rows {
		eid := strings.TrimSpace(r.EventID)
		if eid == "" {
			if *keepNoEventID {
				out = append(out, r)
				continue
			}
			droppedNoID++
			continue
		}
		if _, ok := allowed[eid]; !ok {
			droppedNotListed++
			continue
		}
		out = append(out, r)
	}

	raw, err := bcp.MarshalMatchFileJSON(out)
	if err != nil {
		log.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(*outPath, raw, 0o644); err != nil {
		log.Fatal(err)
	}

	fmt.Fprintf(os.Stderr, "filter: %d → %d rows (allowlist=%d ids; dropped not in list=%d; dropped missing event_id=%d) → %s\n",
		len(rows), len(out), len(allowed), droppedNotListed, droppedNoID, *outPath)
}

func readEventIDs(path string) (map[string]struct{}, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	allowed := make(map[string]struct{})
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		allowed[line] = struct{}{}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(allowed) == 0 {
		return nil, fmt.Errorf("no event ids in %s", path)
	}
	return allowed, nil
}
