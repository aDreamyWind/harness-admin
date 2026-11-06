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

func TestDramaCanvasAssetCreationPersistsCatalogAndDefaultForm(t *testing.T) {
	databaseURL := os.Getenv("HARNESS_TEST_WRITE_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("设置测试数据库后验证画布资产创建")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var org, user string
	if err = db.QueryRow(ctx, `SELECT organization_id,id FROM users WHERE organization_id IS NOT NULL LIMIT 1`).Scan(&org, &user); err != nil {
		t.Fatal(err)
	}
	project := uuid.NewString()
	repo := NewDramaRepository(db)
	_, err = repo.Create(ctx, drama.Project{ID: project, OrganizationID: org, OwnerID: user, Title: "临时资产画布验证", SourceText: "角色来到城门", SourceLength: 6, SourceMode: "upload", StyleID: "dark-concept", Style: "黑暗原画概念风", AspectRatio: "16:9", ModelCode: "test", Status: "ready", Phase: "overview_done"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := db.Exec(context.Background(), `DELETE FROM drama_projects WHERE id=$1`, project); err != nil {
			t.Errorf("清理测试项目失败: %v", err)
		}
	}()
	service := drama.NewService(repo, nil).WithVisuals(repo, nil, nil)
	in := drama.CanvasAssetInput{Name: "测试资产", X: new(-11.5), Y: new(50.0), Revision: 1, Kind: "character"}
	if _, err = service.CreateCanvasAsset(ctx, org, uuid.NewString(), project, in); !errors.Is(err, drama.ErrNotFound) {
		t.Fatalf("其他用户可以写入画布: %v", err)
	}
	wantRatio := map[string]string{}
	var characterID string
	for _, item := range []struct{ kind, ratio string }{{"character", "9:16"}, {"scene", "16:9"}, {"prop", "1:1"}} {
		in.Kind = item.kind
		result, err := service.CreateCanvasAsset(ctx, org, user, project, in)
		if err != nil || result.ID == "" || result.Revision != in.Revision+1 {
			t.Fatalf("创建 %s 失败: %+v %v", item.kind, result, err)
		}
		in.Revision = result.Revision
		wantRatio[result.ID] = item.ratio
		if item.kind == "character" {
			characterID = result.ID
		}
		catalog, err := repo.ListProjectAssets(ctx, org, user, project, item.kind, "")
		if err != nil || len(catalog) != 1 || catalog[0].ID != result.ID || catalog[0].Name != in.Name || catalog[0].Description != in.Description {
			t.Fatalf("目录缺少新资产: %+v %v", catalog, err)
		}
	}
	if _, err = service.CreateCanvasAsset(ctx, org, user, project, in); !errors.Is(err, drama.ErrCanvasAssetNameExists) {
		t.Fatalf("未拒绝同类资产重名: %v", err)
	}
	stale := in
	stale.Name = "过期修改"
	stale.Revision--
	if _, err = service.CreateCanvasAsset(ctx, org, user, project, stale); !errors.Is(err, drama.ErrCanvasConflict) {
		t.Fatalf("未拒绝过期布局版本: %v", err)
	}
	board, err := service.Canvas(ctx, org, user, project)
	if err != nil || len(board.Forms) != len(wantRatio) || board.Revision != in.Revision {
		t.Fatalf("画布版本或资产数错误: %+v %v", board, err)
	}
	for _, form := range board.Forms {
		if form.AssetID != form.ID || form.ParentID != "" || form.Name != in.Name || form.AspectRatio != wantRatio[form.ID] || form.Description != in.Description ||
			form.StyleID != "dark-concept" || form.Style != "黑暗原画概念风" || form.X != *in.X || form.Y != *in.Y {
			t.Fatalf("默认形象与资产关联、位置或默认配置错误: %+v", form)
		}
	}
	in.Kind, in.Name = "character", "已有角色"
	if _, err := service.CreateCanvasAsset(ctx, org, user, project, in); err != nil {
		t.Fatal(err)
	}
	edit := drama.FormEdit{Revision: 1, Name: in.Name, Gender: "unspecified", AspectRatio: "9:16", StyleID: "dark-concept", Style: "黑暗原画概念风"}
	if err := service.EditForm(ctx, org, user, project, characterID, edit); !errors.Is(err, drama.ErrCanvasAssetNameExists) {
		t.Fatalf("角色改名未拒绝重名：%v", err)
	}
	edit.Name = "守门人"
	if err := service.EditForm(ctx, org, user, project, characterID, edit); err != nil {
		t.Fatalf("角色改名未保存：%v", err)
	}
	var assetName, formName string
	if err := db.QueryRow(ctx, `SELECT a.name, f.name FROM drama_assets a JOIN drama_asset_forms f ON f.asset_id=a.id WHERE f.id=$1`, characterID).Scan(&assetName, &formName); err != nil || assetName != edit.Name || formName != edit.Name {
		t.Fatalf("角色目录与原始形象未同步：%q、%q，%v", assetName, formName, err)
	}
}
