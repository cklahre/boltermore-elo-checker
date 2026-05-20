// Command bcp-merge-matches joins multiple bcp-export JSON shards into one file (sorted + optionally deduped).
//
//	Workflow: keep dated archives (mv bcp-matches.json bcp-matches-YYYY-MM-DD.json), export fresh events only,
//	list both in bcp-matches.manifest, pass to local-elo or merge into one file with this command.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"fortyk/eloevent/internal/bcp"
)

func main() {
	listFile := flag.String("list", "", "newline path list (# comments ok); prepend before positional paths")
	out := flag.String("out", "", "write merged JSON here")
	dedupe := flag.Bool("dedupe", true, "dedupe pairing_id / fingerprint after merge")
	noSort := flag.Bool("no-sort", false, "emit JSON without re-sort (order = concatenation)")
	flag.Parse()

	var extras []string
	if strings.TrimSpace(*listFile) != "" {
		p, err := bcp.ParseMatchesManifestPathList(*listFile)
		if err != nil {
			log.Fatalf("-list: %v", err)
		}
		extras = append(extras, p...)
	}
	extras = append(extras, flag.Args()...)
	shards, err := bcp.ResolveMatchShardPaths("", extras, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Usage: go run ./cmd/bcp-merge-matches [-list paths.txt] -out merged.json shard1.json shard2.json …")
		log.Fatal(err)
	}

	merged, err := bcp.MergeMatchFiles(shards, *dedupe)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "merged %d rows from %d files\n", len(merged), len(shards))

	var raw []byte
	if *noSort {
		raw, err = json.MarshalIndent(merged, "", "  ")
		if err != nil {
			log.Fatalf("json: %v", err)
		}
	} else {
		raw, err = bcp.MarshalMatchFileJSON(merged)
		if err != nil {
			log.Fatalf("json: %v", err)
		}
	}
	if strings.TrimSpace(*out) == "" {
		log.Fatal("-out is required (write-safe default)")
	}
	if err := os.WriteFile(*out, raw, 0o644); err != nil {
		log.Fatalf("write: %v", err)
	}
	fmt.Fprintf(os.Stderr, "wrote → %s\n", *out)
}
