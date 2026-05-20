// Command local-elo builds a Stat-Check–style Elo leaderboard from your own match history
// (JSON file). It does not call Stat-Check. Rules: baseline 1500, K=32, 13-week × 20% decay
// toward 1500 (see internal/elo40k).
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"fortyk/eloevent/internal/bcp"
	"fortyk/eloevent/internal/elo40k"
	"fortyk/eloevent/pkg/elodata"
)

type matchPathsFlag []string

func (m *matchPathsFlag) String() string { return strings.Join([]string(*m), ", ") }

func (m *matchPathsFlag) Set(v string) error {
	*m = append(*m, strings.TrimSpace(v))
	return nil
}

func main() {
	matchesManifest := flag.String("matches-manifest", "", "when non-empty, load ONLY these newline paths (# comments ok); ignores repeated -matches flags")
	var matchShards matchPathsFlag
	flag.Var(&matchShards, "matches", "matches JSON shard (repeat for multiple files, merged oldest→newest recommended)")
	asOf := flag.String("as-of", "", "apply final inactivity decay through this date (2006-01-02 or RFC3339); default: now")
	topN := flag.Int("n", 0, "if > 0, print only top N rows")
	outJSON := flag.String("out-json", "", "if set, write full leaderboard snapshot JSON here (for bots; ignores -n)")
	outWebJSON := flag.String("out-web-json", "", "if set, write one monolithic leaderboard JSON (+ recent_*); use -out-web-dir for chunked Pages export")
	outWebDir := flag.String("out-web-dir", "", "if set, write chunked index/outline/page-* JSON files under this directory (for Pages UI)")
	webPageSize := flag.Int("web-page-size", 50, "with -out-web-dir, rankings per chunk file")
	recentN := flag.Int("recent-n", 10, "recent games/events per player in web exports")
	noMergeDedupe := flag.Bool("no-merge-dedupe", false, "when merging multiple -matches shards: skip pairing dedupe (overlap not recommended)")
	flag.Parse()

	paths, err := bcp.ResolveMatchShardPaths(*matchesManifest, matchShards, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Usage: local-elo -matches games.json [-as-of DATE ...] | -matches shard1.json -matches shard2.json | -matches-manifest list.txt ...")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "JSON rows look like:")
		fmt.Fprintln(os.Stderr, `  { "date": "2025-03-01", "a": "Alice Example", "b": "Bob Example", "winner": "a" }`)
		fmt.Fprintln(os.Stderr, "Multiple shards concatenate then dedupe by pairing_id; list archived shards before fresh exports in manifests.")
		log.Fatal(err)
	}

	dedupe := !*noMergeDedupe
	fileRows, err := bcp.MergeMatchFiles(paths, dedupe)
	if err != nil {
		log.Fatalf("matches merge: %v", err)
	}

	ms := make([]elo40k.Match, 0, len(fileRows))
	for i := range fileRows {
		m, err := elodata.RowToMatch(&fileRows[i])
		if err != nil {
			log.Fatalf("row %d: %v", i, err)
		}
		ms = append(ms, m)
	}

	e := elo40k.NewEngine()
	e.PlayAll(ms)

	var cutoff time.Time
	if strings.TrimSpace(*asOf) == "" {
		cutoff = time.Now()
	} else {
		var perr error
		for _, lay := range []string{time.RFC3339, "2006-01-02"} {
			cutoff, perr = time.ParseInLocation(lay, strings.TrimSpace(*asOf), time.Local)
			if perr == nil {
				break
			}
		}
		if perr != nil {
			log.Fatalf("as-of: %v", perr)
		}
	}
	e.FinalizeDecay(cutoff)

	rows := e.Snapshot()
	outPath := strings.TrimSpace(*outJSON)
	webPath := strings.TrimSpace(*outWebJSON)
	webDir := strings.TrimSpace(*outWebDir)
	if webPath != "" && webDir != "" {
		log.Fatal("use only one of -out-web-json or -out-web-dir")
	}
	if outPath != "" {
		if err := elodata.WriteLeaderboardJSON(outPath, cutoff, rows); err != nil {
			log.Fatalf("out-json: %v", err)
		}
		fmt.Fprintf(os.Stderr, "wrote %d players → %s\n", len(rows), outPath)
	}
	if webPath != "" || webDir != "" {
		if webPath != "" {
			if err := elodata.WriteLeaderboardWebJSON(webPath, cutoff, rows, fileRows, *recentN); err != nil {
				log.Fatalf("out-web-json: %v", err)
			}
			fmt.Fprintf(os.Stderr, "wrote %d players (+ recent games) → %s\n", len(rows), webPath)
		}
		if webDir != "" {
			if err := elodata.WriteLeaderboardWebDir(webDir, cutoff, rows, fileRows, *recentN, *webPageSize); err != nil {
				log.Fatalf("out-web-dir: %v", err)
			}
			fmt.Fprintf(os.Stderr, "wrote chunked web data (%d players, page size %d) → %s\n", len(rows), *webPageSize, webDir)
		}
	}

	limit := len(rows)
	if *topN > 0 && *topN < limit {
		limit = *topN
	}

	fmt.Printf("%-6s | %-36s | %8s | %s\n", "Rank", "Player", "Elo", "Games")
	fmt.Println(strings.Repeat("-", 62))
	for i := 0; i < limit; i++ {
		p := rows[i]
		fmt.Printf("%-6d | %-36s | %8.1f | %d\n", i+1, trunc(p.DisplayName, 36), p.Rating, p.Games)
	}
	if *topN > 0 && len(rows) > limit {
		fmt.Fprintf(os.Stderr, "\n(showing top %d of %d players)\n", limit, len(rows))
	}
}

func trunc(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
