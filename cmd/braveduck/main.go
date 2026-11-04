// Command braveduck is the local-first job-board CLI. This is the CLI face over
// the shared core (internal/core/*) and the SQLite store (internal/store); the
// MCP server and embedded web board are separate faces over the same core.
//
// Dispatch is a plain switch over stdlib flag rather than a framework: the
// command set is small, and staying dependency-free keeps release cross-builds
// and this RAM-limited dev box happy. Commands that need a live browser or LLM
// (rate/cv/draft/apply/crawl/sync) are honest stubs in this build.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	mcpserver "github.com/khangpworking/braveduck-cli/internal/mcp"
	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

const usage = `braveduck — local-first job board

Usage: braveduck [--db PATH] <command> [args]

Jobs
  add        add a job            (--title, --company, --url, --salary, --source, --jd)
  list       list jobs            (--status, --source)
  show <id>  job detail
  status <id> <stage>   move a job to a lane (saved/waiting/applied/interview/offer/closed)
  pin <id>   toggle pin
  keep <id>  toggle keep (exempt from auto-clear)
  rm <id>    delete a job

Rating (bring your own AI: braveduck stores no API keys)
  profile              show where your profile lives and whether it's filled in
  rate [--limit N]     print a rating brief for unrated jobs, to paste into any AI
  rate <id>            print a rating brief for one job
  rate <id> --score 4.2 --why "..." [--letter B] [--model m]   save a verdict
  rate-config [--ab] [--models a,b,c] [--default a]   show or set the A/B rater config

Keywords (crawler filter lists — start empty)
  keywords                       show every list and its terms
  keywords add <list> <term>...  add terms to a list
  keywords rm  <list> <term>...  remove terms from a list
  keywords check <id>            test a stored job against the filter

Job-site logins (you log in yourself; braveduck keeps the session, encrypted)
  platforms            list sites and whether you're logged in
  login <site>         open a browser window to log in to a site
  logout <site>|--all  delete a saved login
  platforms check <site>   re-check whether a saved login still works

Crawlers (search queries come from: braveduck keywords add search_queries ...)
  crawl [<source>]                      fetch new jobs now (built-in: vietnamworks)
  crawl-schedule                        how often each source fetches new jobs
  crawl-schedule <source> <2h|30m|off>  change it (built-ins run while the board is up)
  crawl-report <source> --ok            external tools: record a successful run
  crawl-report <source> --fail "why"    external tools: record a failed run

Web board
  serve      run the local web board + JSON API   (--addr, --no-open)

AI agents
  mcp        MCP server on stdio, for Claude Code / Codex / agy
             (claude mcp add braveduck -- braveduck mcp)

Job-site status
  sync topcv           read your TopCV application history; update the board
                       (also runs as the "topcv-sync" source in crawl-schedule)

Not in this build (need a live browser/LLM)
  cv, draft, apply

Other
  doctor     environment + database health check

Global
  --db PATH  database file (default: $BRAVEDUCK_DB or ~/.braveduck/braveduck.db)
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "braveduck: "+err.Error())
		os.Exit(1)
	}
}

// run parses the optional global --db flag, then dispatches the subcommand.
func run(args []string) error {
	dbPath, args := extractDBFlag(args)
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}
	cmd, rest := args[0], args[1:]

	switch cmd {
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	case "doctor":
		return cmdDoctor(dbPath)
	}

	// Everything below needs the store.
	st, resolved, err := openStore(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	ctx := context.Background()
	svc := service.New(st)
	svc.SetDataDir(filepath.Dir(resolved))

	switch cmd {
	case "add":
		return cmdAdd(ctx, svc, rest)
	case "list", "ls":
		return cmdList(ctx, svc, rest)
	case "show", "get":
		return cmdShow(ctx, svc, rest)
	case "status":
		return cmdStatus(ctx, svc, rest)
	case "pin":
		return cmdToggle(ctx, svc, rest, "pin")
	case "keep":
		return cmdToggle(ctx, svc, rest, "keep")
	case "rm", "delete":
		return cmdDelete(ctx, svc, rest)
	case "platforms":
		return cmdPlatforms(ctx, svc, rest)
	case "login":
		return cmdLogin(ctx, svc, rest)
	case "logout":
		return cmdLogout(ctx, svc, rest)
	case "crawl":
		return cmdCrawl(ctx, svc, rest)
	case "crawl-schedule":
		return cmdCrawlSchedule(ctx, svc, rest)
	case "crawl-report":
		return cmdCrawlReport(ctx, svc, rest)
	case "profile":
		return cmdProfile(ctx, svc)
	case "rate":
		return cmdRate(ctx, svc, rest)
	case "rate-config":
		return cmdRateConfig(ctx, svc, rest)
	case "keywords", "kw":
		return cmdKeywords(ctx, st, rest)
	case "serve":
		return cmdServe(ctx, st, svc, rest)
	case "mcp":
		// Runs until the client closes stdin; Ctrl-C/SIGTERM also end it.
		ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
		return mcpserver.Serve(ctx, svc)
	case "sync":
		return cmdSync(ctx, svc, rest)
	case "cv", "draft", "apply":
		return fmt.Errorf("%q needs a live browser/LLM and isn't wired in this build yet", cmd)
	default:
		return fmt.Errorf("unknown command %q (try `braveduck help`)", cmd)
	}
}

// extractDBFlag pulls a leading/trailing `--db PATH` or `--db=PATH` out of args
// so it can appear before the subcommand without a global flag.FlagSet.
func extractDBFlag(args []string) (string, []string) {
	path := os.Getenv("BRAVEDUCK_DB")
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--db" && i+1 < len(args):
			path = args[i+1]
			i++
		case len(a) > 5 && a[:5] == "--db=":
			path = a[5:]
		default:
			out = append(out, a)
		}
	}
	return path, out
}

// defaultDBPath is ~/.braveduck/braveduck.db, the local-first home for the DB.
func defaultDBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".braveduck", "braveduck.db"), nil
}

// openStore resolves the db path (flag/env/default), ensures its directory
// exists, and opens the store (creating + migrating the schema on first run).
// It also returns the resolved path.
func openStore(dbPath string) (*store.Store, string, error) {
	if dbPath == "" {
		p, err := defaultDBPath()
		if err != nil {
			return nil, "", err
		}
		dbPath = p
	}
	// The data dir holds the profile (a CV), the session key and the sealed
	// sessions: owner-only, and tightened if an older braveduck made it 0755.
	if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, "", err
		}
		if dbPath == defaultPath() {
			_ = os.Chmod(dir, 0o700)
		}
	}
	st, err := store.Open(dbPath)
	if err == nil {
		for _, f := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
			_ = os.Chmod(f, 0o600) // best effort; missing -wal/-shm is fine
		}
	}
	return st, dbPath, err
}

// defaultPath is defaultDBPath without the error, for comparisons.
func defaultPath() string {
	p, _ := defaultDBPath()
	return p
}
