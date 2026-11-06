package service

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The profile form: the board's friendly editor for the profile, so nobody
// has to write Markdown. The board shows the fields that matter most for
// ratings; the others are kept and still editable as text. The profile stays the Markdown file every rater
// reads (profile_template.md); the form reads the lines it knows
// ("- Minimum I'll accept: …" under "# Compensation", the "# Deal-breakers"
// list, …) and writes only those back, in place. Anything else the user
// wrote — other lines, other sections, comments — is kept as it was.

// Money is a pay figure. Text holds what the profile said when it isn't in
// a form the fields can show (the board then shows the text as it is).
type Money struct {
	Amount   int64  `json:"amount"`             // 0 = not given
	Currency string `json:"currency,omitempty"` // VND, USD, …
	Period   string `json:"period,omitempty"`   // month / year / hour
	Basis    string `json:"basis,omitempty"`    // gross / net / ""
	Text     string `json:"text,omitempty"`
}

// ProfileForm is the profile as the board's form shows it.
type ProfileForm struct {
	AboutMe string `json:"about_me"` // free text, often the CV's

	Roles           []string `json:"roles"`
	Seniority       []string `json:"seniority"`
	IndustriesWant  []string `json:"industries_want"`
	IndustriesAvoid []string `json:"industries_avoid"`

	MinPay    Money    `json:"min_pay"`
	TargetPay Money    `json:"target_pay"`
	Benefits  []string `json:"benefits"`

	Location   string   `json:"location"`
	WorkModes  []string `json:"work_modes"`
	Relocate   string   `json:"relocate"`
	MaxCommute string   `json:"max_commute"`
	WorkAuth   string   `json:"work_auth"`

	JobTypes  []string `json:"job_types"`
	Schedule  string   `json:"schedule"`
	StartDate string   `json:"start_date"`

	CompanySize string   `json:"company_size"`
	Values      []string `json:"values"`

	DealBreakers []string `json:"deal_breakers"`
	Languages    []string `json:"languages"`
	Notes        string   `json:"notes"`
}

// ProfileFormOptions are the choices and suggestions the form offers.
type ProfileFormOptions struct {
	Seniority    []string `json:"seniority"`
	WorkModes    []string `json:"work_modes"`
	JobTypes     []string `json:"job_types"`
	Currencies   []string `json:"currencies"`
	Periods      []string `json:"periods"`
	DealBreakers []string `json:"deal_breakers"` // suggestions: what the board also flags on job cards
	Languages    []string `json:"languages"`
}

// FormOptions is what the board offers in the profile form.
var FormOptions = ProfileFormOptions{
	Seniority:  []string{"Intern", "Fresher", "Junior", "Middle", "Senior", "Lead / Manager"},
	WorkModes:  []string{"Remote", "Hybrid", "On-site"},
	JobTypes:   []string{"Full-time", "Part-time", "Contract", "Freelance", "Internship"},
	Currencies: []string{"VND", "USD"},
	Periods:    []string{"month", "year", "hour"},
	// Only what the board also spots in a job (core/jobs/dealbreakers.go);
	// "too far" is the Location window's maximum commute.
	DealBreakers: []string{"Working on Saturdays", "Pay not stated (\"thỏa thuận\")"},
	Languages:    []string{"Vietnamese", "English", "Japanese", "Korean", "Chinese"},
}

// formLine ties a form field to its "- Label:" line in a section.
type formLine struct {
	section, label string
	get            func(*ProfileForm) *string // a text field
	list           func(*ProfileForm) *[]string
	money          func(*ProfileForm) *Money
}

