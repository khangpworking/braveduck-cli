package service

import (
	"context"
	"strings"
	"testing"
)

// The form fills the template's lines in place and reads them back; the
// template's comments and the user's other text stay.
func TestProfileFormRoundTrip(t *testing.T) {
	if got := WithProfileForm(profileTemplate, ProfileFormOf(profileTemplate)); got != profileTemplate {
		t.Fatalf("saving the untouched template changed it:\n%s", got)
	}
	f := ProfileFormOf(profileTemplate)
	if len(f.Roles) != 0 || f.MinPay.Amount != 0 || len(f.DealBreakers) != 0 || f.AboutMe != "" {
		t.Fatalf("blank template read as %+v", f)
	}
	f.AboutMe = "Backend engineer, 5 years of Go."
	f.Roles = []string{"Backend Engineer", "Go Developer", "backend engineer", " "}
	f.Seniority = []string{"Senior"}
	f.MinPay = Money{Amount: 30_000_000, Currency: "VND", Period: "month", Basis: "gross"}
	f.WorkModes = []string{"Remote", "Hybrid"}
	f.Location = "Hà Nội\nCầu Giấy"
	f.DealBreakers = []string{"Working on Saturdays", "Night shifts", ""}
	f.Notes = "Career change: value transferable skills."
	text := WithProfileForm(profileTemplate, f)

	for _, want := range []string{
		"- Target roles / job titles: Backend Engineer, Go Developer\n",
		"- Minimum I'll accept: 30,000,000 VND per month (gross)\n",
		"- Remote / hybrid / on-site preference: Remote, Hybrid\n",
		"- Where I live / can work from: Hà Nội Cầu Giấy\n", // one line: no stray bullet
		"# Deal-breakers\n",
		"- Working on Saturdays\n- Night shifts\n",
		"Backend engineer, 5 years of Go.",
		"<!-- Anything that makes a job an automatic no, one per line. -->", // hints kept
		"- Target:\n", // untouched lines stay
		"- Industries or domains I'll avoid:\n\n# Compensation\n", // sections stay apart
	} {
		if !strings.Contains(text, want) {
			t.Errorf("profile missing %q:\n%s", want, text)
		}
	}
	back := ProfileFormOf(text)
	if strings.Join(back.Roles, "|") != "Backend Engineer|Go Developer" || back.MinPay != f.MinPay ||
		strings.Join(back.DealBreakers, "|") != "Working on Saturdays|Night shifts" ||
		back.AboutMe != f.AboutMe || back.Notes != f.Notes || back.Location != "Hà Nội Cầu Giấy" {
		t.Fatalf("read back %+v", back)
	}
	if !profileFilled(text) {
		t.Fatal("a filled form doesn't count as a filled profile")
	}
	// Saving the same form again changes nothing.
	if again := WithProfileForm(text, back); again != text {
		t.Fatalf("not stable:\n%s\n---\n%s", text, again)
	}
}

// A profile written by hand keeps what the form doesn't know about.
func TestProfileFormKeepsOtherText(t *testing.T) {
	hand := "# About me\n\nI build things.\n\n# Compensation\n\n- Minimum I'll accept: 25 triệu/tháng net\n- Stock options: yes please\n\n# My dog\n\nHe needs walks at 6pm.\n"
	f := ProfileFormOf(hand)
	if f.MinPay != (Money{Amount: 25_000_000, Currency: "VND", Period: "month", Basis: "net"}) {
		t.Fatalf("min pay = %+v", f.MinPay)
	}
	f.Roles = []string{"Data Analyst"}
	out := WithProfileForm(hand, f)
	for _, want := range []string{"- Stock options: yes please", "# My dog\n\nHe needs walks at 6pm.", "I build things.",
		"- Minimum I'll accept: 25,000,000 VND per month (net)", "# What I'm looking for\n", "- Target roles / job titles: Data Analyst"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestParseMoney(t *testing.T) {
	for in, want := range map[string]Money{
		"20,000,000 VND per month (gross)": {Amount: 20_000_000, Currency: "VND", Period: "month", Basis: "gross"},
		"20.000.000đ/tháng":                {Amount: 20_000_000, Currency: "VND", Period: "month"},
		"25 triệu":                         {Amount: 25_000_000, Currency: "VND"},
		"20tr gross":                       {Amount: 20_000_000, Currency: "VND", Basis: "gross"},
		"$1,500 a month net":               {Amount: 1500, Currency: "USD", Period: "month", Basis: "net"},
		"1500 USD per year":                {Amount: 1500, Currency: "USD", Period: "year"},
		"depends on the role":              {Text: "depends on the role"},
		"":                                 {},
	} {
		if got := parseMoney(in); got != want {
			t.Errorf("parseMoney(%q) = %+v, want %+v", in, got, want)
		}
	}
	if s := (Money{Text: "depends\non the role"}).String(); s != "depends on the role" {
		t.Errorf("free text = %q", s)
	}
}

func TestSaveProfileForm(t *testing.T) {
	s, _ := newRatingSvc(t)
	ctx := context.Background()
	f, opts, err := s.ProfileFormNow(ctx)
	if err != nil || len(opts.Seniority) == 0 || f.Roles == nil {
		t.Fatalf("form = %+v %+v %v", f, opts, err)
	}
	f.Roles = []string{"QA Engineer"}
	f.WorkModes = []string{"Remote"}
	saved, err := s.SaveProfileForm(ctx, f)
	if err != nil || strings.Join(saved.Roles, ",") != "QA Engineer" {
		t.Fatalf("saved = %+v %v", saved, err)
	}
	if st, _ := s.Setup(ctx); !st.Wants {
		t.Fatal("Get started still asks what you're looking for")
	}
	f.Location = strings.Repeat("x", maxFormVal+1)
	if _, err := s.SaveProfileForm(ctx, f); err == nil {
		t.Fatal("an over-long field was saved")
	}
}
