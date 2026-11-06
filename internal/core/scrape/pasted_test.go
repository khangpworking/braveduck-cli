package scrape

import "testing"

func TestParsePastedJD(t *testing.T) {
	for _, tc := range []struct {
		name, in                      string
		title, company, salary, email string
	}{
		{
			name:    "one line, Vietnamese",
			in:      "Chuyên viên Marketing - Công ty TNHH Vịt Vàng. Mức lương: 15-20 triệu. Làm việc từ thứ 2 đến thứ 6. Gửi CV về tuyendung@vitvang.vn",
			title:   "Chuyên viên Marketing",
			company: "Công ty TNHH Vịt Vàng",
			salary:  "15-20 triệu",
			email:   "tuyendung@vitvang.vn",
		},
		{
			name:    "multi-line, classic paste",
			in:      "Data Analyst\nMoMo\nSalary: $1,500 - $2,000\nRequirements: SQL, Python",
			title:   "Data Analyst",
			company: "MoMo",
			salary:  "$1,500 - $2,000",
		},
		{
			name:    "company named on a later line",
			in:      "Senior Backend Engineer\nWe build payments.\nCompany: Duck Pay JSC\nLương: thỏa thuận",
			title:   "Senior Backend Engineer",
			company: "Duck Pay JSC",
			salary:  "thỏa thuận",
		},
		{
			name:  "no salary, no company",
			in:    "Intern wanted. Apply now.",
			title: "Intern wanted",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jp := ParsePastedJD(tc.in)
			if jp.Title != tc.title || jp.Company != tc.company || jp.Salary != tc.salary || jp.Email != tc.email {
				t.Errorf("got title=%q company=%q salary=%q email=%q\nwant title=%q company=%q salary=%q email=%q",
					jp.Title, jp.Company, jp.Salary, jp.Email, tc.title, tc.company, tc.salary, tc.email)
			}
			if jp.Description == "" {
				t.Error("description dropped")
			}
		})
	}
}

func TestParsePostDoesNotGuessCompany(t *testing.T) {
	post := "🚀 HIRING SENIOR AI ENGINEER\n📍 Location: Ho Chi Minh City\n💰 Salary: Negotiable\nSend your CV to hr@hconnect.com.vn"
	jp := ParsePost(post)
	if jp.Title != "🚀 HIRING SENIOR AI ENGINEER" || jp.Company != "" || jp.Email != "hr@hconnect.com.vn" {
		t.Fatalf("got title=%q company=%q email=%q", jp.Title, jp.Company, jp.Email)
	}
	if jp := ParsePost(post + "\nCông ty TNHH HConnect"); jp.Company != "Công ty TNHH HConnect" {
		t.Fatalf("named company = %q, want it read", jp.Company)
	}
}

// A salary, location or requirements line is common on the second line of a
// pasted JD. None of them may be mistaken for the employer.
func TestParsePastedJDNeverTakesDetailLineAsCompany(t *testing.T) {
	for _, tc := range []struct {
		name    string
		in      string
		company string
		salary  string
	}{
		{
			name: "Vietnamese salary then location",
			in: "Tuyển dụng Backend Engineer\n" +
				"Lương 30-40 triệu gross\n" +
				"Hybrid, Hà Nội\n" +
				"Yêu cầu: 3 năm kinh nghiệm Go, PostgreSQL",
			company: "",
			salary:  "30-40 triệu gross",
		},
		{
			name: "English salary then location",
			in: "Senior Data Analyst\n" +
				"Salary: Negotiable\n" +
				"Location: Ho Chi Minh City\n" +
				"Requirements: SQL, Python, 3 years experience",
			company: "",
			salary:  "Negotiable",
		},
		{
			name: "Vietnamese labelled salary",
			in: "Kế toán tổng hợp\n" +
				"Mức lương: Thỏa thuận\n" +
				"Địa điểm làm việc: Quận 1, TP. Hồ Chí Minh",
			company: "",
			salary:  "Thỏa thuận",
		},
		{
			name: "location only on the second line",
			in: "Nhân viên kinh doanh\n" +
				"Hà Nội\n" +
				"Mô tả công việc: gọi điện cho khách hàng",
			company: "",
		},
		{
			name: "requirements on the second line",
			in: "Frontend Developer\n" +
				"Yêu cầu: React, TypeScript\n" +
				"Quyền lợi: bảo hiểm đầy đủ",
			company: "",
		},
		{
			name: "a real second-line company still counts",
			in: "Data Analyst\n" +
				"MoMo\n" +
				"Salary: $1,500 - $2,000\n" +
				"Requirements: SQL, Python",
			company: "MoMo",
			salary:  "$1,500 - $2,000",
		},
		{
			name: "a named company line still counts",
			in: "Kế toán tổng hợp\n" +
				"Công ty TNHH Vịt Vàng\n" +
				"Mức lương: 15-20 triệu",
			company: "Công ty TNHH Vịt Vàng",
			salary:  "15-20 triệu",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jp := ParsePastedJD(tc.in)
			if jp.Company != tc.company {
				t.Errorf("company = %q, want %q", jp.Company, tc.company)
			}
			if tc.salary != "" && jp.Salary != tc.salary {
				t.Errorf("salary = %q, want %q", jp.Salary, tc.salary)
			}
		})
	}
}

