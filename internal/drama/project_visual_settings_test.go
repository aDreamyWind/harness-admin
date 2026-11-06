package drama

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestUpdateProjectVisualSettingsValidatesAndUpdatesIndependently(t *testing.T) {
	ctx := context.Background()
	id := uuid.NewString()
	repo := &fakeRepository{project: Project{
		ID: id, OrganizationID: "org", OwnerID: "owner",
		Style: "旧版水墨", AspectRatio: "9:16", Overview: []byte(`{"protagonist":"青川"}`),
	}}
	service := NewService(repo, nil).WithStyles(styleRepoStub{})
	ratio, invalidRatio, empty := "16:9", "1:1", ""
	styleID, unknownStyle := " dark-concept ", "黑暗原画概念风"
	for _, input := range []ProjectVisualSettingsInput{
		{}, {AspectRatio: &invalidRatio}, {StyleID: &empty}, {StyleID: &styleID, AspectRatio: &invalidRatio},
	} {
		if _, err := service.UpdateProjectVisualSettings(ctx, "org", "owner", id, input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("无效画面设置未被拒绝：%v", err)
		}
	}
	if _, err := service.UpdateProjectVisualSettings(ctx, "org", "owner", id, ProjectVisualSettingsInput{StyleID: &unknownStyle}); !errors.Is(err, ErrStyleUnavailable) {
		t.Fatalf("风格显示名称不应作为目录标识：%v", err)
	}
	if _, err := service.UpdateProjectVisualSettings(ctx, "org", "other", id, ProjectVisualSettingsInput{AspectRatio: &ratio}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("其他用户不能修改项目：%v", err)
	}
	saved, err := service.UpdateProjectVisualSettings(ctx, "org", "owner", id, ProjectVisualSettingsInput{AspectRatio: &ratio})
	if err != nil || saved.AspectRatio != ratio || saved.Style != "" || saved.StyleID != "" || repo.project.Style != "旧版水墨" {
		t.Fatalf("单独修改比例应兼容旧风格：%+v %v", saved, err)
	}
	saved, err = service.UpdateProjectVisualSettings(ctx, "org", "owner", id, ProjectVisualSettingsInput{StyleID: &styleID})
	if err != nil || saved.StyleID != "dark-concept" || saved.Style != "黑暗原画概念风" || saved.AspectRatio != "" || repo.project.AspectRatio != ratio {
		t.Fatalf("单独修改风格应保留比例并返回目录名称：%+v %v", saved, err)
	}
	if string(repo.project.Overview) != `{"protagonist":"青川"}` {
		t.Fatalf("故事概览被改写：%s", repo.project.Overview)
	}
}
