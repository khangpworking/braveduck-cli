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
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/khangpworking/braveduck-cli/internal/core/cvfile"
	"github.com/khangpworking/braveduck-cli/internal/core/geo"
	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/core/keyword"
	"github.com/khangpworking/braveduck-cli/internal/core/rating"
	"github.com/khangpworking/braveduck-cli/internal/core/scrape"
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
	add(srv, "get_setup", "How far the user is with setting braveduck up: CV uploaded, the profile's About me and what they want filled in, how many search terms, and the optional location and TopCV login. done is true once ratings and crawls can work. Use it to guide the user through what's missing (upload_cv, update_profile, set_keywords).", readOnly, t.getSetup)
	add(srv, "get_today", "What the user should act on now: the best-rated jobs not applied to yet, postings that close within a week, and applications waiting a week or more for an answer. Empty groups are left out.", readOnly, t.getToday)
	add(srv, "get_keywords", "The crawler keyword filter lists.", readOnly, t.getKeywords)
	add(srv, "get_rating_config", "The A/B rater config: which model ids score jobs and which one owns the board badge.", readOnly, t.getRatingConfig)
	add(srv, "get_crawl_status", "Per-source crawl health and how many jobs were found today.", readOnly, t.getCrawlStatus)
	add(srv, "get_activity", "Recent activity-feed events (adds, moves, removals), newest first.", readOnly, t.getActivity)

	// Writes.
	add(srv, "add_job", "Save a job. Pass `content`: a link from TopCV, VietnamWorks, JobOKO or CareerLink (fetched and parsed), "+
		"a Facebook post link followed by the post's text (the link isn't opened), or a pasted job description. "+
		"Links from other sites are refused. Or "+
		"describe it field by field with `title` (+ optional company/url/salary/source/jd). "+
		"Fails if the URL is already on the board.", write, t.addJob)
	add(srv, "update_job", "Change a job's lane, pinned/keep flags, notes, title, company, salary or location. Only the fields you pass change.", idempotentWrite, t.updateJob)
	add(srv, "set_status", "Move a job to a lane: saved, waiting, applied, interview, offer or closed.", idempotentWrite, t.setStatus)
	add(srv, "mark_followed_up", "Record that the user asked the company about an application (Applied lane only) — braveduck sends nothing itself. get_today then stops listing it until another week passes without an answer.", write, t.markFollowedUp)
	add(srv, "pin_job", "Toggle a job's pin (pinned jobs sort first in their lane).", write, t.pinJob)
	add(srv, "keep_job", "Toggle a job's keep flag (kept jobs are exempt from auto-clear).", write, t.keepJob)
	add(srv, "delete_job", "Permanently delete a job, its ratings and its tailored CVs.", destructive, t.deleteJob)
	add(srv, "get_profile", "The user's profile (CV + what they want) that jobs are rated against, its file path, and whether it has been filled in.", readOnly, t.getProfile)
	add(srv, "upload_cv", "Upload the user's CV from a file on this computer (.pdf, .docx, .md or .html, up to 5 MB): it is kept for applying, and its text goes into the profile's \"About me\". If About me already has the user's own text this fails unless replace is true — ask the user first.", write, t.uploadCV)
	add(srv, "update_profile", "Replace the user's profile (Markdown: \"# About me\", \"# What I'm looking for\", compensation, location, deal-breakers…) that jobs are rated against. Read it with get_profile first and keep the user's own words; only write what the user told you.", idempotentWrite, t.updateProfile)
	add(srv, "get_rating_queue", "Jobs waiting for a score (oldest first), plus the user's profile, the scoring rubric and the default rater id. Judge each job, then call submit_rating for it. Fails if the profile is still empty.", readOnly, t.ratingQueue)
	add(srv, "rate_job", "Everything needed to rate (or re-rate) one job: the user's profile, the rubric and the job. Then call submit_rating.", readOnly, t.rateJob)
	add(srv, "submit_rating", "Save your verdict for one job: score 1.0-5.0 (letter is derived if omitted: A>=4.5, B>=4.0, C>=3.5, D>=3.0, else F) and 3-4 short reasons separated by ' • '. "+
		"Leave model empty unless you're an A/B challenger; the default rater's verdict becomes the board badge.", idempotentWrite, t.submitRating)
	add(srv, "list_platforms", "Job sites braveduck can hold a login for, and whether the user is logged in to each (status only — never session data).", readOnly, t.listPlatforms)
	add(srv, "connect_platform", "Open a browser window on the user's screen so THEY can log in to a job site (braveduck never sees the password). Returns at once with status 'connecting'; poll list_platforms to see when it becomes 'connected'. Only call this when the user asks to connect a site.", write, t.connectPlatform)
	add(srv, "disconnect_platform", "Delete the saved login for a job site.", destructive, t.disconnectPlatform)
	add(srv, "crawl", "Fetch new jobs now from braveduck's built-in crawlers (or one `source`, e.g. vietnamworks), using the user's search_queries keyword list and filters. Returns found / filtered / duplicates (already on the board) / added counts.", write, t.crawl)
	add(srv, "retry_crawl", "Same as crawl, for one source (the board's Retry).", write, t.retryCrawl)
	add(srv, "get_crawl_schedule", "How often each job source fetches new jobs (every_minutes; 0 = off).", readOnly, t.getCrawlSchedule)
	crawlMin := service.MinBuiltinEveryMinutes
	if svc.Hosted() != nil {
		crawlMin = service.HostedMinEveryMinutes
	}
	add(srv, "set_crawl_schedule", fmt.Sprintf("Set how often a source fetches new jobs: every_minutes 0 (off) or %d-%d for built-in crawlers (%d-%d for external tools).",
		crawlMin, service.MaxEveryMinutes, service.MinEveryMinutes, service.MaxEveryMinutes), idempotentWrite, t.setCrawlSchedule)
	add(srv, "set_keywords", "Replace one or more keyword lists. search_queries are what crawlers search for; positive_title / negative_title / negative_content / negative_company filter results. Lists you don't pass are left alone.", idempotentWrite, t.setKeywords)
	add(srv, "sync_platform", "Read the user's application history on a job site (platform: topcv) with their saved login, read-only, and update the board: TopCV status per job, Saved/Waiting jobs moved to Applied, missing applications added.", write, t.syncPlatform)
	add(srv, "cv_brief", "Everything needed to tailor the user's CV for one job: their profile, the job, instructions, and the current version if there is one. Write the CV in Markdown, then call submit_cv.", readOnly, t.cvBrief)
	add(srv, "submit_cv", "Save a tailored CV (Markdown) for a job as a new version. The board offers it as a PDF.", write, t.submitCV)
	add(srv, "draft_brief", "Everything needed to write an application email for one job: profile, job, contact email, instructions and the user's notes. Then call submit_draft.", readOnly, t.draftBrief)
	add(srv, "submit_draft", "Save an application email draft (to, subject, body) for the user to review. braveduck never sends it; the user sends it from their own mail app.", write, t.submitDraft)
	add(srv, "reject_draft", "Discard a job's email draft.", destructive, t.rejectDraft)
	add(srv, "apply_job", "Assisted apply on the job's site (TopCV, VietnamWorks, JobOKO, CareerLink… — any site the user is logged in to): opens a visible window on the user's screen with their login; THEY submit it (braveduck never clicks submit). Needs a tailored CV or an uploaded one. Returns at once with the site, the CV file to upload and the cover letter; the outcome appears in get_activity. On TopCV braveduck confirms from the user's application history; on other sites the job waits for confirm_applied once the user says whether they submitted. Only call this when the user asks to apply to that job. Limits: 10 per day, 5 minutes apart.", write, t.applyJob)
	add(srv, "confirm_applied", "After apply_job on a site other than TopCV: record whether the user says they submitted the application (submitted=true moves the job to Applied). Only pass what the user told you.", write, t.confirmApplied)
	add(srv, "get_location", "The user's location and maximum commute (jobs farther than max_km are flagged on the board).", readOnly, t.getLocation)
	add(srv, "set_location", "Set the user's location: either `address` (looked up with OpenStreetMap; the first match is used — confirm it with the user) or lat/lng, plus optional max_km. Only do this when the user asks.", idempotentWrite, t.setLocation)
	add(srv, "follow_facebook_group", "Follow a Facebook group by its link (facebook.com/groups/…). braveduck's scanner checks whether it's public; a public group's new posts that match the user's search terms then come to the board by themselves. A private group's posts are only for its members: the user pastes the ones they want (add_job with the post link and text).", write, t.followGroup)
	add(srv, "list_facebook_groups", "The Facebook groups the user follows, each with its name and visibility (checking, public, private or unavailable).", readOnly, t.listGroups)
	add(srv, "unfollow_facebook_group", "Stop following a Facebook group (by its key from list_facebook_groups). Jobs already on the board stay.", destructive, t.unfollowGroup)
	add(srv, "rate_with_jev", "Rate one job now with Jev (TypeSafe AI's decision model): A-F with probabilities and criterion-based reasons. Sends the user's profile and the job text to api.typesafe.ai.", write, t.rateWithJev)
	add(srv, "set_rating_config", "Set which model ids (1-3) score jobs, which one is the default (owns the badge), and whether A/B challengers run.", idempotentWrite, t.setRatingConfig)

	// Surfaced so an agent can see the whole pipeline; each fails honestly
	// until its subsystem lands (spec: "surface now, body later").
	for _, name := range []string{
		"ask_model",
	} {
		addStub(srv, name)
	}
	if !svc.OnOwnComputer() {
		srv.RemoveTools(localOnlyTools...)
	}
	return srv
}

