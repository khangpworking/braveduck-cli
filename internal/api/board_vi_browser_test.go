//go:build browser

package api

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/input"
	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/service"
)

// The real board must speak Vietnamese across its summary, Today, cards and
// activity, while stored stage values and keyboard controls keep working.
// Existing English tests cannot catch missing board dictionary entries.
func TestBoardVietnamese(t *testing.T) {
	t.Setenv("BRAVEDUCK_LANG", "vi")
	st, svc, ts := newCardsBoard(t)
	ctx := context.Background()
	now := time.Now()
	deadline := "expire:" + now.AddDate(0, 0, 3).Format("2006-01-02")
	applied := "applied:" + now.AddDate(0, 0, -8).Format("2006-01-02")
	seed := []jobs.Job{
		{Title: "Nhân viên kế toán", Status: "saved", Company: "Công ty Vịt Vàng", Salary: "16500000 - 22000000 VND/month", HasScore: true, Score: 4.6, ScoreLetter: "A", ScoreStatus: "scored", ScoreReasoning: "Kinh nghiệm phù hợp", Skills: "Kế toán,Excel", Summary: "Lập báo cáo thu chi", ExpiresAt: deadline, DraftEmail: `{"to":"nhansu@example.com","subject":"Ứng tuyển kế toán","body":"Chào anh chị"}`, CVStatus: "ready", CVVersion: 2, CVGeneratedAt: now.Format(time.RFC3339)},
		{Title: "Nhân viên hành chính", Status: "saved", ScoreStatus: "failed"},
		{Title: "Chuyên viên tuyển dụng", Status: "applied", ExpiresAt: applied},
		{Title: "Chuyên viên nhân sự", Status: "interview"},
	}
	for _, j := range seed {
		if _, err := st.Add(ctx, j, now); err != nil {
			t.Fatal(err)
		}
	}
	// An actual service event, including hostile text, must remain plain text
	// when the activity template is translated.
	if _, err := svc.AddJob(ctx, service.NewJob{Title: `Trợ lý <img src=x onerror=alert(1)>`}); err != nil {
		t.Fatal(err)
	}

	browser := launchBrowser(t)
	for _, width := range []int{1280, 390} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			page := browser.MustPage("")
			defer page.MustClose()
			page.MustSetViewport(width, 900, 1, false)
			page.MustNavigate(ts.URL).MustWaitLoad()
			page.MustElement(".card")
			page.MustElement("#todayList .today-item")
			assertText := func(selector, want string) {
				t.Helper()
				if got := page.MustElement(selector).MustText(); !strings.Contains(strings.ToLower(got), strings.ToLower(want)) {
					t.Fatalf("%s = %q, want %q", selector, got, want)
				}
			}
			assertText("#statPills", "Đã lưu")
			assertText("#todaySummary", "việc cần xem")
			assertText("#col-Saved .column-title", "Đã lưu")
			assertText("#col-Waiting-to-apply .column-title", "Chờ ứng tuyển")
			if got := page.MustElement("#jobSearch").MustAttribute("placeholder"); got == nil || *got != "Tìm việc đã lưu  ( / )" {
				t.Fatalf("search placeholder = %v", got)
			}
			page.MustElement("#todayWidget summary").MustEval(`() => this.click()`)
			assertText("#todayList", "Việc phù hợp nên ứng tuyển")
			assertText("#todayList", "Sắp hết hạn trong tuần")
			assertText("#todayList", "Còn 3 ngày để ứng tuyển")
			assertText("#todayList", "Đã ứng tuyển 8 ngày trước")
			assertText("#todayList", "A · 4,6")
			assertText("#moraleSubline", "1 lượt phỏng vấn / 1 hồ sơ đã có kết quả · 1 đang chờ")

			// Open a card using its translated accessible name and keyboard.
			page.MustElement(`#col-Saved .expand-btn[aria-label="Chi tiết: Nhân viên kế toán"]`).MustFocus()
			page.Keyboard.MustType(input.Enter)
			page.MustWait(`() => document.querySelector('#col-Saved .card.open') !== null`)
			page.MustElement("#col-Saved .card.open .card-more summary").MustFocus()
			page.Keyboard.MustType(input.Enter)
			page.MustElement("#col-Saved .card.open .edit-details summary").MustEval(`() => this.click()`)
			assertText("#col-Saved .card.open", "Ghi chú của bạn (tự lưu khi nhập)")
			assertText("#col-Saved .card.open", "CV cho việc này")
			assertText("#col-Saved .card.open", "Tiêu đề email")
			assertText("#col-Saved .card.open", "Lưu thông tin")
			assertText("#col-Saved .card.open .score-badge", "4,6")
			assertText("#col-Saved .card.open .salary-chip", "16,5–22 triệu/tháng")
			if got := page.MustElement("#col-Saved .card.open .move-select").MustProperty("value").Str(); got != "Saved" {
				t.Fatalf("translated option changed stored stage to %q", got)
			}
			clickID(page, "activityBtn")
			assertText("#activityPanel", "Hoạt động gần đây")
			assertText("#logFeed", `Đã thêm "Trợ lý <img src=x onerror=alert(1)>"`)
			if page.MustHas("#logFeed img") {
				t.Fatal("translated activity rendered job text as HTML")
			}
			clickID(page, "activityCloseBtn")
			clickID(page, "settingsBtn")
			assertText("#notifyBtn", "Báo việc phù hợp: tắt")
			clickID(page, "settingsBtn")

			if problem := page.MustEval(`() => {
				const roots = ['#statPills', '#todayWidget', '#kanbanBoard', '#activityPanel'];
				const english = /\b(Saved|Applied|Interview|Closed|Details|Hide|More|Rating|Recipient|Subject|Keep|Delete|Pin|Skills|Summary|Nothing|Deadline|Stage|Open|Show|Change|Move|Newest|new jobs|to look at)\b/i;
				for (const root of roots) {
					const el = document.querySelector(root);
					if (english.test(el.innerText) || /undefined|Invalid Date|NaN/.test(el.textContent)) return root + ': ' + el.innerText;
					for (const control of el.querySelectorAll('button, select, summary, [role="button"]')) {
						const name = control.getAttribute('aria-label') || control.textContent.trim();
						if (!name || english.test(name)) return 'untranslated/empty control: ' + control.outerHTML;
						for (const attr of ['title', 'aria-label', 'placeholder']) {
							if (control.hasAttribute(attr) && (english.test(control.getAttribute(attr)) || /undefined|Invalid Date/.test(control.getAttribute(attr)))) return control.outerHTML;
						}
					}
				}
				if (document.documentElement.scrollWidth > document.documentElement.clientWidth) return 'page scrolls sideways';
				return '';
			}`).Str(); problem != "" {
				t.Fatal(problem)
			}
		})
	}
}
