// Package jev is a client for Jev, TypeSafe AI's decision model
// (https://api.typesafe.ai/v1/systemone). Jev doesn't write text: it answers
// multiple-choice questions about a piece of text ("state"), returning a
// choice and a probability for every option. braveduck uses it to rate jobs.
//
// The API key comes from TYPESAFE_API_KEY or the key file jev-route already
// uses; it is never printed, logged or stored, and is redacted from errors.
package jev

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// DefaultModel is the Jev model braveduck rates with.
const DefaultModel = "jev-1.13.0"

// Question is one multiple-choice question; Criteria maps each option to
// what it means.
type Question struct {
	Type         string            `json:"type"` // "choice"
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

// Answer is Jev's verdict on one question.
type Answer struct {
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
}

// Confidence is the probability of the chosen option.
func (a Answer) Confidence() float64 { return a.Probabilities[a.Choice] }

// ErrNoKey means no API key was found.
var ErrNoKey = errors.New("no Jev API key: set TYPESAFE_API_KEY or create ~/.config/jev-route/typesafe.env")

// Client calls the Jev API.
type Client struct {
	Endpoint string
	Model    string
	Key      string
	HTTP     *http.Client
}

// keyFiles are where jev-route keeps the key (first that exists wins).
var keyFiles = []string{"~/.config/jev-route/typesafe.env", "~/.config/tdn-growth-os/secrets/typesafe-jev.env"}

// LoadKey finds the API key and says where it came from ("env" or the file
// path); the key itself is never returned in an error or log.
func LoadKey() (key, source string, err error) {
	if k := strings.TrimSpace(os.Getenv("TYPESAFE_API_KEY")); k != "" {
		return k, "TYPESAFE_API_KEY", nil
	}
	home, _ := os.UserHomeDir()
	for _, p := range keyFiles {
		p = filepath.Join(home, strings.TrimPrefix(p, "~/"))
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if v, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "TYPESAFE_API_KEY="); ok {
				f.Close()
				if k := strings.Trim(strings.TrimSpace(v), `'"`); k != "" {
					return k, p, nil
				}
			}
		}
		f.Close()
	}
	return "", "", ErrNoKey
}

// New returns a client with the given key and production settings.
func New(key string) *Client {
	return &Client{
		Endpoint: "https://api.typesafe.ai/v1/systemone",
		Model:    DefaultModel,
		Key:      key,
		HTTP:     &http.Client{Timeout: 30 * time.Second},
	}
}

// Clef models: Cloudflare's decision models on Workers AI. They take Jev's
// request and answer in Jev's shape, wrapped in Cloudflare's {"result": …}.
const (
	ClefFlash = "clef-flash" // fast; the default
	Clef      = "clef"       // larger and slower, for careful ratings
)

// IsClefModel reports whether id is one of the Clef models.
func IsClefModel(id string) bool { return id == ClefFlash || id == Clef }

var accountIDRE = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

// ValidAccountID reports whether s looks like a Cloudflare account ID.
func ValidAccountID(s string) bool { return accountIDRE.MatchString(s) }

// NewClef returns a client for a Clef model on Workers AI, with a Cloudflare
// API token that has Workers AI permission on accountID.
func NewClef(token, accountID, model string) *Client {
	if !IsClefModel(model) {
		model = ClefFlash
	}
	return &Client{
		Endpoint: "https://api.cloudflare.com/client/v4/accounts/" + strings.ToLower(accountID) + "/ai/run/@cf/cloudflare/" + model,
		Model:    model,
		Key:      token,
		HTTP:     &http.Client{Timeout: 60 * time.Second},
	}
}

// name is the service in error messages: "clef" or "jev".
func (c *Client) name() string {
	if IsClefModel(c.Model) {
		return "clef"
	}
	return "jev"
}

var bearerRE = regexp.MustCompile(`(?i)bearer\s+\S+`)

func (c *Client) redact(s string) string {
	s = bearerRE.ReplaceAllString(s, "Bearer ***")
	if c.Key != "" {
		s = strings.ReplaceAll(s, c.Key, "***")
	}
	return s
}

// Ask sends state and questions, and returns a validated answer for every
// question (probabilities over exactly its options, summing to 1 ± 0.02).
func (c *Client) Ask(ctx context.Context, state string, questions map[string]Question) (map[string]Answer, error) {
	body, _ := json.Marshal(map[string]any{"model": c.Model, "state": state, "questions": questions})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %s", c.name(), c.redact(err.Error()))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return nil, fmt.Errorf("%s: HTTP %d: %s", c.name(), resp.StatusCode, c.redact(msg))
	}
	// Jev answers {"answers": …}; Workers AI wraps the same in {"result": …}.
	var payload struct {
		Answers map[string]Answer `json:"answers"`
		Result  *struct {
			Answers map[string]Answer `json:"answers"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil && payload.Answers == nil && payload.Result != nil {
		payload.Answers = payload.Result.Answers
	}
	if payload.Answers == nil {
		return nil, fmt.Errorf("%s: unexpected response (no answers)", c.name())
	}
	for name, q := range questions {
		a, ok := payload.Answers[name]
		if !ok || !validAnswer(a, q) {
			return nil, fmt.Errorf("%s: invalid answer for %q", c.name(), name)
		}
	}
	return payload.Answers, nil
}

func validAnswer(a Answer, q Question) bool {
	if _, ok := q.Criteria[a.Choice]; !ok || len(a.Probabilities) != len(q.Criteria) {
		return false
	}
	sum := 0.0
	for opt, p := range a.Probabilities {
		if _, ok := q.Criteria[opt]; !ok || p < 0 || p > 1 || math.IsNaN(p) {
			return false
		}
		sum += p
	}
	return math.Abs(sum-1) <= 0.02
}
