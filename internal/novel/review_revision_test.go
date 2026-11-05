package novel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestNovelAuthorInstructionReachesReviewAndRemainsWithDraft(t *testing.T) {
	service, repo, writer := newReviewRevisionFixture(t)
	prompts := stubReviewRevisionResponses(t, writer,
		`{"body":"他唤了一声皇兄。","summary":"两人在廊下见面。"}`,
		`{"review":[]}`,
		`{"review":[]}`,
	)
	instruction := "本章用皇兄，不用哥。"
	project, err := service.Apply(context.Background(), repo.owner, repo.project.ID, Action{
		Kind: "write", Chapter: 2, ModelCode: "writer", Version: repo.project.Version, Instruction: " " + instruction + " ",
	})
	if err != nil {
		t.Fatal(err)
	}
	chapter := project.Chapters[1]
	if chapter.AuthorInstruction != instruction || !chapter.ReviewPassed || chapter.ReviewFailed {
		t.Fatalf("作者称呼要求和审稿通过状态未保存: %+v", chapter)
	}
	if _, err := service.Apply(context.Background(), repo.owner, project.ID, Action{
		Kind: "review", Chapter: 2, ModelCode: "writer", Version: project.Version,
	}); err != nil {
		t.Fatal(err)
	}
	if len(*prompts) != 3 {
		t.Fatalf("预期写作、自动审稿和手动审稿各一次，收到 %d 次", len(*prompts))
	}
	for i, prompt := range *prompts {
		if !strings.Contains(prompt, `"Instruction":"`+instruction+`"`) ||
			!strings.Contains(prompt, "前章称哥") {
			t.Fatalf("第 %d 次调用缺少作者明确要求或用于比对的前章: %s", i+1, prompt)
		}
	}
}

