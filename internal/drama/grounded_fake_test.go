package drama

import "context"

func (r *fakeRepository) ListReviewIssues(_ context.Context, _, _, _, batch string) ([]ReviewIssue, error) {
	out := []ReviewIssue{}
	run := ""
	for _, b := range r.batches {
		if b.ID == batch {
			run = b.AnalysisRunID
		}
	}
	for _, i := range r.reviewIssues {
		if i.RunID == run {
			out = append(out, i)
		}
	}
	return out, nil
}
func (r *fakeRepository) SaveReviewIssues(_ context.Context, _ BatchAnalysisJob, issues []ReviewIssue) error {
	for _, issue := range issues {
		found := false
		for i, old := range r.reviewIssues {
			if old.ID == issue.ID {
				r.reviewIssues[i] = issue
				found = true
				break
			}
		}
		if !found {
			r.reviewIssues = append(r.reviewIssues, issue)
		}
	}
	return nil
}
func (r *fakeRepository) SaveRepairedEpisode(_ context.Context, _ BatchAnalysisJob, number int, result, hash string) error {
	for i, s := range r.steps {
		if s.Stage == "director" && s.ChunkNumber == number {
			r.steps[i].ResultText = result
			r.steps[i].InputHash = hash
			r.steps[i].Status = "completed"
			return nil
		}
	}
	return ErrNotFound
}
