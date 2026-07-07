// Command braveduck is the local-first job-board CLI. This is the CLI face over
// the shared core (internal/core/*) and the SQLite store (internal/store); the
// MCP server and embedded web board are separate faces over the same core.
//
// Dispatch is a plain switch over stdlib flag rather than a framework: the
// command set is small, and staying dependency-free keeps release cross-builds
// and this RAM-limited dev box happy. Commands that need a live browser or LLM
// (rate/cv/draft/apply/crawl/sync/serve/mcp) are honest stubs in this build.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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

Rating
  rate-config [--ab] [--models a,b,c] [--default a]   show or set the A/B rater config
  rate <id>            (stub — needs a rater harness)

Keywords (crawler filter lists — start empty)
  keywords                       show every list and its terms
  keywords add <list> <term>...  add terms to a list
  keywords rm  <list> <term>...  remove terms from a list
  keywords check <id>            test a stored job against the filter

Web board
  serve      run the local web board + JSON API   (--addr, --no-open)

Not in this build (need a live browser/LLM)
  cv, draft, apply, crawl, sync, mcp

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
	st, err := openStore(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	ctx := context.Background()

	switch cmd {
	case "add":
		return cmdAdd(ctx, st, rest)
	case "list", "ls":
		return cmdList(ctx, st, rest)
	case "show", "get":
		return cmdShow(ctx, st, rest)
	case "status":
		return cmdStatus(ctx, st, rest)
	case "pin":
		return cmdToggle(ctx, st, rest, "pin")
	case "keep":
		return cmdToggle(ctx, st, rest, "keep")
	case "rm", "delete":
		return cmdDelete(ctx, st, rest)
	case "rate-config":
		return cmdRateConfig(ctx, st, rest)
	case "keywords", "kw":
		return cmdKeywords(ctx, st, rest)
	case "serve":
		return cmdServe(ctx, st, rest)
	case "rate", "cv", "draft", "apply", "crawl", "sync", "mcp":
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
func openStore(dbPath string) (*store.Store, error) {
	if dbPath == "" {
		p, err := defaultDBPath()
		if err != nil {
			return nil, err
		}
		dbPath = p
	}
	if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	return store.Open(dbPath)
}