// localOnlyTools only make sense with braveduck on the user's own computer:
// upload_cv reads a file path on the machine braveduck runs on (on a shared
// server, another user's files), and the others open a browser window there.
// A shared server leaves them out; the board uploads CVs instead.
var localOnlyTools = []string{"upload_cv", "connect_platform", "apply_job", "confirm_applied"}

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
	Query  string `json:"query,omitempty" jsonschema:"only jobs whose title, company, location, skills, salary or notes contain every word (accents optional, e.g. ky su ai)"`
}

// Object wrappers: structured tool output is an object in every MCP client.
type jobsOut struct {
	Jobs []jobs.Job `json:"jobs"`
}

type eventsOut struct {
	Events []store.Event `json:"events"`
}

type addIn struct {
	Content string `json:"content,omitempty" jsonschema:"a supported job link (optionally followed by the job text — required to read a Facebook post), or a pasted job description; use this OR title"`
	Title   string `json:"title,omitempty" jsonschema:"job title, when describing the job field by field"`
	Company string `json:"company,omitempty"`
	URL     string `json:"url,omitempty" jsonschema:"listing URL (field-by-field mode; not fetched)"`
	Salary  string `json:"salary,omitempty"`
	Source  string `json:"source,omitempty" jsonschema:"discovery source; inferred from the URL when omitted"`
	JD      string `json:"jd,omitempty" jsonschema:"job description text"`
}

