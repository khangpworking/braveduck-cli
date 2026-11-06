package hosted

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/core/keyword"
	"github.com/khangpworking/braveduck-cli/internal/service"
)

const scannerKey = "scanner-key-0123456789abcdef0123456789"

func scan(t *testing.T, h http.Handler, key, method, path, body string) (int, string) {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

// One scan of a public group serves every follower, each through their own
// search terms; a private group's posts go to nobody; the API needs its key.
func TestScannerAPI(t *testing.T) {
	s := newServer(t, t.TempDir(), 5)
	ctx := context.Background()
	follow := func(user, link string, terms ...string) *service.Service {
		t.Helper()
		tn, err := s.user(user)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := tn.svc.FollowGroup(ctx, link); err != nil {
			t.Fatal(err)
		}
		if len(terms) > 0 {
			if _, err := tn.svc.SetKeywords(ctx, keyword.Sets{"search_queries": terms}); err != nil {
				t.Fatal(err)
			}
		}
		return tn.svc
	}
	alice := follow("alice", "https://www.facebook.com/groups/ai.jobs/", "n8n")
	bob := follow("bob", "https://m.facebook.com/groups/AI.Jobs/?ref=x", "python")
	carol := follow("carol", "https://www.facebook.com/groups/secret.club/", "n8n")
	h := s.Scanner(scannerKey)

	if code, _ := scan(t, h, "", "GET", "/groups", ""); code != http.StatusUnauthorized {
		t.Fatalf("no key: %d", code)
	}
	if code, _ := scan(t, h, "wrong-key-0123456789abcdef0123456789", "GET", "/groups", ""); code != http.StatusUnauthorized {
		t.Fatalf("wrong key: %d", code)
	}
	code, body := scan(t, h, scannerKey, "GET", "/groups", "")
	var groups []ScanGroup
	json.Unmarshal([]byte(body), &groups)
	if code != 200 || len(groups) != 2 {
		t.Fatalf("groups = %d %s", code, body)
	}
	for _, g := range groups {
		if g.Key == "ai.jobs" && g.Followers != 2 || g.Visibility != service.FBChecking {
			t.Fatalf("group %+v", g)
		}
	}

	if code, body := scan(t, h, scannerKey, "POST", "/groups/ai.jobs/status", `{"name":"AI Jobs VN","visibility":"public"}`); code != 200 || !strings.Contains(body, `"boards":2`) {
		t.Fatalf("status = %d %s", code, body)
	}
	scan(t, h, scannerKey, "POST", "/groups/secret.club/status", `{"name":"Secret","visibility":"private"}`)
	if code, _ := scan(t, h, scannerKey, "POST", "/groups/ai.jobs/status", `{"visibility":"maybe"}`); code != http.StatusBadRequest {
		t.Fatalf("bad visibility: %d", code)
	}

	posts := `{"posts":[
		{"url":"https://www.facebook.com/groups/123/posts/1/","text":"Tuyển n8n developer, remote"},
		{"url":"https://www.facebook.com/groups/123/posts/2/","text":"Tuyển Python backend, Hà Nội"},
		{"url":"https://www.facebook.com/groups/123/posts/3/","text":"Cho thuê nhà"}]}`
	code, body = scan(t, h, scannerKey, "POST", "/groups/ai.jobs/posts", posts)
	if code != 200 || !strings.Contains(body, `"added":2`) {
		t.Fatalf("posts = %d %s", code, body)
	}
	jobsOf := func(svc *service.Service) []string {
		list, _ := svc.ListJobs(ctx, service.ListFilter{})
		var out []string
		for _, j := range list {
			out = append(out, j.FullJD)
		}
		return out
	}
	if a, b := jobsOf(alice), jobsOf(bob); len(a) != 1 || !strings.Contains(a[0], "n8n") || len(b) != 1 || !strings.Contains(b[0], "Python") {
		t.Fatalf("alice %q, bob %q", a, b)
	}
	if code, body := scan(t, h, scannerKey, "POST", "/groups/secret.club/posts", posts); code != 200 || !strings.Contains(body, `"added":0`) {
		t.Fatalf("private group's posts = %d %s", code, body)
	}
	if c := jobsOf(carol); len(c) != 0 {
		t.Fatalf("carol got a private group's posts: %q", c)
	}
}
