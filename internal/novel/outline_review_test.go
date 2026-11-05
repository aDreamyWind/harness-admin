package novel

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const (
	testOutlineFinding = `{"issues":[{"evidence":[{"chapter":1,"quote":"播种救急"},{"chapter":2,"quote":"灌溉水才首次送到田头"}],"issue":"第一章把播种写成救急，第二章才有灌溉用水。","fix":"先用现有存粮救急，通水后再安排播种。"}]}`
	testOutlinePatch   = `{"changes":[{"chapter":1,"title":"开仓备种","goal":"开仓用存粮救急，登记种子备下，等待灌溉水到田头。"},{"chapter":2,"goal":"拆除私闸后确认水到田头，再组织试种。"}]}`
	testOutlinePass    = `{"checks":[{"id":1,"resolved":true}],"regressions":[]}`
)

type outlineCheckTestWriter struct {
	responses []string
	prompts   []string
}

func (w *outlineCheckTestWriter) Models(context.Context, Owner) ([]Model, error) {
	return nil, nil
}

func (w *outlineCheckTestWriter) Complete(_ context.Context, _ Owner, _ string, prompt string) (string, error) {
	index := len(w.prompts)
	w.prompts = append(w.prompts, prompt)
	if index >= len(w.responses) || w.responses[index] == "" {
		return "", errors.New("模拟上游失败")
	}
	return w.responses[index], nil
}

func testCausalOutline() Outline {
	return Outline{
		Synopsis: "县令赈灾并查出隐匿水源的人。", World: "河道被私闸截断，官仓尚有存粮。", Characters: "县令与负责渠务的老周。",
		Beats: []Beat{
			{Title: "播种救急", Goal: "灾民播下谷种，次日就靠新粮渡过饥荒。"},
			{Title: "水渠开通", Goal: "查出私闸后重新放水，灌溉水才首次送到田头。"},
			{Title: "核查账册", Goal: "县令据私闸用料记录追查幕后的人。"},
		},
	}
}

func TestOutlineCheckRepairsOnlyCitedChaptersAfterVerification(t *testing.T) {
	writer := &outlineCheckTestWriter{responses: []string{testOutlineFinding, testOutlinePatch, testOutlinePass}}
	s := NewService(nil, writer, nil, nil)
	original := testCausalOutline()
	outline := original
	p := &Project{Brief: "县令赈灾", Workspace: Workspace{StoryMode: StoryModeSerial, AuthorGuidance: "保持救灾写实"}}
	check := s.checkOutline(context.Background(), Owner{}, p, "writer", "以恢复水渠为主线", &outline)
	if check.Status != OutlineCheckPassed || !check.Repaired || len(check.Issues) != 0 {
		t.Fatalf("局部修订未通过: %+v", check)
	}
	if outline.Beats[0].Title != "开仓备种" || !strings.Contains(outline.Beats[1].Goal, "再组织试种") || outline.Beats[1].Title != original.Beats[1].Title {
		t.Fatalf("局部补丁没有仅替换相应字段: %+v", outline.Beats)
	}
	want := testCausalOutline()
	if !reflect.DeepEqual(original, want) || len(outline.Beats) != len(want.Beats) || outline.Beats[2] != want.Beats[2] || outline.Synopsis != want.Synopsis || outline.World != want.World || outline.Characters != want.Characters {
		t.Fatalf("原稿或无关设定被改动: %+v %+v", original, outline)
	}
	if len(writer.prompts) != 3 {
		t.Fatalf("检查、修订、复核的调用次数无效: %d", len(writer.prompts))
	}
	for _, material := range []string{p.Brief, p.AuthorGuidance, "以恢复水渠为主线", want.Synopsis, want.World, want.Characters, want.Beats[2].Goal} {
		if !strings.Contains(writer.prompts[0], material) {
			t.Errorf("独立检查缺少创作资料 %q", material)
		}
	}
	if !strings.Contains(writer.prompts[2], `"id":1`) || !strings.Contains(writer.prompts[2], want.Beats[0].Goal) || !strings.Contains(writer.prompts[2], outline.Beats[0].Goal) {
		t.Errorf("复核缺少新旧章节或原意见编号")
	}
}

func TestOutlineCheckOnlyAcceptsGroundedAndVerifiedResults(t *testing.T) {
	cases := []struct {
		name      string
		responses []string
		status    string
		issues    int
	}{
		{"明确没有问题时通过", []string{`{"issues":[]}`}, OutlineCheckPassed, 0},
		{"模型调用失败", []string{""}, OutlineCheckUnavailable, 0},
		{"缺少检查结果", []string{`{}`}, OutlineCheckUnavailable, 0},
		{"拒绝错章原文", []string{strings.Replace(testOutlineFinding, `"chapter":2`, `"chapter":3`, 1)}, OutlineCheckUnavailable, 0},
		{"拒绝修改未引用的章节", []string{testOutlineFinding, `{"changes":[{"chapter":3,"goal":"换掉不相关目标"}]}`}, OutlineCheckNeedsReview, 1},
		{"拒绝重复章号", []string{testOutlineFinding, `{"changes":[{"chapter":1,"title":"开仓"},{"chapter":1,"goal":"先开仓放粮"}]}`}, OutlineCheckNeedsReview, 1},
		{"无安全修法时保留原稿", []string{testOutlineFinding, `{"changes":[]}`}, OutlineCheckNeedsReview, 1},
		{"复核失败保留原稿", []string{testOutlineFinding, testOutlinePatch, ""}, OutlineCheckNeedsReview, 1},
		{"复核没有明确结果", []string{testOutlineFinding, testOutlinePatch, `{"checks":[{"id":1}],"regressions":[]}`}, OutlineCheckNeedsReview, 1},
		{"原硬伤仍在时保留原稿", []string{testOutlineFinding, testOutlinePatch, `{"checks":[{"id":1,"resolved":false}],"regressions":[]}`}, OutlineCheckNeedsReview, 1},
		{"引入新硬伤时保留原稿", []string{testOutlineFinding, strings.Replace(testOutlinePatch, "再组织试种", "先把渠水全部改送官府，再组织试种", 1), `{"checks":[{"id":1,"resolved":true}],"regressions":[{"evidence":[{"chapter":2,"quote":"先把渠水全部改送官府"}],"issue":"第二章把渠水全改送官府，又直接安排需要渠水的试种。","fix":"试种前先确保田头留有灌溉水。"}]}`}, OutlineCheckNeedsReview, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writer := &outlineCheckTestWriter{responses: tc.responses}
			s := NewService(nil, writer, nil, nil)
			outline := testCausalOutline()
			check := s.checkOutline(context.Background(), Owner{}, &Project{}, "writer", "", &outline)
			if check.Status != tc.status || check.Repaired || len(check.Issues) != tc.issues || len(writer.prompts) != len(tc.responses) {
				t.Fatalf("检查状态、意见或调用次数无效: %+v %d", check, len(writer.prompts))
			}
			if !reflect.DeepEqual(outline, testCausalOutline()) {
				t.Fatalf("原稿被未验证的修改替换: %+v", outline)
			}
			if tc.issues > 0 && (check.Issues[0].Chapter != 1 || check.Issues[0].Quote != "播种救急") {
				t.Fatalf("原始意见丢失定位证据: %+v", check.Issues[0])
			}
		})
	}
}
