package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"harness-admin/internal/drama"
)

func TestDramaBatchResultDatabaseRoundTrip(t *testing.T) {
	url := os.Getenv("HARNESS_TEST_WRITE_DATABASE_URL")
	if url == "" {
		t.Skip("设置 HARNESS_TEST_WRITE_DATABASE_URL 后验证批次解析结果读写")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var org, owner string
	if err = pool.QueryRow(ctx, `SELECT o.id,u.id FROM organizations o CROSS JOIN users u LIMIT 1`).Scan(&org, &owner); err != nil {
		t.Skipf("测试数据库没有企业或用户: %v", err)
	}
	id := uuid.NewString()
	repo := NewDramaRepository(pool)
	_, err = repo.Create(ctx, drama.Project{ID: id, OrganizationID: org, OwnerID: owner, Title: "批次解析往返测试", SourceText: "张三走入竹林", SourceLength: 6, SourceMode: "upload", Style: "国风", AspectRatio: "9:16", ModelCode: "writer", Status: "ready", Phase: "overview_done"})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM drama_projects WHERE id=$1`, id)
	batch, err := repo.CreateBatch(ctx, org, owner, drama.Batch{ID: uuid.NewString(), ProjectID: id, StartOffset: 0, EndOffset: 6, SelectionMode: "beginning", Boundary: "end"})
	if err != nil {
		t.Fatal(err)
	}
	batch, err = repo.QueueBatchAnalysis(ctx, org, owner, id, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Status != "running" {
		t.Fatalf("批次状态 %s", batch.Status)
	}
	step := drama.BatchStep{Stage: "art", ChunkNumber: 1, ChunkTotal: 1}
	if err = repo.StartBatchStep(ctx, org, owner, id, batch.ID, batch.AnalysisRevision, step); err != nil {
		t.Fatal(err)
	}
	step.ResultText = `{"characters":[{"name":"张三"}],"scenes":[],"props":[]}`
	step.DurationMS = 1200
	if err = repo.CompleteBatchStep(ctx, org, owner, id, batch.ID, batch.AnalysisRevision, step); err != nil {
		t.Fatal(err)
	}
	steps, err := repo.ListBatchSteps(ctx, org, owner, id, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Status != "completed" || steps[0].ResultText != step.ResultText {
		t.Fatalf("逐段结果未保存: %+v", steps)
	}
	progress, err := drama.NewService(repo, nil).BatchProgress(ctx, org, owner, id, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(progress.Steps) != 1 || len(progress.Assets.Characters) != 1 || progress.Assets.Characters[0].Name != "张三" {
		t.Fatalf("阶段成果预览不完整: %+v", progress)
	}
	draft := drama.BatchDraft{Characters: []drama.AssetDraft{{Name: "张三", Description: "主角", AppearanceDescription: "黑发青衣", VoiceDescription: "低沉清亮"}}, Scenes: []drama.AssetDraft{{Name: "竹林", Description: "林间"}}, Episodes: []drama.EpisodeDraft{{Title: "入林", Synopsis: "张三走入竹林", ScriptText: "【竹林 外 日】\n张三走入竹林。", Characters: []drama.EpisodeAssetDraft{{Name: "张三", StateLabel: "默认形象"}}, Scenes: []drama.EpisodeAssetDraft{{Name: "竹林", StateLabel: "白天"}}}}}
	if err = repo.SaveBatchResult(ctx, org, owner, id, batch.ID, batch.AnalysisRevision, draft); err != nil {
		t.Fatal(err)
	}
	result, err := repo.GetBatchResult(ctx, org, owner, id, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Batch.Status != "ready" || len(result.Assets.Characters) != 1 || len(result.Episodes) != 1 || len(result.Episodes[0].Scenes) != 1 || result.Assets.Characters[0].AppearanceDescription != "黑发青衣" || result.Episodes[0].Characters[0].VoiceDescription != "低沉清亮" {
		t.Fatalf("批次结果不完整: %+v", result)
	}
	assets, err := repo.ListProjectAssets(ctx, org, owner, id, "character", "张")
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 {
		t.Fatalf("搜索项目资产错误: %+v", assets)
	}
	link, err := repo.AddEpisodeAsset(ctx, org, owner, id, result.Episodes[0].ID, assets[0].ID, "受伤状态")
	if err != nil {
		t.Fatal(err)
	}
	if link.StateLabel != "受伤状态" {
		t.Fatalf("关联状态丢失: %+v", link)
	}
	if err = repo.RemoveEpisodeAsset(ctx, org, owner, id, result.Episodes[0].ID, link.ID); err != nil {
		t.Fatal(err)
	}
	project, err := repo.Get(ctx, org, owner, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Characters) == 0 || len(project.Episodes) == 0 {
		t.Fatal("项目资产和分集兼容视图为空")
	}
}
