package bcp

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	BaseURL  = "https://newprod-api.bestcoastpairings.com"
	ClientID = "web-app"
)

// Client calls Best Coast Pairings’ public web API (same headers as their SPA).
type Client struct {
	HTTP *http.Client
	// MinInterval, if > 0, enforces a minimum delay between completed GETs (rate limiting).
	MinInterval time.Duration
	// BearerToken is optional (e.g. Cognito JWT from a logged-in BCP session). Required for
	// GET /v1/armylists/{id} which returns "unauthorized access" without it.
	BearerToken string

	lastTick    time.Time
	throttleMu  sync.Mutex
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) throttle() {
	if c.MinInterval <= 0 {
		return
	}
	c.throttleMu.Lock()
	defer c.throttleMu.Unlock()
	elapsed := time.Since(c.lastTick)
	if rem := c.MinInterval - elapsed; rem > 0 {
		time.Sleep(rem)
	}
	c.lastTick = time.Now()
}

func (c *Client) get(u *url.URL) ([]byte, error) {
	c.throttle()

	var lastErr error
	for attempt := 0; attempt < 8; attempt++ {
		req, err := http.NewRequest(http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("client-id", ClientID)
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; fortyk-bcp-harvest/1.0)")
		req.Header.Set("Accept", "application/json")
		if t := strings.TrimSpace(c.BearerToken); t != "" {
			req.Header.Set("Authorization", "Bearer "+t)
		}

		resp, err := c.httpClient().Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(backoff(attempt))
			continue
		}

		body, rerr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if rerr != nil {
			return nil, rerr
		}

		if resp.StatusCode == http.StatusOK {
			return body, nil
		}

		lastErr = fmt.Errorf("GET %s: HTTP %d: %s", u.Path, resp.StatusCode, strings.TrimSpace(string(body)))
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 502 {
			time.Sleep(backoff(attempt))
			continue
		}
		return nil, lastErr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("GET %s: exhausted retries", u.Path)
	}
	return nil, lastErr
}

func backoff(attempt int) time.Duration {
	d := time.Duration(250*(1<<attempt)) * time.Millisecond
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d
}

// GetJSON performs GET /v1/{path} with query values (path without leading slash).
func (c *Client) GetJSON(path string, q url.Values) ([]byte, error) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u, err := url.Parse(BaseURL + "/v1" + path)
	if err != nil {
		return nil, err
	}
	u.RawQuery = q.Encode()
	return c.get(u)
}