var formLines = []formLine{
	{section: "What I'm looking for", label: "Target roles / job titles", list: func(f *ProfileForm) *[]string { return &f.Roles }},
	{section: "What I'm looking for", label: "Seniority level", list: func(f *ProfileForm) *[]string { return &f.Seniority }},
	{section: "What I'm looking for", label: "Industries or domains I want", list: func(f *ProfileForm) *[]string { return &f.IndustriesWant }},
	{section: "What I'm looking for", label: "Industries or domains I'll avoid", list: func(f *ProfileForm) *[]string { return &f.IndustriesAvoid }},
	{section: "Compensation", label: "Minimum I'll accept", money: func(f *ProfileForm) *Money { return &f.MinPay }},
	{section: "Compensation", label: "Target", money: func(f *ProfileForm) *Money { return &f.TargetPay }},
	{section: "Compensation", label: "Benefits that matter to me", list: func(f *ProfileForm) *[]string { return &f.Benefits }},
	{section: "Location and work mode", label: "Where I live / can work from", get: func(f *ProfileForm) *string { return &f.Location }},
	{section: "Location and work mode", label: "Remote / hybrid / on-site preference", list: func(f *ProfileForm) *[]string { return &f.WorkModes }},
	{section: "Location and work mode", label: "Willing to relocate (where?)", get: func(f *ProfileForm) *string { return &f.Relocate }},
	{section: "Location and work mode", label: "Maximum commute", get: func(f *ProfileForm) *string { return &f.MaxCommute }},
	{section: "Location and work mode", label: "Work authorization / visa needs", get: func(f *ProfileForm) *string { return &f.WorkAuth }},
	{section: "Schedule and contract", label: "Full-time / part-time / contract / freelance", list: func(f *ProfileForm) *[]string { return &f.JobTypes }},
	{section: "Schedule and contract", label: "Working days and hours I accept", get: func(f *ProfileForm) *string { return &f.Schedule }},
	{section: "Schedule and contract", label: "Notice period / earliest start date", get: func(f *ProfileForm) *string { return &f.StartDate }},
	{section: "Company preferences", label: "Company size or stage I prefer", get: func(f *ProfileForm) *string { return &f.CompanySize }},
	{section: "Company preferences", label: "Things I value (growth, stability, mission, team, tech stack, ...)", list: func(f *ProfileForm) *[]string { return &f.Values }},
	{section: "Languages", label: "Languages I can work in (and level)", list: func(f *ProfileForm) *[]string { return &f.Languages }},
}

// Sections the form writes as a whole (their text, comments kept).
const (
	secAbout   = "About me"
	secDeals   = "Deal-breakers"
	secNotes   = "Notes for the rater"
	maxFormVal = 2000 // characters in one field
)

// ProfileFormOf reads the form's fields out of a profile's text.
func ProfileFormOf(text string) ProfileForm {
	doc := parseProfileDoc(text)
	var f ProfileForm
	for _, fl := range formLines {
		v := doc.lineValue(fl.section, fl.label)
		switch {
		case fl.get != nil:
			*fl.get(&f) = v
		case fl.list != nil:
			*fl.list(&f) = splitList(v)
		case fl.money != nil:
			*fl.money(&f) = parseMoney(v)
		}
	}
	f.AboutMe = doc.body(secAbout)
	f.Notes = doc.body(secNotes)
	f.DealBreakers = bulletItems(doc.body(secDeals))
	return f.normalized()
}

// normalized makes nil lists empty (JSON [] for the board).
func (f ProfileForm) normalized() ProfileForm {
	for _, l := range []*[]string{&f.Roles, &f.Seniority, &f.IndustriesWant, &f.IndustriesAvoid, &f.Benefits,
		&f.WorkModes, &f.JobTypes, &f.Values, &f.DealBreakers, &f.Languages} {
		if *l == nil {
			*l = []string{}
		}
	}
	return f
}

// WithProfileForm returns text with the form's fields written in, in place.
func WithProfileForm(text string, f ProfileForm) string {
	doc := parseProfileDoc(text)
	for _, fl := range formLines {
		var v string
		switch {
		case fl.get != nil:
			v = oneLine(*fl.get(&f))
		case fl.list != nil:
			v = joinList(*fl.list(&f))
		case fl.money != nil:
			if parseMoney(doc.lineValue(fl.section, fl.label)) == *fl.money(&f) {
				continue // the same figure: keep it as the user wrote it
			}
			v = (*fl.money(&f)).String()
		}
		doc.setLine(fl.section, fl.label, v)
	}
	doc.setBody(secAbout, strings.TrimSpace(f.AboutMe))
	doc.setBody(secNotes, strings.TrimSpace(f.Notes))
	var deals, lines []string
	for _, d := range f.DealBreakers {
		if d = oneLine(d); d != "" {
			deals = append(deals, d)
			lines = append(lines, "- "+d)
		}
	}
	if !slices.Equal(deals, bulletItems(doc.body(secDeals))) { // unchanged: keep it as written
		doc.setBody(secDeals, strings.Join(lines, "\n"))
	}
	return doc.String()
}

// ProfileFormNow is the profile's form, with the options the board offers.
func (s *Service) ProfileFormNow(ctx context.Context) (ProfileForm, ProfileFormOptions, error) {
	p, err := s.Profile(ctx)
	if err != nil {
		return ProfileForm{}, FormOptions, err
	}
	return ProfileFormOf(p.Text), FormOptions, nil
}

