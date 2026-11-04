package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/khangpworking/braveduck-cli/internal/core/browser"
	"github.com/khangpworking/braveduck-cli/internal/core/markdown"
	"github.com/khangpworking/braveduck-cli/internal/service"
)

// idFirst splits `<id> [flags]` (the id may also come after the flags).
func idFirst(args []string) (string, []string) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:]
	}
	return "", args
}

// cmdCV: `cv <id>` prints a brief for any AI; `--file cv.md` saves the CV the
// AI wrote; `--pdf out.pdf` renders the current CV.
func cmdCV(ctx context.Context, svc *service.Service, args []string) error {
	idArg, rest := idFirst(args)
	fs := flag.NewFlagSet("cv", flag.ContinueOnError)
	file := fs.String("file", "", "save this Markdown file as the job's tailored CV")
	pdf := fs.String("pdf", "", "write the current CV as a PDF to this path")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if idArg == "" && fs.NArg() > 0 {
		idArg = fs.Arg(0)
	}
	if idArg == "" {
		return errors.New("usage: braveduck cv <id> [--file cv.md | --pdf out.pdf]")
	}
	id, err := parseID(idArg)
	if err != nil {
		return err
	}
	switch {
	case *file != "":
		b, err := os.ReadFile(*file)
		if err != nil {
			return err
		}
		j, err := svc.SubmitCV(ctx, id, string(b))
		if err != nil {
			return friendly(err, id)
		}
		fmt.Printf("job #%d: tailored CV v%d saved (%s)\n", j.ID, j.CVVersion, j.CVPath)
	case *pdf != "":
		md, j, err := svc.CV(ctx, id)
		if err != nil {
			return friendly(err, id)
		}
		out, err := browser.RenderPDF(ctx, markdown.Page(j.Title, markdown.ToHTML(md)))
		if err != nil {
			return err
		}
		if err := os.WriteFile(*pdf, out, 0o600); err != nil {
			return err
		}
		fmt.Printf("job #%d: CV v%d written to %s\n", j.ID, j.CVVersion, *pdf)
	default:
		b, err := svc.CVBrief(ctx, id)
		if err != nil {
			return friendly(err, id)
		}
		fmt.Print(service.RenderDocBrief(b))
	}
	return nil
}

// cmdDraft: `draft <id>` prints a brief; `--file email.txt` saves the email
// ("Subject: …" first line, blank line, body); `--sent` / `--reject`.
func cmdDraft(ctx context.Context, svc *service.Service, args []string) error {
	idArg, rest := idFirst(args)
	fs := flag.NewFlagSet("draft", flag.ContinueOnError)
	file := fs.String("file", "", `save this email ("Subject: ..." line, blank line, body) as the draft`)
	to := fs.String("to", "", "recipient (default: the job's contact email)")
	notes := fs.String("notes", "", "steering notes for the AI (tone, what to stress)")
	sent := fs.Bool("sent", false, "you sent the draft: mark it sent and move the job to Applied")
	reject := fs.Bool("reject", false, "discard the draft")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if idArg == "" && fs.NArg() > 0 {
		idArg = fs.Arg(0)
	}
	if idArg == "" {
		return errors.New("usage: braveduck draft <id> [--notes N | --file email.txt [--to addr] | --sent | --reject]")
	}
	id, err := parseID(idArg)
	if err != nil {
		return err
	}
	switch {
	case *sent:
		j, err := svc.MarkDraftSent(ctx, id)
		if err != nil {
			return friendly(err, id)
		}
		fmt.Printf("job #%d: marked sent → %s\n", j.ID, j.Status)
	case *reject:
		if _, err := svc.RejectDraft(ctx, id); err != nil {
			return friendly(err, id)
		}
		fmt.Printf("job #%d: draft discarded\n", id)
	case *file != "":
		b, err := os.ReadFile(*file)
		if err != nil {
			return err
		}
		d := parseEmail(string(b))
		d.To = *to
		j, err := svc.SubmitDraft(ctx, id, d)
		if err != nil {
			return friendly(err, id)
		}
		recipient := j.ContactEmail
		if d.To != "" {
			recipient = d.To
		}
		if recipient == "" {
			recipient = "no address yet — add one on the board"
		}
		fmt.Printf("job #%d: draft saved for review (to %s)\n", j.ID, recipient)
	default:
		b, err := svc.DraftBrief(ctx, id, *notes)
		if err != nil {
			return friendly(err, id)
		}
		fmt.Print(service.RenderDocBrief(b))
	}
	return nil
}

// parseEmail reads "Subject: …" from the first line; the rest is the body.
func parseEmail(s string) service.Draft {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	first, rest, _ := strings.Cut(s, "\n")
	if subj, ok := strings.CutPrefix(strings.TrimSpace(first), "Subject:"); ok {
		return service.Draft{Subject: strings.TrimSpace(subj), Body: strings.TrimSpace(rest)}
	}
	return service.Draft{Body: s}
}

// cmdApply runs an assisted apply in the foreground.
func cmdApply(ctx context.Context, svc *service.Service, args []string) error {
	id, err := oneID(args, "apply")
	if err != nil {
		return err
	}
	prep, err := svc.PrepareApply(ctx, id)
	if err != nil {
		return friendly(err, id)
	}
	fmt.Println("Opening the job on TopCV with your login. In that window:")
	fmt.Println("  1. click Ứng tuyển (Apply)")
	fmt.Println("  2. upload this CV:", prep.CVPDF)
	if prep.CoverLetter != "" {
		fmt.Println("  3. paste this cover letter:")
		fmt.Println()
		for _, l := range strings.Split(prep.CoverLetter, "\n") {
			fmt.Println("     " + l)
		}
		fmt.Println()
		fmt.Println("  4. submit, then close the window")
	} else {
		fmt.Println("  3. submit, then close the window")
	}
	res, err := svc.AssistApply(ctx, id)
	if err != nil {
		return friendly(err, id)
	}
	fmt.Println(res.Message)
	return nil
}
