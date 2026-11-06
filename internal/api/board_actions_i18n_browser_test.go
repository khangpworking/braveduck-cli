//go:build browser

package api

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// Adding a job and filling in a profile read in Vietnamese on a phone, including
// generated fields, feedback and the server's common errors. What the profile
// saves stays as the form's own values: translation is only what's on screen.
func TestAddingAndProfileVietnamese(t *testing.T) {
	t.Setenv("BRAVEDUCK_LANG", "vi")
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dir := t.TempDir()
	svc := service.New(st)
	svc.SetDataDir(dir)
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage(ts.URL).MustWaitLoad()
	page.MustElement(".column")
	setViewportWidth(page, 390)

	// says checks what's on screen under a selector, and that nothing there is
	// blank, undefined, an unfilled {placeholder} or wider than the phone.
	says := func(sel string, wants, gone []string) {
		t.Helper()
		waitVisible(page, sel)
		// A window fades in, and its buttons are missing from innerText until they're visible.
		page.MustWait(`sel => [...document.querySelectorAll(sel + " button")]
			.filter(e => e.getClientRects().length).every(e => getComputedStyle(e).visibility === "visible")`, sel)
		text := page.MustElement(sel).MustText()
		for _, w := range wants {
			if !strings.Contains(text, w) {
				t.Errorf("%s lacks %q: %q", sel, w, text)
			}
		}
		for _, g := range gone {
			if strings.Contains(text, g) {
				t.Errorf("%s still says %q: %q", sel, g, text)
			}
		}
		if bad := page.MustEval(`sel => {
			const root = document.querySelector(sel);
			if (root.scrollWidth > root.clientWidth + 1) return "wider than its box: " + root.scrollWidth + " > " + root.clientWidth;
			for (const e of root.querySelectorAll("button, summary, label, option, [aria-label], [placeholder]")) {
				const names = [e.getAttribute("aria-label"), e.getAttribute("placeholder"), e.textContent];
				if (!names.some(n => n && n.trim())) return "blank: " + e.outerHTML;
				if (names.some(n => n && /undefined|\{\w+\}/.test(n))) return "unfilled: " + e.outerHTML;
			}
			return /undefined|\{\w+\}/.test(root.innerText) ? "unfilled text: " + root.innerText : "";
		}`, sel).Str(); bad != "" {
			t.Errorf("%s: %s", sel, bad)
		}
	}
	toast := func(want string) {
		t.Helper()
		page.MustWait(`want => document.getElementById("toastContainer").textContent.includes(want)`, want)
	}
	paste := func(text string) {
		t.Helper()
		page.MustEval(`text => { document.getElementById("extractResultBanner").textContent = ""; document.getElementById("pasteJDTextarea").value = text; document.getElementById("extractAddBtn").click(); }`, text)
	}
	banner := func(want string) {
		t.Helper()
		page.MustWait(`want => document.getElementById("extractResultBanner").innerText.includes(want)`, want)
	}

	// Get started.
	says("#setupCard", []string{"Bắt đầu", "Còn 3 bước", "Tải CV lên", "Trả lời vài câu hỏi", "Thêm từ khóa"},
		[]string{"Get started", "steps left", "Upload", "Answer a few"})

	// The Add Job drawer, and what it answers.
	clickID(page, "openDrawerBtn")
	says("#addJobDrawer", []string{"Thêm việc", "Dán link tin tuyển dụng, link nhóm Facebook"}, []string{"Add Job", "Paste a job link"})
	if got := page.MustElement("#pasteJDTextarea").MustProperty("placeholder").Str(); got != "Dán vào đây" {
		t.Errorf("paste placeholder = %q", got)
	}
	clickID(page, "extractAddBtn")
	toast("Bạn hãy dán link hoặc nội dung tin trước")
	paste("abc")
	banner("Bạn hãy dán link hoặc nội dung tin tuyển dụng.")
	paste("https://example.com/job/1") // a word changed in this server message must not leave it English
	banner("braveduck đọc được link từ TopCV")
	paste("https://m.facebook.com/groups/AI.Jobs.VN/?ref=share")
	banner("Đã theo dõi nhóm Facebook này.")
	page.MustWait(`() => !document.getElementById("fbGroupsBlock").hidden`)
	says("#fbGroupsBlock", []string{"Nhóm Facebook bạn theo dõi", "Đang kiểm tra", "Bỏ theo dõi"}, []string{"Checking", "Unfollow"})
	svc.SetGroupStatus(context.Background(), "ai.jobs.vn", "AI Jobs VN", service.FBPrivate, "")
	page.MustEval(`() => renderFBGroups()`)
	page.MustWait(`() => document.getElementById("fbGroupsList").textContent.includes("Riêng tư")`)
	jobText := "Tuyển dụng Kế toán\nCông ty Vịt Vàng\nLương 30-40 triệu gross\nYêu cầu: 3 năm kinh nghiệm"
	paste(jobText)
	banner("Đã thêm việc")
	toast("Đã thêm việc vào cột Đã lưu!")
	paste(jobText)
	banner("Chưa thêm: việc này đã có trên bảng.")
	says("#extractResultBanner", []string{"(Đã lưu)", "Xem"}, []string{"Show"})
	if got := page.MustEval(`() => document.getElementById("extractAddBtn").textContent`).Str(); got != "Thêm" {
		t.Errorf("Add button after adding = %q", got)
	}
	clickID(page, "closeDrawerBtn")
	page.MustWait(`() => AppState.jobs.length === 1`)
	id := page.MustEval(`() => String(AppState.jobs[0].id)`).Str()

	// Rating before there's a profile opens the questions, in the same words.
	page.MustEval(`id => askAI("rate", id)`, id)
	page.MustWait(`() => document.getElementById("profileModalOverlay").classList.contains("open")`)
	toast("Bạn hãy trả lời vài câu hỏi trước")
	says("#profileModalOverlay .modal", []string{"Hồ sơ của bạn", "Bạn chưa điền hồ sơ"}, []string{"Your profile", "Not filled in"})
	page.MustEval(`() => document.querySelector("#profileModalOverlay .modal-close-btn").click()`)

	// My CV: empty, a file it can't take, and a file it can.
	openModal(t, page, "myCVBtn", "cvModalOverlay")
	page.MustWait(`() => document.getElementById("cvStatus").textContent !== ""`)
	says("#cvModalOverlay .modal", []string{"CV của bạn", "Thêm CV để braveduck", "Chọn tệp", "hoặc kéo thả vào đây", "Tệp PDF hoặc Word"},
		[]string{"My CV", "Choose file", "drop it here"})
	bad := filepath.Join(t.TempDir(), "cv.exe")
	if err := os.WriteFile(bad, []byte("not a cv"), 0o600); err != nil {
		t.Fatal(err)
	}
	page.MustElement("#cvFileInput").MustSetFiles(bad)
	page.MustWait(`() => document.getElementById("cvResult").textContent.includes("Bạn hãy tải lên CV dạng PDF")`)
	good := filepath.Join(t.TempDir(), "cv.md")
	if err := os.WriteFile(good, []byte("# Nguyễn Văn A\n\nKế toán tổng hợp, 5 năm kinh nghiệm thuế và báo cáo tại Vịt Vàng."), 0o600); err != nil {
		t.Fatal(err)
	}
	page.MustElement("#cvFileInput").MustSetFiles(good)
	toast("Đã thêm CV. Hồ sơ của bạn đã được cập nhật.")
	page.MustWait(`() => document.getElementById("cvStatus").textContent.includes("đã thêm") && document.getElementById("cvChooseBtn").textContent === "Thay CV khác"`)
	says("#cvModalOverlay .modal", []string{"Tải về", "Vừa xong"}, []string{"Download", "Replace it", "just now"})
	page.MustEval(`() => document.querySelector("#cvModalOverlay .modal-close-btn").click()`)

	// The profile form: the offered choices show translated and save unchanged.
	openModal(t, page, "profileBtn", "profileModalOverlay")
	page.MustWait(`() => document.getElementById("profileStatus").textContent.includes("✓ Đã điền xong")`) // the CV filled it
	page.MustEval(`() => document.querySelector("#profileFormBody details").open = true`)
	says("#profileModalOverlay .modal", []string{"1. Bạn muốn làm việc gì?", "Vị trí công việc", "Mới ra trường", "Toàn thời gian", "Làm từ xa",
		"2. Lương", "mỗi tháng", "Gross hay net?", "3. Điều bạn không chấp nhận", "+ Làm thứ Bảy", "Sửa dạng văn bản", "Tiếng Anh"},
		[]string{"What job", "Job titles", "Fresher", "Full-time", "Remote", "per month", "gross or net", "Deal-breakers", "Working on Saturdays", "Edit as text", "English"})
	page.MustEval(`() => {
		const click = (sel, text) => [...document.querySelectorAll(sel)].find(b => b.textContent.includes(text)).click();
		click("#profileFormBody .pf-choice", "Toàn thời gian");
		click("#profileFormBody .pf-suggestion", "Làm thứ Bảy");
		click("#profileFormBody .pf-suggestion", "Tiếng Anh");
	}`)
	if got := page.MustEval(`() => [...document.querySelectorAll("#profileFormBody .pf-tag")].map(e => e.firstChild.textContent).join("|")`).Str(); !strings.Contains(got, "Làm thứ Bảy") {
		t.Errorf("tags = %q", got)
	}
	page.MustElement(`#profileFormBody input.pf-amount`).MustInput("20000000")
	if got := page.MustElement(`#profileFormBody input.pf-amount`).MustProperty("value").Str(); got != "20.000.000" {
		t.Errorf("amount = %q", got)
	}
	clickID(page, "saveProfileBtn")
	toast("Đã lưu hồ sơ")
	page.MustWait(`() => document.getElementById("profileStatus").textContent.includes("✓ Đã điền xong")
		&& [...document.querySelectorAll("#profileFormBody .pf-choice.on")].some(b => b.textContent === "Toàn thời gian")
		&& [...document.querySelectorAll("#profileFormBody .pf-tag")].some(e => e.firstChild.textContent === "Làm thứ Bảy")`)
	saved, err := os.ReadFile(filepath.Join(dir, "profile.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Full-time", "Working on Saturdays", "English"} {
		if !strings.Contains(string(saved), want) {
			t.Errorf("profile lost %q:\n%s", want, saved)
		}
	}
	for _, shown := range []string{"Toàn thời gian", "Làm thứ Bảy", "Tiếng Anh"} {
		if strings.Contains(string(saved), shown) {
			t.Errorf("profile saved the translated %q:\n%s", shown, saved)
		}
	}
	clickID(page, "profileModeBtn")
	waitVisible(page, "#profileText")
	says("#profileModalOverlay .modal-body", []string{"Toàn bộ hồ sơ dạng văn bản", "<!-- -->"}, []string{"The whole profile"})
	page.MustWait(`() => document.getElementById("profileModeBtn").textContent === "Quay lại biểu mẫu"`)
	page.MustEval(`() => document.querySelector("#profileModalOverlay .modal-close-btn").click()`)

	// Ask your AI.
	page.MustEval(`id => askAI("cv", id)`, id)
	page.MustElement("#askModalOverlay.open")
	says("#askModalOverlay .modal", []string{"Nhờ AI của bạn chỉnh CV cho việc này", "braveduck không có sẵn AI", "Sao chép yêu cầu", "Sao chép toàn bộ nội dung", "Lưu nội dung AI đã viết"},
		[]string{"doesn't run", "Copy request", "Save what my AI wrote"})
	if got := page.MustEval(`() => document.querySelector("#askIntro strong").textContent`).Str(); got != "Claude Code" {
		t.Errorf("ask intro emphasis = %q", got)
	}
	if got := page.MustElement("#askPaste").MustProperty("placeholder").Str(); !strings.Contains(got, "hoặc dán CV") {
		t.Errorf("CV paste placeholder = %q", got)
	}
	page.MustEval(`id => askAI("draft", id)`, id)
	page.MustWait(`() => document.getElementById("askTitle").textContent === "Nhờ AI của bạn soạn email"`)
	if got := page.MustElement("#askPaste").MustProperty("placeholder").Str(); !strings.Contains(got, "Subject: …") || !strings.Contains(got, "hoặc dán email") {
		t.Errorf("email paste placeholder = %q", got)
	}
	page.MustElement("#askPaste").MustInput("Subject: Ứng tuyển Kế toán\n\nKính gửi anh chị, em xin gửi CV ứng tuyển vị trí Kế toán.")
	page.MustEval(`() => saveAskPaste()`)
	toast("Đã lưu email nháp để bạn xem lại.")
	if j, err := svc.GetJob(context.Background(), mustJobID(t, id)); err != nil || !strings.Contains(j.DraftEmail, "Ứng tuyển Kế toán") {
		t.Errorf("draft = %+v, %v", j.DraftEmail, err)
	}

	// Moving a job names the stage the way the board does.
	page.MustEval(`id => transitionCardStatus(id, "Applied")`, id)
	toast("Đã chuyển việc sang Đã ứng tuyển")

	// The Jev notice and opt-out: translating the label keeps its checkbox.
	openModal(t, page, "ratingBtn", "ratingModalOverlay")
	if got := page.MustEval(`() => document.getElementById("jevNotice").textContent`).Str(); !strings.Contains(got, "Hồ sơ và các việc của bạn được gửi đến Jev") {
		t.Errorf("Jev notice = %q", got)
	}
	if got := page.MustEval(`() => document.getElementById("jevOptOutCheckbox").closest("label").textContent.trim()`).Str(); got != "Không gửi hồ sơ của bạn đến Jev" {
		t.Errorf("Jev opt-out = %q", got)
	}
}

func mustJobID(t *testing.T, s string) int64 {
	t.Helper()
	var id int64
	if _, err := fmt.Sscan(s, &id); err != nil {
		t.Fatal(err)
	}
	return id
}