// SaveProfileForm writes the form into the profile, keeping everything else
// in it, and returns the form as saved.
func (s *Service) SaveProfileForm(ctx context.Context, f ProfileForm) (ProfileForm, error) {
	if err := f.check(); err != nil {
		return ProfileForm{}, err
	}
	profileMu.Lock() // read, change and write as one step
	defer profileMu.Unlock()
	p, err := s.Profile(ctx)
	if err != nil {
		return ProfileForm{}, err
	}
	saved, err := s.saveProfileLocked(ctx, WithProfileForm(p.Text, f))
	if err != nil {
		return ProfileForm{}, err
	}
	return ProfileFormOf(saved.Text), nil
}

func (f ProfileForm) check() error {
	long := func(s string) bool { return len([]rune(s)) > maxFormVal }
	for _, fl := range formLines {
		switch {
		case fl.get != nil && long(*fl.get(&f)):
			return fmt.Errorf("%w: %q is too long", ErrInvalid, fl.label)
		case fl.list != nil && (len(*fl.list(&f)) > 50 || long(strings.Join(*fl.list(&f), ", "))):
			return fmt.Errorf("%w: %q has too much in it", ErrInvalid, fl.label)
		case fl.money != nil && (fl.money(&f).Amount < 0 || long(fl.money(&f).Text)):
			return fmt.Errorf("%w: %q isn't a pay figure", ErrInvalid, fl.label)
		}
	}
	if len(f.DealBreakers) > 50 {
		return fmt.Errorf("%w: too many deal-breakers", ErrInvalid)
	}
	return nil
}

// --- the Markdown document ---------------------------------------------------

// profileDoc is a profile split into its "# " sections, order kept.
type profileDoc struct {
	pre      []string // lines before the first heading
	sections []*docSection
}

type docSection struct {
	heading string // as written, without "# "
	lines   []string
}

func parseProfileDoc(text string) *profileDoc {
	d := &profileDoc{}
	var cur *docSection
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if h1RE.MatchString(line) {
			cur = &docSection{heading: strings.TrimSpace(strings.TrimPrefix(line, "#"))}
			d.sections = append(d.sections, cur)
			continue
		}
		if cur == nil {
			d.pre = append(d.pre, line)
		} else {
			cur.lines = append(cur.lines, line)
		}
	}
	return d
}

