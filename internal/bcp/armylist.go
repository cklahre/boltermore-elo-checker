package bcp

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// FetchArmyListRaw returns GET /v1/armylists/{id} JSON. Client.BearerToken must be set;
// otherwise BCP responds with 401 ("unauthorized access").
func FetchArmyListRaw(c *Client, listID string) ([]byte, error) {
	listID = strings.TrimSpace(listID)
	if listID == "" {
		return nil, fmt.Errorf("empty list id")
	}
	q := url.Values{}
	q.Add("expand[]", "army")
	q.Add("expand[]", "subFaction")
	q.Add("expand[]", "warhammer")
	q.Add("expand[]", "character")
	q.Add("expand[]", "gameSystem")
	q.Add("expand[]", "user")
	return c.GetJSON("/armylists/"+url.PathEscape(listID), q)
}

var (
	detachmentLabelKeys = []string{
		"detachment",
		"battleFormation",
		"battleFormationName",
		"subFactionName",
	}
	// armyListText often contains a printable line after heading.
	armyListDetachmentPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:^|\n)[\s#*_]*(?:detachment|battle\s*formation)\s*[:#\-–]\s*([^\n\r]+)`),
		regexp.MustCompile(`(?i)\b(?:detachment|battle\s*formation)\s*:\s*([^\n\r]+)`),
	}
	trailingParenDetachment = regexp.MustCompile(`\s*\([^)]*\)\s*$`)
	// Line before detachment on exported lists, e.g. "Strike Force (2000 points)".
	battleSizeLine = regexp.MustCompile(`(?i)^(Strike\s+Force|Incursion|Onslaught|Combat\s+Patrol|Boarding\s+Action|Storm\s+of\s+War|Crusade\s+Force)\s*\([^)]*\)\s*$`)
	// Printed list section titles — not detachments (common after battle size line when detachment is missing).
	warhammerListSectionHeaders = map[string]struct{}{
		"characters": {}, "units": {}, "battleline": {}, "battlelines": {},
		"dedicated transport": {}, "other datasheets": {}, "enhancements": {},
		"stratagems": {}, "weapons": {}, "weapon": {}, "notes": {},
		"faction abilities": {}, "keywords": {}, "order of battle": {},
		"datasheets": {}, "lords of war": {}, "flyers": {}, "flyer": {},
		"epic heroes": {}, "supporting units": {}, "allied units": {},
		"agents of the imperium": {}, "agent of the imperium": {},
	}

	// List export unit / warlord lines mistaken for detachments (e.g. "• 1x Drakkis", "• Warlord").
	junkUnitCountPattern = regexp.MustCompile(`(?i)\d+\s*x\s`)
)

// isJunkListExportDetachment is true for roster bullets, loadout lines, lone "Warlord", etc.
func isJunkListExportDetachment(s string) bool {
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	// Strip markdown/list bullets and re-check.
	for {
		stripped := strings.TrimLeft(s, " \t*•·─-")
		if stripped == s {
			break
		}
		s = strings.TrimSpace(stripped)
	}
	if s == "" {
		return true
	}
	if strings.ContainsAny(s, "•·") {
		return true
	}
	low := strings.ToLower(s)
	if low == "warlord" {
		return true
	}
	if junkUnitCountPattern.MatchString(s) {
		return true
	}
	return false
}

// lineLooksLikeDetachmentName helps pick a line *above* battle size when the list puts
// detachment before "Strike Force" (some exports).
func lineLooksLikeDetachmentName(s string) bool {
	if isJunkListExportDetachment(s) {
		return false
	}
	if isWarhammerListSectionHeader(s) {
		return false
	}
	sl := strings.ToLower(strings.TrimSpace(s))
	// Typical detachment / OOB wording (not exhaustive).
	for _, w := range []string{
		"warband", "berzerker", "task force", "formation", "company", "host", "fleet", "cadre",
		"lance", "oath", "hunt", "arsenal", "cult", "coven", "swarm", "swift",
		"spear", "retaliation", "spearpoint", "blade", "shadowmark", "reclamation",
		"headhunter", "armoured", "gladius", "mont", // Mont'ka
	} {
		if strings.Contains(sl, w) {
			return true
		}
	}
	return false
}

// isWarhammerListSectionHeader is true for GW roster headings mis-parsed as detachments.
func isWarhammerListSectionHeader(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Trim(s, "*#_` ")
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return false
	}
	_, ok := warhammerListSectionHeaders[s]
	return ok
}

// DetachmentFromArmyListJSON picks a display detachment / battle formation / sub-faction label
// from an armylists response body (BCP uses several shapes across game systems).
func DetachmentFromArmyListJSON(body []byte) string {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(body, &probe); err != nil {
		return ""
	}

	// Some responses wrap the list in `data`.
	if raw, ok := probe["data"]; ok && len(raw) > 0 && raw[0] == '{' {
		if s := DetachmentFromArmyListJSON(raw); s != "" {
			return s
		}
	}

	if s := detachmentFromMap(probe, 0); s != "" {
		return s
	}
	if raw, ok := probe["armyListText"]; ok {
		if s := detachmentFromArmyListText(raw); s != "" {
			return s
		}
	}
	return ""
}

// NormalizeDetachmentDisplay cleans list-derived names for grouping: Unicode spaces,
// repeated whitespace, and one trailing parenthetical (e.g. rule subtitle).
func NormalizeDetachmentDisplay(s string) string {
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = strings.TrimSpace(s)
	for range 3 {
		loc := trailingParenDetachment.FindStringIndex(s)
		if loc == nil {
			break
		}
		s = strings.TrimSpace(s[:loc[0]])
	}
	s = strings.Join(strings.Fields(s), " ")
	return s
}