type updateIn struct {
	ID       int64   `json:"id" jsonschema:"the job id"`
	Status   *string `json:"status,omitempty" jsonschema:"new lane: saved, waiting, applied, interview, offer or closed"`
	Pinned   *bool   `json:"pinned,omitempty"`
	Keep     *bool   `json:"keep,omitempty" jsonschema:"exempt from auto-clear"`
	Notes    *string `json:"notes,omitempty" jsonschema:"replaces the job's notes"`
	Title    *string `json:"title,omitempty"`
	Company  *string `json:"company,omitempty"`
	Salary   *string `json:"salary,omitempty"`
	Location *string `json:"location,omitempty"`
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

type crawlIn struct {
	Source string `json:"source,omitempty" jsonschema:"built-in source id, e.g. vietnamworks; all built-in crawlers when omitted"`
}

type crawlOut struct {
	Results []service.CrawlResult `json:"results"`
	Errors  []string              `json:"errors,omitempty"`
}

type schedulesOut struct {
	Sources []service.SourceSchedule `json:"sources"`
}

type scheduleIn struct {
	Source       string `json:"source" jsonschema:"source id, e.g. vietnamworks or facebook"`
	EveryMinutes int    `json:"every_minutes" jsonschema:"0 = off, else minutes between runs"`
}

type keywordsIn struct {
	Lists map[string][]string `json:"lists" jsonschema:"list key → full replacement list of terms"`
}

type cvIn struct {
	ID       int64  `json:"id" jsonschema:"the job id"`
	Markdown string `json:"markdown" jsonschema:"the tailored CV in Markdown (# name, ## sections, - bullets)"`
}

type draftBriefIn struct {
	ID    int64  `json:"id" jsonschema:"the job id"`
	Notes string `json:"notes,omitempty" jsonschema:"the user's steering notes, if any"`
}

type draftIn struct {
	ID      int64  `json:"id" jsonschema:"the job id"`
	To      string `json:"to,omitempty" jsonschema:"recipient; defaults to the job's contact email"`
	Subject string `json:"subject"`
	Body    string `json:"body" jsonschema:"plain text"`
}

type locationIn struct {
	Address string  `json:"address,omitempty" jsonschema:"a street address, district or city"`
	Lat     float64 `json:"lat,omitempty"`
	Lng     float64 `json:"lng,omitempty"`
	MaxKM   float64 `json:"max_km,omitempty" jsonschema:"maximum commute in km (1-500); omit to keep the current one"`
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
	list, err := t.svc.ListJobs(ctx, service.ListFilter{Status: in.Status, Source: in.Source, Query: in.Query})
	return jobsOut{list}, err
}

func (t tools) getJob(ctx context.Context, in idIn) (jobs.Job, error) {
	return t.svc.GetJob(ctx, in.ID)
}

func (t tools) getStats(ctx context.Context, _ struct{}) (service.Stats, error) {
	return t.svc.Stats(ctx)
}

func (t tools) getSetup(ctx context.Context, _ struct{}) (service.SetupStatus, error) {
	return t.svc.Setup(ctx)
}

type todayOut struct {
	Groups []service.TodayGroup `json:"groups"`
}

func (t tools) getToday(ctx context.Context, _ struct{}) (todayOut, error) {
	g, err := t.svc.Today(ctx)
	return todayOut{g}, err
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
		if _, _, ok := scrape.FacebookGroup(strings.TrimSpace(in.Content)); ok {
			return jobs.Job{}, fmt.Errorf("%w: that's a Facebook group's link — use follow_facebook_group to follow the group", service.ErrInvalid)
		}
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
		Title: in.Title, Company: in.Company, Salary: in.Salary, Location: in.Location,
	})
}

