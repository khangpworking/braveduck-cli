package scrape

import "testing"

func TestSupportedLink(t *testing.T) {
	for _, tc := range []struct {
		in, source, canonical string
	}{
		{"https://www.topcv.vn/viec-lam/senior-marketing-executive/2193898.html?ta_source=JobSearchList&u_sr_id=x#apply",
			"topcv", "https://www.topcv.vn/viec-lam/senior-marketing-executive/2193898.html"},
		{"http://VN.JobOKO.com/viec-lam-ky-su-ai-xvi6818257", "joboko", "https://vn.joboko.com/viec-lam-ky-su-ai-xvi6818257"},
		{"https://www.careerlink.vn/tim-viec-lam/nhan-vien-kinh-doanh/3647763", "careerlink", "https://www.careerlink.vn/tim-viec-lam/nhan-vien-kinh-doanh/3647763"},
		{"https://www.vietnamworks.com/truong-phong-2126493-jv?source=searchResults", "vietnamworks", "https://www.vietnamworks.com/truong-phong-2126493-jv"},
		// Facebook: every copy of one group post lands on fb-watcher's form.
		{"https://www.facebook.com/groups/1329241984892387/posts/1828228748327039/", "facebook", "https://www.facebook.com/groups/1329241984892387/posts/1828228748327039/"},
		{"https://m.facebook.com/groups/1329241984892387/posts/1828228748327039?__cft__[0]=AZ&__tn__=R", "facebook", "https://www.facebook.com/groups/1329241984892387/posts/1828228748327039/"},
		{"https://web.facebook.com/groups/1329241984892387/permalink/1828228748327039/", "facebook", "https://www.facebook.com/groups/1329241984892387/posts/1828228748327039/"},
		{"https://www.facebook.com/permalink.php?story_fbid=123&id=456&mibextid=x", "facebook", "https://www.facebook.com/permalink.php?id=456&story_fbid=123"},
		// Distinct posts keep distinct links: the post id survives, tracking goes.
		{"https://www.facebook.com/groups/1329/?multi_permalinks=456&ref=share", "facebook", "https://www.facebook.com/groups/1329/posts/456/"},
		{"https://m.facebook.com/photo.php?fbid=777&set=a.1&__cft__=x", "facebook", "https://www.facebook.com/photo.php?fbid=777"},
		{"https://www.facebook.com/watch/?v=888&ref=sharing", "facebook", "https://www.facebook.com/watch/?v=888"},
	} {
		src, got, ok := SupportedLink(tc.in)
		if !ok || src.ID != tc.source || got != tc.canonical {
			t.Errorf("SupportedLink(%q) = %q %q %v\nwant %q %q", tc.in, src.ID, got, ok, tc.source, tc.canonical)
		}
	}

	for _, in := range []string{
		"https://www.linkedin.com/jobs/view/1",
		"https://topcv.vn.evil.example/job", // the site's name, but not its domain
		"https://notfacebook.com/groups/1/posts/2/",
		"http://127.0.0.1:6379/",
		"ftp://topcv.vn/job",
		"topcv.vn/viec-lam/1",
	} {
		if _, _, ok := SupportedLink(in); ok {
			t.Errorf("SupportedLink(%q) accepted, want refused", in)
		}
	}
}
