package novel

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestGeneratedOutlineRepairsChapterBeforeFirstSave(t *testing.T) {
	ctx := context.Background()
	owner := Owner{OrganizationID: "org", UserID: "author"}
	repo, writer := &memoryRepository{}, &testWriter{
		result: `{"synopsis":"让谷地重新通航","world":"燃料需要干燥储存","characters":"舟工与守闸人","beats":[{"title":"断供","goal":"风暴淹没仓库，船上没有可用燃料"},{"title":"渡河","goal":"舟工驾驶燃油船救人"},{"title":"新航路","goal":"补给船送来第一批干燥燃料，舟工测试新航路"}]}`,
	}
	service := NewService(repo, writer, nil, nil)
	p, err := service.Create(ctx, owner, CreateInput{Title: "谷地船工", Brief: "风暴后重新打通航路", ChapterCount: 3})
	if err != nil {
		t.Fatal(err)
	}
	var stages []string
	writer.outlineCheck = func(prompt string) (string, error) {
		if repo.project.Outline != nil || repo.project.Version != p.Version {
			t.Fatal("通过复核之前已保存大纲")
		}
		switch {
		case strings.HasPrefix(prompt, outlineCheckPrompt):
			stages = append(stages, "检查")
			return `{"issues":[{"evidence":[{"chapter":2,"quote":"舟工驾驶燃油船救人"},{"chapter":3,"quote":"补给船送来第一批干燥燃料"}],"issue":"救援船在补给到达前无法使用燃油。","fix":"第2章改为沿岸接应和准备救援。"}]}`, nil
		case strings.HasPrefix(prompt, outlineRepairPrompt):
			stages = append(stages, "修订")
			return `{"changes":[{"chapter":2,"goal":"舟工划桨沿岸接应，保留燃油船等待补给"}]}`, nil
		case strings.HasPrefix(prompt, outlineVerifyPrompt):
			stages = append(stages, "复核")
			if !strings.Contains(prompt, "舟工划桨沿岸接应") {
				t.Fatal("复核没有读取修订后的章纲")
			}
			return `{"checks":[{"id":1,"resolved":true}],"regressions":[]}`, nil
		default:
			t.Fatal("出现意外的模型调用")
			return "", nil
		}
	}
	next, err := service.Apply(ctx, owner, p.ID, Action{Kind: "generate_outline", Version: p.Version, ModelCode: "writer"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(stages, []string{"检查", "修订", "复核"}) || next.Version != p.Version+1 || next.Confirmed ||
		next.OutlineCheck == nil || next.OutlineCheck.Status != OutlineCheckPassed || !next.OutlineCheck.Repaired {
		t.Fatalf("首次大纲未按顺序完成检查和保存: %v %+v", stages, next)
	}
	if next.Outline == nil || next.Outline.Beats[1].Goal != "舟工划桨沿岸接应，保留燃油船等待补给" ||
		next.Outline.Beats[0].Goal != "风暴淹没仓库，船上没有可用燃料" ||
		next.Outline.Beats[2].Goal != "补给船送来第一批干燥燃料，舟工测试新航路" {
		t.Fatalf("没有按复核结果局部修改大纲: %+v", next.Outline)
	}
}

func TestOutlineCheckTracksSavedDraftAndAuthorGuidance(t *testing.T) {
	ctx := context.Background()
	owner := Owner{OrganizationID: "org", UserID: "author"}
	service := NewService(&memoryRepository{}, &testWriter{}, nil, nil)
	p, err := service.Create(ctx, owner, CreateInput{Title: "雾笛", Brief: "守塔人发现呼救", ChapterCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	apply := func(action Action) {
		t.Helper()
		action.Version = p.Version
		p, err = service.Apply(ctx, owner, p.ID, action)
		if err != nil {
			t.Fatal(err)
		}
	}
	draft := &Outline{Synopsis: "追查呼救", World: "孤岛", Characters: "守塔人", Beats: []Beat{{Title: "雾笛", Goal: "发现异常信号"}}}
	check := Action{Kind: "check_outline", ModelCode: "writer"}
	check.Outline = draft
	apply(check)
	if p.Outline == nil || p.OutlineCheck == nil || p.OutlineCheck.Status != OutlineCheckPassed {
		t.Fatalf("手写大纲未保存检查结果: %+v", p)
	}
	apply(Action{Kind: "save_outline", Outline: draft})
	if p.OutlineCheck == nil {
		t.Fatal("保存相同大纲应保留检查结果")
	}
	edited := *draft
	edited.World = "常年大雾的孤岛"
	apply(Action{Kind: "save_outline", Outline: &edited})
	if p.OutlineCheck != nil {
		t.Fatal("大纲改动后保留了过期的检查结果")
	}
	check.Outline = nil
	apply(check)
	if p.OutlineCheck == nil || p.OutlineCheck.Status != OutlineCheckPassed {
		t.Fatalf("已保存大纲无法重新检查: %+v", p.OutlineCheck)
	}
	guidance := "大雾持续时船只不能进入港口"
	apply(Action{Kind: "save_author_guidance", AuthorGuidance: &guidance})
	if p.OutlineCheck != nil {
		t.Fatal("写作约定改动后保留了过期的检查结果")
	}
}
