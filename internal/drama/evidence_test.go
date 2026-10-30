package drama

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestEvidenceReferencesUseOriginalPositionsAndMultipleSources(t *testing.T) {
	text := "　　甲没有离开。\r\n　　他拿起信。\r\n　　乙等待。"
	unit := sourceUnit{ID: 3, Start: 100, End: 100 + len([]rune(text)), Text: text}
	raw := json.RawMessage(`{"units":[{"id":"3","event":"甲留下并拿信","facts":[{"claim":"甲没有离开，乙等待","sourceIds":["U3S3","U3S1"],"quote":"伪造引文","startOffset":-999}]}]}`)
	data, err := groundedEvidence(raw, []sourceUnit{unit}, 5000)
	if err != nil {
		t.Fatal(err)
	}
	var d evidenceDigest
	if err = json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	fact := d.Units[0].Facts[0]
	if len(fact.References) != 2 || fact.Quote != "甲没有离开。" || fact.StartOffset != 5102 || fact.References[1].Quote != "乙等待。" {
		t.Fatalf("程序没有正确回填原文：%+v", fact)
	}
	for _, ref := range fact.References {
		if string([]rune(text)[ref.StartOffset-5100:ref.EndOffset-5100]) != ref.Quote {
			t.Fatal("引用偏移未对应完整原文")
		}
	}
	if strings.Contains(string(data), "伪造引文") {
		t.Fatal("不应采用模型填入的引文与位置")
	}
	for _, bad := range []string{"U2S1", "U3S99"} {
		test := strings.Replace(string(raw), "U3S3", bad, 1)
		if _, err = groundedEvidence(json.RawMessage(test), []sourceUnit{unit}, 5000); err == nil {
			t.Fatal("范围外或不存在编号未拒绝")
		}
	}
}
func TestQuoteFormattingCompatibilityKeepsWordsAndPunctuation(t *testing.T) {
	original := "标题\r\n　　😀甲没有离开。\r\n　　乙还在。"
	quote := "😀甲没有离开。\n乙还在。"
	start, end, ok := locateQuote(original, quote)
	if !ok || start != 6 || string([]rune(original)[start:end]) != "😀甲没有离开。\r\n　　乙还在。" {
		t.Fatalf("换行映射失败：%d %d %v", start, end, ok)
	}
	for _, bad := range []string{"😀甲有离开。\n乙还在。", "😀甲没有离开\n乙还在。", "😀甲没有离开。乙还在。"} {
		if quoteExists(original, bad) {
			t.Fatal("不得通过删除否定、标点或拼接原文来匹配")
		}
	}
	for _, id := range []string{`"1.0"`, `1.5`, `null`, `"-1"`, `"abc"`} {
		var unit evidenceUnit
		if err := json.Unmarshal([]byte(`{"id":`+id+`}`), &unit); err == nil {
			t.Fatal("无效编号未拒绝", id)
		}
	}
}
func TestStoryGroupPersistsValidUnitsAndResumesOnlyFailedUnit(t *testing.T) {
	repo := &fakeRepository{}
	calls := 0
	writer := &groundedWriter{}
	group := []sourceUnit{{ID: 1, Text: "甲来到城门。", Start: 0, End: 6}, {ID: 2, Text: "乙留在城内。", Start: 6, End: 12}}
	writer.respond = func(prompt string) (string, error) {
		calls++
		if calls == 1 {
			return `{"units":[{"id":"1","event":"甲到城门","facts":[{"claim":"甲来到城门","sourceIds":["U1S1"]}]},{"id":2,"event":"乙留城内","facts":[{"claim":"乙留城内","sourceIds":["U1S1"]}]}]}`, nil
		}
		if strings.Contains(prompt, "【原文单元 1】") {
			t.Fatal("重试不应再次发送已成功单元")
		}
		return `{"units":[{"id":2,"event":"乙留城内","facts":[{"claim":"乙留城内","sourceIds":["U2S99"]}]}]}`, nil
	}
	service := NewService(repo, writer)
	job := BatchAnalysisJob{WorkflowVersion: 3, ModelCode: "writer"}
	job.Batch.AnalysisRevision = 1
	job.Batch.AnalysisRunID = "run"
	w := &batchWorkflow{service: service, job: job, ctx: context.Background(), saved: map[string]BatchStep{}}
	if _, err := w.readStoryGroup(group, 1, 1, 2); err == nil {
		t.Fatal("无效引用应保留失败状态")
	}
	if calls != 2 {
		t.Fatal("自动重试次数错误")
	}
	saved := map[string]BatchStep{}
	for _, step := range repo.steps {
		saved[fmt.Sprintf("%s:%d", step.Stage, step.ChunkNumber)] = step
	}
	if saved["evidence:1"].Status != "completed" || saved["evidence:2"].Status != "failed" {
		t.Fatalf("通过与失败单元未分开保存：%+v", repo.steps)
	}
	writer.respond = func(prompt string) (string, error) {
		calls++
		if strings.Contains(prompt, "【原文单元 1】") {
			t.Fatal("续跑不应发送已保存单元")
		}
		return `{"units":[{"id":2,"event":"乙留城内","facts":[{"claim":"乙留城内","sourceIds":["U2S1"]}]}]}`, nil
	}
	resumed := &batchWorkflow{service: service, job: job, ctx: context.Background(), saved: saved}
	result, err := resumed.readStoryGroup(group, 1, 1, 2)
	if err != nil || len(result.Units) != 2 || calls != 3 {
		t.Fatalf("续跑异常：%+v %v", result, err)
	}
}