func TestNovelReviewRevisionKeepsOnlyUnresolvedIssuesAndGroundedRegressions(t *testing.T) {
	service, repo, writer := newReviewRevisionFixture(t)
	oldBody := "他经过庭院，钟声一直从前院传来。"
	openIssue := "【缺少交代】须确认钥匙由谁保管。"
	repo.project.Chapters[1] = Chapter{
		Body: oldBody, Summary: "他在庭院听见钟声。", AuthorInstruction: "称呼用皇兄。",
		Review: "【明确矛盾】时间顺序与前章不符。\n\n" + openIssue + "\n\n【可选润色】试换一种比喻。",
	}
	prompts := stubReviewRevisionResponses(t, writer,
		`{"body":"他经过庭院，钟声一直从前院传来。窗外明明还是午后。","summary":"他在庭院听见钟声。"}`,
		`{"checks":[{"id":2,"resolved":false},{"id":1,"resolved":true}],"regressions":[
			{"quote":"钟声一直从前院传来","issue":"原稿已有的问题","fix":"删除钟声"},
			{"quote":"水下冒出一块石碑","issue":"编造原文的问题","fix":"去掉石碑"},
			{"quote":"窗外明明还是午后","issue":"新稿改成午后，与前章午夜不符","fix":"改成午夜"}
		]}`,
		`{"body":"午夜，他经过庭院，从守卫手中接过钥匙。","summary":"午夜拿到了钥匙。"}`,
		`{"checks":[{"id":1,"resolved":true},{"id":2,"resolved":true}],"regressions":[]}`,
	)
	action := Action{Kind: "revise_review", Chapter: 2, ModelCode: "writer", Version: repo.project.Version}
	first, err := service.Apply(context.Background(), repo.owner, repo.project.ID, action)
	if err != nil {
		t.Fatal(err)
	}
	chapter := first.Chapters[1]
	if chapter.ReviewPassed || chapter.ReviewFailed || !strings.Contains(chapter.Review, openIssue) ||
		!strings.Contains(chapter.Review, "【本次修订引入】新稿改成午后") ||
		!strings.Contains(chapter.Review, "窗外明明还是午后") {
		t.Fatalf("复核未保留仍存在的问题和可定位的新回归: %+v", chapter)
	}
	for _, discarded := range []string{"时间顺序与前章不符", "换一种比喻", "原稿已有的问题", "编造原文的问题"} {
		if strings.Contains(chapter.Review, discarded) {
			t.Fatalf("复核错误保留了 %q: %s", discarded, chapter.Review)
		}
	}
	var material struct {
		Issues       []reviewIssueToVerify `json:"issues"`
		PreviousBody string                `json:"previousBody"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix((*prompts)[1], verifyReviewPrompt)), &material); err != nil {
		t.Fatal(err)
	}
	if len(material.Issues) != 2 || material.Issues[0].ID != 1 || material.Issues[1].ID != 2 ||
		material.Issues[1].Text != openIssue || material.PreviousBody != oldBody {
		t.Fatalf("复核缺少原稿和待核对的旧问题编号: %+v", material)
	}
	if !strings.Contains((*prompts)[0], `"Instruction":"称呼用皇兄。"`) ||
		!strings.Contains((*prompts)[1], `"Instruction":"称呼用皇兄。"`) {
		t.Fatal("按意见修订或复核丢掉了作者称呼要求")
	}

	action.Version = first.Version
	second, err := service.Apply(context.Background(), repo.owner, first.ID, action)
	if err != nil || !second.Chapters[1].ReviewPassed || second.Chapters[1].Review != "" {
		t.Fatalf("旧问题全部解决后应结束修订循环: %+v, %v", second.Chapters, err)
	}
	action.Version = second.Version
	if _, err := service.Apply(context.Background(), repo.owner, second.ID, action); !errors.Is(err, ErrState) {
		t.Fatalf("复核通过后没有待修意见，不应再次改写: %v", err)
	}
}

func TestNovelReviewRevisionKeepsNewDraftWhenVerificationOmitsIssue(t *testing.T) {
	service, repo, writer := newReviewRevisionFixture(t)
	oldReview := "【明确矛盾】时间顺序不对。\n\n【缺少交代】钥匙来源不明。"
	repo.project.Chapters[1] = Chapter{Body: "旧正文", Summary: "旧摘要", Review: oldReview, AuthorInstruction: "称呼用皇兄。"}
	stubReviewRevisionResponses(t, writer,
		`{"body":"修订后的正文","summary":"修订后的摘要"}`,
		`{"checks":[{"id":1,"resolved":true}],"regressions":[]}`,
	)
	before := repo.project.Version
	project, err := service.Apply(context.Background(), repo.owner, repo.project.ID, Action{
		Kind: "revise_review", Chapter: 2, ModelCode: "writer", Version: before,
	})
	if err != nil {
		t.Fatal(err)
	}
	chapter := repo.project.Chapters[1]
	if project.Version != before+1 || chapter.Body != "修订后的正文" || chapter.Summary != "修订后的摘要" ||
		chapter.AuthorInstruction != "称呼用皇兄。" || chapter.Review != oldReview || !chapter.ReviewFailed || chapter.ReviewPassed {
		t.Fatalf("复核漏检旧问题时仍应保存新稿、保留可重试的旧意见: %+v", chapter)
	}
}

func newReviewRevisionFixture(t *testing.T) (*Service, *memoryRepository, *testWriter) {
	t.Helper()
	repo, writer := &memoryRepository{}, &testWriter{}
	service := NewService(repo, writer, nil, nil)
	owner := Owner{OrganizationID: "org", UserID: "author"}
	if _, err := service.Create(context.Background(), owner, CreateInput{
		Title: "庭院夜话", Brief: "兄弟在庭院重逢", ChapterCount: 2,
	}); err != nil {
		t.Fatal(err)
	}
	repo.project.Confirmed = true
	repo.project.Outline = &Outline{Synopsis: "兄弟重逢", World: "架空王朝", Characters: "两位皇子", Beats: []Beat{
		{Title: "入府", Goal: "等候兄长"}, {Title: "重逢", Goal: "在庭院交谈"},
	}}
	repo.project.Chapters[0] = Chapter{Body: "午夜，前章称哥。", Summary: "两人尚未见面。", Final: true}
	return service, repo, writer
}

func stubReviewRevisionResponses(t *testing.T, writer *testWriter, responses ...string) *[]string {
	t.Helper()
	prompts := make([]string, 0, len(responses))
	writer.beforeReturn = func() {
		if len(prompts) >= len(responses) {
			t.Fatalf("出现了额外的模型调用: %s", writer.prompt)
		}
		writer.result = responses[len(prompts)]
		prompts = append(prompts, writer.prompt)
	}
	return &prompts
}
