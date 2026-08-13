package bcp

import (
	"fmt"
	"net/url"
	"strings"
)

const eventPageBase = "https://www.bestcoastpairings.com/event/"

// ParseEventID accepts a bare BCP event id or a full bestcoastpairings.com event URL.
func ParseEventID(input string) string {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		if u, err := url.Parse(raw); err == nil {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			for i, p := range parts {
				if strings.EqualFold(p, "event") && i+1 < len(parts) {
					id, _ := url.PathUnescape(parts[i+1])
					return strings.TrimSpace(id)
				}
			}
		}
	}
	if i := strings.Index(lower, "/event/"); i >= 0 {
		rest := raw[i+len("/event/"):]
		if j := strings.IndexAny(rest, "?#/ \t"); j >= 0 {
			rest = rest[:j]
		}
		id, _ := url.PathUnescape(rest)
		return strings.TrimSpace(id)
	}
	return raw
}

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
