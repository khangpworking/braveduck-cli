package company

import "testing"

func TestProvince(t *testing.T) {
	for address, want := range map[string]string{
		"Etown Building, 364 Cong Hoa street, Ward 13, Tan Binh, Ho Chi Minh":       "ho chi minh",
		"79 đường Nguyễn Thế Truyện, Phường Tân Sơn Nhì, TP Hồ Chí Minh, Việt Nam.": "ho chi minh",
		"TP HCM": "ho chi minh", "TP.HCM": "ho chi minh", "TPHCM": "ho chi minh",
		"Thủ Dầu Một, Bình Dương": "ho chi minh", "Ba Ria–Vung Tau": "ho chi minh",
		"Hà Nội": "ha noi", "Ha Noi": "ha noi", "Hanoi": "ha noi",
		"Yên Bái": "lao cai", "Bắc Kạn": "thai nguyen", "Vĩnh Phúc": "phu tho",
		"Hà Nam": "ninh binh", "Quảng Nam": "da nang", "Đắk Nông": "lam dong",
		"Phú Yên": "dak lak", "Bến Tre": "vinh long", "Kiên Giang": "an giang",
		"Thừa Thiên Huế": "hue", "TP Huế": "hue", "Điện Biên": "dien bien",
		"12 Hà Nội, phường Bình Thuận, TP Hồ Chí Minh": "ho chi minh",
		"Remote": "", "": "", "Hanoian": "",
	} {
		if got := Province(address); got != want {
			t.Errorf("Province(%q) = %q, want %q", address, got, want)
		}
	}
}
