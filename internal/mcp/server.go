// Package mcp is braveduck's MCP face: the job pipeline exposed as tools over
// stdio, so Claude Code / Codex / agy can drive it natively. Like the CLI and
// the REST API it is a thin adapter — every tool calls internal/service and
// returns the same JSON shapes the REST API does. See docs/specs/mcp-server.md.
//
// stdout is the MCP channel: nothing in this package (or anything it calls)
// may print to it. Logs go to stderr.
package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/core/keyword"
	"github.com/khangpworking/braveduck-cli/internal/core/rating"
	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// Version is reported to clients in the MCP handshake.
var Version = "dev"

// Serve runs the MCP server over stdio until the client disconnects or ctx ends.
func Serve(ctx context.Context, svc *service.Service) error {
	return NewServer(svc).Run(ctx, &mcp.StdioTransport{})
}

// NewServer builds the server with every tool registered (tests connect to it
// over in-memory transports).
func NewServer(svc *service.Service) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "braveduck",
		Title:   "braveduck job board",
		Version: Version,
	}, &mcp.ServerOptions{
		Instructions: "braveduck is the user's local job-hunt board. Jobs move through lanes " +
			"saved → waiting → applied → interview → offer → closed. Use list_jobs to see the " +
			"board, add_job to save a listing (a URL or pasted job description), set_status to " +
			"move a job. To rate jobs: call get_rating_queue (or rate_job for one job), judge each " +
			"job against the user's profile using the rubric it returns, then call submit_rating " +
			"once per job. If the profile is empty, tell the user which file to fill in — don't " +
			"invent a profile. Tools marked 'not wired' fail honestly — don't retry them.",
	})
	t := tools{svc}

	// Reads.
	add(srv, "list_jobs", "List jobs on the board, newest first. Optionally filter by lane (status) or discovery source.", readOnly, t.listJobs)
	add(srv, "get_job", "Get one job with every field (JD, score, apply state, notes...).", readOnly, t.getJob)
	add(srv, "get_stats", "Funnel counts per lane plus the interview rate and morale line.", readOnly, t.getStats)
	add(srv, "get_keywords", "The crawler keyword filter lists.", readOnly, t.getKeywords)
	add(srv, "get_rating_config", "The A/B rater config: which model ids score jobs and which one owns the board badge.", readOnly, t.getRatingConfig)
	add(srv, "get_crawl_status", "Per-source crawl health and how many jobs were found today.", readOnly, t.getCrawlStatus)
	add(srv, "get_activity", "Recent activity-feed events (adds, moves, removals), newest first.", readOnly, t.getActivity)

	// Writes.
	add(srv, "add_job", "Save a job. Pass `content` (a listing URL — fetched and parsed — or a pasted job description), "+
		"or describe it field by field with `title` (+ optional company/url/salary/source/jd). "+
		"Fails if the URL is already on the board.", write, t.addJob)
	add(srv, "update_job", "Change a job's lane, pinned/keep flags or notes. Only the fields you pass change.", idempotentWrite, t.updateJob)
	add(srv, "set_status", "Move a job to a lane: saved, waiting, applied, interview, offer or closed.", idempotentWrite, t.setStatus)
	add(srv, "pin_job", "Toggle a job's pin (pinned jobs sort first in their lane).", write, t.pinJob)
	add(srv, "keep_job", "Toggle a job's keep flag (kept jobs are exempt from auto-clear).", write, t.keepJob)
	add(srv, "delete_job", "Permanently delete a job and its ratings.", destructive, t.deleteJob)
	add(srv, "get_profile", "The user's profile (CV + what they want) that jobs are rated against, its file path, and whether it has been filled in.", readOnly, t.getProfile)
	add(srv, "get_rating_queue", "Jobs waiting for a score (oldest first), plus the user's profile, the scoring rubric and the default rater id. Judge each job, then call submit_rating for it. Fails if the profile is still empty.", readOnly, t.ratingQueue)
	add(srv, "rate_job", "Everything needed to rate (or re-rate) one job: the user's profile, the rubric and the job. Then call submit_rating.", readOnly, t.rateJob)
	add(srv, "submit_rating", "Save your verdict for one job: score 1.0-5.0 (letter is derived if omitted: A>=4.5, B>=4.0, C>=3.5, D>=3.0, else F) and 3-4 short reasons separated by ' • '. "+
		"Leave model empty unless you're an A/B challenger; the default rater's verdict becomes the board badge.", idempotentWrite, t.submitRating)
	add(srv, "list_platforms", "Job sites braveduck can hold a login for, and whether the user is logged in to each (status only — never session data).", readOnly, t.listPlatforms)
	add(srv, "connect_platform", "Open a browser window on the user's screen so THEY can log in to a job site (braveduck never sees the password). Returns at once with status 'connecting'; poll list_platforms to see when it becomes 'connected'. Only call this when the user asks to connect a site.", write, t.connectPlatform)
	add(srv, "disconnect_platform", "Delete the saved login for a job site.", destructive, t.disconnectPlatform)
	add(srv, "set_rating_config", "Set which model ids (1-3) score jobs, which one is the default (owns the badge), and whether A/B challengers run.", idempotentWrite, t.setRatingConfig)

	// Surfaced so an agent can see the whole pipeline; each fails honestly
	// until its subsystem lands (spec: "surface now, body later").
	for _, name := range []string{
		"ask_model", "generate_cv", "draft_email", "approve_draft", "reject_draft",
		"apply_job", "crawl", "retry_crawl", "set_keywords",
	} {
		addStub(srv, name)
	}
	return srv
}

