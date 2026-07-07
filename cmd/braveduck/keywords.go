package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/khangpworking/braveduck-cli/internal/core/keyword"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// cmdKeywords manages the crawler keyword filter lists: `keywords` (show all),
// `keywords add/rm <list> <term>...`, and `keywords check <id>` to explain how
// the filter would treat a stored job.
func cmdKeywords(ctx context.Context, st *store.Store, args []string) error {
	if len(args) == 0 {
		return keywordsShow(ctx, st)
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list", "ls":
		return keywordsShow(ctx, st)
	case "add":
		return keywordsMutate(ctx, st, rest, true)
	case "rm", "remove", "del":
		return keywordsMutate(ctx, st, rest, false)
	case "check":
		return keywordsCheck(ctx, st, rest)
	default:
		return fmt.Errorf("keywords: unknown subcommand %q (list/add/rm/check)", sub)
	}
}

func keywordsShow(ctx context.Context, st *store.Store) error {
	for _, l := range keyword.Lists {
		terms, err := st.ListKeywords(ctx, l.Key)
		if err != nil {
			return err
		}
		fmt.Printf("%-18s %s\n", l.Key, l.Label)
		if len(terms) == 0 {
			printGuide(l.Key)
		} else {
			fmt.Printf("  %s\n", strings.Join(terms, ", "))
		}
	}
	return nil
}

// listGuide is the first-run help shown (dimmed) under an empty list: what
// adding a term does, the matching rules, and three worked examples so a brand
// new user knows what a good keyword looks like without reading docs.
type listGuide struct {
	what     string
	rule     string
	examples []string
}

var guides = map[string]listGuide{
	"search_queries": {
		what: "Phrases the crawler types into job-site search boxes. Not a filter.",
		rule: "Write them like you'd search a job board.",
		examples: []string{
			`marketing executive     — a phrase crawlers will search for`,
			`content creator hcm     — mix role + city`,
			`nhân viên kinh doanh    — Vietnamese phrases work too`,
		},
	},
	"positive_title": {
		what: "Keep a job only if its TITLE contains one of these words.",
		rule: "Case-insensitive; Vietnamese accents optional.",
		examples: []string{
			`marketing   — matches "Marketing Executive", "Digital Marketing"`,
			`content     — matches "Content Creator", "content lead"`,
			`nhân sự     — matches "Nhan Su" and "nhân sự" (accents optional)`,
		},
	},
	"negative_title": {
		what: "Drop a job if its TITLE contains any of these words.",
		rule: "Case-insensitive; Vietnamese accents optional.",
		examples: []string{
			`intern      — drops "Marketing Intern"`,
			`senior      — drops "Senior Developer"`,
			`thực tập    — drops "Thuc Tap Sinh" (accents optional)`,
		},
	},
	"negative_content": {
		what: "Drop a job if its title OR description mentions any of these.",
		rule: "Case-insensitive; Vietnamese accents optional.",
		examples: []string{
			`commission only  — drops jobs that say it anywhere in the post`,
			`đa cấp           — drops "da cap" / "đa cấp" (MLM)`,
			`call center      — drops call-center roles`,
		},
	},
	"negative_company": {
		what: "Drop a job if the COMPANY name contains any of these.",
		rule: "Case-insensitive; Vietnamese accents optional.",
		examples: []string{
			`nha khoa    — drops dental clinics`,
			`bảo hiểm    — drops "Bao Hiem" / "bảo hiểm" (insurance)`,
			`spa         — drops spa businesses`,
		},
	},
}

// printGuide renders the dimmed first-run instructions for an empty list.
func printGuide(key string) {
	g, ok := guides[key]
	if !ok {
		fmt.Println(dim("  (empty)"))
		return
	}
	fmt.Println(dim("  # " + g.what))
	fmt.Println(dim("  # " + g.rule))
	fmt.Println(dim("  # examples:"))
	for _, ex := range g.examples {
		fmt.Println(dim("  #   " + ex))
	}
}

// dim wraps text in ANSI grey when stdout is an interactive terminal (and
// NO_COLOR is unset), so piped/redirected output stays clean.
func dim(s string) string {
	if !colorEnabled {
		return s
	}
	return "\033[90m" + s + "\033[0m"
}

var colorEnabled = func() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("CLICOLOR_FORCE") != "" {
		return true
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}()

func keywordsMutate(ctx context.Context, st *store.Store, args []string, add bool) error {
	verb := "rm"
	if add {
		verb = "add"
	}
	if len(args) < 2 {
		return fmt.Errorf("usage: braveduck keywords %s <list> <term>...", verb)
	}
	list, terms := args[0], args[1:]
	var n int
	var err error
	if add {
		n, err = st.AddKeywords(ctx, list, terms)
	} else {
		n, err = st.RemoveKeywords(ctx, list, terms)
	}
	if err != nil {
		return err
	}
	fmt.Printf("%s: %d term(s) %sed in %s\n", list, n, verb, list)
	return nil
}

func keywordsCheck(ctx context.Context, st *store.Store, args []string) error {
	id, err := oneID(args, "keywords check")
	if err != nil {
		return err
	}
	j, err := getJob(ctx, st, id)
	if err != nil {
		return err
	}
	sets, err := st.AllKeywords(ctx)
	if err != nil {
		return err
	}
	d := keyword.Filter(keyword.Job{Title: j.Title, Description: j.FullJD, Company: j.Company}, sets)
	if d.Keep {
		fmt.Printf("job #%d KEEP — passes all keyword filters\n", id)
		return nil
	}
	fmt.Printf("job #%d REJECT — %s", id, d.Reason)
	if d.List != "" {
		fmt.Printf(" (list: %s)", d.List)
	}
	fmt.Println()
	return nil
}
