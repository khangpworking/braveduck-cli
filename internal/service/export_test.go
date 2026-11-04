package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
)

func TestExport(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	s.AddJob(ctx, NewJob{Title: "Kỹ Sư AI, \"senior\"", Company: "Sao Mai", Salary: "16–22 triệu", URL: "https://vn.joboko.com/viec-lam-x-xvi1"})
	s.st.Add(ctx, jobs.Job{Title: "Rated", Status: "saved", Score: 4.25, HasScore: true, ScoreLetter: "A", ScoreStatus: "scored", Notes: "line one\nline two"}, s.now())

	var b bytes.Buffer
	if err := s.ExportCSV(ctx, &b); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(b.String(), "\ufeff") {
		t.Fatal("no byte-order mark: Excel would mangle Vietnamese")
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(b.String(), "\ufeff"))).ReadAll()
	if err != nil || len(rows) != 3 || strings.Join(rows[0], ",") != strings.Join(exportColumns, ",") {
		t.Fatalf("csv = %v %v", rows, err)
	}
	byTitle := map[string][]string{}
	for _, r := range rows[1:] {
		byTitle[r[1]] = r
	}
	if r := byTitle[`Kỹ Sư AI, "senior"`]; r == nil || r[2] != "Sao Mai" || r[6] != "16–22 triệu" || r[8] != "joboko" || r[4] != "" {
		t.Fatalf("quoted row = %v", r)
	}
	if r := byTitle["Rated"]; r[4] != "4.2" && r[4] != "4.3" || r[5] != "A" || r[14] != "line one\nline two" {
		t.Fatalf("rated row = %v", r)
	}

	b.Reset()
	if err := s.ExportJSON(ctx, &b); err != nil {
		t.Fatal(err)
	}
	var list []jobs.Job
	if err := json.Unmarshal(b.Bytes(), &list); err != nil || len(list) != 2 {
		t.Fatalf("json = %v %v", list, err)
	}
}