// --- annotations ---

var (
	readOnly        = &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(false)}
	write           = &mcp.ToolAnnotations{DestructiveHint: ptr(false), OpenWorldHint: ptr(false)}
	idempotentWrite = &mcp.ToolAnnotations{DestructiveHint: ptr(false), IdempotentHint: true, OpenWorldHint: ptr(false)}
	destructive     = &mcp.ToolAnnotations{DestructiveHint: ptr(true), IdempotentHint: true, OpenWorldHint: ptr(false)}
)

func ptr[T any](v T) *T { return &v }

// add registers a typed tool whose handler only needs its input.
func add[In, Out any](srv *mcp.Server, name, desc string, ann *mcp.ToolAnnotations, h func(context.Context, In) (Out, error)) {
	mcp.AddTool(srv, &mcp.Tool{Name: name, Description: desc, Annotations: ann},
		func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
			out, err := h(ctx, in)
			return nil, out, toolErr(err)
		})
}

// ErrNotWired is returned by tools whose subsystem isn't in this build yet.
var ErrNotWired = errors.New("not wired in this build yet")

func addStub(srv *mcp.Server, name string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        name,
		Description: "Not wired in this build yet — always fails. Listed so you can see the full pipeline.",
		Annotations: readOnly,
	}, func(context.Context, *mcp.CallToolRequest, map[string]any) (*mcp.CallToolResult, any, error) {
		return nil, nil, fmt.Errorf("%s: %w", name, ErrNotWired)
	})
}

// toolErr phrases service errors for an agent: what went wrong and whether a
// retry with different input could help.
func toolErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, service.ErrNotFound):
		return fmt.Errorf("%w — check the id with list_jobs", err)
	case errors.Is(err, service.ErrDuplicate):
		return fmt.Errorf("%w — use list_jobs to find it", err)
	default:
		return err
	}
}

// --- tool inputs/outputs ---

type idIn struct {
	ID int64 `json:"id" jsonschema:"the job id (from list_jobs)"`
}

type listIn struct {
	Status string `json:"status,omitempty" jsonschema:"only jobs in this lane: saved, waiting, applied, interview, offer or closed"`
	Source string `json:"source,omitempty" jsonschema:"only jobs from this discovery source, e.g. topcv, vietnamworks, paste"`
}

