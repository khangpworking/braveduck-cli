package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/khangpworking/braveduck-cli/internal/core/browser"
	"github.com/khangpworking/braveduck-cli/internal/core/platform"
	"github.com/khangpworking/braveduck-cli/internal/service"
)

func cmdPlatforms(ctx context.Context, svc *service.Service, args []string) error {
	if len(args) == 2 && args[0] == "check" {
		st, err := svc.CheckPlatform(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Printf("%s: %s\n", st.Name, statusLine(st))
		return nil
	}
	if len(args) != 0 {
		return errors.New("usage: braveduck platforms [check <site>]")
	}
	all, err := svc.Platforms(ctx)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "SITE\tSTATUS\tSINCE\tLOGIN CHECK")
	for _, p := range all {
		since := ""
		if len(p.Since) >= 10 {
			since = p.Since[:10]
		}
		check := "unverified (confirmed on your first login)"
		if p.Verified {
			check = "verified"
		}
		if !p.Builtin {
			check = "custom (platforms.json)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.ID, strings.ReplaceAll(p.Status, "_", " "), since, check)
	}
	return w.Flush()
}

func statusLine(st service.PlatformStatus) string {
	switch st.Status {
	case "connected":
		return "logged in ✓"
	case "expired":
		return "logged out — run: braveduck login " + st.ID
	case "unknown":
		return "saved, but couldn't confirm the login (the site's check isn't verified yet)"
	}
	return strings.ReplaceAll(st.Status, "_", " ")
}

func cmdLogin(ctx context.Context, svc *service.Service, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: braveduck login <site>   (see: braveduck platforms)")
	}
	st, err := svc.Connect(ctx, args[0], service.LoginHooks{
		OnStart: func(p platform.Platform) {
			if !browser.Provisioned() {
				fmt.Println("First login: downloading braveduck's built-in browser (a few hundred MB).")
				fmt.Println("This happens once and can take several minutes on a slow connection…")
			}
			fmt.Printf("Opening a browser window — log in to %s there yourself (password, OTP,\n", p.Name)
			fmt.Println("captcha: whatever the site asks). braveduck never sees your password.")
			fmt.Println("The window closes on its own once you're logged in; or close it when done.")
		},
		OnDetected: func() { fmt.Println("✓ login detected — saving the session…") },
	})
	if err != nil {
		return err
	}
	fmt.Printf("%s: %s (session saved, encrypted)\n", st.Name, statusLine(st))
	return nil
}

func cmdLogout(ctx context.Context, svc *service.Service, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: braveduck logout <site> | --all")
	}
	ids := []string{args[0]}
	if args[0] == "--all" {
		all, err := svc.Platforms(ctx)
		if err != nil {
			return err
		}
		ids = nil
		for _, p := range all {
			ids = append(ids, p.ID)
		}
	}
	n := 0
	for _, id := range ids {
		deleted, err := svc.Disconnect(ctx, id)
		if err != nil {
			return err
		}
		if deleted {
			fmt.Printf("deleted the saved %s login\n", id)
			n++
		}
	}
	if n == 0 {
		fmt.Println("no saved login to delete")
	}
	return nil
}
