package bcp

import (
	"encoding/json"
	"net/url"
)

// Event is the public metadata returned by GET /v1/events/{id}.
type Event struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	EventDate       string `json:"eventDate"`
	EventEndDate    string `json:"eventEndDate"`
	TimeZone        string `json:"timeZone"`
	NumberOfRounds  int    `json:"numberOfRounds"`
	CurrentRound    int    `json:"currentRound"`
	PairingStyle    string `json:"pairingStyle"`
	Ended           bool   `json:"ended"`
	TotalPlayers    int    `json:"totalPlayers"`
	GameSystemName  string `json:"gameSystemName"`
	GameStoreName   string `json:"gameStoreName"`
}

func FetchEvent(c *Client, eventID string) (*Event, error) {
	body, err := c.GetJSON("/events/"+url.PathEscape(eventID), nil)
	if err != nil {
		return nil, err
	}
	var ev Event
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, err
	}
	return &ev, nil
}
