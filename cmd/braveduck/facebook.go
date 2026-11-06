package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/khangpworking/braveduck-cli/internal/service"
)

// cmdFacebook is `braveduck facebook …`: follow Facebook groups, and — for a
// scanner such as fb-watcher on this computer — report what a group is and
// hand over its posts (the shared server's scanner API does the same).
func cmdFacebook(ctx context.Context, svc *service.Service, args []string) error {
	if len(args) == 0 {
		return errors.New(`facebook: follow LINK | groups [--json] | unfollow KEY |
  status KEY --visibility public|private|unavailable [--name NAME] [--note TEXT] |
  ingest KEY [FILE]   (posts as JSON: {"posts":[{"url":…,"text":…}]}, FILE or stdin)`)
	}
	switch args[0] {
	case "follow":
		if len(args) != 2 {
			return errors.New("facebook follow LINK")
		}
		g, added, err := svc.FollowGroup(ctx, args[1])
		if err != nil {
			return err
		}
		if added {
			fmt.Printf("following %s (%s)\n", g.URL, g.Key)
		} else {
			fmt.Printf("already following %s\n", g.URL)
		}
		return nil
	case "groups":
		gs, err := svc.FBGroups(ctx)
		if err != nil {
			return err
		}
		if len(args) > 1 && args[1] == "--json" {
			return json.NewEncoder(os.Stdout).Encode(gs)
		}
		if len(gs) == 0 {
			fmt.Println("no Facebook groups followed — paste a group's link: braveduck facebook follow LINK")
		}
		for _, g := range gs {
			name := g.Name
			if name == "" {
				name = g.URL
			}
			fmt.Printf("%-24s %-11s %s\n", g.Key, g.Visibility, name)
		}
		return nil
	case "unfollow":
		if len(args) != 2 {
			return errors.New("facebook unfollow KEY")
		}
		return svc.UnfollowGroup(ctx, args[1])
	case "status":
		fs := flag.NewFlagSet("facebook status", flag.ContinueOnError)
		vis := fs.String("visibility", "", "public, private or unavailable")
		name := fs.String("name", "", "the group's name")
		note := fs.String("note", "", "why it can't be read, if it can't")
		if len(args) < 2 {
			return errors.New("facebook status KEY --visibility …")
		}
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		updated, err := svc.SetGroupStatus(ctx, strings.ToLower(args[1]), *name, *vis, *note)
		if err == nil && !updated {
			err = fmt.Errorf("not following %s", args[1])
		}
		return err
	case "ingest":
		if len(args) < 2 || len(args) > 3 {
			return errors.New("facebook ingest KEY [FILE]")
		}
		var r io.Reader = os.Stdin
		if len(args) == 3 && args[2] != "-" {
			f, err := os.Open(args[2])
			if err != nil {
				return err
			}
			defer f.Close()
			r = f
		}
		var in struct {
			Posts []service.GroupPost `json:"posts"`
		}
		if err := json.NewDecoder(io.LimitReader(r, 8<<20)).Decode(&in); err != nil {
			return fmt.Errorf("facebook ingest: posts JSON: %w", err)
		}
		n, err := svc.IngestGroupPosts(ctx, strings.ToLower(args[1]), in.Posts)
		if err != nil {
			return err
		}
		fmt.Printf("added %d job(s) from %d post(s)\n", n, len(in.Posts))
		return nil
	}
	return fmt.Errorf("facebook: unknown command %q", args[0])
}
