package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/novel"
)

func TestNovelCoverHistoryMigrationAndSelection(t *testing.T) {
	databaseURL := os.Getenv("HARNESS_TEST_WRITE_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("设置测试数据库后验证小说封面历史迁移与版本选用")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 1
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	migration := func(name string) (string, string) {
		t.Helper()
		raw, err := os.ReadFile("../../database/migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		up, down, ok := strings.Cut(string(raw), "-- +goose Down")
		if !ok {
			t.Fatal("迁移缺少回滚脚本")
		}
		return up, down
	}
	exec(`CREATE TEMP TABLE novel_projects (
		id uuid PRIMARY KEY, organization_id uuid NOT NULL, owner_id uuid NOT NULL,
		title text NOT NULL, brief text NOT NULL, chapter_count integer NOT NULL,
		workspace jsonb NOT NULL, version integer NOT NULL, updated_at timestamptz NOT NULL DEFAULT NOW()
	)`)
	exec(`SET search_path TO pg_temp`)
	previousUp, _ := migration("00061_ai_novel_covers.sql")
	exec(previousUp)

	owner := novel.Owner{OrganizationID: uuid.NewString(), UserID: uuid.NewString()}
	project, legacy, candidate, conflict := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO novel_projects (id, organization_id, owner_id, title, brief, chapter_count, workspace, version)
		VALUES ($1,$2,$3,'历史测试','封面恢复测试',1,'{}',1)`, project, owner.OrganizationID, owner.UserID)
	exec(`INSERT INTO novel_project_covers (project_id, revision, source, mime_type, image_data, updated_at)
		VALUES ($1,$2,'upload','image/jpeg',$3,NOW()-INTERVAL '1 day')`, project, legacy, []byte("旧封面"))
	up, down := migration("00073_novel_cover_history.sql")
	exec(up)

	repo := NewNovelRepository(db)
	if items, err := repo.ListCoverHistory(ctx, owner, project); err != nil || len(items) != 1 || items[0].Revision != legacy || !items[0].Current {
		t.Fatalf("没有回填原有的当前封面: %+v %v", items, err)
	}
	data := novel.CoverImage{Data: []byte("候选封面"), MIME: "image/png"}
	if err := repo.SaveCoverVersion(ctx, owner, project, novel.Cover{Revision: candidate, Source: "ai"}, data); err != nil {
		t.Fatal(err)
	}
	if image, err := repo.GetCover(ctx, owner, project, ""); err != nil || string(image.Data) != "旧封面" || image.MIME != "image/jpeg" {
		t.Fatalf("归档候选时更换了当前封面: %+v %v", image, err)
	}
	if _, err := repo.SaveCover(ctx, owner, project, "", novel.Cover{Revision: conflict, Source: "upload"}, data); !errors.Is(err, novel.ErrConflict) {
		t.Fatalf("过期写入未返回冲突: %v", err)
	}
	if _, err := repo.GetCover(ctx, owner, project, conflict); !errors.Is(err, novel.ErrNotFound) {
		t.Fatalf("有冲突的封面版本不应提交: %v", err)
	}
	saved, err := repo.SelectCoverVersion(ctx, owner, project, candidate)
	if err != nil || saved.Revision != candidate || saved.Source != "ai" {
		t.Fatalf("不能直接选用归档的候选: %+v %v", saved, err)
	}
	if repeated, err := repo.SelectCoverVersion(ctx, owner, project, candidate); err != nil || !repeated.UpdatedAt.Equal(saved.UpdatedAt) {
		t.Fatalf("重试选用改变了更新时间: %+v %v", repeated, err)
	}
	if books, err := repo.List(ctx, owner); err != nil || len(books) != 1 || books[0].Cover == nil || books[0].Cover.Revision != candidate {
		t.Fatalf("书架未使用选中的版本: %+v %v", books, err)
	}
	p, err := repo.Get(ctx, owner, project)
	if err != nil || p.Cover == nil || p.Cover.Revision != candidate || p.Version != 1 {
		t.Fatalf("作品未使用选中的版本或创作版本被修改: %+v %v", p, err)
	}
	if p, err := repo.Save(ctx, owner, p); err != nil || p.Cover == nil || p.Cover.Revision != candidate || p.Version != 2 {
		t.Fatalf("保存作品时未保留封面版本: %+v %v", p, err)
	}
	if image, err := repo.GetCover(ctx, owner, project, legacy); err != nil || string(image.Data) != "旧封面" {
		t.Fatalf("历史地址不能读取原封面: %+v %v", image, err)
	}
	other := novel.Owner{OrganizationID: owner.OrganizationID, UserID: uuid.NewString()}
	if _, err := repo.GetCover(ctx, other, project, legacy); !errors.Is(err, novel.ErrNotFound) {
		t.Fatalf("其他作者能读取历史封面: %v", err)
	}
	if _, err := repo.ListCoverHistory(ctx, other, project); !errors.Is(err, novel.ErrNotFound) {
		t.Fatalf("其他作者能看到历史封面列表: %v", err)
	}
	if _, err := repo.SelectCoverVersion(ctx, other, project, legacy); !errors.Is(err, novel.ErrNotFound) {
		t.Fatalf("其他作者能修改当前封面: %v", err)
	}
	if restored, err := repo.SelectCoverVersion(ctx, owner, project, legacy); err != nil || restored.Revision != legacy || restored.Source != "upload" {
		t.Fatalf("不能切换回历史上传版本: %+v %v", restored, err)
	}
	if items, err := repo.ListCoverHistory(ctx, owner, project); err != nil || len(items) != 2 || items[0].Revision != candidate || items[0].Current || !items[1].Current {
		t.Fatalf("切换后历史顺序或当前标记有误: %+v %v", items, err)
	}

	exec(down)
	var image []byte
	if err := db.QueryRow(ctx, `SELECT image_data FROM novel_project_covers WHERE project_id=$1`, project).Scan(&image); err != nil || string(image) != "旧封面" {
		t.Fatalf("回滚时没有保留当前封面: %q %v", image, err)
	}
}
