package browser

import "testing"

func TestMaskAccount(t *testing.T) {
	for in, want := range map[string]string{
		"nguyen.van.a@gmail.com": "ng…@gmail.com",
		"a@x.vn":                 "a…@x.vn",
		"Nguyễn Văn A":           "Ng…",
		"  ":                     "",
	} {
		if got := MaskAccount(in); got != want {
			t.Errorf("MaskAccount(%q) = %q, want %q", in, got, want)
		}
	}
}
