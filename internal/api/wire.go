package api

import (
	"github.com/khangpworking/braveduck-cli/internal/service"
)

// The REST API's own request and response shapes (the rest are the service's
// and the store's types, sent as they are). Named and exported so tygo can
// write them as TypeScript for the board (tygo.yaml → web/src/lib/gen/).
// Changing a json tag here changes the API: the board's types change with it.

// CreateJobRequest is the board's "Add": a link, the text of a posting, or a
// Facebook group's link.
type CreateJobRequest struct {
	Content string `json:"content"`
}

// FollowGroupReply answers a pasted Facebook group link (201).
type FollowGroupReply struct {
	Group service.FBGroup `json:"group"`
	Added bool            `json:"added"` // false: followed already
}

// DuplicateMatch is a job a refused paste matches.
type DuplicateMatch struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status" tstype:"Status"`
}

// DuplicateReply refuses a paste that's already on the board (409).
type DuplicateReply struct {
	Error      string           `json:"error"`
	Duplicates []DuplicateMatch `json:"duplicates"`
}

// JobPatch is the mutable subset a board edits. Pointers distinguish "field
// omitted" from a zero value, so a PATCH touches only what it names.
type JobPatch struct {
	Status   *string `json:"status,omitempty" tstype:"Status"`
	Pinned   *bool   `json:"pinned,omitempty"`
	Keep     *bool   `json:"keep,omitempty"`
	Notes    *string `json:"notes,omitempty"`
	Title    *string `json:"title,omitempty"`
	Company  *string `json:"company,omitempty"`
	Salary   *string `json:"salary,omitempty"`
	Location *string `json:"location,omitempty"`
}

// RatingConfig is the board's wire shape for rating.Config.
type RatingConfig struct {
	DefaultModel string   `json:"default_model"`
	ABMode       bool     `json:"ab_mode"`
	Models       []string `json:"models"`
}

// JevState is whether Jev can rate here and whether this user opted out of
// sending their profile to it.
type JevState struct {
	Available bool `json:"available"`
	OptOut    bool `json:"opt_out"`
}

// CrawlReply answers "check now" for one source. Result is there when the
// crawl ran.
type CrawlReply struct {
	OK      bool                 `json:"ok"`
	Source  string               `json:"source"`
	Result  *service.CrawlResult `json:"result,omitempty"`
	Message string               `json:"message"`
}

// SourceOnRequest turns a job source on or off for the board.
type SourceOnRequest struct {
	On bool `json:"on"`
}

// GroupPausedRequest turns a followed Facebook group off (paused) or on.
type GroupPausedRequest struct {
	Paused bool `json:"paused"`
}

// ScheduleRequest sets how often a source is crawled (0 = off).
type ScheduleRequest struct {
	EveryMinutes int `json:"every_minutes"`
}

// LocationRequest sets the user's location.
type LocationRequest struct {
	Lat   float64 `json:"lat"`
	Lng   float64 `json:"lng"`
	Label string  `json:"label"`
	MaxKM float64 `json:"max_km"`
}

// ProfileText replaces the profile's Markdown.
type ProfileText struct {
	Text string `json:"text"`
}

// ProfileFormRequest saves the profile form.
type ProfileFormRequest struct {
	Form service.ProfileForm `json:"form"`
}

// ProfileFormReply is the profile as a form, with the choices it offers.
type ProfileFormReply struct {
	Form    service.ProfileForm        `json:"form"`
	Options service.ProfileFormOptions `json:"options"`
}

// BaseCVReply is the uploaded CV, if any, and whether the profile is filled.
type BaseCVReply struct {
	CV      *service.BaseCV `json:"cv"` // null before the first upload
	Profile ProfileFilled   `json:"profile"`
}

// ProfileFilled says where the profile is and whether it's filled in.
type ProfileFilled struct {
	Path   string `json:"path"`
	Filled bool   `json:"filled"`
}

// RateRequest asks for a rating; Model "" is the default rater.
type RateRequest struct {
	Model string `json:"model"`
}

// DraftRequest asks for an email brief, with notes for the user's AI.
type DraftRequest struct {
	Notes string `json:"notes"`
}

// CVRequest saves the CV the user's AI wrote for a job.
type CVRequest struct {
	Markdown string `json:"markdown"`
}

// AskReply is what to paste into the user's AI: a short request for an agent
// with braveduck connected, and the full brief for any chat.
type AskReply struct {
	Prompt string `json:"prompt"` // for Claude Code etc. (braveduck over MCP)
	Brief  string `json:"brief"`  // paste-ready, for any AI chat
}

// ApplyConfirmRequest answers "did you submit it?" after applying on a site.
type ApplyConfirmRequest struct {
	Submitted bool `json:"submitted"`
}

// AgentKeyState says whether the user's AI agent has a key.
type AgentKeyState struct {
	Exists bool `json:"exists"`
}

// NewAgentKey is a new agent key, shown once.
type NewAgentKey struct {
	Key string `json:"key"`
}

// ErrorReply is every error's body. Code names a case the board handles
// itself ("profile_empty").
type ErrorReply struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}
