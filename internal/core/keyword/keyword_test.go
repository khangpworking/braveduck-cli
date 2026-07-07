package keyword

import "testing"

func TestFold(t *testing.T) {
	// Fold strips diacritics but preserves case; callers lowercase separately.
	// đ/Đ both map to lowercase 'd' (they don't decompose under NFD).
	cases := map[string]string{
		"Thẩm Mỹ":    "Tham My",
		"Đà Nẵng":    "da Nang",
		"café":       "cafe",
		"no-diacrit": "no-diacrit",
	}
	for in, want := range cases {
		if got := Fold(in); got != want {
			t.Errorf("Fold(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeTerm(t *testing.T) {
	if got := NormalizeTerm("  Thẩm Mỹ ", true); got != "tham my" {
		t.Errorf("folded normalize = %q, want %q", got, "tham my")
	}
	if got := NormalizeTerm("  Content Lead ", false); got != "content lead" {
		t.Errorf("unfolded normalize = %q, want %q", got, "content lead")
	}
	if got := NormalizeTerm("   ", false); got != "" {
		t.Errorf("blank should normalize to empty, got %q", got)
	}
}

func TestFilterPositiveGate(t *testing.T) {
	sets := Sets{"positive_title": {"content", "marketing"}}

	if d := Filter(Job{Title: "Content Strategist"}, sets); !d.Keep {
		t.Errorf("matching title should pass: %+v", d)
	}
	d := Filter(Job{Title: "Backend Engineer"}, sets)
	if d.Keep || d.List != "positive_title" {
		t.Errorf("non-matching title should be rejected by positive gate: %+v", d)
	}
}

func TestFilterPositiveGateFolded(t *testing.T) {
	// positive_title is diacritic-folded: an accented required term matches an
	// unaccented title and vice versa.
	sets := Sets{"positive_title": {"nhân sự"}}
	if d := Filter(Job{Title: "Nhan Su Executive"}, sets); !d.Keep {
		t.Errorf("accents-optional positive match should pass: %+v", d)
	}
	if d := Filter(Job{Title: "Backend Engineer"}, sets); d.Keep {
		t.Errorf("unrelated title should still be gated out: %+v", d)
	}
}

func TestFilterEmptyPositiveMeansNoGate(t *testing.T) {
	// No positive_title list → everything passes stage 1.
	if d := Filter(Job{Title: "Anything At All"}, Sets{}); !d.Keep {
		t.Errorf("empty sets should keep everything: %+v", d)
	}
}

func TestFilterNegativeTitleFolded(t *testing.T) {
	// Term stored WITH accents; job title WITHOUT — folded match must still hit.
	sets := Sets{"negative_title": {"thẩm mỹ", "intern"}}
	d := Filter(Job{Title: "Nhan vien Tham My Vien"}, sets)
	if d.Keep || d.List != "negative_title" || d.Term != "tham my" {
		t.Errorf("folded negative title should reject: %+v", d)
	}
	if d := Filter(Job{Title: "Marketing Executive"}, sets); !d.Keep {
		t.Errorf("clean title should pass: %+v", d)
	}
}

func TestFilterNegativeContentVsTitleScope(t *testing.T) {
	// negative_content matches title OR description; negative_title only title.
	titleOnly := Sets{"negative_title": {"cosmetic"}}
	if d := Filter(Job{Title: "Marketing Lead", Description: "for a cosmetic brand"}, titleOnly); !d.Keep {
		t.Errorf("negative_title must NOT look at description: %+v", d)
	}
	contentList := Sets{"negative_content": {"cosmetic"}}
	if d := Filter(Job{Title: "Marketing Lead", Description: "for a cosmetic brand"}, contentList); d.Keep {
		t.Errorf("negative_content must scan description: %+v", d)
	}
}

func TestFilterNegativeCompany(t *testing.T) {
	sets := Sets{"negative_company": {"nha khoa"}}
	d := Filter(Job{Title: "Marketing", Company: "Nha Khoa ABC"}, sets)
	if d.Keep || d.List != "negative_company" {
		t.Errorf("company match should reject: %+v", d)
	}
}

func TestFilterPrecedence(t *testing.T) {
	// Positive gate runs before negatives: a title failing the positive gate is
	// rejected there even if a negative term would also match.
	sets := Sets{
		"positive_title": {"marketing"},
		"negative_title": {"engineer"},
	}
	d := Filter(Job{Title: "Backend Engineer"}, sets)
	if d.Keep || d.List != "positive_title" {
		t.Errorf("positive gate should win precedence: %+v", d)
	}
}
