package drama

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"testing"
)

type continuationRepo struct {
	*sbRepo
	forked, resumed bool
}

func (r *continuationRepo) QueueStoryboard(_ context.Context, _, _ string, in StoryboardRun, resume bool) (StoryboardRun, error) {
	r.resumed = resume
	return in, nil
}
func (r *continuationRepo) QueueStoryboardContinuation(_ context.Context, _, _ string, in StoryboardRun, _ int) (StoryboardRun, error) {
	r.forked = true
	in.ID = uuid.NewString()
	in.Version++
	return in, nil
}
func TestStoryboardContinuationForksOnlyWhenBudgetExhausted(t *testing.T) {
	for _, mode := range []string{"正常续跑", "全局预算", "当前片段预算", "同一问题无进展", "其他已完成片段预算"} {
		t.Run(mode, func(t *testing.T) {
			source, repo, seg := pacingFixture()
			repo.run.Status = "failed"
			repo.run.Segments[0].Status = "pending"
			source.work.ScriptText += "\n结束。"
			repo.run.ScriptText = source.work.ScriptText
			repo.run.SourceHash = storyboardHash(source.work)
			repair := &repairMemory{}
			total, item := 0, 1
			switch mode {
			case "全局预算":
				total, item = 40, 9
			case "当前片段预算":
				total = 8
			case "同一问题无进展":
				total = 3
			case "其他已完成片段预算":
				total, item = 8, 9
			}
			for i := 0; i < total; i++ {
				hash := fmt.Sprint(i)
				if mode == "同一问题无进展" {
					hash = "same"
				}
				repair.tickets = append(repair.tickets, RepairTicket{ProjectID: repo.run.ProjectID, RunID: repo.run.ID, Kind: "storyboard", Item: item, StateHash: hash, Status: "feedback"})
			}
			queue := &continuationRepo{sbRepo: repo}
			service := NewService(source, &fakeWriter{}).WithVisuals(sbVisual{}, nil, nil).WithStoryboards(queue).WithRepairCoordinator(repair)
			job, err := service.StartStoryboard(context.Background(), "org", "user", repo.run.ProjectID, repo.run.EpisodeID, true)
			if err != nil {
				t.Fatal(err)
			}
			fork := mode != "正常续跑" && mode != "其他已完成片段预算"
			if queue.forked != fork || queue.resumed == fork || (job.Run.ID != repo.run.ID) != fork || job.Run.Segments[0].ID != seg.ID || len(repair.tickets) != total {
				t.Fatal("应保留成果与旧记录，仅预算耗尽新建尝试", mode, queue)
			}
		})
	}
}
func TestStoryboardContinuationRejectsStaleOrActiveRuns(t *testing.T) {
	source, repo, _ := pacingFixture()
	queue := &continuationRepo{sbRepo: repo}
	service := NewService(source, &fakeWriter{}).WithVisuals(sbVisual{}, nil, nil).WithStoryboards(queue)
	for _, status := range []string{"queued", "running", "ready"} {
		repo.run.Status = status
		if _, err := service.StartStoryboard(context.Background(), "org", "user", repo.run.ProjectID, repo.run.EpisodeID, true); !errors.Is(err, ErrStoryboardConflict) {
			t.Fatal(status, err)
		}
	}
	repo.run.Status = "failed"
	source.work.ScriptText = "来源已修改"
	if _, err := service.StartStoryboard(context.Background(), "org", "user", repo.run.ProjectID, repo.run.EpisodeID, true); !errors.Is(err, ErrStoryboardConflict) {
		t.Fatal(err)
	}
}
