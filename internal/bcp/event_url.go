package bcp

import (
	"fmt"
	"net/url"
	"strings"
)

const eventPageBase = "https://www.bestcoastpairings.com/event/"

// EventPageURL is the public Best Coast Pairings event page for an event id.
func EventPageURL(eventID string) string {
	id := strings.TrimSpace(eventID)
	if id == "" {
		return ""
	}
	return eventPageBase + url.PathEscape(id)
}

// EventMarkdownLink is a Discord/markdown link to the BCP event page.
// Empty ids render as an em dash (not a link).
func EventMarkdownLink(eventID string) string {
	id := strings.TrimSpace(eventID)
	if id == "" {
		return "—"
	}
	return fmt.Sprintf("[%s](%s)", id, EventPageURL(id))
}
