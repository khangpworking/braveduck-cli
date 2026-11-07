// Package fbwatcher talks to fb-watcher's HTTP API (khangpworking/fb-watcher,
// `watcher.py api`): read one Facebook post, or check whether a group is
// public. braveduck never opens Facebook itself; fb-watcher does, with its
// own account, only for public groups, and limits how often.
//
// The API answers on localhost (or inside a private network) with a Bearer
// key; it must never be reachable from the internet.
package fbwatcher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Client calls one fb-watcher API.
type Client struct {
	URL  string // e.g. http://127.0.0.1:8091
	Key  string
	HTTP *http.Client
}

// FromEnv builds a client from BRAVEDUCK_FBWATCHER_URL and
// BRAVEDUCK_FBWATCHER_KEY_FILE; nil when the URL isn't set.
func FromEnv() (*Client, error) {
	url := strings.TrimSpace(os.Getenv("BRAVEDUCK_FBWATCHER_URL"))
	if url == "" {
		return nil, nil
	}
	file := os.Getenv("BRAVEDUCK_FBWATCHER_KEY_FILE")
	if file == "" {
		return nil, errors.New("BRAVEDUCK_FBWATCHER_URL is set but BRAVEDUCK_FBWATCHER_KEY_FILE isn't")
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read the fb-watcher key: %w", err)
	}
	return &Client{URL: strings.TrimRight(url, "/"), Key: strings.TrimSpace(string(b))}, nil
}

// Post is a post fb-watcher read (its schema 1, the fields braveduck uses).
type Post struct {
	URL       string `json:"url"`
	GroupID   string `json:"group_id"`
	GroupName string `json:"group_name"`
	Author    string `json:"author"`
	Text      string `json:"text"`
}

// Group is what fb-watcher found about a group.
type Group struct {
	Name       string `json:"name"`
	Visibility string `json:"visibility"` // public / private / unavailable
	Note       string `json:"note"`
	GroupID    string `json:"group_id"`
}

// Error is an answer fb-watcher refused with.
type Error struct {
	Status     int
	Kind       string        // not_public, not_visible, rate_limited, profile_busy, login_required, timeout, …
	Message    string        // fb-watcher's own words, for the server's log
	RetryAfter time.Duration // when it says when to try again
}

func (e *Error) Error() string {
	return fmt.Sprintf("fb-watcher: %s (%d): %s", e.Kind, e.Status, e.Message)
}

// FetchPost reads the post at link (a group post or share link).
func (c *Client) FetchPost(ctx context.Context, link string) (Post, error) {
	var p Post
	err := c.call(ctx, "/fetch-post", map[string]string{"url": link}, &p)
	return p, err
}

// CheckGroup reads whether the group at link is public.
func (c *Client) CheckGroup(ctx context.Context, link string) (Group, error) {
	var g Group
	err := c.call(ctx, "/check-group", map[string]string{"url": link}, &g)
	return g, err
}

// Reading a post takes 10–25 s and fb-watcher gives up at 120 s.
const callTimeout = 150 * time.Second

func (c *Client) call(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Key)
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	res, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.NewDecoder(res.Body).Decode(&e)
		secs, _ := strconv.Atoi(res.Header.Get("Retry-After"))
		return &Error{Status: res.StatusCode, Kind: e.Error, Message: e.Message, RetryAfter: time.Duration(secs) * time.Second}
	}
	return json.NewDecoder(res.Body).Decode(out)
}
