package bcp

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// EventListHit is a compact row from GET /v1/events search (public, geo + searchString).
type EventListHit struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	EventDate      string `json:"eventDate"`
	EventEndDate   string `json:"eventEndDate"`
	Ended          bool   `json:"ended"`
	Started        bool   `json:"started"`
	GameSystemID   string `json:"gameSystemId"`
	GameType       int    `json:"gameType"`
	TotalPlayers   int    `json:"totalPlayers"`
	NumberOfRounds int    `json:"numberOfRounds"`
}

// ListHitAsEvent maps search-list fields into Event so EventSkippedByFilter applies without an extra GET.
func ListHitAsEvent(h EventListHit) *Event {
	return &Event{
		ID:             h.ID,
		Name:           h.Name,
		EventDate:      h.EventDate,
		EventEndDate:   h.EventEndDate,
		NumberOfRounds: h.NumberOfRounds,
		TotalPlayers:   h.TotalPlayers,
	}
}

// EventSearchParams drives one paginated call to GET /v1/events.
// BCP requires gameSystemId, latitude, longitude, distance, and searchString (min 3 runes).
type EventSearchParams struct {
	Limit        int
	GameSystemID string
	Latitude     float64
	Longitude    float64
	// Distance is passed through to the API (same units BCP uses in their web app).
	Distance     int
	SearchString string
	NextKey      string
}

type eventsListEnvelope struct {
	Data    []EventListHit `json:"data"`
	NextKey string         `json:"nextKey"`
}

// SearchEventsPage returns one page of events. Use NextKey from the result for the following page.
func SearchEventsPage(c *Client, p EventSearchParams) (hits []EventListHit, nextKey string, err error) {
	if p.Limit <= 0 {
		p.Limit = 100
	}
	if p.GameSystemID == "" {
		return nil, "", fmt.Errorf("GameSystemID is required")
	}
	s := p.SearchString
	if len([]rune(s)) < 3 {
		return nil, "", fmt.Errorf("searchString must be at least 3 characters (BCP rule)")
	}

	q := url.Values{}
	q.Set("limit", strconv.Itoa(p.Limit))
	q.Set("gameSystemId", p.GameSystemID)
	q.Set("latitude", strconv.FormatFloat(p.Latitude, 'f', -1, 64))
	q.Set("longitude", strconv.FormatFloat(p.Longitude, 'f', -1, 64))
	q.Set("distance", strconv.Itoa(p.Distance))
	q.Set("searchString", s)
	if p.NextKey != "" {
		q.Set("nextKey", p.NextKey)
	}

	body, err := c.GetJSON("/events", q)
	if err != nil {
		return nil, "", err
	}
	var env eventsListEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, "", fmt.Errorf("events list JSON: %w", err)
	}
	return env.Data, env.NextKey, nil
}

// SearchEventsAll follows nextKey until empty or maxPages reached (0 = no limit).
func SearchEventsAll(c *Client, p EventSearchParams, maxPages int) ([]EventListHit, error) {
	var out []EventListHit
	seen := make(map[string]struct{})
	key := p.NextKey
	pages := 0
	for {
		p.NextKey = key
		hits, nk, err := SearchEventsPage(c, p)
		if err != nil {
			return out, err
		}
		for _, h := range hits {
			if h.ID == "" {
				continue
			}
			if _, ok := seen[h.ID]; ok {
				continue
			}
			seen[h.ID] = struct{}{}
			out = append(out, h)
		}
		pages++
		if nk == "" {
			break
		}
		key = nk
		if maxPages > 0 && pages >= maxPages {
			break
		}
	}
	return out, nil
}
