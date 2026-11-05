package novel

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestNovelDraftRepairsInvalidSummaryBeforeReview(t *testing.T) {
	for _, test := range []struct {
		name    string
		summary string
	}{
		{name: "摘要过长", summary: strconv.Quote(strings.Repeat("字", 501))},
		{name: "摘要不是文本", summary: "42"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, repo, writer := newReviewRevisionFixture(t)
			const body = "他听见钟声，从守卫手中接过钥匙。"
			const summary = "守卫将钥匙交给了他。"
			prompts := stubReviewRevisionResponses(t, writer,
				`{"body":`+strconv.Quote(body)+`,"summary":`+test.summary+`}`,
				`{"summary":`+strconv.Quote(summary)+`}`,
				`{"review":[]}`,
			)
			project, err := service.Apply(context.Background(), repo.owner, repo.project.ID, Action{
				Kind: "write", Chapter: 2, ModelCode: "writer", Version: repo.project.Version,
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := project.Chapters[1]; got.Body != body || got.Summary != summary || !got.ReviewPassed || got.ReviewFailed {
				t.Fatalf("应保留生成的正文，并用补全后的摘要通过审稿: %+v", got)
			}
			if len(*prompts) != 3 || !strings.Contains((*prompts)[1], body) ||
				!strings.HasPrefix((*prompts)[2], reviewPrompt) || !strings.Contains((*prompts)[2], summary) {
				t.Fatal("应先生成正文，再用原正文补摘要，最后审查补全后的草稿")
			}
		})
	}
}

func TestNovelDraftKeepsBodyWhenSummaryRepairFails(t *testing.T) {
	service, repo, writer := newReviewRevisionFixture(t)
	const body = "他在廊下唤了一声皇兄。"
	const instruction = "称呼用皇兄。"
	const summary = "两人在廊下重逢。"
	prompts := stubReviewRevisionResponses(t, writer,
		`{"body":`+strconv.Quote(body)+`}`,
		`{"summary":null}`,
		`{"summary":`+strconv.Quote(summary)+`}`,
		`{"review":[]}`,
	)
	version := repo.project.Version
	project, err := service.Apply(context.Background(), repo.owner, repo.project.ID, Action{
		Kind: "write", Chapter: 2, ModelCode: "writer", Version: version, Instruction: instruction,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := repo.project.Chapters[1]; got.Body != body || got.Summary != "" ||
		got.AuthorInstruction != instruction || !got.ReviewFailed || got.ReviewPassed ||
		project.Version != version+1 || len(*prompts) != 2 {
		t.Fatalf("补摘要失败应保存正文和作者要求，暂不审稿: %+v，版本 %d", got, project.Version)
	}
	project, err = service.Apply(context.Background(), repo.owner, project.ID, Action{
		Kind: "review", Chapter: 2, ModelCode: "writer", Version: project.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := repo.project.Chapters[1]; got.Body != body || got.Summary != summary ||
		got.AuthorInstruction != instruction || got.ReviewFailed || !got.ReviewPassed || project.Version != version+2 {
		t.Fatalf("重试应补摘要并审稿，保持正文及要求: %+v，版本 %d", got, project.Version)
	}
	if len(*prompts) != 4 || !strings.Contains((*prompts)[2], body) ||
		!strings.HasPrefix((*prompts)[3], reviewPrompt) || !strings.Contains((*prompts)[3], summary) {
		t.Fatal("重试应从已保存的正文补摘要，再进行审稿")
	}
}

func TestNovelDraftKeepsPreviousVersionWhenModelStillOmitsBody(t *testing.T) {
	service, repo, writer := newReviewRevisionFixture(t)
	original := Chapter{Body: "原有正文", Summary: "原有摘要", Review: "原有审稿意见"}
	repo.project.Chapters[1] = original
	version := repo.project.Version
	prompts := stubReviewRevisionResponses(t, writer, `{"summary":"只有摘要"}`, `{"body":"  ","summary":"仍无正文"}`)
	_, err := service.Apply(context.Background(), repo.owner, repo.project.ID, Action{
		Kind: "write", Chapter: 2, ModelCode: "writer", Version: version,
	})
	if !errors.Is(err, ErrGeneration) || !strings.Contains(err.Error(), "正文") {
		t.Fatalf("两次均没有有效正文时应说明失败字段: %v", err)
	}
	if len(*prompts) != 2 || repo.project.Version != version || repo.project.Chapters[1] != original {
		t.Fatal("正文重试应最多一次，两次都失败则保留原稿和版本")
	}
}
