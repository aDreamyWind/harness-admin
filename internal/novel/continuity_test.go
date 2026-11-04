package novel

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestChapterContextIncludesRecentProseAndOnlyFinalizedHistory(t *testing.T) {
	book := Project{
		Workspace: Workspace{
			Outline: &Outline{Beats: []Beat{{Title: "旧章"}, {Title: "长章"}, {Title: "前章"}, {Title: "本章"}, {Title: "下一章"}}},
			Chapters: []Chapter{
				{Final: true, Body: "更早的正文不发送", Summary: "旧线索仍未回收"},
				{Final: true, Body: "开头的时间" + strings.Repeat("中", 8000) + "结尾的证据", Summary: "长章摘要"},
				{Body: "未定稿内容不发送", Summary: "未定稿摘要不发送"},
				{Body: "本章草稿", Summary: "本章待审摘要", Review: "上一轮审稿"},
				{Body: "下一章草稿不发送"},
			},
		},
	}
	var got struct {
		NextBeat      Beat
		DraftSummary  string
		Review        string
		PriorChapters []struct {
			Number         int
			Summary        string
			FinalizedProse string
		}
	}
	raw := chapterContext(book, 3, "", "")
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.PriorChapters) != 2 || got.PriorChapters[0].Summary != "旧线索仍未回收" || got.PriorChapters[0].FinalizedProse != "" {
		t.Fatalf("较早的章节应只提供定稿摘要: %+v", got.PriorChapters)
	}
	recent := got.PriorChapters[1]
	if recent.Number != 2 || !strings.HasPrefix(recent.FinalizedProse, "开头的时间") ||
		!strings.HasSuffix(recent.FinalizedProse, "结尾的证据") ||
		!strings.Contains(recent.FinalizedProse, "中间正文省略") || utf8.RuneCountInString(recent.FinalizedProse) > recentChapterRunes {
		t.Fatal("近期长章应保留开头和结尾，且遵守正文预算")
	}
	if got.NextBeat.Title != "下一章" || got.DraftSummary != "本章待审摘要" || got.Review != "" ||
		strings.Contains(raw, "不发送") || strings.Contains(raw, "上一轮审稿") {
		t.Fatal("审稿上下文包含了未定稿内容或旧意见，或缺少本章摘要与后续目标")
	}
}

func TestLongChapterContextLimitsHistoryAndPreservesRecentContinuity(t *testing.T) {
	const chapterCount = 500
	fullSummary := "【时间】从日出到日落。" + strings.Repeat("事", 460) + "【线索】古印仍在守塔人手中。"
	book := Project{Workspace: Workspace{
		Outline:  &Outline{Beats: make([]Beat, chapterCount)},
		Chapters: make([]Chapter, chapterCount),
	}}
	for i := range book.Chapters {
		book.Chapters[i] = Chapter{Final: true, Body: "定稿正文", Summary: fullSummary}
	}
	book.Chapters[100] = Chapter{Body: "不应进入上下文的草稿"}
	book.Chapters[chapterCount-1] = Chapter{}

	var got struct{ PriorChapters []priorChapter }
	if err := json.Unmarshal([]byte(chapterContext(book, chapterCount-1, "", "")), &got); err != nil {
		t.Fatal(err)
	}
	history := got.PriorChapters
	if len(history) != 30 || history[0].Number != 1 || history[9].Number != 479 || history[10].Number != 480 || history[29].Number != 499 {
		t.Fatal("应只提供近期完整摘要和覆盖较早阶段的摘要样本")
	}
	for i, entry := range history {
		if entry.Number == 101 || (i > 0 && entry.Number <= history[i-1].Number) {
			t.Fatal("历史上下文应只包含按章号排序的定稿")
		}
		if i < 10 {
			if utf8.RuneCountInString(entry.Summary) > sampledSummaryRunes ||
				!strings.HasPrefix(entry.Summary, "【时间】") || !strings.HasSuffix(entry.Summary, "古印仍在守塔人手中。") {
				t.Fatal("较早的摘要应压缩并保留首尾的时间和线索")
			}
		} else if entry.Summary != fullSummary {
			t.Fatal("近期摘要应完整保留")
		}
		if (entry.FinalizedProse != "") != (entry.Number >= 498) {
			t.Fatal("应只提供前两章的定稿正文")
		}
	}
	for _, entry := range priorChapters(book, len(book.Chapters), false) {
		if entry.FinalizedProse != "" {
			t.Fatal("续章规划只需要历史摘要")
		}
	}
}
