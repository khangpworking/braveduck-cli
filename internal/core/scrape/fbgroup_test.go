package scrape

import "testing"

func TestFacebookGroup(t *testing.T) {
	for in, want := range map[string]string{
		"https://www.facebook.com/groups/3989249071208661":            "3989249071208661",
		"https://m.facebook.com/groups/3989249071208661/?ref=share":   "3989249071208661",
		"https://facebook.com/groups/TuyenDungAI/":                    "tuyendungai",
		"https://web.facebook.com/groups/tuyendung.ai/#about":         "tuyendung.ai",
		"https://www.facebook.com/groups/3989249071208661/posts/123/": "",
		"https://www.facebook.com/groups/398/permalink/123/":          "",
		"https://www.facebook.com/groups/398/?multi_permalinks=55":    "",
		"https://www.facebook.com/somepage":                           "",
		"https://www.topcv.vn/viec-lam/x/1.html":                      "",
		"not a link":                                                  "",
		"https://www.facebook.com/groups/a%2Fb/":                      "",
	} {
		key, link, ok := FacebookGroup(in)
		if want == "" {
			if ok {
				t.Errorf("%s: a group (%s)?", in, key)
			}
			continue
		}
		if !ok || key != want || link != "https://www.facebook.com/groups/"+want+"/" {
			t.Errorf("%s = %q %q %v, want %q", in, key, link, ok, want)
		}
	}
}