func (d *profileDoc) String() string {
	var parts []string
	if len(d.pre) > 0 {
		parts = append(parts, strings.Join(d.pre, "\n"))
	}
	for _, s := range d.sections {
		parts = append(parts, "# "+s.heading+"\n"+strings.Join(s.lines, "\n"))
	}
	out := strings.Join(parts, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

func (d *profileDoc) section(heading string, create bool) *docSection {
	for _, s := range d.sections {
		if strings.EqualFold(s.heading, heading) {
			return s
		}
	}
	if !create {
		return nil
	}
	s := &docSection{heading: heading, lines: []string{""}}
	if n := len(d.sections); n > 0 {
		last := d.sections[n-1]
		if len(last.lines) == 0 || strings.TrimSpace(last.lines[len(last.lines)-1]) != "" {
			last.lines = append(last.lines, "")
		}
	}
	d.sections = append(d.sections, s)
	return s
}

// bulletRE matches "- Label: value"; the label is matched case-insensitively.
func bulletRE(label string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)^\s*[-*]\s*` + regexp.QuoteMeta(label) + `\s*:(.*)$`)
}

func (d *profileDoc) lineValue(section, label string) string {
	s := d.section(section, false)
	if s == nil {
		return ""
	}
	re := bulletRE(label)
	for _, line := range s.lines {
		if m := re.FindStringSubmatch(line); m != nil {
			return strings.TrimSpace(htmlCommentRE.ReplaceAllString(m[1], ""))
		}
	}
	return ""
}

func (d *profileDoc) setLine(section, label, value string) {
	re := bulletRE(label)
	if s := d.section(section, false); s != nil {
		for i, line := range s.lines {
			if re.MatchString(line) {
				if strings.TrimSpace(htmlCommentRE.ReplaceAllString(re.FindStringSubmatch(line)[1], "")) != value {
					s.lines[i] = strings.TrimRight("- "+label+": "+value, " ")
				}
				return
			}
		}
	}
	if value == "" {
		return // nothing to add
	}
	s := d.section(section, true)
	// After the section's last bullet, or its first line.
	at := 1
	for i, line := range s.lines {
		if strings.HasPrefix(strings.TrimSpace(line), "- ") || strings.HasPrefix(strings.TrimSpace(line), "* ") {
			at = i + 1
		}
	}
	if at > len(s.lines) {
		at = len(s.lines)
	}
	s.lines = append(s.lines[:at], append([]string{"- " + label + ": " + value}, s.lines[at:]...)...)
}

// body is a section's text without its HTML comments, trimmed.
func (d *profileDoc) body(section string) string {
	s := d.section(section, false)
	if s == nil {
		return ""
	}
	return strings.TrimSpace(htmlCommentRE.ReplaceAllString(strings.Join(s.lines, "\n"), ""))
}

// setBody replaces a section's text, keeping its HTML comments (the hints).
func (d *profileDoc) setBody(section, body string) {
	if d.body(section) == body {
		return // unchanged: keep the section exactly as written
	}
	s := d.section(section, body != "")
	if s == nil {
		return
	}
	comments := htmlCommentRE.FindAllString(strings.Join(s.lines, "\n"), -1)
	var lines []string
	lines = append(lines, "")
	for _, c := range comments {
		lines = append(lines, strings.Split(c, "\n")...)
		lines = append(lines, "")
	}
	if body != "" {
		lines = append(lines, strings.Split(body, "\n")...)
		lines = append(lines, "")
	}
	s.lines = append(lines, "")
}

// --- values ------------------------------------------------------------------

var listSepRE = regexp.MustCompile(`\s*[,;]\s*`)

func splitList(v string) []string {
	var out []string
	for _, p := range listSepRE.Split(v, -1) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func joinList(items []string) string {
	var out []string
	seen := map[string]bool{}
	for _, it := range items {
		it = strings.TrimSpace(listSepRE.ReplaceAllString(oneLine(it), " "))
		if it != "" && !seen[strings.ToLower(it)] {
			seen[strings.ToLower(it)] = true
			out = append(out, it)
		}
	}
	return strings.Join(out, ", ")
}

func bulletItems(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimSpace(strings.TrimLeft(line, "-*•"))
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// oneLine keeps a value on its line: a newline would start a new bullet.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// moneyRE reads "20,000,000 VND per month (gross)" and the like:
// "20.000.000đ/tháng", "25 triệu", "$1,500 a month net", "20tr gross".
var moneyRE = regexp.MustCompile(`(?i)^\s*(\$)?\s*([\d][\d.,\s]*)\s*(tr|triệu|trieu|k|m)?\s*(vnd|vnđ|đ|usd|\$)?\s*(?:(?:/|per|a|một|mỗi)\s*(month|tháng|year|năm|hour|giờ))?\s*\(?\s*(gross|net|gộp|thực nhận)?\s*\)?\s*$`)

func parseMoney(v string) Money {
	v = strings.TrimSpace(v)
	if v == "" {
		return Money{}
	}
	m := moneyRE.FindStringSubmatch(v)
	if m == nil {
		return Money{Text: v}
	}
	digits := strings.NewReplacer(",", "", ".", "", " ", "").Replace(m[2])
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || n <= 0 {
		return Money{Text: v}
	}
	switch strings.ToLower(m[3]) {
	case "tr", "triệu", "trieu", "m":
		n *= 1_000_000
	case "k":
		n *= 1_000
	}
	cur := strings.ToUpper(m[4])
	switch {
	case m[1] == "$" || cur == "$":
		cur = "USD"
	case cur == "VNĐ" || cur == "Đ" || (cur == "" && m[3] != "" && strings.ToLower(m[3]) != "k"):
		cur = "VND"
	}
	period := map[string]string{"month": "month", "tháng": "month", "year": "year", "năm": "year", "hour": "hour", "giờ": "hour"}[strings.ToLower(m[5])]
	basis := map[string]string{"gross": "gross", "gộp": "gross", "net": "net", "thực nhận": "net"}[strings.ToLower(m[6])]
	return Money{Amount: n, Currency: cur, Period: period, Basis: basis}
}

// String writes the figure back: "20,000,000 VND per month (gross)".
func (m Money) String() string {
	if m.Amount <= 0 {
		return oneLine(m.Text)
	}
	s := groupDigits(m.Amount)
	if m.Currency != "" {
		s += " " + m.Currency
	}
	if m.Period != "" {
		s += " per " + m.Period
	}
	if m.Basis != "" {
		s += " (" + m.Basis + ")"
	}
	return s
}

func groupDigits(n int64) string {
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
