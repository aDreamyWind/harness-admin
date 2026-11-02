package drama

import (
	"context"
	"errors"
	"testing"
)

func TestStoryboardMentionsUseUnicodeRangesAndBoundRealForms(t *testing.T) {
	seg, lines := narrationFixture()
	seg.FormIDs = []string{"form"}
	seg.Shots[0].Description = "😀林夏推门。"
	seg.Shots[0].DescriptionMentions = []StoryboardMention{{Start: 1, End: 3, FormID: "form"}}
	forms := []AssetForm{{ID: "form", AssetName: "林夏", Kind: "character"}}
	if err := validateStoryboardSegment(&seg, lines, forms, false); err != nil {
		t.Fatal(err)
	}
	for _, refs := range [][]StoryboardMention{
		{{Start: -1, End: 2, FormID: "form"}}, {{Start: 1, End: 99, FormID: "form"}}, {{Start: 1, End: 1, FormID: "form"}}, {{Start: 1, End: 3, FormID: "other"}}, {{Start: 1, End: 3, FormID: "form"}, {Start: 2, End: 4, FormID: "form"}},
	} {
		seg.Shots[0].DescriptionMentions = refs
		if validateStoryboardSegment(&seg, lines, forms, false) == nil {
			t.Fatal("越界、重叠或未授权引用被接受", refs)
		}
	}
}

type mentionVisual struct {
	sbVisual
	forms []AssetForm
}

func (v mentionVisual) ReadCanvas(context.Context, string, string, string) (Canvas, error) {
	return Canvas{Forms: v.forms}, nil
}

type mentionEditRepo struct {
	*sbRepo
	edited *StoryboardRun
}

func (r *mentionEditRepo) EditStoryboard(_ context.Context, _, _ string, in StoryboardRun, _ int) (StoryboardRun, error) {
	r.edited = &in
	return in, nil
}
func TestManualStoryboardMentionCanUseProjectCatalogButCannotCrossScope(t *testing.T) {
	source, repo, seg := pacingFixture()
	repo.run.Status = "ready"
	forms := []AssetForm{{ID: "catalog-form", AssetID: "catalog-asset", AssetName: "林夏", Kind: "character"}}
	store := &mentionEditRepo{sbRepo: repo}
	service := NewService(source, &fakeWriter{}).WithStoryboards(store).WithVisuals(mentionVisual{forms: forms}, nil, nil)
	seg.Shots[0].Description = "林夏进门。"
	seg.Shots[0].DescriptionMentions = []StoryboardMention{{Start: 0, End: 2, FormID: "catalog-form"}}
	saved, err := service.EditStoryboard(context.Background(), "org", "user", repo.run.ProjectID, repo.run.EpisodeID, repo.run.ID, StoryboardEdit{Revision: 1, Segments: []StoryboardSegment{seg}})
	if err != nil || store.edited == nil || len(saved.Segments[0].FormIDs) != 1 || saved.Segments[0].FormIDs[0] != "catalog-form" {
		t.Fatal("真实文字引用没有加入分镜绑定", err)
	}
	store.edited = nil
	seg.FormIDs = nil
	seg.Shots[0].DescriptionMentions[0].FormID = "other-project-form"
	if _, err = service.EditStoryboard(context.Background(), "org", "user", repo.run.ProjectID, repo.run.EpisodeID, repo.run.ID, StoryboardEdit{Revision: 1, Segments: []StoryboardSegment{seg}}); !errors.Is(err, ErrInvalidInput) || store.edited != nil {
		t.Fatal("不应接受其他项目或不存在的形象", err)
	}
}

func TestPacingDoesNotReuseOffsetsAfterRewritingPicture(t *testing.T) {
	before, lines := narrationFixture()
	before.FormIDs = []string{"form"}
	before.Shots[0].DescriptionMentions = []StoryboardMention{{Start: 0, End: 2, FormID: "form"}}
	shot := before.Shots[0]
	shot.Description = "雨夜"
	shot.DescriptionMentions = []StoryboardMention{{Start: 0, End: 100, FormID: "form"}}
	shot.Duration = 8
	got, err := applyPacingPatch(before, []pacingIssue{{Shots: []int{1}}}, pacingPatch{Edits: []pacingEdit{{StartShot: 1, EndShot: 1, Shots: []StoryboardShot{shot}}}}, lines, []AssetForm{{ID: "form"}})
	if err != nil || len(got.Shots[0].DescriptionMentions) != 0 {
		t.Fatal("模型重写文字不能沿用旧位置或自报错误位置", err)
	}
}
