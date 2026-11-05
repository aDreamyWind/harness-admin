package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"harness-admin/internal/drama"
	"os"
	"testing"
	"time"
)

func TestDramaCanvasFormsIsolationAndImageLifecycle(t *testing.T) {
	databaseURL := os.Getenv("HARNESS_TEST_WRITE_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("设置测试数据库后验证画布持久化")
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
	repo := NewDramaRepository(db)
	project := uuid.NewString()
	_, err = repo.Create(ctx, drama.Project{ID: project, OrganizationID: org, OwnerID: user, Title: "临时画布验证", SourceText: "角色来到城堡", SourceLength: 7, SourceMode: "upload", Style: "日系动漫", AspectRatio: "16:9", ModelCode: "test", Status: "ready", Phase: "overview_done"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, e := db.Exec(context.Background(), `DELETE FROM drama_projects WHERE id=$1`, project); e != nil {
			t.Errorf("清理失败: %v", e)
		}
	}()
	root, scene := uuid.NewString(), uuid.NewString()
	if _, err = db.Exec(ctx, `INSERT INTO drama_assets(id,project_id,kind,name,description) VALUES($1,$3,'character','林夏','青年男性'),($2,$3,'scene','城堡','石砌城堡')`, root, scene, project); err != nil {
		t.Fatal(err)
	}
	assertAssetPreview := func(imageID string) {
		t.Helper()
		projects, err := repo.List(ctx, org, user)
		if err != nil {
			t.Fatal(err)
		}
		want := ""
		if imageID != "" {
			want = "/api/v1/ai-drama/projects/" + project + "/images/" + imageID + "/content"
		}
		for _, item := range projects {
			if item.ID != project {
				continue
			}
			var characters, scenes []map[string]json.RawMessage
			if err := json.Unmarshal(item.Characters, &characters); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(item.Scenes, &scenes); err != nil {
				t.Fatal(err)
			}
			if len(characters) != 1 || len(scenes) != 1 {
				t.Fatalf("项目资产数量不符: %s %s", item.Characters, item.Scenes)
			}
			if _, ok := scenes[0]["previewImageUrl"]; ok {
				t.Fatalf("没有生成图片的场景返回了预览图: %s", item.Scenes)
			}
			preview, hasPreview := characters[0]["previewImageUrl"]
			if imageID == "" {
				if hasPreview {
					t.Fatalf("没有生成图片的角色返回了预览图: %s", item.Characters)
				}
			} else {
				var got string
				if err := json.Unmarshal(preview, &got); err != nil || got != want {
					t.Fatalf("角色预览图不符，期望 %q，实际 %q: %v", want, got, err)
				}
			}
			return
		}
		t.Fatal("项目列表缺少测试项目")
	}
	assertAssetPreview("")
	board, err := repo.ReadCanvas(ctx, org, user, project)
	if err != nil || len(board.Forms) != 2 || board.Revision != 1 || board.Extras != nil {
		t.Fatalf("初始画布异常: %+v %v", board, err)
	}
	if _, err = repo.ReadCanvas(ctx, org, uuid.NewString(), project); !errors.Is(err, drama.ErrNotFound) {
		t.Fatal("画布未按用户隔离")
	}
	cardID := uuid.NewString()
	layout := drama.CanvasLayout{Revision: 1, X: 100, Y: 200, Zoom: 0.8, Positions: []drama.CanvasPosition{{ID: root, X: 123, Y: 456}}, Extras: &drama.CanvasExtras{
		Hidden: []string{scene},
		Cards:  []drama.CanvasCard{{ID: cardID, Kind: "text", Title: "记录", Content: "灯火点亮", Position: drama.CanvasPoint{X: 200, Y: 300}}},
		Links:  []drama.CanvasLink{{ID: "manual-edge", Source: cardID, Target: scene}},
	}}
	revision, err := repo.SaveCanvasLayout(ctx, org, user, project, layout)
	if err != nil || revision != 2 {
		t.Fatal(err)
	}
	if _, err = repo.SaveCanvasLayout(ctx, org, user, project, layout); !errors.Is(err, drama.ErrCanvasConflict) {
		t.Fatal("未拒绝旧布局版本")
	}
	badExtras := layout
	badExtras.Revision = revision
	badExtras.Extras = &drama.CanvasExtras{Links: []drama.CanvasLink{{ID: "outside", Source: root, Target: uuid.NewString()}}}
	if _, err = repo.SaveCanvasLayout(ctx, org, user, project, badExtras); !errors.Is(err, drama.ErrInvalidInput) {
		t.Fatal("未拒绝其他画布的连线端点", err)
	}
	badLayout := layout
	badLayout.Revision = 2
	badLayout.Positions = []drama.CanvasPosition{{ID: uuid.NewString()}}
	badLayout.Extras = nil
	if _, err = repo.SaveCanvasLayout(ctx, org, user, project, badLayout); !errors.Is(err, drama.ErrNotFound) {
		t.Fatal("未拒绝其他项目节点")
	}
	child, err := repo.DeriveAssetForm(ctx, org, user, project, root, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	board, err = repo.ReadCanvas(ctx, org, user, project)
	if err != nil || len(board.Forms) != 3 || board.Revision != 3 {
		t.Fatal("派生未持久化或布局版本错误", err)
	}
	if board.Extras == nil || len(board.Extras.Cards) != 1 || board.Extras.Cards[0].Position.X != 200 ||
		len(board.Extras.Links) != 1 || board.Extras.Links[0].Target != scene || len(board.Extras.Hidden) != 1 || board.Extras.Hidden[0] != scene {
		t.Fatalf("画布素材未正确保存: %+v", board.Extras)
	}
	if _, err = repo.SaveCanvasLayout(ctx, org, user, project, drama.CanvasLayout{Revision: board.Revision, Zoom: 1}); err != nil {
		t.Fatal("旧客户端保存布局失败", err)
	}
	board, err = repo.ReadCanvas(ctx, org, user, project)
	if err != nil || board.Extras == nil || len(board.Extras.Cards) != 1 {
		t.Fatal("省略 extras 时覆盖了已保存素材", err)
	}
	var childForm drama.AssetForm
	for _, f := range board.Forms {
		if f.ID == child {
			childForm = f
		}
		if f.ID == root && (f.X != 123 || f.Y != 456) {
			t.Fatal("位置丢失")
		}
	}
	if childForm.AssetID != root || childForm.ParentID != root {
		t.Fatal("派生形象必须关联原资产")
	}
	edit := drama.FormEdit{Revision: 1, Name: "变异形象", Description: "眼睛变成金色", Gender: "male", Style: "国风水墨", AspectRatio: "9:16"}
	if err = repo.EditAssetForm(ctx, org, user, project, child, edit); err != nil {
		t.Fatal(err)
	}
	if err = repo.EditAssetForm(ctx, org, user, project, child, edit); !errors.Is(err, drama.ErrCanvasConflict) {
		t.Fatal("未拒绝旧形象版本")
	}
	if _, err = repo.QueueImage(ctx, org, user, project, child, "painter", drama.ImageInput{}); !errors.Is(err, drama.ErrParentImageMissing) {
		t.Fatal("缺少父图片不得生成派生形象")
	}
	job, err := repo.QueueImage(ctx, org, user, project, root, "painter", drama.ImageInput{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.QueueImage(ctx, org, user, project, root, "painter", drama.ImageInput{}); !errors.Is(err, drama.ErrImageBusy) {
		t.Fatal("重复生成未拒绝")
	}
	if err = repo.EditAssetForm(ctx, org, user, project, root, edit); !errors.Is(err, drama.ErrImageBusy) {
		t.Fatal("生成中不得修改描述")
	}
	if ok, err := repo.ClaimImage(ctx, job.ID); err != nil || !ok {
		t.Fatal(err)
	}
	if ok, _ := repo.ClaimImage(ctx, job.ID); ok {
		t.Fatal("任务被重复执行")
	}
	job.Status = "succeeded"
	job.StorageKey = job.ID + ".png"
	job.MIME = "image/png"
	job.ByteSize = 100
	if err = repo.FinishImage(ctx, job); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ReadImage(ctx, org, uuid.NewString(), project, job.ID); !errors.Is(err, drama.ErrNotFound) {
		t.Fatal("图片内容未隔离")
	}
	derived, err := repo.QueueImage(ctx, org, user, project, child, "painter", drama.ImageInput{})
	if err != nil || derived.ReferenceID != job.ID {
		t.Fatal("没有使用父形象图片", err)
	}
	retry, err := repo.QueueImage(ctx, org, user, project, root, "painter", drama.ImageInput{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ClaimImage(ctx, retry.ID); err != nil {
		t.Fatal(err)
	}
	retry.Status = "failed"
	retry.Error = "模型超时"
	if err = repo.FinishImage(ctx, retry); err != nil {
		t.Fatal(err)
	}
	board, err = repo.ReadCanvas(ctx, org, user, project)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range board.Forms {
		if f.ID == root && (f.CurrentImageID != job.ID || f.ImageStatus != "failed" || f.ImageError != "模型超时" || f.ImageURL == "") {
			t.Fatal("生成失败覆盖旧图片或丢失错误")
		}
	}
	assertAssetPreview(job.ID)
	if ok, err := repo.ClaimImage(ctx, derived.ID); err != nil || !ok {
		t.Fatalf("领取派生形象任务失败: %v", err)
	}
	derived.Status = "succeeded"
	derived.StorageKey = derived.ID + ".png"
	derived.MIME = "image/png"
	derived.ByteSize = 100
	if err = repo.FinishImage(ctx, derived); err != nil {
		t.Fatal(err)
	}
	// 较新的派生图不替换默认形象的预览，只有默认形象无图时才使用派生图。
	assertAssetPreview(job.ID)
	if _, err = db.Exec(ctx, `UPDATE drama_asset_forms SET current_image_id=NULL WHERE id=$1`, root); err != nil {
		t.Fatal(err)
	}
	assertAssetPreview(derived.ID)
	// 跨资产派生关系必须被数据库本身拒绝。
	if _, err = db.Exec(ctx, `INSERT INTO drama_asset_forms(id,asset_id,parent_id,name,aspect_ratio) VALUES($1,$2,$3,'非法派生','1:1')`, uuid.NewString(), scene, root); err == nil {
		t.Fatal("数据库允许跨资产派生")
	}
}
