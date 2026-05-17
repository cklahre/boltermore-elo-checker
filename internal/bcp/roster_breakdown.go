package bcp

import (
	"fmt"
	"sort"
	"strings"
)

// CountRow is one label with a player count (e.g. one faction).
type CountRow struct {
	Label string
	Count int
}

// ArmyFactionName is the leaf faction name BCP stores for the list (subfaction / army label).
func (p RosterPlayer) ArmyFactionName() string {
	if p.Faction == nil {
		return ""
	}
	return strings.TrimSpace(p.Faction.Name)
}

// RollupFactionName groups by grand alliance / parent when the API provides it; otherwise the leaf name.
func (p RosterPlayer) RollupFactionName() string {
	if p.Faction == nil {
		return ""
	}
	if p.Faction.ParentFaction != nil {
		s := strings.TrimSpace(p.Faction.ParentFaction.Name)
		if s != "" {
			return s
		}
	}
	return strings.TrimSpace(p.Faction.Name)
}

// FactionCounts returns rows sorted by count (desc), then label; unknown labels last.
func FactionCounts(roster []RosterPlayer, label func(RosterPlayer) string) []CountRow {
	m := make(map[string]int)
	var unknown int
	for _, p := range roster {
		k := strings.TrimSpace(label(p))
		if k == "" {
			unknown++
			continue
		}
		m[k]++
	}
	out := make([]CountRow, 0, len(m))
	for k, n := range m {
		out = append(out, CountRow{Label: k, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Label < out[j].Label
	})
	if unknown > 0 {
		out = append(out, CountRow{Label: "(no faction on roster)", Count: unknown})
	}
	return out
}

// CountRowsEqual is true when both slices have the same length and identical label/count pairs in order.
func CountRowsEqual(a, b []CountRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Label != b[i].Label || a[i].Count != b[i].Count {
			return false
		}
	}
	return true
}

// UniqueListIDs returns distinct non-empty listId values in roster order (first occurrence kept).
func UniqueListIDs(roster []RosterPlayer) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, p := range roster {
		id := strings.TrimSpace(p.ListID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// DetachmentLabel for one roster row after ListDetachmentIndex has run.
func DetachmentLabel(p RosterPlayer, det map[string]string, fetchFailed map[string]struct{}) string {
	id := strings.TrimSpace(p.ListID)
	if id == "" {
		return "(no list id)"
	}
	if fetchFailed != nil {
		if _, ok := fetchFailed[id]; ok {
			return "(list fetch failed)"
		}
	}
	if det == nil {
		return "(no detachment data)"
	}
	s := strings.TrimSpace(det[id])
	if s == "" {
		return "(no detachment in list)"
	}
	return s
}

// DetachmentCounts aggregates players by detachment label.
func DetachmentCounts(roster []RosterPlayer, det map[string]string, fetchFailed map[string]struct{}) []CountRow {
	return FactionCounts(roster, func(p RosterPlayer) string {
		return DetachmentLabel(p, det, fetchFailed)
	})
}

// ArmyDetachmentBullet is one detachment label under an army with player count.
type ArmyDetachmentBullet struct {
	Label string
	Count int
}

// ArmyDetachmentGroup is one army (BCP faction name) with detachments sorted by label.
type ArmyDetachmentGroup struct {
	Army  string
	Lines []ArmyDetachmentBullet
}

// ArmyDetachmentTree groups each roster row by ArmyFactionName and buckets DetachmentLabel counts.
func ArmyDetachmentTree(roster []RosterPlayer, det map[string]string, fetchFailed map[string]struct{}) []ArmyDetachmentGroup {
	buckets := make(map[string]map[string]int)
	for _, p := range roster {
		army := p.ArmyFactionName()
		if army == "" {
			army = "(no faction on roster)"
		}
		d := normalizeForDetachmentBucket(DetachmentLabel(p, det, fetchFailed))
		if buckets[army] == nil {
			buckets[army] = make(map[string]int)
		}
		buckets[army][d]++
	}
	armies := make([]string, 0, len(buckets))
	for a := range buckets {
		armies = append(armies, a)
	}
	sort.Strings(armies)
	out := make([]ArmyDetachmentGroup, 0, len(armies))
	for _, army := range armies {
		dm := buckets[army]
		labels := make([]string, 0, len(dm))
		for l := range dm {
			labels = append(labels, l)
		}
		sort.Strings(labels)
		lines := make([]ArmyDetachmentBullet, 0, len(labels))
		for _, l := range labels {
			lines = append(lines, ArmyDetachmentBullet{Label: l, Count: dm[l]})
		}
		out = append(out, ArmyDetachmentGroup{Army: army, Lines: lines})
	}
	return out
}

// FormatArmyDetachmentTree renders (total) - Army headers and - (n) Detachment bullets.
func FormatArmyDetachmentTree(groups []ArmyDetachmentGroup, boldArmyHeaders bool) string {
	var b strings.Builder
	for _, g := range groups {
		total := 0
		for _, line := range g.Lines {
			total += line.Count
		}
		header := fmt.Sprintf("(%d) - %s", total, g.Army)
		if boldArmyHeaders {
			b.WriteString("**")
			b.WriteString(header)
			b.WriteString("**\n")
		} else {
			b.WriteString(header)
			b.WriteString("\n")
		}
		for _, line := range g.Lines {
			b.WriteString(fmt.Sprintf("- (%d) %s\n", line.Count, line.Label))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
