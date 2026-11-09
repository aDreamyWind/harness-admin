package novel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
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
		`{"edits":[{"issueIds":[1,2],"before":"他经过庭院，钟声一直从前院传来。","after":"他走进庭院，钟声一直从前院传来。窗外明明还是午后。"}],"summary":"他在庭院听见钟声。"}`,
		`{"checks":[{"id":2,"resolved":false},{"id":1,"resolved":true}],"regressions":[
			{"quote":"钟声一直从前院传来","issue":"原稿已有的问题","fix":"删除钟声"},
			{"quote":"水下冒出一块石碑","issue":"编造原文的问题","fix":"去掉石碑"},
			{"quote":"窗外明明还是午后","issue":"新稿改成午后，与前章午夜不符","fix":"改成午夜"}
		]}`,
		`{"edits":[{"issueIds":[1],"before":"他走进庭院，钟声一直从前院传来。","after":"他经过庭院，从守卫手中接过钥匙。"},
			{"issueIds":[2],"before":"窗外明明还是午后。","after":"此时已经是午夜。"}],"summary":"午夜拿到了钥匙。"}`,
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
		Current         struct{ Draft string } `json:"current"`
		Issues          []reviewIssueToVerify  `json:"issues"`
		ChangedText     []string               `json:"changedText"`
		PreviousBody    json.RawMessage        `json:"previousBody"`
		PreviousSummary json.RawMessage        `json:"previousSummary"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix((*prompts)[1], verifyReviewPrompt)), &material); err != nil {
		t.Fatal(err)
	}
	if len(material.Issues) != 2 || material.Issues[0].ID != 1 || material.Issues[1].ID != 2 ||
		material.Issues[1].Text != openIssue || material.Current.Draft != "他走进庭院，钟声一直从前院传来。窗外明明还是午后。" ||
		!strings.Contains(strings.Join(material.ChangedText, "\n"), "窗外明明还是午后") ||
		material.PreviousBody != nil || material.PreviousSummary != nil || strings.Contains((*prompts)[1], oldBody) {
		t.Fatalf("复核应使用当前正文、当前改动及旧意见，不能引入已替换的原稿: %+v", material)
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
	repo.project.Chapters[1] = Chapter{Body: "他在午后经过庭院。", Summary: "旧摘要", Review: oldReview, AuthorInstruction: "称呼用皇兄。"}
	stubReviewRevisionResponses(t, writer,
		`{"edits":[{"issueIds":[1,2],"before":"他在午后经过庭院。","after":"午夜，他从守卫手中接过钥匙。"}],"summary":"午夜交接钥匙。"}`,
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
	if project.Version != before+1 || chapter.Body != "午夜，他从守卫手中接过钥匙。" || chapter.Summary != "午夜交接钥匙。" ||
		chapter.AuthorInstruction != "称呼用皇兄。" || chapter.Review != oldReview || !chapter.ReviewFailed || chapter.ReviewPassed {
		t.Fatalf("复核漏检旧问题时仍应保存新稿、保留可重试的旧意见: %+v", chapter)
	}
}

func TestNovelReviewRevisionRecordsSummaryOnlyAfterReload(t *testing.T) {
	service, repo, writer := newReviewRevisionFixture(t)
	original := Chapter{Body: "他空手走过庭院。", Summary: "他带着钥匙走过庭院。", Review: "【仅摘要】正文说他空手，摘要误写为带着钥匙。"}
	repo.project.Chapters[1] = original
	stubReviewRevisionResponses(t, writer,
		`{"edits":[],"summary":"他穿过庭院时手里没有东西。"}`,
		`{"checks":[{"id":1,"resolved":true,"scope":"summary"}],"regressions":[]}`,
	)
	ctx := context.Background()
	if _, err := service.Apply(ctx, repo.owner, repo.project.ID, Action{
		Kind: "revise_review", Chapter: 2, ModelCode: "writer", Version: repo.project.Version,
	}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := service.Get(ctx, repo.owner, repo.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	chapter := reloaded.Chapters[1]
	outcome := chapter.LastReviewRevision
	if outcome == nil || outcome.BodyChanged || !outcome.SummaryChanged || outcome.CompletedAt.IsZero() ||
		chapter.Body != original.Body || chapter.Summary == original.Summary || chapter.Review != "" || !chapter.ReviewPassed || chapter.ReviewFailed {
		t.Fatalf("仅摘要意见复核通过后未保留准确的结果: %+v", chapter)
	}
	updated, err := service.Apply(ctx, repo.owner, reloaded.ID, Action{
		Kind: "save_draft", Chapter: 2, Version: reloaded.Version, Body: "他从守卫手中接过钥匙。", Summary: chapter.Summary,
	})
	if err != nil || updated.Chapters[1].LastReviewRevision != nil {
		t.Fatalf("手动修改正文后仍使用旧改稿结果: %v", err)
	}
}

func TestNovelReviewRevisionRetriesSummaryOnlyAndAppliesTwoBodyEdits(t *testing.T) {
	service, repo, writer := newReviewRevisionFixture(t)
	original := Chapter{
		Body:    "雨打屋檐，灯芯忽明忽暗。\n午前，他写好午夜巡查的结果。\n\n旧钥匙仍锁在柜中。他把钥匙交给守卫。",
		Summary: "他记录巡查结果，并将钥匙交给守卫。",
		Review:  "【明确矛盾】午前无法得知午夜巡查的结果。\n\n【缺少交代】柜子仍锁着，钥匙如何交到守卫手中？",
	}
	repo.project.Chapters[1] = original
	prompts := stubReviewRevisionResponses(t, writer,
		`{"edits":[],"summary":"他安排巡查，并请守卫看紧柜门。"}`,
		`{"checks":[{"id":1,"resolved":false,"scope":"body"},{"id":2,"resolved":false,"scope":"body"}],"regressions":[]}`,
		`{"edits":[{"issueIds":[2],"before":"他把钥匙交给守卫。","after":"他无法交出钥匙，只请守卫看紧柜门。"},
			{"issueIds":[1],"before":"午前，他写好午夜巡查的结果。","after":"午前，他记下午夜巡查的安排。"}],"summary":"他安排巡查，并请守卫看紧柜门。"}`,
		`{"checks":[{"id":1,"resolved":true},{"id":2,"resolved":true}],"regressions":[]}`,
	)
	version := repo.project.Version
	project, err := service.Apply(context.Background(), repo.owner, repo.project.ID, Action{
		Kind: "revise_review", Chapter: 2, ModelCode: "writer", Version: version,
	})
	if err != nil {
		t.Fatal(err)
	}
	const wantBody = "雨打屋檐，灯芯忽明忽暗。\n午前，他记下午夜巡查的安排。\n\n旧钥匙仍锁在柜中。他无法交出钥匙，只请守卫看紧柜门。"
	chapter := project.Chapters[1]
	if chapter.Body != wantBody || chapter.Summary != "他安排巡查，并请守卫看紧柜门。" ||
		!chapter.ReviewPassed || chapter.ReviewFailed || chapter.Review != "" || project.Version != version+1 {
		t.Fatalf("两条正文意见未准确修订，或改动了无关段落: %+v", chapter)
	}
	if chapter.LastReviewRevision == nil || !chapter.LastReviewRevision.BodyChanged || !chapter.LastReviewRevision.SummaryChanged {
		t.Fatal("修订结果未记录正文与摘要的变动")
	}
	if len(*prompts) != 4 || !strings.Contains((*prompts)[2], "午前无法得知午夜巡查的结果") ||
		!strings.Contains((*prompts)[2], "钥匙如何交到守卫手中") {
		t.Fatal("仅摘要的候选应该退回，并按两条原正文意见补做一次")
	}
}

func TestNovelReviewRevisionCorrectsRemainingIssuesAndKeepsVerifiedProgress(t *testing.T) {
	const originalBody = "灯芯忽明忽暗。\n午前他写好午夜巡查的结果。\n柜门仍锁着，他已拿走钥匙。"
	const originalSummary = "他提前记录巡查结果并带走钥匙。"
	const firstBody = "灯芯忽明忽暗。\n午前他记下午夜巡查的安排。\n柜门还锁着，他让守卫拿走钥匙。"
	const firstSummary = "他提前安排巡查，让守卫拿走钥匙。"
	const finalBody = "灯芯忽明忽暗。\n午前他记下午夜巡查的安排。\n柜门还锁着，他只请守卫留在门口。"
	const finalSummary = "他提前安排巡查，请守卫留在柜门口。"
	const timingIssue = "【明确矛盾】午前无法知道午夜巡查的结果。"
	const keyIssue = "【缺少交代】柜门仍锁着，没人能够拿走钥匙。"
	const firstEdit = `{"edits":[{"issueIds":[1],"before":"午前他写好午夜巡查的结果。","after":"午前他记下午夜巡查的安排。"},
		{"issueIds":[2],"before":"柜门仍锁着，他已拿走钥匙。","after":"柜门还锁着，他让守卫拿走钥匙。"}],"summary":"` + firstSummary + `"}`
	const feedback = "新稿仍让守卫在未开柜门的情况下拿走钥匙。"
	const resolvedFeedback = "时点已经修复，不再重复修改。"
	const firstChecks = `{"checks":[{"id":2,"resolved":false,"reason":"` + feedback + `"},
		{"id":1,"resolved":true,"reason":"` + resolvedFeedback + `"}],"regressions":[]}`
	const targetedEdit = `{"edits":[{"issueIds":[2],"blockId":3,"after":"柜门还锁着，他只请守卫留在门口。"}],"summary":"` + finalSummary + `"}`
	const finalChecks = `{"checks":[{"id":1,"resolved":true},{"id":2,"resolved":true}],"regressions":[]}`
	for _, test := range []struct {
		name     string
		followup []string
		retry    bool
		applied  bool
	}{
		{"定向补做完成剩余意见", []string{targetedEdit, finalChecks}, false, true},
		{"已解决的意见重新出现则保留首稿", []string{
			strings.Replace(targetedEdit, finalSummary, "他提前记录了巡查结果，请守卫留在柜门口。", 1),
			`{"checks":[{"id":1,"resolved":false},{"id":2,"resolved":true}],"regressions":[]}`,
		}, false, false},
		{"补做引入新回归则保留首稿", []string{targetedEdit,
			`{"checks":[{"id":1,"resolved":true},{"id":2,"resolved":true}],"regressions":[
			{"quote":"他只请守卫留在门口","issue":"新设行动冲突","fix":"明确守卫的位置"}]}`,
		}, false, false},
		{"已补过格式时不再发起定向补做", nil, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, repo, writer := newReviewRevisionFixture(t)
			repo.project.Chapters[1] = Chapter{Body: originalBody, Summary: originalSummary, Review: timingIssue + "\n\n" + keyIssue}
			responses := []string{firstEdit, firstChecks}
			if test.retry {
				responses = append([]string{`{"edits":[`}, responses...)
			}
			responses = append(responses, test.followup...)
			prompts := stubReviewRevisionResponses(t, writer, responses...)
			version := repo.project.Version
			project, err := service.Apply(context.Background(), repo.owner, repo.project.ID, Action{
				Kind: "revise_review", Chapter: 2, ModelCode: "writer", Version: version,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(*prompts) != len(responses) || project.Version != version+1 {
				t.Fatalf("改稿超出调用预算或重复保存: %d 次调用，版本 %d", len(*prompts), project.Version)
			}
			chapter := project.Chapters[1]
			wantBody, wantSummary, wantReview := firstBody, firstSummary, keyIssue
			if test.applied {
				wantBody, wantSummary, wantReview = finalBody, finalSummary, ""
			}
			if chapter.Body != wantBody || chapter.Summary != wantSummary || chapter.Review != wantReview ||
				chapter.ReviewFailed || chapter.ReviewPassed != test.applied {
				t.Fatalf("未保留可复核的改稿进展: %+v", chapter)
			}
			if len(test.followup) != 0 {
				var material struct {
					Current struct{ Draft string } `json:"current"`
					Issues  []reviewIssueToVerify  `json:"issues"`
				}
				targeted := (*prompts)[2]
				if err := json.Unmarshal([]byte(targeted[strings.LastIndexByte(targeted, '\n')+1:]), &material); err != nil ||
					material.Current.Draft != firstBody || len(material.Issues) != 1 || material.Issues[0].ID != 2 ||
					material.Issues[0].Text != keyIssue || material.Issues[0].Feedback != feedback || strings.Contains(targeted, resolvedFeedback) {
					t.Fatalf("定向补做必须基于首稿，只提交仍未解决的原编号意见: %+v, %v", material, err)
				}
				blocks := readReviewRevisionBlocks(t, targeted)
				if len(blocks) != 3 || blocks[2].Text != "柜门还锁着，他让守卫拿走钥匙。" {
					t.Fatalf("定向补做应按首稿重新生成定位段落: %+v", blocks)
				}
				verified := (*prompts)[3]
				if err := json.Unmarshal([]byte(verified[strings.LastIndexByte(verified, '\n')+1:]), &material); err != nil ||
					len(material.Issues) != 2 || material.Issues[0].ID != 1 || material.Issues[1].ID != 2 {
					t.Fatalf("补做后应复核所有原意见: %+v, %v", material.Issues, err)
				}
			}
		})
	}
}

func TestNovelReviewRevisionKeepsFirstFixEvenIfLaterCandidateHasFewerIssues(t *testing.T) {
	service, repo, writer := newReviewRevisionFixture(t)
	const keyIssue = "钥匙仍锁着，不能带走。"
	const badgeIssue = "令牌尚未找到，不能出示。"
	repo.project.Chapters[1] = Chapter{
		Body:    "他预先知道午夜的结果。钥匙仍锁着，他却带走了钥匙。令牌没有找到，他却向守卫出示令牌。",
		Summary: "他预先知道结果，带走钥匙并出示令牌。",
		Review:  "他不能预先知道午夜的结果。\n\n" + keyIssue + "\n\n" + badgeIssue,
	}
	prompts := stubReviewRevisionResponses(t, writer,
		`{"edits":[{"issueIds":[1],"before":"他预先知道午夜的结果。","after":"他留在庭院等候午夜。"}],"summary":"他等候午夜，随后带走钥匙并出示令牌。"}`,
		`{"checks":[{"id":1,"resolved":true},{"id":2,"resolved":false},{"id":3,"resolved":false}],"regressions":[]}`,
		`{"edits":[{"issueIds":[2],"before":"钥匙仍锁着，他却带走了钥匙。","after":"钥匙仍锁着，他空手离开。"},
			{"issueIds":[3],"before":"令牌没有找到，他却向守卫出示令牌。","after":"他没有找到令牌，只得向守卫说明。"}],"summary":"他预先知道午夜的结果，没带走钥匙，也没找到令牌。"}`,
		`{"checks":[{"id":1,"resolved":false},{"id":2,"resolved":true},{"id":3,"resolved":true}],"regressions":[]}`,
	)
	project, err := service.Apply(context.Background(), repo.owner, repo.project.ID, Action{
		Kind: "revise_review", Chapter: 2, ModelCode: "writer", Version: repo.project.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if chapter := project.Chapters[1]; len(*prompts) != 4 || chapter.Review != keyIssue+"\n\n"+badgeIssue ||
		chapter.Summary != "他等候午夜，随后带走钥匙并出示令牌。" || !strings.HasPrefix(chapter.Body, "他留在庭院等候午夜。") {
		t.Fatal("未解决数量减少，也不能撤销首轮已经核实的修复")
	}
}

func TestNovelReviewRevisionRejectsRepeatedSummaryOnlyWhenBodyNeedsWork(t *testing.T) {
	service, repo, writer := newReviewRevisionFixture(t)
	original := Chapter{
		Body: "钥匙仍锁在柜中。守卫已拿着钥匙离开。", Summary: "守卫拿走了钥匙。", Review: "缺少打开柜门和交接钥匙的过程。",
	}
	repo.project.Chapters[1] = original
	const noEdits = `{"edits":[],"summary":"钥匙仍锁在柜中。"}`
	const unresolved = `{"checks":[{"id":1,"resolved":false,"scope":"body"}],"regressions":[]}`
	const wrongScope = `{"checks":[{"id":1,"resolved":true,"scope":"body"}],"regressions":[]}`
	prompts := stubReviewRevisionResponses(t, writer, noEdits, unresolved, noEdits, wrongScope)
	version := repo.project.Version
	_, err := service.Apply(context.Background(), repo.owner, repo.project.ID, Action{
		Kind: "revise_review", Chapter: 2, ModelCode: "writer", Version: version,
	})
	if !errors.Is(err, ErrGeneration) || len(*prompts) != 4 || repo.project.Version != version || repo.project.Chapters[1] != original {
		t.Fatalf("仅摘要的两次无效候选不应保存、清除正文意见或额外补做: %v", err)
	}
}

func TestNovelReviewRevisionKeepsOriginalWhenNoIssueIsResolved(t *testing.T) {
	service, repo, writer := newReviewRevisionFixture(t)
	original := Chapter{Body: "他穿过庭院，袖中没有钥匙。", Summary: "他独自走过庭院。", Review: "需要交代他之后如何拿到钥匙。"}
	repo.project.Chapters[1] = original
	const unresolved = `{"checks":[{"id":1,"resolved":false,"reason":"仍未交代钥匙的来源。"}],"regressions":[]}`
	prompts := stubReviewRevisionResponses(t, writer,
		`{"edits":[{"issueIds":[1],"before":"他穿过庭院","after":"他走过长廊"}],"summary":"他独自走过长廊。"}`,
		unresolved,
		`{"edits":[{"issueIds":[1],"blockId":1,"after":"他踱至正厅，袖中没有钥匙。"}],"summary":"他独自踱至正厅。"}`,
		unresolved,
	)
	version := repo.project.Version
	_, err := service.Apply(context.Background(), repo.owner, repo.project.ID, Action{
		Kind: "revise_review", Chapter: 2, ModelCode: "writer", Version: version,
	})
	if !errors.Is(err, ErrGeneration) || len(*prompts) != 4 || repo.project.Version != version || repo.project.Chapters[1] != original {
		t.Fatalf("补做后仍没有解决任何原意见时，应保留原稿且不再调用模型: %v", err)
	}
}

func TestNovelReviewRevisionRestartsFromOriginalWhenFirstCandidateResolvesNothing(t *testing.T) {
	service, repo, writer := newReviewRevisionFixture(t)
	const keyLine = "他进庭院时还没有钥匙，却立刻开了柜门。"
	const timeLine = "午前他写好午夜巡查的结果。"
	const tail = "\r\n\r\n雨声越过檐角。"
	const originalBody = keyLine + "\r\n" + timeLine + tail
	const keyIssue = "没有钥匙时不能直接打开锁着的柜门。"
	const timeIssue = "午前不能提前知道午夜巡查结果。"
	const keyFeedback = "首稿改了地点，但仍未交代开锁前提。"
	const timeFeedback = "首稿仍把午夜巡查写成已经完成。"
	repo.project.Chapters[1] = Chapter{
		Body: originalBody, Summary: "他立即开柜，并记录巡查结果。", Review: keyIssue + "\n\n" + timeIssue,
	}
	prompts := stubReviewRevisionResponses(t, writer,
		`{"edits":[{"issueIds":[1],"blockId":1,"after":"他经过长廊时还没有钥匙，却立刻开了柜门。"},
			{"issueIds":[2],"blockId":2,"after":"他午前坐在案前，写好午夜巡查的结果。"}],"summary":"他开柜并提前记录结果。"}`,
		`{"checks":[{"id":2,"resolved":false,"reason":"`+timeFeedback+`"},{"id":1,"resolved":false,"reason":"`+keyFeedback+`"}],"regressions":[]}`,
		`{"edits":[{"issueIds":[1],"blockId":1,"after":"他进庭院时还没有钥匙，只得等守卫来开柜。"},
			{"issueIds":[2],"blockId":2,"after":"午前他记下午夜巡查的安排。"}],"summary":"他等候开柜，并记录巡查安排。"}`,
		`{"checks":[{"id":1,"resolved":true},{"id":2,"resolved":true}],"regressions":[]}`,
	)
	version := repo.project.Version
	project, err := service.Apply(context.Background(), repo.owner, repo.project.ID, Action{
		Kind: "revise_review", Chapter: 2, ModelCode: "writer", Version: version,
	})
	if err != nil {
		t.Fatal(err)
	}
	const wantBody = "他进庭院时还没有钥匙，只得等守卫来开柜。\r\n午前他记下午夜巡查的安排。" + tail
	chapter := project.Chapters[1]
	if len(*prompts) != 4 || project.Version != version+1 || chapter.Body != wantBody || !chapter.ReviewPassed || chapter.Review != "" {
		t.Fatalf("补做应修正两个问题并且只保存一次: %+v", chapter)
	}
	var material struct {
		Current struct{ Draft string } `json:"current"`
		Issues  []reviewIssueToVerify  `json:"issues"`
	}
	retry := (*prompts)[2]
	if err := json.Unmarshal([]byte(retry[strings.LastIndexByte(retry, '\n')+1:]), &material); err != nil ||
		material.Current.Draft != originalBody || len(material.Issues) != 2 ||
		material.Issues[0] != (reviewIssueToVerify{ID: 1, Text: keyIssue, Feedback: keyFeedback}) ||
		material.Issues[1] != (reviewIssueToVerify{ID: 2, Text: timeIssue, Feedback: timeFeedback}) {
		t.Fatalf("全部意见未通过时应按原稿重新补做，并逐条提供复核反馈: %+v, %v", material, err)
	}
	blocks := readReviewRevisionBlocks(t, retry)
	if len(blocks) != 3 || blocks[0].Text != keyLine || blocks[1].Text != timeLine {
		t.Fatalf("应根据原稿定位，不使用未通过复核的首稿: %+v", blocks)
	}
}

func TestNovelReviewRevisionKeepsBodyWithSummaryAndNotApplicableIssues(t *testing.T) {
	service, repo, writer := newReviewRevisionFixture(t)
	original := Chapter{
		Body: "他只在纸上记录午夜巡查的计划。", Summary: "他提前记录巡查结果。",
		Review: "正文提前记录了巡查结果。\n\n【仅摘要】摘要把计划写成了结果。",
	}
	repo.project.Chapters[1] = original
	prompts := stubReviewRevisionResponses(t, writer,
		`{"edits":[],"summary":"他提前记录巡查计划。"}`,
		`{"checks":[{"id":1,"resolved":true,"scope":"not_applicable"},{"id":2,"resolved":true,"scope":"summary"}],"regressions":[]}`,
	)
	project, err := service.Apply(context.Background(), repo.owner, repo.project.ID, Action{
		Kind: "revise_review", Chapter: 2, ModelCode: "writer", Version: repo.project.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	chapter := project.Chapters[1]
	if len(*prompts) != 2 || chapter.Body != original.Body || chapter.Summary != "他提前记录巡查计划。" || !chapter.ReviewPassed || chapter.Review != "" {
		t.Fatal("不适用的审稿意见不应迫使修改原稿")
	}
}

func TestNovelReviewRevisionKeepsDraftWhenNoIssueApplies(t *testing.T) {
	service, repo, writer := newReviewRevisionFixture(t)
	original := Chapter{
		Body: "他只在纸上记录午夜巡查的计划。", Summary: "他提前记录巡查计划。", Review: "正文提前记录了巡查结果。",
	}
	repo.project.Chapters[1] = original
	prompts := stubReviewRevisionResponses(t, writer,
		`{"edits":[],"summary":"他提前记录巡查计划。"}`,
		`{"checks":[{"id":1,"resolved":true,"scope":"not_applicable"}],"regressions":[]}`,
	)
	project, err := service.Apply(context.Background(), repo.owner, repo.project.ID, Action{
		Kind: "revise_review", Chapter: 2, ModelCode: "writer", Version: repo.project.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	chapter := project.Chapters[1]
	if len(*prompts) != 2 || chapter.Body != original.Body || chapter.Summary != original.Summary || !chapter.ReviewPassed || chapter.Review != "" {
		t.Fatal("原意见确实不适用时不应编造正文或摘要改动")
	}
}

func TestNovelReviewRevisionTargetsOneOfTwoIdenticalBlocks(t *testing.T) {
	service, repo, writer := newReviewRevisionFixture(t)
	const repeated = "他望见门外亮着灯。"
	const changed = "他再望向门外，灯已经熄了。"
	const untouchedTail = "\r\n\r\n\t旁人没有回头。\n院中风过树梢。"
	const body = repeated + "\r\n" + repeated + untouchedTail
	repo.project.Chapters[1] = Chapter{
		Body: body, Summary: "门外一直亮着灯。", Review: "第二次望向门外时灯应已熄灭。",
	}
	prompts := stubReviewRevisionResponses(t, writer,
		`{"edits":[{"issueIds":[1],"blockId":2,"after":"`+changed+`"}],"summary":"第二次看时，门外的灯已熄灭。"}`,
		`{"checks":[{"id":1,"resolved":true}],"regressions":[]}`,
	)
	version := repo.project.Version
	project, err := service.Apply(context.Background(), repo.owner, repo.project.ID, Action{
		Kind: "revise_review", Chapter: 2, ModelCode: "writer", Version: version,
	})
	if err != nil {
		t.Fatal(err)
	}
	chapter := project.Chapters[1]
	if len(*prompts) != 2 || chapter.Body != repeated+"\r\n"+changed+untouchedTail || !chapter.ReviewPassed || project.Version != version+1 {
		t.Fatalf("应只应用指定的重复段并保留原有换行与缩进: %+v", chapter)
	}
	blocks := readReviewRevisionBlocks(t, (*prompts)[0])
	if len(blocks) < 2 || blocks[0].ID != 1 || blocks[1].ID != 2 || blocks[0].Text != repeated || blocks[1].Text != repeated {
		t.Fatalf("提示词未对相同正文段落提供不同的编号: %+v", blocks)
	}
}

func TestNovelReviewRevisionRejectsInvalidBlockTargetsWithoutGuessing(t *testing.T) {
	const original = "她说：“拿走钥匙。”\n他把柜门锁上。"
	const exact = "她说：“拿走钥匙。”"
	after, first, zero, unknown := "她说：“钥匙先留在柜里。”", 1, 0, 50
	issues := []reviewIssueToVerify{{ID: 1, Text: "钥匙仍需留在柜里。"}}
	for _, test := range []struct {
		name  string
		edits []reviewEdit
	}{
		{"编号为零", []reviewEdit{{IssueIDs: []int{1}, BlockID: &zero, After: &after}}},
		{"编号不存在", []reviewEdit{{IssueIDs: []int{1}, BlockID: &unknown, After: &after}}},
		{"重复替换同一编号", []reviewEdit{
			{IssueIDs: []int{1}, BlockID: &first, After: &after}, {IssueIDs: []int{1}, BlockID: &first, After: &after},
		}},
		{"同时给出编号与原句", []reviewEdit{{IssueIDs: []int{1}, BlockID: &first, Before: exact, After: &after}}},
		{"错误编号不得回退至原句", []reviewEdit{{IssueIDs: []int{1}, BlockID: &unknown, Before: exact, After: &after}}},
		{"旧协议不得猜测引号", []reviewEdit{{IssueIDs: []int{1}, Before: "她说：‘拿走钥匙。’", After: &after}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := applyReviewEdits(original, test.edits, issues)
			var output *DraftOutputError
			if !errors.As(err, &output) {
				t.Fatalf("定位信息无效时必须拒绝写回: %v", err)
			}
		})
	}
}

func TestNovelReviewRevisionSplitsLongUnbrokenDraftIntoBoundedBlocks(t *testing.T) {
	service, repo, _ := newReviewRevisionFixture(t)
	const end = "句尾。"
	body := strings.Repeat("甲乙丙", 540) + end
	repo.project.Chapters[1] = Chapter{Body: body}
	prompt := service.chapterRevisionPrompt(context.Background(), repo.owner, repo.project, 1, []reviewIssueToVerify{{ID: 1, Text: "核对结尾。"}})
	blocks := readReviewRevisionBlocks(t, prompt)
	if len(blocks) < 3 {
		t.Fatalf("无换行的长正文未拆成足够小的段落: %d 段", len(blocks))
	}
	var joined strings.Builder
	for i, block := range blocks {
		if block.ID != i+1 || !utf8.ValidString(block.Text) || utf8.RuneCountInString(block.Text) > 800 {
			t.Fatalf("长正文的分段编号、字符边界或长度不正确: 第 %d 段", i+1)
		}
		joined.WriteString(block.Text)
	}
	if joined.String() != body {
		t.Fatal("拆分长正文时丢失或改变了原文")
	}
}

type revisionPromptBlock struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
}

func readReviewRevisionBlocks(t *testing.T, prompt string) []revisionPromptBlock {
	t.Helper()
	var material struct {
		Blocks []revisionPromptBlock `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(prompt[strings.LastIndexByte(prompt, '\n')+1:]), &material); err != nil {
		t.Fatalf("修订提示词末尾应包含结构化段落: %v", err)
	}
	return material.Blocks
}

