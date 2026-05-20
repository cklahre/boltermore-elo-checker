package bcp

import (
	"fmt"
	"strings"
)

// ResolveMatchShardPaths builds an ordered shard list.
// When manifestPath is non-empty, ONLY that manifest is used (newline paths, relative to manifest dir).
// Otherwise shards are matchesExtra (e.g. repeated -matches) followed by defaultIfEmpty when still empty.
func ResolveMatchShardPaths(manifestPath string, matchesExtra []string, defaultIfEmpty string) ([]string, error) {
	if mf := strings.TrimSpace(manifestPath); mf != "" {
		return ParseMatchesManifestPathList(mf)
	}
	out := make([]string, 0)
	for _, p := range matchesExtra {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) > 0 {
		return out, nil
	}
	if df := strings.TrimSpace(defaultIfEmpty); df != "" {
		return []string{df}, nil
	}
	return nil, fmt.Errorf("need -matches shards, -matches-manifest, or legacy default path")
}
