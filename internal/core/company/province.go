package company

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// The first name is the province after the July 2025 mergers; the rest are
// former names or common spellings. Resolution 202/2025/QH15, Article 1:
// https://baochinhphu.vn/nghi-quyet-cua-quoc-hoi-ve-sap-xep-don-vi-hanh-chinh-cap-tinh-102250612191145158.htm
var provinces = [][]string{
	{"tuyen quang", "ha giang"},
	{"lao cai", "yen bai"},
	{"thai nguyen", "bac kan"},
	{"phu tho", "vinh phuc", "hoa binh"},
	{"bac ninh", "bac giang"},
	{"hung yen", "thai binh"},
	{"hai phong", "hai duong"},
	{"ninh binh", "ha nam", "nam dinh"},
	{"quang tri", "quang binh"},
	{"da nang", "quang nam", "danang"},
	{"quang ngai", "kon tum"},
	{"gia lai", "binh dinh"},
	{"khanh hoa", "ninh thuan"},
	{"lam dong", "dak nong", "binh thuan"},
	{"dak lak", "phu yen", "daklak"},
	{"ho chi minh", "binh duong", "ba ria vung tau", "hcm", "tphcm", "hcmc", "saigon", "sai gon"},
	{"dong nai", "binh phuoc"},
	{"tay ninh", "long an"},
	{"can tho", "soc trang", "hau giang"},
	{"vinh long", "ben tre", "tra vinh"},
	{"dong thap", "tien giang"},
	{"ca mau", "bac lieu"},
	{"an giang", "kien giang"},
	{"cao bang"}, {"dien bien"}, {"ha tinh"}, {"lai chau"},
	{"lang son"}, {"nghe an"}, {"quang ninh"}, {"thanh hoa"}, {"son la"},
	{"ha noi", "hanoi"}, {"hue", "thua thien hue"},
}

// Province returns a current province key, or "" when the address has none.
// Addresses run from street to province, so prefer the last named province
// (a street or ward can itself be named after another province).
func Province(address string) string {
	s := strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		if r == 'đ' {
			return 'd'
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return ' '
		}
		return r
	}, norm.NFD.String(strings.ToLower(address)))
	s = " " + strings.Join(strings.Fields(s), " ") + " "
	province, last := "", -1
	for _, names := range provinces {
		for _, name := range names {
			if i := strings.LastIndex(s, " "+name+" "); i > last {
				province, last = names[0], i
			}
		}
	}
	return province
}

// LookupKey scopes both caches to the name and the workplace's province.
func LookupKey(name, location string) string {
	return Key(name) + "\x1f" + Province(location)
}