func (t tools) setStatus(ctx context.Context, in statusIn) (jobs.Job, error) {
	return t.svc.SetStatus(ctx, in.ID, in.Stage)
}

func (t tools) markFollowedUp(ctx context.Context, in idIn) (jobs.Job, error) {
	return t.svc.MarkFollowedUp(ctx, in.ID)
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

type uploadCVIn struct {
	Path    string `json:"path" jsonschema:"absolute path of the CV file the user named"`
	Replace bool   `json:"replace,omitempty" jsonschema:"overwrite text already in the profile's About me (only after the user agreed)"`
}

func (t tools) uploadCV(ctx context.Context, in uploadCVIn) (service.BaseCV, error) {
	if !t.svc.OnOwnComputer() { // never registered there; refuse all the same
		return service.BaseCV{}, service.ErrHosted
	}
	fi, err := os.Stat(in.Path)
	if err != nil {
		return service.BaseCV{}, fmt.Errorf("%w: %w", service.ErrInvalid, err)
	}
	if !fi.Mode().IsRegular() || fi.Size() > cvfile.MaxBytes {
		return service.BaseCV{}, fmt.Errorf("%w: %s isn't a CV file of at most %d MB", service.ErrInvalid, in.Path, cvfile.MaxBytes>>20)
	}
	data, err := os.ReadFile(in.Path)
	if err != nil {
		return service.BaseCV{}, err
	}
	return t.svc.UploadCV(ctx, in.Path, data, in.Replace)
}

type confirmIn struct {
	ID        int64 `json:"id" jsonschema:"the job id"`
	Submitted bool  `json:"submitted" jsonschema:"true if the user says they submitted the application"`
}

func (t tools) confirmApplied(ctx context.Context, in confirmIn) (jobs.Job, error) {
	return t.svc.ConfirmApplied(ctx, in.ID, in.Submitted)
}

type profileIn struct {
	Text string `json:"text" jsonschema:"the whole new profile, in Markdown"`
}

func (t tools) updateProfile(ctx context.Context, in profileIn) (service.Profile, error) {
	return t.svc.SaveProfile(ctx, in.Text)
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

func (t tools) crawl(ctx context.Context, in crawlIn) (crawlOut, error) {
	sources := t.svc.BuiltinCrawlers()
	if in.Source != "" {
		sources = []string{in.Source}
	}
	var out crawlOut
	for _, src := range sources {
		res, err := t.svc.Crawl(ctx, src)
		if err != nil {
			if in.Source != "" {
				return out, err
			}
			out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", src, err))
			continue
		}
		out.Results = append(out.Results, res)
	}
	return out, nil
}

func (t tools) retryCrawl(ctx context.Context, in crawlIn) (crawlOut, error) {
	if in.Source == "" {
		return crawlOut{}, fmt.Errorf("%w: source is required", service.ErrInvalid)
	}
	return t.crawl(ctx, in)
}

func (t tools) getCrawlSchedule(ctx context.Context, _ struct{}) (schedulesOut, error) {
	sc, err := t.svc.CrawlSchedules(ctx)
	return schedulesOut{sc}, err
}

func (t tools) setCrawlSchedule(ctx context.Context, in scheduleIn) (service.SourceSchedule, error) {
	return t.svc.SetCrawlSchedule(ctx, in.Source, in.EveryMinutes)
}

func (t tools) setKeywords(ctx context.Context, in keywordsIn) (keyword.Sets, error) {
	return t.svc.SetKeywords(ctx, keyword.Sets(in.Lists))
}

func (t tools) syncPlatform(ctx context.Context, in platformIn) (service.SyncResult, error) {
	if in.Platform != "topcv" {
		return service.SyncResult{}, fmt.Errorf("%w: status sync is available for topcv only", service.ErrInvalid)
	}
	return t.svc.SyncTopCV(ctx)
}

func (t tools) cvBrief(ctx context.Context, in idIn) (service.DocBrief, error) {
	return t.svc.CVBrief(ctx, in.ID)
}

func (t tools) submitCV(ctx context.Context, in cvIn) (jobs.Job, error) {
	return t.svc.SubmitCV(ctx, in.ID, in.Markdown)
}

func (t tools) draftBrief(ctx context.Context, in draftBriefIn) (service.DocBrief, error) {
	return t.svc.DraftBrief(ctx, in.ID, in.Notes)
}

func (t tools) submitDraft(ctx context.Context, in draftIn) (jobs.Job, error) {
	return t.svc.SubmitDraft(ctx, in.ID, service.Draft{To: in.To, Subject: in.Subject, Body: in.Body})
}

func (t tools) rejectDraft(ctx context.Context, in idIn) (jobs.Job, error) {
	return t.svc.RejectDraft(ctx, in.ID)
}

func (t tools) applyJob(_ context.Context, in idIn) (service.ApplyPrep, error) {
	return t.svc.StartAssistApply(in.ID)
}

func (t tools) getLocation(ctx context.Context, _ struct{}) (service.Location, error) {
	return t.svc.GetLocation(ctx)
}

func (t tools) setLocation(ctx context.Context, in locationIn) (service.Location, error) {
	p, label := geo.Point{Lat: in.Lat, Lng: in.Lng}, ""
	if in.Address != "" {
		places, err := t.svc.SearchPlaces(ctx, in.Address)
		if err != nil {
			return service.Location{}, err
		}
		if len(places) == 0 {
			return service.Location{}, fmt.Errorf("%w: no place matches %q", service.ErrInvalid, in.Address)
		}
		p, label = places[0].Point, places[0].Label
	}
	l, err := t.svc.SetLocation(ctx, p, label, in.MaxKM)
	if err == nil {
		go t.svc.UpdateDistances(context.Background(), service.MaxLookupsPerPass)
	}
	return l, err
}

func (t tools) rateWithJev(ctx context.Context, in idIn) (jobs.Job, error) {
	return t.svc.JevRate(ctx, in.ID)
}

type followGroupIn struct {
	Link string `json:"link" jsonschema:"the group's link, e.g. https://www.facebook.com/groups/123456/"`
}

type followGroupOut struct {
	Group service.FBGroup `json:"group"`
	Added bool            `json:"added" jsonschema:"false when the user already followed it"`
}

func (t tools) followGroup(ctx context.Context, in followGroupIn) (followGroupOut, error) {
	g, added, err := t.svc.FollowGroup(ctx, in.Link)
	return followGroupOut{g, added}, err
}

type groupsOut struct {
	Groups []service.FBGroup `json:"groups"`
}

func (t tools) listGroups(ctx context.Context, _ struct{}) (groupsOut, error) {
	gs, err := t.svc.FBGroups(ctx)
	return groupsOut{gs}, err
}

type groupKeyIn struct {
	Key string `json:"key" jsonschema:"the group's key, from list_facebook_groups"`
}

func (t tools) unfollowGroup(ctx context.Context, in groupKeyIn) (struct{}, error) {
	return struct{}{}, t.svc.UnfollowGroup(ctx, in.Key)
}
