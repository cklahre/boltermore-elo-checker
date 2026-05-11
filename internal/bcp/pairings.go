package bcp

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// Pairing is one row from GET /v1/pairings with pairingType=Pairing and expand[] set.
type Pairing struct {
	ID      string        `json:"id"`
	Round   int           `json:"round"`
	Table   int           `json:"table"`
	EventID string        `json:"eventId"`
	Player1 *PairingSide `json:"player1"`
	Player2 *PairingSide `json:"player2"`
	P1Game  *GameRecord  `json:"player1Game"`
	P2Game  *GameRecord  `json:"player2Game"`
}

type PairingSide struct {
	ID        string   `json:"id"`
	UserID    string   `json:"userId"`
	FirstName string   `json:"firstName"`
	LastName  string   `json:"lastName"`
	TeamName  string   `json:"teamName"`
	User      *bcpUser `json:"user"`
}

type GameRecord struct {
	GamePoints float64 `json:"gamePoints"`
	GameResult *int    `json:"gameResult"` // SPA uses 2=win, 1=draw, 0=loss for that side
}

type pairingsEnvelope struct {
	Data []Pairing `json:"data"`
}

// FetchPairings returns Swiss-style table pairings for one round (min limit=1).
func FetchPairings(c *Client, eventID string, round int) ([]Pairing, error) {
	q := url.Values{}
	q.Set("limit", "500")
	q.Set("eventId", eventID)
	q.Set("pairingType", "Pairing")
	q.Set("round", strconv.Itoa(round))
	q.Add("expand[]", "player1")
	q.Add("expand[]", "player2")
	q.Add("expand[]", "player1Game")
	q.Add("expand[]", "player2Game")

	body, err := c.GetJSON("/pairings", q)
	if err != nil {
		return nil, err
	}
	var env pairingsEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("pairings JSON: %w", err)
	}
	return env.Data, nil
}
