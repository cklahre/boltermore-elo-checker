package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"

	"fortyk/eloevent/internal/statcheckelo"
)

// BCPPlayer is the normalized name fields used for Stat-Check matching.
type BCPPlayer struct {
	FirstName string
	LastName  string
}

// bcpPlayerWire matches BCP roster JSON: names may be on the row or under "user".
type bcpPlayerWire struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	User      *struct {
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
	} `json:"user"`
}

func (w bcpPlayerWire) toPlayer() BCPPlayer {
	if w.User != nil {
		return BCPPlayer{FirstName: w.User.FirstName, LastName: w.User.LastName}
	}
	return BCPPlayer{FirstName: w.FirstName, LastName: w.LastName}
}

// Public BCP web app sends this value (see REACT_APP_AUTH_CLIENT_ID in their bundle).
const bcpClientID = "web-app"

func main() {
	bcpURL := "https://newprod-api.bestcoastpairings.com/v1/events/8wCnzC2HxkCw/players"

	// 1. Fetch BCP Roster
	fmt.Println("Fetching roster from BCP...")
	roster, err := fetchBCPRoster(bcpURL)
	if err != nil {
		log.Fatalf("Failed to fetch BCP roster: %v", err)
	}

	// 2. Load Stat-Check Elo data (file wins over HTTP; see env vars above).
	var list []statcheckelo.StatCheckPlayer
	eloFile := os.Getenv("STATCHECK_ELO_FILE")
	if eloFile != "" {
		fmt.Printf("Loading Elo database from file %s...\n", eloFile)
		list, err = statcheckelo.LoadEloPlayersFromFile(eloFile)
	} else {
		statURL := os.Getenv("STATCHECK_ELO_URL")
		if statURL == "" {
			statURL = statcheckelo.DefaultEloJSONURL
		}
		fmt.Println("Fetching Elo database from Stat-Check...")
		list, err = statcheckelo.FetchEloPlayers(statURL)
	}
	if err != nil {
		log.Fatalf("Failed to load Elo data: %v", err)
	}
	eloMap := statcheckelo.MapFromPlayers(list)

	// 3. Match, sort by Elo (highest first), print
	type row struct {
		fullName string
		elo      float64
		found    bool
	}
	rows := make([]row, 0, len(roster))
	for _, p := range roster {
		fullName := strings.TrimSpace(p.FirstName + " " + p.LastName)
		e, ok := eloMap[strings.ToLower(fullName)]
		if ok {
			rows = append(rows, row{fullName, e, true})
		} else {
			rows = append(rows, row{fullName, 1500, false})
		}
	}
	slices.SortFunc(rows, func(a, b row) int {
		if a.elo > b.elo {
			return -1
		}
		if a.elo < b.elo {
			return 1
		}
		return strings.Compare(a.fullName, b.fullName)
	})

	fmt.Printf("\n%-25s | %-10s\n", "Player Name", "Elo Rating")
	fmt.Println(strings.Repeat("-", 40))

	for _, r := range rows {
		if r.found {
			fmt.Printf("%-25s | %.1f\n", r.fullName, r.elo)
		} else {
			fmt.Printf("%-25s | 1500.0 (Unranked)\n", r.fullName)
		}
	}
}

func fetchBCPRoster(url string) ([]BCPPlayer, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("client-id", bcpClientID)

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
		return nil, fmt.Errorf("BCP HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	players, err := decodeBCPPlayers(body)
	if err != nil {
		return nil, err
	}
	return players, nil
}

// decodeBCPPlayers accepts either a raw JSON array of players or an object that wraps
// the array. The live BCP API returns {"active":[...]} with nested user.{firstName,lastName}.
func decodeBCPPlayers(body []byte) ([]BCPPlayer, error) {
	var direct []bcpPlayerWire
	if err := json.Unmarshal(body, &direct); err == nil {
		return wireListToPlayers(direct), nil
	}

	var probe map[string]json.RawMessage
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, fmt.Errorf("decode roster: %w", err)
	}

	for _, key := range []string{"active", "data", "players", "results", "items", "content", "value"} {
		raw, ok := probe[key]
		if !ok {
			continue
		}
		var wires []bcpPlayerWire
		if err := json.Unmarshal(raw, &wires); err != nil {
			continue
		}
		return wireListToPlayers(wires), nil
	}

	return nil, fmt.Errorf("decode roster: JSON object has no known player array key (got keys: %s)", strings.Join(mapKeysSorted(probe), ", "))
}

func wireListToPlayers(wires []bcpPlayerWire) []BCPPlayer {
	out := make([]BCPPlayer, len(wires))
	for i := range wires {
		out[i] = wires[i].toPlayer()
	}
	return out
}

func mapKeysSorted(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

