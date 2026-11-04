package service

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"strconv"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
)

// Export: the user's jobs, out of braveduck — a CSV for spreadsheets (with a
// UTF-8 byte-order mark, so Excel reads Vietnamese correctly) or the full
// records as JSON.

var exportColumns = []string{"id", "title", "company", "status", "score", "letter", "salary", "location",
	"source", "url", "saved_at", "expires_at", "contact_email", "skills", "notes"}

// ExportCSV writes every job as one CSV row, newest first.
func (s *Service) ExportCSV(ctx context.Context, w io.Writer) error {
	list, err := s.ListJobs(ctx, ListFilter{})
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, "\ufeff"); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	if err := cw.Write(exportColumns); err != nil {
		return err
	}
	for _, j := range list {
		if err := cw.Write(exportRow(j)); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func exportRow(j jobs.Job) []string {
	score := ""
	if j.ScoreStatus == "scored" {
		score = strconv.FormatFloat(j.Score, 'f', 1, 64)
	}
	return []string{strconv.FormatInt(j.ID, 10), j.Title, j.Company, j.Status, score, j.ScoreLetter, j.Salary,
		j.Location, j.DiscoverySource, j.URL, j.SavedAt, j.ExpiresAt, j.ContactEmail, j.Skills, j.Notes}
}

// ExportJSON writes every job's full record as a JSON array, newest first.
func (s *Service) ExportJSON(ctx context.Context, w io.Writer) error {
	list, err := s.ListJobs(ctx, ListFilter{})
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(list)
}
