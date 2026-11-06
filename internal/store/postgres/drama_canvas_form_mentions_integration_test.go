package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/drama"
)

func TestDramaCanvasFormMentionsSaveAtomically(t *testing.T) {
	databaseURL := os.Getenv("HARNESS_TEST_WRITE_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("设置测试数据库后验证画布引用保存")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var org, user string
	if err := db.QueryRow(ctx, `SELECT organization_id,id FROM users WHERE organization_id IS NOT NULL LIMIT 1`).Scan(&org, &user); err != nil {
		t.Fatal(err)
	}
	project, form, hidden, source := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	repo := NewDramaRepository(db)
	_, err = repo.Create(ctx, drama.Project{
		ID: project, OrganizationID: org, OwnerID: user, Title: "临时画布引用验证", SourceText: "角色来到城门",
		SourceLength: 6, SourceMode: "upload", StyleID: "dark-concept", Style: "黑暗原画概念风",
		AspectRatio: "16:9", ModelCode: "test", Status: "ready", Phase: "overview_done",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := db.Exec(context.Background(), `DELETE FROM drama_projects WHERE id=$1`, project); err != nil {
			t.Errorf("清理测试项目失败: %v", err)
		}
	}()
	if _, err := db.Exec(ctx, `INSERT INTO drama_assets(id,project_id,kind,name,description)
		VALUES($1,$3,'character','守门人','身披斗篷'),($2,$3,'prop','雨伞','收起的红伞')`, form, hidden, project); err != nil {
		t.Fatal(err)
	}
	service := drama.NewService(repo, nil).WithVisuals(repo, nil, nil)
	board, err := service.Canvas(ctx, org, user, project)
	if err != nil {
		t.Fatal(err)
	}
	other := uuid.NewString()
	revision, err := service.SaveCanvas(ctx, org, user, project, drama.CanvasLayout{
		Revision: board.Revision, X: 12, Y: 34, Zoom: .8,
		Extras: &drama.CanvasExtras{
			Hidden: []string{hidden},
			Cards:  []drama.CanvasCard{{ID: source, Kind: "text", Title: "天气", Content: "正在下雨"}, {ID: other, Kind: "text", Title: "光线"}},
			Links:  []drama.CanvasLink{{ID: "原有连线", Source: other, Target: source}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	editor := drama.FormEdit{Revision: 1, Name: "城门守卫", Description: "@天气 身披斗篷", Gender: "unspecified",
		StyleID: "dark-concept", Style: "黑暗原画概念风", AspectRatio: "9:16", CanvasRevision: &revision,
		MentionLinks: []drama.CanvasLink{{ID: "天气引用", Source: source, Target: form}, {ID: "重复引用", Source: source, Target: form}},
	}
	check := func(wantRevision int, wantName, wantDescription string, wantLinks int) {
		t.Helper()
		current, err := service.Canvas(ctx, org, user, project)
		if err != nil || current.Revision != wantRevision || current.X != 12 || current.Y != 34 || current.Zoom != .8 ||
			current.Extras == nil || len(current.Extras.Links) != wantLinks || current.Extras.Links[0].ID != "原有连线" ||
			len(current.Extras.Hidden) != 1 || current.Extras.Hidden[0] != hidden || len(current.Extras.Cards) != 2 {
			t.Fatalf("画布保存结果不符: %+v %v", current, err)
		}
		for _, shape := range current.Forms {
			if shape.ID == form && shape.Name == wantName && shape.Description == wantDescription {
				return
			}
		}
		t.Fatalf("形象保存结果不符: %+v", current.Forms)
	}
	stale := editor
	stale.CanvasRevision = new(revision - 1)
	if err := service.EditForm(ctx, org, user, project, form, stale); !errors.Is(err, drama.ErrCanvasConflict) {
		t.Fatalf("未拒绝过期的画布版本: %v", err)
	}
	for _, from := range []string{hidden, uuid.NewString()} {
		invalid := editor
		invalid.MentionLinks = []drama.CanvasLink{{ID: "无效引用", Source: from, Target: form}}
		if err := service.EditForm(ctx, org, user, project, form, invalid); !errors.Is(err, drama.ErrInvalidInput) {
			t.Fatalf("未拒绝隐藏节点或其他画布的引用: %v", err)
		}
	}
	check(revision, "守门人", "身披斗篷", 1)
	if err := service.EditForm(ctx, org, user, project, form, editor); err != nil {
		t.Fatal(err)
	}
	revision++
	check(revision, editor.Name, editor.Description, 2)
	if err := service.EditForm(ctx, org, user, project, form, editor); !errors.Is(err, drama.ErrCanvasConflict) {
		t.Fatalf("未拒绝过期的形象版本: %v", err)
	}
	check(revision, editor.Name, editor.Description, 2)
	editor.Revision++
	if err := service.EditForm(ctx, org, user, project, form, editor); err != nil {
		t.Fatal(err)
	}
	check(revision+1, editor.Name, editor.Description, 2)
}
