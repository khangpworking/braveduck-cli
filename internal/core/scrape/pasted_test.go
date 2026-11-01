package scrape

import "testing"

func TestParsePastedJD(t *testing.T) {
	for _, tc := range []struct {
		name, in                       string
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