// A second line that names an employer stays the company, even when the name
// mentions a place or a common word; only clear pay, place or requirement
// lines are dropped.
func TestPastedSecondLineCompany(t *testing.T) {
	for _, tc := range []struct{ line, company string }{
		{"Shopee Vietnam", "Shopee Vietnam"},
		{"Samsung Vietnam", "Samsung Vietnam"},
		{"Bosch Global Software Technologies Vietnam", "Bosch Global Software Technologies Vietnam"},
		{"VNG - Hồ Chí Minh", "VNG - Hồ Chí Minh"},
		{"Experian Vietnam", "Experian Vietnam"},
		{"Qualcomm", "Qualcomm"},
		{"3S Group", "3S Group"},
		{"Lương 30-40 triệu gross", ""},
		{"Salary: $1,500", ""},
		{"$1,500 - $2,000", ""},
		{"Up to 2000 USD", ""},
		{"30-40 triệu", ""},
		{"Hà Nội", ""},
		{"Ho Chi Minh City, Vietnam", ""},
		{"Quận 1, TP. Hồ Chí Minh", ""},
		{"Cầu Giấy, Hà Nội", ""},
		{"Thủ Đức, TP.HCM", ""},
		{"Bình Thạnh, Hồ Chí Minh", ""},
		{"Bình Dương", ""},
		{"Đồng Nai", ""},
		{"Full-time, Hà Nội", ""},
		{"Tầng 5, 123 Lê Lợi, Quận 1", ""},
		{"Remote", ""},
		{"Yêu cầu: React", ""},
		{"Requirements - 3 years Go", ""},
	} {
		t.Run(tc.line, func(t *testing.T) {
			jp := ParsePastedJD("Data Analyst\n" + tc.line + "\nMô tả: phân tích số liệu")
			if jp.Company != tc.company {
				t.Errorf("company = %q, want %q", jp.Company, tc.company)
			}
		})
	}
}

func TestParsePostNeverTakesDetailLineAsCompany(t *testing.T) {
	post := "HIRING BACKEND ENGINEER\nLương 30-40 triệu gross\nHybrid, Hà Nội\nGửi CV về hr@duck.vn"
	if jp := ParsePost(post); jp.Company != "" {
		t.Fatalf("company = %q, want empty", jp.Company)
	}
	if jp := ParsePost(post + "\nCông ty TNHH HConnect"); jp.Company != "Công ty TNHH HConnect" {
		t.Fatalf("named company = %q, want it read", jp.Company)
	}
}

func TestSalaryWordTakesNoUnit(t *testing.T) {
	page := `<script type="application/ld+json">{"@type":"JobPosting","title":"T",
		"baseSalary":{"@type":"MonetaryAmount","currency":"USD","value":{"value":"Thương lượng","unitText":"HOUR"}}}</script>`
	jp, ok := ExtractJobPosting(page)
	if !ok || jp.Salary != "Thương lượng" {
		t.Fatalf("salary = %q (ok=%v), want the bare word", jp.Salary, ok)
	}
}
