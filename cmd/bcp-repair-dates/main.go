// Command bcp-repair-dates rewrites match dates written by the old export, which dated
// round N as event start + (N-1) days (so an 8-round GT spanned 8 days, often into the future).
// Rounds are re-placed inside each event's real start/end dates (bcp.MatchExportOptions.RoundTime).
// Also fills event_name on rows exported before names were recorded.
//
// Fetches event metadata from BCP (cached in -meta-cache so reruns skip known events) and
// rewrites each shard in place (or to -out-dir). Safe to rerun: already-repaired rows are left alone.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"fortyk/eloevent/internal/bcp"
)

func main() {
	matchesManifest := flag.String("matches-manifest", "", "manifest of match shards to repair")
	var matchShards matchPathsFlag
	flag.Var(&matchShards, "matches", "match JSON shard (repeatable); ignored when -matches-manifest is set")
	metaCache := flag.String("meta-cache", "bcp-event-meta-cache.json", "event metadata cache (read + updated); empty = no cache")
	outDir := flag.String("out-dir", "", "if set, write repaired shards here (same basenames); default: rewrite in place")
	sleepMs := flag.Int("sleep-ms", 300, "pause between BCP metadata fetches")
	dryRun := flag.Bool("dry-run", false, "report what would change without writing shards")
	flag.Parse()

	paths, err := bcp.ResolveMatchShardPaths(*matchesManifest, matchShards, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Usage: bcp-repair-dates -matches-manifest bcp-matches.manifest [-meta-cache cache.json] [-dry-run]")
		log.Fatal(err)
	}

	shards := make([][]bcp.MatchFileRow, len(paths))
	idSet := map[string]struct{}{}
	for i, p := range paths {
		rows, err := bcp.ReadMatchFileJSON(p)
		if err != nil {
			log.Fatalf("%s: %v", p, err)
		}
		shards[i] = rows
		for _, r := range rows {
			if eid := strings.TrimSpace(r.EventID); eid != "" {
				idSet[eid] = struct{}{}
			}
		}
	}
	fmt.Fprintf(os.Stderr, "loaded %d shards (%d unique event ids)\n", len(paths), len(idSet))

	meta := map[string]*bcp.Event{}
	if *metaCache != "" {
		if err := loadMetaCache(*metaCache, meta); err != nil {
			log.Fatalf("meta-cache: %v", err)
		}
		fmt.Fprintf(os.Stderr, "meta-cache: %d events from %s\n", len(meta), *metaCache)
	}

	var need []string
	for id := range idSet {
		if meta[id] == nil {
			need = append(need, id)
		}
	}
	sort.Strings(need)

	client := &bcp.Client{MinInterval: time.Duration(*sleepMs) * time.Millisecond}
	var fetchFail int
	for i, id := range need {
		ev, err := bcp.FetchEvent(client, id)
		if err != nil {
			fetchFail++
			log.Printf("fetch %s [%d/%d]: %v", id, i+1, len(need), err)
			continue
		}
		meta[id] = &bcp.Event{
			ID:             id,
			Name:           ev.Name,
			EventDate:      ev.EventDate,
			EventEndDate:   ev.EventEndDate,
			NumberOfRounds: ev.NumberOfRounds,
		}
		if (i+1)%50 == 0 || i+1 == len(need) {
			fmt.Fprintf(os.Stderr, "fetched %d/%d (fail=%d)\n", i+1, len(need), fetchFail)
			if *metaCache != "" {
				if err := saveMetaCache(*metaCache, meta); err != nil {
					log.Fatalf("meta-cache: %v", err)
				}
			}
		}
	}

	st := bcp.RedateLegacyShards(shards, meta)
	fmt.Fprintf(os.Stderr, "rows=%d changed=%d skipped=%d events_changed=%d events_missing_meta=%d names_filled=%d fetch_fail=%d\n",
		st.Rows, st.Changed, st.Skipped, st.EventsChanged, st.EventsMissingMeta, st.NamesFilled, fetchFail)

	if *dryRun {
		fmt.Fprintln(os.Stderr, "dry-run: no shards written")
		return
	}

	for i, p := range paths {
		raw, err := bcp.MarshalMatchFileJSON(shards[i])
		if err != nil {
			log.Fatalf("marshal %s: %v", p, err)
		}
		dest := p
		if d := strings.TrimSpace(*outDir); d != "" {
			if err := os.MkdirAll(d, 0o755); err != nil {
				log.Fatal(err)
			}
			dest = filepath.Join(d, filepath.Base(p))
		}
		tmp := dest + ".tmp"
		if err := os.WriteFile(tmp, raw, 0o644); err != nil {
			log.Fatalf("write %s: %v", tmp, err)
		}
		if err := os.Rename(tmp, dest); err != nil {
			log.Fatalf("rename %s → %s: %v", tmp, dest, err)
		}
		fmt.Fprintf(os.Stderr, "wrote %s (%d rows)\n", dest, len(shards[i]))
	}
}

type matchPathsFlag []string

func (m *matchPathsFlag) String() string { return strings.Join([]string(*m), ", ") }

func (m *matchPathsFlag) Set(v string) error {
	*m = append(*m, strings.TrimSpace(v))
	return nil
}

func loadMetaCache(path string, into map[string]*bcp.Event) error {
	body, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(body, &into)
}

func saveMetaCache(path string, meta map[string]*bcp.Event) error {
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