// normalizeForDetachmentBucket applies NormalizeDetachmentDisplay except for sentinel labels.
func normalizeForDetachmentBucket(s string) string {
	switch s {
	case "(no list id)", "(list fetch failed)", "(no detachment data)", "(no detachment in list)":
		return s
	default:
		t := NormalizeDetachmentDisplay(s)
		if isWarhammerListSectionHeader(t) {
			return "(no detachment in list)"
		}
		if isJunkListExportDetachment(t) {
			return "(no detachment in list)"
		}
		return t
	}
}

func detachmentFromBattleSizeBlock(text string) string {
	text = strings.ReplaceAll(text, "\u00a0", " ")
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t != "" {
			lines = append(lines, t)
		}
	}
	for i := 0; i < len(lines); i++ {
		if !battleSizeLine.MatchString(lines[i]) {
			continue
		}
		// Some exports put the detachment on the line *before* battle size (e.g. Berzerker Warband).
		if i > 0 && lineLooksLikeDetachmentName(lines[i-1]) {
			return lines[i-1]
		}
		for j := i + 1; j < len(lines); j++ {
			next := lines[j]
			if isWarhammerListSectionHeader(next) {
				continue
			}
			if isJunkListExportDetachment(next) {
				continue
			}
			if strings.Contains(strings.ToLower(next), " points)") {
				continue
			}
			if len(next) > 120 {
				continue
			}
			return next
		}
	}
	return ""
}

func tryStringishDetachment(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		s = strings.TrimSpace(s)
		if s != "" {
			return s
		}
	}
	var o struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &o); err == nil {
		s := strings.TrimSpace(o.Name)
		if s != "" {
			return s
		}
	}
	return ""
}

func detachmentFromMap(probe map[string]json.RawMessage, depth int) string {
	if depth > 4 || len(probe) == 0 {
		return ""
	}

	for _, key := range detachmentLabelKeys {
		if raw, ok := probe[key]; ok {
			if s := tryStringishDetachment(raw); s != "" {
				return s
			}
		}
	}

	if raw, ok := probe["subFaction"]; ok {
		if s := tryStringishDetachment(raw); s != "" {
			return s
		}
	}

	// Warhammer 40k: detachment often under `warhammer`.
	if raw, ok := probe["warhammer"]; ok && len(raw) > 0 && raw[0] == '{' {
		var wh map[string]json.RawMessage
		if err := json.Unmarshal(raw, &wh); err == nil {
			for _, key := range detachmentLabelKeys {
				if braw, ok := wh[key]; ok {
					if s := tryStringishDetachment(braw); s != "" {
						return s
					}
				}
			}
			if braw, ok := wh["subFaction"]; ok {
				if s := tryStringishDetachment(braw); s != "" {
					return s
				}
			}
		}
	}

	if raw, ok := probe["army"]; ok && len(raw) > 0 && raw[0] == '{' {
		var inner map[string]json.RawMessage
		if err := json.Unmarshal(raw, &inner); err == nil {
			for _, key := range detachmentLabelKeys {
				if braw, ok := inner[key]; ok {
					if s := tryStringishDetachment(braw); s != "" {
						return s
					}
				}
			}
			if braw, ok := inner["subFaction"]; ok {
				if s := tryStringishDetachment(braw); s != "" {
					return s
				}
			}
		}
	}

	// `master` often mirrors list builder state when `warhammer` on the root is empty.
	if raw, ok := probe["master"]; ok && len(raw) > 0 && raw[0] == '{' {
		var inner map[string]json.RawMessage
		if err := json.Unmarshal(raw, &inner); err == nil {
			if s := detachmentFromMap(inner, depth+1); s != "" {
				return s
			}
		}
	}

	return ""
}

func detachmentFromArmyListText(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, re := range armyListDetachmentPatterns {
		m := re.FindStringSubmatch(s)
		if len(m) >= 2 {
			t := strings.TrimSpace(m[1])
			t = strings.TrimRight(t, "`*_#-– \t")
			if t != "" && len(t) < 200 && !isWarhammerListSectionHeader(t) && !isJunkListExportDetachment(t) {
				return t
			}
		}
	}
	if t := detachmentFromBattleSizeBlock(s); t != "" && !isWarhammerListSectionHeader(t) && !isJunkListExportDetachment(t) {
		return t
	}
	return ""
}

// ListDetachmentIndex fetches each list id (deduped) and returns detachment text per list.
// fetchFailed lists ids where the HTTP request failed; missing keys in det mean empty or unparsed detachment.
func ListDetachmentIndex(c *Client, listIDs []string) (det map[string]string, fetchFailed map[string]struct{}) {
	det = make(map[string]string)
	fetchFailed = make(map[string]struct{})
	seen := make(map[string]struct{})
	for _, id := range listIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		body, err := FetchArmyListRaw(c, id)
		if err != nil {
			fetchFailed[id] = struct{}{}
			continue
		}
		det[id] = normalizeStoredDetachment(DetachmentFromArmyListJSON(body))
	}
	return det, fetchFailed
}

func normalizeStoredDetachment(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	d := NormalizeDetachmentDisplay(raw)
	if isWarhammerListSectionHeader(d) {
		return ""
	}
	if isJunkListExportDetachment(d) {
		return ""
	}
	return d
}
