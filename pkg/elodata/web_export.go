package elodata

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fortyk/eloevent/internal/bcp"
	"fortyk/eloevent/internal/elo40k"
)

// WebLeaderboardIndex drives the chunked static UI under data/index.json (see site/js/home.js).
type WebLeaderboardIndex struct {
	Version      int      `json:"version"`
	AsOfRFC3339  string   `json:"as_of"`
	PageSize     int      `json:"page_size"`
	TotalPlayers int      `json:"total_players"`
	PageCount    int      `json:"page_count"`
	Pages        []string `json:"pages"`        // filenames (e.g. page-000001.json)
	OutlineFile  string   `json:"outline_file"` // outline.json path relative to dir
}

// WebOutlineEntry is minimal per-player identity for filtering without loading chunk payloads.
type WebOutlineEntry struct {
	Rank int    `json:"rank"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

// WebOutlineFile is data/outline.json companion to chunked pages.
type WebOutlineFile struct {
	AsOfRFC3339 string            `json:"as_of"`
	Players     []WebOutlineEntry `json:"players"`
}

// WebLeaderboardChunk is one page-* JSON file loaded on demand by the UI.
type WebLeaderboardChunk struct {
	Page        int              `json:"page"` // 1-based
	PageCount   int              `json:"page_count"`
	PageSize    int              `json:"page_size"`
	StartRank   int              `json:"start_rank"` // inclusive
	EndRank     int              `json:"end_rank"`   // inclusive
	Total       int              `json:"total_players"`
	AsOfRFC3339 string           `json:"as_of,omitempty"`
	Players     []LeaderboardRow `json:"players"`
}

// WriteLeaderboardWebDir writes index.json + outline.json + padded page-%06d.json under dir (chunks of pageSize).
func WriteLeaderboardWebDir(dir string, asOf time.Time, snap []elo40k.Player, matchRows []bcp.MatchFileRow, recentN int, pageSize int) error {
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("empty web dir path")
	}
	if pageSize < 1 {
		pageSize = 50
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir web dir: %w", err)
	}

	asOfRFC := asOf.UTC().Format(time.RFC3339)

	var rows []LeaderboardRow
	if len(snap) > 0 {
		buf, err := buildLeaderboardWebRows(snap, matchRows, recentN)
		if err != nil {
			return err
		}
		rows = buf
	}

	n := len(rows)

	// Strip prior manifest + chunks + supersized mono before writing fresh export.
	ent, err := os.ReadDir(dir)
	if err == nil {
		for _, e := range ent {
			name := e.Name()
			if name == "." || name == ".." {
				continue
			}
			if name != "index.json" && name != "outline.json" && name != "leaderboard.json" &&
				!(strings.HasPrefix(name, "page-") && strings.HasSuffix(name, ".json")) {
				continue
			}
			_ = os.Remove(filepath.Join(dir, name))
		}
	}

	pageCount := 0
	pageNames := make([]string, 0)
	if n > 0 {
		pageCount = (n + pageSize - 1) / pageSize
		for pg := 0; pg < pageCount; pg++ {
			from := pg * pageSize
			to := min(from+pageSize, n)
			chunkRows := rows[from:to]

			fn := filepath.Join(dir, fmt.Sprintf("page-%06d.json", pg+1))

			pgFile := WebLeaderboardChunk{
				Page:        pg + 1,
				PageCount:   pageCount,
				PageSize:    pageSize,
				StartRank:   chunkRows[0].Rank,
				EndRank:     chunkRows[len(chunkRows)-1].Rank,
				Total:       n,
				AsOfRFC3339: asOfRFC,
				Players:     chunkRows,
			}
			raw, err := json.MarshalIndent(pgFile, "", "  ")
			if err != nil {
				return fmt.Errorf("%s marshal: %w", fn, err)
			}
			if err := os.WriteFile(fn, raw, 0o644); err != nil {
				return fmt.Errorf("write page: %w", err)
			}
			pageNames = append(pageNames, filepath.Base(fn))
		}
	}

	idx := WebLeaderboardIndex{
		Version:      2,
		AsOfRFC3339:  asOfRFC,
		PageSize:     pageSize,
		TotalPlayers: n,
		PageCount:    pageCount,
		Pages:        pageNames,
		OutlineFile:  "outline.json",
	}

	indexPath := filepath.Join(dir, "index.json")
	idxRaw, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(indexPath, idxRaw, 0o644); err != nil {
		return fmt.Errorf("index.json: %w", err)
	}

	outlines := make([]WebOutlineEntry, 0, n)
	for _, r := range rows {
		outlines = append(outlines, WebOutlineEntry{Rank: r.Rank, Key: r.Key, Name: r.Name})
	}

	outBlob := WebOutlineFile{AsOfRFC3339: asOfRFC, Players: outlines}
	outRaw, err := json.MarshalIndent(outBlob, "", "  ")
	if err != nil {
		return err
	}
	outpath := filepath.Join(dir, idx.OutlineFile)
	if err := os.WriteFile(outpath, outRaw, 0o644); err != nil {
		return fmt.Errorf("outline: %w", err)
	}

	return nil
}
