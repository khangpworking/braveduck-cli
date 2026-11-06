//go:build browser

package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
	"github.com/khangpworking/braveduck-cli/internal/core/keyword"
	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// Settings are readable in Vietnamese, including generated fields, accessible
// names, saved-state feedback and validation. Existing settings tests own the
// English behavior; this one exercises the same screens in the shipped language.
func TestSettingsVietnamese(t *testing.T) {
	t.Setenv("BRAVEDUCK_LANG", "vi")
	ctx := context.Background()
	page, svc, _ := settingsBoard(t, func(svc *service.Service, st *store.Store) {
		svc.SetCrawlers(&countingCrawler{id: "vietnamworks"}, &countingCrawler{id: "careerlink"})
		svc.SetCrawlSchedule(ctx, "vietnamworks", 0)
		svc.SetCrawlSchedule(ctx, "careerlink", 0)
		svc.SetGeocoder(boardFakeGeo{})
		if _, err := svc.SetKeywords(ctx, keyword.Sets{"search_queries": {"Kế toán"}, "negative_company": {"Bad Co"}}); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.AddJob(ctx, service.NewJob{Title: "Kế toán", Company: "Vịt Vàng"}); err != nil {
			t.Fatal(err)
		}
		if err := st.RecordCrawlRun(ctx, "vietnamworks", true, "", time.Now().Add(-5*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if err := st.SaveSession(ctx, store.PlatformSession{Platform: "topcv", Enc: []byte{1}, CreatedAt: "2026-11-01T00:00:00Z", FPSeed: "1", Status: store.SessionConnected}); err != nil {
			t.Fatal(err)
		}
	})
	page.MustWait(`() => AppState.jobs.length === 1`)
	// An offline map must also explain itself in Vietnamese, with address
	// search still usable. Block actual requests rather than replace UI code.
	if err := (proto.NetworkEnable{}).Call(page); err != nil {
		t.Fatal(err)
	}
	if err := (proto.NetworkSetBlockedURLs{Urls: []string{"https://unpkg.com/*"}}).Call(page); err != nil {
		t.Fatal(err)
	}

	check := func(overlay string, wants ...string) {
		t.Helper()
		waitVisible(page, "#"+overlay+" .modal-title")
		page.MustWait(`id => [...document.querySelectorAll('#' + id + ' button')]
			.filter(e => e.getClientRects().length).every(e => getComputedStyle(e).visibility === 'visible')`, overlay)
		text := onScreen(page, "#"+overlay)
		assertSays(t, overlay, text, wants...)
		assertNotSays(t, overlay, text, "undefined", "Search terms", "Check now", "Not checked yet", "How jobs get rated", "Job-site logins", "Your location", "Connect your AI agent", "Advanced", "Couldn't", "Click the map", "Save", "Cancel", "Close")
		bad := page.MustEval(`id => {
			const modal = document.querySelector('#' + id + ' .modal');
			if (!modal.getAttribute('aria-label')?.trim()) return 'dialog name';
			for (const e of modal.querySelectorAll('button, summary, [aria-label], [placeholder]')) {
				if (!e.getClientRects().length) continue;
				const name = e.getAttribute('aria-label') || e.getAttribute('placeholder') || e.textContent;
				if (!name.trim() || name.includes('undefined')) return e.outerHTML;
				if (/Type and press Enter|Remove |Close|Search terms|Check for new jobs|e\.g\./.test(name)) return name;
			}
			return '';
		}`, overlay).Str()
		if bad != "" {
			t.Errorf("blank or untranslated control in %s: %s", overlay, bad)
		}
		assertFitsPhone(t, page, overlay)
		if t.Failed() {
			t.FailNow()
		}
	}
	closeWindow := func(overlay string) {
		page.MustElement("#" + overlay + " .modal-close-btn").MustEval(`() => this.click()`)
	}

	openModal(t, page, "keywordsBtn", "keywordsModalOverlay")
	page.MustElement("#keywordsForm summary").MustClick()
	check("keywordsModalOverlay", "Từ khóa tìm việc", "Bạn muốn tìm việc gì?", "Thêm bộ lọc (đang dùng 1 từ)", "Ẩn việc từ các công ty này", "Lưu", "Huỷ")
	assertSays(t, "keyword count", onScreen(page, "#keywordsCount"), "Có 1 việc")
	in := page.MustElement(`#keywordsForm input[aria-label="Bạn muốn tìm việc gì?"]`)
	in.MustInput("n8n")
	page.Keyboard.MustType(input.Enter)
	page.MustElement(`button[aria-label="Xóa n8n"]`)
	page.MustElement("#saveKeywordsBtn").MustClick()
	page.MustElementR("#toastContainer", "Đã lưu từ khóa tìm việc")

	openModal(t, page, "sourcesBtn", "sourcesModalOverlay")
	page.MustElementR("#sourcesList", "Kiểm tra gần nhất: 5 phút trước")
	page.MustElement("#sourcesList summary").MustClick()
	check("sourcesModalOverlay", "Nguồn tìm việc", "VietnamWorks", "CareerLink", "Tìm việc mới:", "Mỗi 2 giờ", "Nâng cao", "Tự chấm điểm việc", "Cập nhật đơn ứng tuyển TopCV", "Kiểm tra ngay")
	assertSays(t, "sources hint", onScreen(page, "#sourcesHint"), "Các trang đều đang tắt")
	page.MustElement(`#sourcesList select[aria-label^="VietnamWorks"]`).MustSelect("Mỗi 2 giờ")
	page.MustWait(`() => !document.getElementById('checkSourcesBtn').disabled`)
	page.MustElement("#checkSourcesBtn").MustClick()
	page.MustElementR("#toastContainer", "Tìm thấy 1 việc mới")
	page.MustWait(`() => document.getElementById('checkSourcesBtn').textContent === 'Kiểm tra ngay'`)
	closeWindow("sourcesModalOverlay")

	openModal(t, page, "ratingBtn", "ratingModalOverlay")
	openRatingAdvanced(page)
	check("ratingModalOverlay", "Cách chấm điểm việc", "dựa trên hồ sơ của bạn", "So sánh điểm từ nhiều AI", "AI chấm điểm chính", "Sửa hồ sơ của bạn")
	page.MustElement("#defaultModelInput").MustSelectAllText().MustInput("")
	page.MustElement("#saveRatingConfigBtn").MustClick()
	page.MustElementR("#ratingConfigError", "Bạn hãy nhập mã AI chấm điểm")
	page.MustElement("#defaultModelInput").MustInput("claude-opus-5-5")
	page.MustElement("#saveRatingConfigBtn").MustClick()
	page.MustElementR("#toastContainer", "Đã lưu cài đặt chấm điểm")

	openModal(t, page, "platformsBtn", "platformsModalOverlay")
	page.MustElementR("#platformsList", "Đã đăng nhập ✓")
	check("platformsModalOverlay", "Tài khoản tuyển dụng", "TopCV", "Đã đăng nhập ✓", "Cập nhật đơn ứng tuyển", "Đăng nhập lại", "Đăng xuất")
	assertNotSays(t, "logins", onScreen(page, "#platformsList"), "Not connected", "Logged in", "Connect", "Disconnect", "Sync applications")
	closeWindow("platformsModalOverlay")

	openModal(t, page, "locationBtn", "locationModalOverlay")
	page.MustElementR("#locationMap", "Không mở được bản đồ")
	check("locationModalOverlay", "Vị trí của bạn", "Dùng vị trí hiện tại", "Khoảng cách đi làm tối đa", "Xóa vị trí của bạn")
	page.MustElement("#saveLocationBtn").MustClick()
	page.MustElementR("#toastContainer", "Bạn hãy chọn vị trí hiện tại")
	page.MustElement("#placeSearchBtn").MustClick()
	page.MustElementR("#placeResults", "Bạn hãy nhập ít nhất 3 ký tự")
	page.MustElement("#placeSearch").MustInput("Landmark 81")
	page.Keyboard.MustType(input.Enter)
	page.MustElementR("#placeResults button", "Bình Thạnh").MustClick()
	assertSays(t, "picked location", onScreen(page, "#locationPicked"), "Vị trí chọn:", "10,79490", "106,72190")
	page.MustElement("#saveLocationBtn").MustClick()
	page.MustElementR("#toastContainer", "Đã lưu vị trí")

	// The AI settings window is offered on shared boards. Creating a local
	// connection key does not contact an AI provider.
	svc.SetHosted(service.HostedInfo{User: "ban"})
	page.MustEval(`() => refreshAppState()`)
	page.MustWait(`() => !document.getElementById('agentBtn').hidden`)
	openModal(t, page, "agentBtn", "agentModalOverlay")
	page.MustElementR("#agentConnectText", "Tạo mã rồi chạy lệnh")
	check("agentModalOverlay", "Kết nối AI của bạn", "không dùng mật khẩu của bạn", "Tạo mã kết nối AI")
	page.MustElement("#agentKeyNew").MustClick()
	page.MustWait(`() => !document.getElementById('agentConnectCmd').hidden`)
	assertSays(t, "connection instructions", onScreen(page, "#agentConnectText"), "Mã chỉ hiện lần này")
	closeWindow("agentModalOverlay")
	openModal(t, page, "agentBtn", "agentModalOverlay")
	page.MustElementR("#agentConnectText", "AI của bạn đã có mã kết nối")
	check("agentModalOverlay", "Tạo mã mới (mã cũ sẽ hết hiệu lực)", "Xóa mã kết nối")
	page.MustElement("#agentKeyRevoke").MustClick()
	page.MustElementR("#toastContainer", "Đã xóa mã kết nối AI")
	page.MustWait(`() => document.getElementById('agentKeyRevoke').hidden`)
	if got := onScreen(page, "#agentModalOverlay"); strings.Contains(got, "undefined") {
		t.Fatalf("invalid text after revoking a key: %s", got)
	}
}
