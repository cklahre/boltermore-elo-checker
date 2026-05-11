package bcp

import (
	"encoding/json"
	"net/url"
	"strings"
)

// RosterPlayer is one active entry from GET /v1/events/{id}/players (active[]).
type RosterPlayer struct {
	ID     string     `json:"id"`
	UserID string     `json:"userId"`
	User   *bcpUser   `json:"user"`
	Dropped bool      `json:"dropped"`
}

type bcpUser struct {
	ID        string `json:"id"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
}

func (p RosterPlayer) FullName() string {
	if p.User != nil {
		return strings.TrimSpace(p.User.FirstName + " " + p.User.LastName)
	}
	return ""
}

type playersEnvelope struct {
	Active []RosterPlayer `json:"active"`
}

// FetchRoster returns currently active roster rows (includes dropped flag per row).
func FetchRoster(c *Client, eventID string) ([]RosterPlayer, error) {
	body, err := c.GetJSON("/events/"+url.PathEscape(eventID)+"/players", nil)
	if err != nil {
		return nil, err
	}
	var env playersEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, err
	}
	return env.Active, nil
}

// NameLookup maps BCP roster / user / registration ids to display names (pairings may reference any of these).
func NameLookup(roster []RosterPlayer) map[string]string {
	m := make(map[string]string)
	for _, p := range roster {
		n := p.FullName()
		if n == "" {
			continue
		}
		if p.ID != "" {
			m[p.ID] = n
		}
		if p.UserID != "" {
			m[p.UserID] = n
		}
		if p.User != nil && p.User.ID != "" {
			m[p.User.ID] = n
		}
	}
	return m
}