func TestNovelReviewRevisionRejectsInvalidAnchorsWithinRetryBudget(t *testing.T) {
	for _, test := range []struct {
		name, edits string
	}{
		{"原文不存在", `[{"issueIds":[1],"before":"院里落着白雪。","after":"院里落着春雨。"}]`},
		{"同一原文出现两处", `[{"issueIds":[1],"before":"庭院里风很急。","after":"庭院里风小了。"}]`},
		{"原文的两处匹配互相重叠", `[{"issueIds":[1],"before":"快走！快走！","after":"别停下！"}]`},
		{"两处改动重叠", `[{"issueIds":[1],"before":"钟声越过屋檐。","after":"钟声没有传来。"},
			{"issueIds":[1],"before":"越过屋檐。他没有回头。","after":"响过一声。他回头。"}]`},
		{"原样返回", `[{"issueIds":[1],"before":"钟声越过屋檐。","after":"钟声越过屋檐。"}]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, repo, writer := newReviewRevisionFixture(t)
			original := Chapter{
				Body: "庭院里风很急。钟声越过屋檐。他没有回头。庭院里风很急。快走！快走！快走！", Summary: "他听到钟声。", Review: "核对钟声的来源。",
			}
			repo.project.Chapters[1] = original
			invalid := `{"edits":` + test.edits + `,"summary":"他在庭院。"}`
			prompts := stubReviewRevisionResponses(t, writer, invalid, invalid)
			version := repo.project.Version
			_, err := service.Apply(context.Background(), repo.owner, repo.project.ID, Action{
				Kind: "revise_review", Chapter: 2, ModelCode: "writer", Version: version,
			})
			if !errors.Is(err, ErrGeneration) || len(*prompts) != 2 || repo.project.Version != version || repo.project.Chapters[1] != original {
				t.Fatalf("无效局部修改不得复核或写回，且最多补做一次: %v", err)
			}
		})
	}
}

func TestNovelReviewRevisionPreservesLiteralWhitespaceInModelJSON(t *testing.T) {
	service, repo, writer := newReviewRevisionFixture(t)
	repo.project.Chapters[1] = Chapter{
		Body: "他在午后经过庭院。", Summary: "他经过庭院。", Review: "【明确矛盾】正文误写成午后。",
	}
	const body = "午夜，他唤道：\"皇兄。\"\r\n\t钥匙被他交给守卫。\n守卫收下钥匙。"
	prompts := stubReviewRevisionResponses(t, writer,
		`{"edits":[{"issueIds":[1],"before":"他在午后经过庭院。","after":"午夜，他唤道：\"皇兄。\"`+"\r\n\t"+
			`钥匙被他交给守卫。\n守卫收下钥匙。"}],"summary":"午夜交接钥匙。"}`,
		`{"checks":[{"id":1,"resolved":true}],"regressions":[{"quote":"钥匙被他交给守卫","issue":"交接缘由缺失。","fix":"补上交接的缘由。`+
			"\n"+`交代守卫为何接手。"}]}`,
	)
	version := repo.project.Version
	project, err := service.Apply(context.Background(), repo.owner, repo.project.ID, Action{
		Kind: "revise_review", Chapter: 2, ModelCode: "writer", Version: version,
	})
	if err != nil {
		t.Fatal(err)
	}
	chapter := project.Chapters[1]
	if chapter.Body != body || chapter.Summary != "午夜交接钥匙。" || chapter.ReviewFailed || chapter.ReviewPassed ||
		!strings.Contains(chapter.Review, "补上交接的缘由。\n交代守卫为何接手。") || strings.Contains(chapter.Review, "正文误写成午后") {
		t.Fatalf("修订正文和复核意见没有保留原始空白及复核结果: %+v", chapter)
	}
	if len(*prompts) != 2 || project.Version != version+1 {
		t.Fatalf("本地修复不应增加模型调用，实际调用 %d 次，版本 %d", len(*prompts), project.Version)
	}
}

func TestNovelReviewRevisionRetriesIncompleteJSONOnce(t *testing.T) {
	const incomplete = "{\n" + `"edits":[{"issueIds":[1],"before":"原稿误写成午后。","after":"午夜，他经过庭院。` + "\n" + "正文在此中断。"
	for _, test := range []struct {
		name      string
		responses []string
		wantBody  string
	}{
		{"纠正后通过复核", []string{incomplete, `{"edits":[{"issueIds":[1],"before":"原稿误写成午后。","after":"午夜，他等到兄长。"}],"summary":"午夜相见。"}`,
			`{"checks":[{"id":1,"resolved":true}],"regressions":[]}`}, "午夜，他等到兄长。"},
		{"两次都不完整", []string{incomplete, incomplete}, ""},
		{"正文校验和 JSON 错误共用一次补做", []string{`{"summary":"只有摘要。"}`, incomplete}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, repo, writer := newReviewRevisionFixture(t)
			original := Chapter{Body: "原稿误写成午后。", Summary: "兄弟见面。", Review: "【明确矛盾】正文误写成午后。"}
			repo.project.Chapters[1] = original
			version := repo.project.Version
			prompts := stubReviewRevisionResponses(t, writer, test.responses...)
			project, err := service.Apply(context.Background(), repo.owner, repo.project.ID, Action{
				Kind: "revise_review", Chapter: 2, ModelCode: "writer", Version: version,
			})
			if len(*prompts) != len(test.responses) || !strings.Contains((*prompts)[1], "局部改动") ||
				strings.Contains((*prompts)[1], "正文在此中断") {
				t.Fatalf("修订的格式补做次数或反馈错误，调用了 %d 次", len(*prompts))
			}
			if test.wantBody == "" {
				var format *ModelOutputFormatError
				if !errors.Is(err, ErrGeneration) || !errors.As(err, &format) || !strings.Contains(err.Error(), "unexpected EOF") ||
					draftJobErrorMessage(err) != ModelOutputFormatMessage || repo.project.Version != version || repo.project.Chapters[1] != original {
					t.Fatalf("格式补做失败应保留原稿、复核状态和版本: %v", err)
				}
				return
			}
			if err != nil || project.Chapters[1].Body != test.wantBody || !project.Chapters[1].ReviewPassed || project.Version != version+1 {
				t.Fatalf("正确的补做结果未能保存并通过复核: %v", err)
			}
		})
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
