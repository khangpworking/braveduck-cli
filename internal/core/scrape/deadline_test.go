package scrape

import "testing"

func TestExtractDeadline(t *testing.T) {
	cases := []struct {
		name string
		html string
		want string
	}{
		{
			name: "joboko/careerlink plain JSON-LD validThrough (date only)",
			html: `<script type="application/ld+json">{"@type":"JobPosting","title":"X","validThrough":"2026-08-15"}</script>`,
			want: "2026-08-15",
		},
		{
			name: "validThrough with offset datetime",
			html: `{"validThrough": "2026-08-15T23:59:59+07:00"}`,
			want: "2026-08-15T23:59:59+07:00",
		},
		{
			name: "VNW escaped RSC expiredOn",
			html: `self.__next_f.push([1,"...\"expiredOn\":\"2026-09-01\",\"title\":..."])`,
			want: "2026-09-01",
		},
		{
			name: "validThrough preferred over expiredOn when both present",
			html: `{"expiredOn":"2026-01-01","validThrough":"2026-12-31"}`,
			want: "2026-12-31",
		},
		{
			name: "no deadline field → empty",
			html: `<html><body>no structured data here</body></html>`,
			want: "",
		},
		{
			name: "unparseable date → empty",
			html: `{"validThrough":"sometime next month"}`,
			want: "",
		},
		{
			name: "whitespace trimmed",
			html: `{"validThrough": "  2026-08-15  "}`,
			want: "2026-08-15",
		},
	}
	for _, c := range cases {
		if got := ExtractDeadline(c.html); got != c.want {
			t.Errorf("%s: ExtractDeadline = %q, want %q", c.name, got, c.want)
		}
	}
}