// Object wrappers: structured tool output is an object in every MCP client.
type jobsOut struct {
	Jobs []jobs.Job `json:"jobs"`
}

type eventsOut struct {
	Events []store.Event `json:"events"`
}

type addIn struct {
	Content string `json:"content,omitempty" jsonschema:"a listing URL or a pasted job description; use this OR title"`
	Title   string `json:"title,omitempty" jsonschema:"job title, when describing the job field by field"`
	Company string `json:"company,omitempty"`
	URL     string `json:"url,omitempty" jsonschema:"listing URL (field-by-field mode; not fetched)"`
	Salary  string `json:"salary,omitempty"`
	Source  string `json:"source,omitempty" jsonschema:"discovery source; inferred from the URL when omitted"`
	JD      string `json:"jd,omitempty" jsonschema:"job description text"`
}

type updateIn struct {
	ID     int64   `json:"id" jsonschema:"the job id"`
	Status *string `json:"status,omitempty" jsonschema:"new lane: saved, waiting, applied, interview, offer or closed"`
	Pinned *bool   `json:"pinned,omitempty"`
	Keep   *bool   `json:"keep,omitempty" jsonschema:"exempt from auto-clear"`
	Notes  *string `json:"notes,omitempty" jsonschema:"replaces the job's notes"`
}

type statusIn struct {
	ID    int64  `json:"id" jsonschema:"the job id"`
	Stage string `json:"stage" jsonschema:"saved, waiting, applied, interview, offer or closed"`
}

type activityIn struct {
	Limit int `json:"limit,omitempty" jsonschema:"how many events (default 20)"`
}

type ratingIO struct {
	DefaultModel string   `json:"default_model" jsonschema:"must be one of models; owns the board badge"`
	ABMode       bool     `json:"ab_mode" jsonschema:"also run the other models as challengers"`
	Models       []string `json:"models" jsonschema:"1-3 rater model ids"`
}

type deletedOut struct {
	Deleted jobs.Job `json:"deleted"`
}

type platformIn struct {
	Platform string `json:"platform" jsonschema:"site id from list_platforms, e.g. topcv"`
}

type platformsOut struct {
	Platforms []service.PlatformStatus `json:"platforms"`
}

type disconnectOut struct {
	Deleted bool `json:"deleted"`
}

type queueIn struct {
	Limit int `json:"limit,omitempty" jsonschema:"how many jobs (default 10)"`
}

type verdictIn struct {
	ID        int64   `json:"id" jsonschema:"the job id"`
	Score     float64 `json:"score" jsonschema:"1.0 (skip) to 5.0 (apply now)"`
	Letter    string  `json:"letter,omitempty" jsonschema:"A, B, C, D or F; derived from score when omitted"`
	Reasoning string  `json:"reasoning" jsonschema:"3-4 concise points separated by ' • ': fit, strongest proof point, comp if notable, key gap or red flag"`
	Model     string  `json:"model,omitempty" jsonschema:"your model id, only when rating as an A/B challenger"`
}

// --- handlers ---

type tools struct{ svc *service.Service }

func (t tools) listJobs(ctx context.Context, in listIn) (jobsOut, error) {
	list, err := t.svc.ListJobs(ctx, service.ListFilter{Status: in.Status, Source: in.Source})
	return jobsOut{list}, err
}

func (t tools) getJob(ctx context.Context, in idIn) (jobs.Job, error) {
	return t.svc.GetJob(ctx, in.ID)
}

func (t tools) getStats(ctx context.Context, _ struct{}) (service.Stats, error) {
	return t.svc.Stats(ctx)
}

func (t tools) getKeywords(ctx context.Context, _ struct{}) (keyword.Sets, error) {
	return t.svc.Keywords(ctx)
}

