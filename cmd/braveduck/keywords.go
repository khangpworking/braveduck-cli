package main

import (
	"context"
	"fmt"
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
			fmt.Printf("  (empty)\n")
		} else {
			fmt.Printf("  %s\n", strings.Join(terms, ", "))
		}
	}
	return nil
}

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
