package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"harness-admin/internal/drama"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDramaStylesPersistIDsAndKeepTemplatesServerSide(t *testing.T) {
	url := os.Getenv("HARNESS_TEST_WRITE_DATABASE_URL")
	if url == "" {
		t.Skip("设置测试数据库后验证风格快照")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var org, owner string
	if err = db.QueryRow(ctx, `SELECT organization_id,id FROM users WHERE organization_id IS NOT NULL LIMIT 1`).Scan(&org, &owner); err != nil {
		t.Fatal(err)
	}
	repo := NewDramaRepository(db)
	options, err := repo.ListStyles(ctx)
	if err != nil || len(options) < 12 {
		t.Fatal("风格目录缺少内置选项", err)
	}
	private, err := repo.ResolveStyle(ctx, "dark-concept")
	if err != nil || private.ImagePrompt == "" || private.VideoPrompt == "" {
		t.Fatal("风格模板未保存", err)
	}
	style, err := repo.ResolveCatalogStyle(ctx, private.ID)
	if err != nil || style != private {
		t.Fatal("目录查询未读取已选风格及模板", err)
	}
	for _, unavailable := range []string{private.Name, "cinematic-realism"} {
		if _, err = repo.ResolveCatalogStyle(ctx, unavailable); !errors.Is(err, drama.ErrStyleUnavailable) {
			t.Fatalf("目录查询应拒绝显示名称和历史风格 %q: %v", unavailable, err)
		}
	}
	raw, _ := json.Marshal(options)
	if strings.Contains(string(raw), private.ImagePrompt) || strings.Contains(string(raw), "imagePrompt") {
		t.Fatal("公开目录泄露模板")
	}
	project, asset := uuid.NewString(), uuid.NewString()
	created, err := repo.Create(ctx, drama.Project{ID: project, OrganizationID: org, OwnerID: owner, Title: "临时风格验证", SourceText: "甲来到书店", SourceLength: 6, SourceMode: "upload", StyleID: "dark-comic", Style: "暗黑漫画", AspectRatio: "16:9", ModelCode: "writer", Status: "ready", Phase: "overview_done"})
	if err != nil || created.StyleID != "dark-comic" {
		t.Fatal("项目风格主键未保存", err)
	}
	defer func() {
		if _, err := db.Exec(context.Background(), `DELETE FROM drama_projects WHERE id=$1`, project); err != nil {
			t.Errorf("清理失败：%v", err)
		}
	}()
	if _, err = db.Exec(ctx, `INSERT INTO drama_assets(id,project_id,kind,name,description) VALUES($1,$2,'character','甲','青年男性，灰色风衣')`, asset, project); err != nil {
		t.Fatal(err)
	}
	board, err := repo.ReadCanvas(ctx, org, owner, project)
	if err != nil || len(board.Forms) != 1 || board.Forms[0].StyleID != "dark-comic" {
		t.Fatal("默认形象没有继承风格ID", err)
	}
	if _, err := db.Exec(ctx, `UPDATE drama_asset_forms SET name='默认形象' WHERE id=$1`, asset); err != nil {
		t.Fatal(err)
	}
	board, err = repo.ReadCanvas(ctx, org, owner, project)
	if err != nil || len(board.Forms) != 1 || board.Forms[0].Name != "甲" {
		t.Fatal("旧默认形象没有显示资产名", err)
	}
	service := drama.NewService(repo, nil).WithStyles(repo).WithVisuals(repo, nil, nil)
	input := drama.FormEdit{Revision: 1, Name: "默认形象", Description: "青年男性，灰色风衣", Gender: "male", AspectRatio: "9:16", StyleID: "dark-concept", Style: "伪造前端名称"}
	if err = service.EditForm(ctx, org, owner, project, asset, input); err != nil {
		t.Fatal(err)
	}
	board, err = repo.ReadCanvas(ctx, org, owner, project)
	if err != nil || board.Forms[0].StyleID != "dark-concept" || board.Forms[0].Style != "黑暗原画概念风" || board.Forms[0].Name != "甲" {
		t.Fatal("形象风格没有规范化", err)
	}
	job, err := repo.QueueImage(ctx, org, owner, project, asset, "painter", drama.ImageInput{})
	if err != nil || job.StyleID != "dark-concept" || !strings.Contains(job.Prompt, private.ImagePrompt) || strings.Contains(job.Prompt, private.VideoPrompt) {
		t.Fatal("图片任务没有使用服务端模板", err)
	}
	var storedStyle, storedPrompt string
	if err = db.QueryRow(ctx, `SELECT style_id,prompt FROM drama_image_jobs WHERE id=$1`, job.ID).Scan(&storedStyle, &storedPrompt); err != nil || storedStyle != job.StyleID || storedPrompt != job.Prompt {
		t.Fatal("实际提示词快照不完整", err)
	}
	encoded, _ := json.Marshal(job)
	if strings.Contains(string(encoded), private.ImagePrompt) || strings.Contains(string(encoded), "prompt") {
		t.Fatal("任务响应泄露模板")
	}
	disabledID := "test-" + uuid.NewString()
	if _, err = db.Exec(ctx, `INSERT INTO drama_visual_styles(id,name,category,preview_url,preview_x,preview_y,image_prompt,video_prompt,enabled) VALUES($1,$1,'历史','/drama-style-sheet.png',0,0,'private-image','private-video',false)`, disabledID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := db.Exec(context.Background(), `DELETE FROM drama_visual_styles WHERE id=$1`, disabledID); err != nil {
			t.Errorf("清理停用风格失败: %v", err)
		}
	}()
	if _, err = repo.ResolveStyle(ctx, disabledID); !errors.Is(err, drama.ErrStyleUnavailable) {
		t.Fatal("停用风格不应可用")
	}
}