func (t tools) getRatingConfig(ctx context.Context, _ struct{}) (ratingIO, error) {
	c, err := t.svc.RatingConfig(ctx)
	return ratingIO{DefaultModel: c.DefaultModel, ABMode: c.ABEnabled, Models: c.Models}, err
}

func (t tools) getCrawlStatus(ctx context.Context, _ struct{}) (store.CrawlStatusView, error) {
	return t.svc.CrawlStatus(ctx)
}

func (t tools) getActivity(ctx context.Context, in activityIn) (eventsOut, error) {
	ev, err := t.svc.Activity(ctx, in.Limit)
	return eventsOut{ev}, err
}

func (t tools) addJob(ctx context.Context, in addIn) (jobs.Job, error) {
	switch {
	case in.Content != "" && in.Title != "":
		return jobs.Job{}, fmt.Errorf("%w: pass content or title, not both", service.ErrInvalid)
	case in.Content != "":
		return t.svc.AddFromContent(ctx, in.Content)
	default:
		return t.svc.AddJob(ctx, service.NewJob{
			Title: in.Title, Company: in.Company, URL: in.URL,
			Salary: in.Salary, Source: in.Source, JD: in.JD,
		})
	}
}

func (t tools) updateJob(ctx context.Context, in updateIn) (jobs.Job, error) {
	return t.svc.UpdateJob(ctx, in.ID, service.Patch{
		Status: in.Status, Pinned: in.Pinned, Keep: in.Keep, Notes: in.Notes,
	})
}

func (t tools) setStatus(ctx context.Context, in statusIn) (jobs.Job, error) {
	return t.svc.SetStatus(ctx, in.ID, in.Stage)
}

func (t tools) pinJob(ctx context.Context, in idIn) (jobs.Job, error) {
	return t.svc.TogglePin(ctx, in.ID)
}

func (t tools) keepJob(ctx context.Context, in idIn) (jobs.Job, error) {
	return t.svc.ToggleKeep(ctx, in.ID)
}

func (t tools) deleteJob(ctx context.Context, in idIn) (deletedOut, error) {
	j, err := t.svc.DeleteJob(ctx, in.ID)
	return deletedOut{j}, err
}

func (t tools) setRatingConfig(ctx context.Context, in ratingIO) (ratingIO, error) {
	c, err := t.svc.SetRatingConfig(ctx, rating.Config{
		ABEnabled: in.ABMode, Models: in.Models, DefaultModel: in.DefaultModel,
	})
	return ratingIO{DefaultModel: c.DefaultModel, ABMode: c.ABEnabled, Models: c.Models}, err
}

func (t tools) getProfile(ctx context.Context, _ struct{}) (service.Profile, error) {
	return t.svc.Profile(ctx)
}

func (t tools) ratingQueue(ctx context.Context, in queueIn) (service.RatingBrief, error) {
	return t.svc.RatingQueue(ctx, in.Limit)
}

func (t tools) rateJob(ctx context.Context, in idIn) (service.RatingBrief, error) {
	return t.svc.RateBrief(ctx, in.ID)
}

func (t tools) submitRating(ctx context.Context, in verdictIn) (jobs.Job, error) {
	return t.svc.SubmitRating(ctx, service.Verdict{
		JobID: in.ID, Model: in.Model, Score: in.Score, Letter: in.Letter, Reasoning: in.Reasoning,
	})
}

func (t tools) listPlatforms(ctx context.Context, _ struct{}) (platformsOut, error) {
	ps, err := t.svc.Platforms(ctx)
	return platformsOut{ps}, err
}

func (t tools) connectPlatform(_ context.Context, in platformIn) (service.PlatformStatus, error) {
	return t.svc.StartConnect(in.Platform)
}

func (t tools) disconnectPlatform(ctx context.Context, in platformIn) (disconnectOut, error) {
	deleted, err := t.svc.Disconnect(ctx, in.Platform)
	return disconnectOut{deleted}, err
}
