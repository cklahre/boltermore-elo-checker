package statcheckelo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// StatCheckPlayer is one row in Stat-Check’s Elo export / API.
type StatCheckPlayer struct {
	PlayerName string  `json:"player_name"`
	Elo        float64 `json:"elo"`
}

// DefaultEloJSONURL was Stat-Check’s legacy JSON path; it often returns HTTP 404 with an
// HTML error page. Prefer STATCHECK_ELO_FILE (export from stat-check.com/elo) or a working STATCHECK_ELO_URL.
const DefaultEloJSONURL = "https://www.stat-check.com/s/elo_data.json"

// FetchEloPlayers loads the JSON array from url (HTTP GET).
func FetchEloPlayers(url string) ([]StatCheckPlayer, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; statcheck-elo/1.0)")
	req.Header.Set("Accept", "application/json,text/plain;q=0.9,*/*;q=0.8")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, httpLoadError(url, resp.StatusCode, body)
	}
	if looksLikeHTML(body) {
		return nil, fmt.Errorf(`GET %q: HTTP 200 but the body looks like HTML, not JSON (you may be using a web page URL). Use a direct JSON URL or STATCHECK_ELO_FILE`, url)
	}

	return DecodeStatCheckEloList(body)
}

func looksLikeHTML(body []byte) bool {
	b := bytes.TrimSpace(body)
	if len(b) == 0 {
		return false
	}
	if b[0] != '<' {
		return false
	}
	prefix := strings.ToLower(string(b))
	// Typical Squarespace / error pages and most HTML documents.
	if strings.HasPrefix(prefix, "<!doctype") || strings.HasPrefix(prefix, "<html") {
		return true
	}
	// Catch fragments that still aren't JSON.
	head := prefix
	if len(head) > 32 {
		head = head[:32]
	}
	return strings.HasPrefix(head, "<head") || strings.HasPrefix(head, "<title")
}

func httpLoadError(url string, code int, body []byte) error {
	var b strings.Builder
	fmt.Fprintf(&b, "GET %q: HTTP %d %s", url, code, http.StatusText(code))
	if code == http.StatusNotFound {
		b.WriteString(". That Stat-Check JSON path often 404s; export the leaderboard from stat-check.com/elo and set STATCHECK_ELO_FILE to the file, or set STATCHECK_ELO_URL to a working JSON URL.")
	}
	if looksLikeHTML(body) {
		b.WriteString(" (response was an HTML page, not JSON — omitted from this message)")
		return errors.New(b.String())
	}
	s := strings.TrimSpace(string(body))
	if s != "" {
		const max = 180
		if len(s) > max {
			s = s[:max] + "…"
		}
		b.WriteString(": ")
		b.WriteString(s)
	}
	return errors.New(b.String())
}

// LoadEloPlayersFromFile loads JSON (array or wrapped object), or reads ranks from an XLSX export.
func LoadEloPlayersFromFile(path string) ([]StatCheckPlayer, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".xlsx":
		return loadEloPlayersFromXLSX(path)
	default:
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return DecodeStatCheckEloList(body)
	}
}

// MapFromPlayers builds a lowercased full-name → Elo map (last entry wins on duplicate names).
func MapFromPlayers(data []StatCheckPlayer) map[string]float64 {
	m := make(map[string]float64)
	for _, p := range data {
		m[strings.ToLower(p.PlayerName)] = p.Elo
	}
	return m
}

// DecodeStatCheckEloList parses JSON from Stat-Check’s feed or a saved export.
func DecodeStatCheckEloList(body []byte) ([]StatCheckPlayer, error) {
	body = bytes.TrimSpace(body)
	body = bytes.TrimPrefix(body, []byte{0xEF, 0xBB, 0xBF})
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, fmt.Errorf("empty Elo data")
	}
	if looksLikeHTML(body) {
		return nil, fmt.Errorf("got HTML instead of JSON — use STATCHECK_ELO_FILE with a downloaded export, or STATCHECK_ELO_URL pointing at real JSON (not the /elo web page)")
	}

	var direct []StatCheckPlayer
	if err := json.Unmarshal(body, &direct); err == nil {
		return direct, nil
	}

	var probe map[string]json.RawMessage
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, fmt.Errorf("parse Elo JSON: %w", err)
	}
	for _, key := range []string{"data", "players", "items"} {
		raw, ok := probe[key]
		if !ok {
			continue
		}
		var list []StatCheckPlayer
		if err := json.Unmarshal(raw, &list); err != nil {
			continue
		}
		return list, nil
	}
	return nil, fmt.Errorf("Elo JSON: expected a JSON array of {player_name, elo}, or an object with key data/players/items")
}

func loadEloPlayersFromXLSX(path string) ([]StatCheckPlayer, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	sheet := f.GetSheetName(0)
	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("xlsx: first sheet %q is empty", sheet)
	}

	nameCol, eloCol, start := detectXLSColumns(rows)
	if nameCol < 0 || eloCol < 0 {
		return nil, fmt.Errorf("xlsx: could not find Player and Elo columns")
	}

	out := make([]StatCheckPlayer, 0)
	for i := start; i < len(rows); i++ {
		r := rows[i]
		if nameCol >= len(r) || eloCol >= len(r) {
			continue
		}
		name := strings.TrimSpace(r[nameCol])
		if name == "" {
			continue
		}
		eloStr := strings.TrimSpace(r[eloCol])
		eloStr = strings.ReplaceAll(eloStr, ",", "")
		elo, err := strconv.ParseFloat(eloStr, 64)
		if err != nil {
			continue
		}
		out = append(out, StatCheckPlayer{PlayerName: name, Elo: elo})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("xlsx: no numeric Elo rows found")
	}
	return out, nil
}

func detectXLSColumns(rows [][]string) (nameCol, eloCol, start int) {
	nameCol, eloCol = -1, -1
	if len(rows) == 0 {
		return -1, -1, 0
	}

	hdr := rows[0]
	for i, cell := range hdr {
		t := strings.TrimSpace(cell)
		h := strings.ToLower(t)
		if nameCol >= 0 {
			continue
		}
		if h == "player" ||
			(strings.Contains(h, "player") && strings.Contains(h, "name")) ||
			strings.EqualFold(t, "Player Name") {
			nameCol = i
		}
	}
	for i, cell := range hdr {
		h := strings.ToLower(strings.TrimSpace(cell))
		if eloCol < 0 && (h == "elo" || h == "rating") {
			eloCol = i
		}
	}

	if nameCol >= 0 && eloCol >= 0 {
		return nameCol, eloCol, 1
	}

	r0 := rows[0]
	if len(r0) >= 3 {
		if _, err := strconv.ParseFloat(strings.TrimSpace(r0[0]), 64); err == nil {
			return 1, 2, 0
		}
		if _, err := strconv.Atoi(strings.TrimSpace(r0[0])); err == nil {
			return 1, 2, 0
		}
	}
	if len(r0) >= 2 {
		return 0, 1, 0
	}
	return -1, -1, 0
}

// SortByEloDesc sorts players by Elo (highest first), then by name for ties.
func SortByEloDesc(players []StatCheckPlayer) {
	sort.Slice(players, func(i, j int) bool {
		if players[i].Elo != players[j].Elo {
			return players[i].Elo > players[j].Elo
		}
		return strings.Compare(players[i].PlayerName, players[j].PlayerName) < 0
	})
}
