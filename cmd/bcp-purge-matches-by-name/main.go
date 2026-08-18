// Command bcp-purge-matches-by-name drops match rows whose BCP event name matches
// exclude-name substrings (same rules as harvest/export MetadataFilter).
//
// Resolves names from an optional events JSON cache, then fetches remaining IDs from BCP.
// Rewrites each input shard in place (or to -out-dir) after a successful pass.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"fortyk/eloevent/internal/bcp"
)

func main() {
	matchesManifest := flag.String("matches-manifest", "", "manifest of match shards to purge")
	var matchShards matchPathsFlag
	flag.Var(&matchShards, "matches", "match JSON shard (repeatable); ignored when -matches-manifest is set")
	excludeName := flag.String("exclude-name", "team,teams", "comma-separated name substrings to drop (case-insensitive)")
	eventsCache := flag.String("events-cache", "", "optional events JSON (id+name) to avoid refetching known events")
	denylistPath := flag.String("denylist", "", "optional: id[\\tname] lines to drop (skips name resolution when set)")
	outDenylist := flag.String("out-denylist", "", "optional: write dropped event id\\tname lines here")
	outDir := flag.String("out-dir", "", "if set, write filtered shards here (same basenames); default: rewrite in place")
	sleepMs := flag.Int("sleep-ms", 300, "pause between BCP metadata fetches")
	dryRun := flag.Bool("dry-run", false, "report what would be dropped without writing shards")
	dropUnresolved := flag.Bool("drop-unresolved", false, "also drop rows whose event name could not be resolved")
	flag.Parse()

	subs := bcp.ParseCommaTerms(*excludeName)
	if len(subs) == 0 && strings.TrimSpace(*denylistPath) == "" {
		log.Fatal("need -exclude-name terms and/or -denylist")
	}

	paths, err := bcp.ResolveMatchShardPaths(*matchesManifest, matchShards, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Usage: bcp-purge-matches-by-name -matches-manifest bcp-matches.manifest [-exclude-name team,teams] [-events-cache bcp-events-new.json]")
		fmt.Fprintln(os.Stderr, "   or: bcp-purge-matches-by-name -matches shard.json -denylist team-events.txt")
		log.Fatal(err)
	}

	shards := make([][]bcp.MatchFileRow, len(paths))
	idSet := map[string]struct{}{}
	var totalRows int
	for i, p := range paths {
		rows, err := bcp.ReadMatchFileJSON(p)
		if err != nil {
			log.Fatalf("%s: %v", p, err)
		}
		shards[i] = rows
		totalRows += len(rows)
		for _, r := range rows {
			if eid := strings.TrimSpace(r.EventID); eid != "" {
				idSet[eid] = struct{}{}
			}
		}
	}
	fmt.Fprintf(os.Stderr, "loaded %d rows across %d shards (%d unique event ids)\n", totalRows, len(paths), len(idSet))

	deny := map[string]string{} // id -> name
	unresolved := map[string]struct{}{}

	if strings.TrimSpace(*denylistPath) != "" {
		n, err := loadDenylist(*denylistPath, deny)
		if err != nil {
			log.Fatalf("denylist: %v", err)
		}
		fmt.Fprintf(os.Stderr, "denylist: loaded %d event ids from %s\n", n, *denylistPath)
	} else {
		nameByID := map[string]string{}
		if strings.TrimSpace(*eventsCache) != "" {
			n, err := loadEventsCache(*eventsCache, nameByID)
			if err != nil {
				log.Fatalf("events-cache: %v", err)
			}
			fmt.Fprintf(os.Stderr, "events-cache: loaded %d names from %s\n", n, *eventsCache)
		}

		needFetch := make([]string, 0)
		for id := range idSet {
			if strings.TrimSpace(nameByID[id]) == "" {
				needFetch = append(needFetch, id)
			}
		}
		sort.Strings(needFetch)

		client := &bcp.Client{MinInterval: time.Duration(*sleepMs) * time.Millisecond}
		var fetchFail int
		for i, id := range needFetch {
			ev, err := bcp.FetchEvent(client, id)
			if err != nil {
				fetchFail++
				log.Printf("fetch %s [%d/%d]: %v", id, i+1, len(needFetch), err)
				continue
			}
			nameByID[id] = ev.Name
			if (i+1)%50 == 0 || i+1 == len(needFetch) {
				fmt.Fprintf(os.Stderr, "fetched names %d/%d (fail=%d)\n", i+1, len(needFetch), fetchFail)
			}
		}

		for id := range idSet {
			name := strings.TrimSpace(nameByID[id])
			if name == "" {
				unresolved[id] = struct{}{}
				continue
			}
			reason := bcp.EventSkippedByFilter(&bcp.Event{ID: id, Name: name}, bcp.MetadataFilter{
				ExcludeNameSubstrings: subs,
			})
			if reason != "" {
				deny[id] = name
			}
		}
	}

	denyIDs := make([]string, 0, len(deny))
	for id := range deny {
		denyIDs = append(denyIDs, id)
	}
	sort.Slice(denyIDs, func(i, j int) bool {
		return strings.ToLower(deny[denyIDs[i]]) < strings.ToLower(deny[denyIDs[j]])
	})

	fmt.Fprintf(os.Stderr, "exclude-name=%q → %d events to purge; unresolved names=%d\n", *excludeName, len(deny), len(unresolved))
	for _, id := range denyIDs {
		fmt.Fprintf(os.Stderr, "  drop %s\t%s\n", id, deny[id])
	}
	if len(unresolved) > 0 {
		ids := make([]string, 0, len(unresolved))
		for id := range unresolved {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		max := 20
		if len(ids) < max {
			max = len(ids)
		}
		fmt.Fprintf(os.Stderr, "unresolved sample (%d of %d): %s\n", max, len(unresolved), strings.Join(ids[:max], ", "))
	}

	if strings.TrimSpace(*outDenylist) != "" {
		var b strings.Builder
		for _, id := range denyIDs {
			b.WriteString(id)
			b.WriteByte('\t')
			b.WriteString(deny[id])
			b.WriteByte('\n')
		}
		if err := os.WriteFile(*outDenylist, []byte(b.String()), 0o644); err != nil {
			log.Fatalf("out-denylist: %v", err)
		}
		fmt.Fprintf(os.Stderr, "wrote denylist %s (%d events)\n", *outDenylist, len(denyIDs))
	}

	var droppedRows, keptRows, droppedUnresolvedRows int
	for i, rows := range shards {
		var out []bcp.MatchFileRow
		for _, r := range rows {
			eid := strings.TrimSpace(r.EventID)
			if eid == "" {
				out = append(out, r)
				continue
			}
			if _, bad := deny[eid]; bad {
				droppedRows++
				continue
			}
			if *dropUnresolved {
				if _, u := unresolved[eid]; u {
					droppedUnresolvedRows++
					continue
				}
			}
			out = append(out, r)
		}
		keptRows += len(out)
		shards[i] = out
	}

	fmt.Fprintf(os.Stderr, "rows: %d → %d (dropped team-name=%d unresolved=%d)\n",
		totalRows, keptRows, droppedRows, droppedUnresolvedRows)

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

func loadEventsCache(path string, into map[string]string) (int, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var hits []bcp.EventListHit
	if err := json.Unmarshal(body, &hits); err != nil {
		return 0, err
	}
	n := 0
	for _, h := range hits {
		id := strings.TrimSpace(h.ID)
		name := strings.TrimSpace(h.Name)
		if id == "" || name == "" {
			continue
		}
		if _, ok := into[id]; !ok {
			n++
		}
		into[id] = name
	}
	return n, nil
}

func loadDenylist(path string, into map[string]string) (int, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		id, name, ok := strings.Cut(line, "\t")
		if !ok {
			id = line
			name = ""
		}
		id = strings.TrimSpace(id)
		name = strings.TrimSpace(name)
		if id == "" {
			continue
		}
		if _, exists := into[id]; !exists {
			n++
		}
		into[id] = name
	}
	if n == 0 {
		return 0, fmt.Errorf("no event ids in %s", path)
	}
	return n, nil
}
