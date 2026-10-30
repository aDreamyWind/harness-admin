package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
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
	batch, err = repo.QueueBatchAnalysis(ctx, org, owner, id, batch.ID, drama.BatchRunOptions{})
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
	draft.Episodes[0].Characters = append(draft.Episodes[0].Characters, drama.EpisodeAssetDraft{Name: "张三", StateLabel: "受伤状态"})
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
	if link.AssetID != assets[0].ID || link.ID != result.Episodes[0].Characters[0].ID || !strings.Contains(link.StateLabel, "默认形象") || !strings.Contains(link.StateLabel, "受伤状态") {
		t.Fatalf("同一资产应复用关联并保留已有状态：%+v", link)
	}
	again, err := repo.AddEpisodeAsset(ctx, org, owner, id, result.Episodes[0].ID, assets[0].ID, "")
	if err != nil || again.ID != link.ID || again.StateLabel != link.StateLabel {
		t.Fatalf("空状态重复添加应幂等：%v %+v", err, again)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM drama_episode_asset_links WHERE episode_id=$1 AND asset_id=$2`, result.Episodes[0].ID, assets[0].ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("重复添加产生多个关联：%d %v", count, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE drama_assets SET description='修改后的项目角色描述' WHERE id=$1`, assets[0].ID); err != nil {
		t.Fatal(err)
	}
	linkedResult, err := repo.GetBatchResult(ctx, org, owner, id, batch.ID)
	if err != nil || len(linkedResult.Episodes[0].Characters) != 1 || linkedResult.Episodes[0].Characters[0].Description != "修改后的项目角色描述" {
		t.Fatal("剧集必须直接引用项目资产的真实数据")
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
	if _, err = repo.QueueBatchAnalysis(ctx, org, owner, id, batch.ID, drama.BatchRunOptions{Resume: true}); !errors.Is(err, drama.ErrResumeNotAllowed) {
		t.Fatalf("已完成批次不应续跑：%v", err)
	}
	job := drama.BatchAnalysisJob{Batch: batch, OrganizationID: org, OwnerID: owner}
	if err = repo.AppendBatchEvent(ctx, job, drama.BatchEvent{Stage: "art", StepNumber: 1, Kind: "completed", Message: "已保存"}); err != nil {
		t.Fatal(err)
	}
	events, err := repo.ListBatchEvents(ctx, org, owner, id, batch.ID, 0)
	if err != nil || len(events) != 1 {
		t.Fatalf("事件读写失败：%v %+v", err, events)
	}
	if events, err = repo.ListBatchEvents(ctx, org, owner, id, batch.ID, events[0].ID); err != nil || len(events) != 0 {
		t.Fatal("事件游标未生效")
	}
	if _, err = repo.ListBatchEvents(ctx, org, uuid.NewString(), id, batch.ID, 0); !errors.Is(err, drama.ErrNotFound) {
		t.Fatal("不得读取其他用户事件")
	}
	batch, err = repo.QueueBatchAnalysis(ctx, org, owner, id, batch.ID, drama.BatchRunOptions{ContextText: `{"version":2,"model":"writer"}`})
	if err != nil {
		t.Fatal(err)
	}
	steps, err = repo.ListBatchSteps(ctx, org, owner, id, batch.ID)
	if err != nil || len(steps) != 0 {
		t.Fatal("重新解析没有清空检查点")
	}
	previous, err := repo.GetBatchResult(ctx, org, owner, id, batch.ID)
	if err != nil || len(previous.Episodes) != 1 {
		t.Fatal("新结果尚未完成时应保留正式剧集")
	}
	step = drama.BatchStep{Stage: "art", ChunkNumber: 1, ChunkTotal: 1, ResultText: `{"characters":[],"scenes":[],"props":[]}`}
	if err = repo.StartBatchStep(ctx, org, owner, id, batch.ID, batch.AnalysisRevision, step); err != nil {
		t.Fatal(err)
	}
	if err = repo.CompleteBatchStep(ctx, org, owner, id, batch.ID, batch.AnalysisRevision, step); err != nil {
		t.Fatal(err)
	}
	job.Batch = batch
	if err = repo.StartBatchStep(ctx, org, owner, id, batch.ID, batch.AnalysisRevision, drama.BatchStep{Stage: "review", ChunkNumber: 1, ChunkTotal: 1}); err != nil {
		t.Fatal(err)
	}
	if err = repo.FailBatchStep(ctx, job, "review", 1, "审校待修正"); err != nil {
		t.Fatal(err)
	}
	if err = repo.FailBatchAnalysis(ctx, org, owner, id, batch.ID, batch.AnalysisRevision, "审校待修正"); err != nil {
		t.Fatal(err)
	}
	batch, err = repo.QueueBatchAnalysis(ctx, org, owner, id, batch.ID, drama.BatchRunOptions{Resume: true})
	if err != nil {
		t.Fatal(err)
	}
	steps, err = repo.ListBatchSteps(ctx, org, owner, id, batch.ID)
	if err != nil || len(steps) != 2 || steps[0].Status != "completed" || batch.AnalysisContext == "" {
		t.Fatalf("续跑丢失检查点或上下文：%v %+v", err, steps)
	}
}

func TestDramaBatchNamesAndAssetProvenance(t *testing.T) {
	url := os.Getenv("HARNESS_TEST_WRITE_DATABASE_URL")
	if url == "" {
		t.Skip("设置专用测试数据库后验证批次名称及来源")
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
		t.Skipf("无测试企业或用户: %v", err)
	}
	repo := NewDramaRepository(pool)
	project := uuid.NewString()
	_, err = repo.Create(ctx, drama.Project{ID: project, OrganizationID: org, OwnerID: owner, Title: "批次名称与资产来源测试", SourceText: "张三走入竹林", SourceLength: 6, SourceMode: "upload", Style: "国风", AspectRatio: "9:16", ModelCode: "writer", Status: "ready", Phase: "overview_done"})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM drama_projects WHERE id=$1`, project)
	batch1, err := repo.CreateBatch(ctx, org, owner, drama.Batch{ID: uuid.NewString(), ProjectID: project, EndOffset: 6, SelectionMode: "beginning", Boundary: "end"})
	if err != nil {
		t.Fatal(err)
	}
	batch2, err := repo.CreateBatch(ctx, org, owner, drama.Batch{ID: uuid.NewString(), ProjectID: project, EndOffset: 6, SelectionMode: "beginning", Boundary: "end"})
	if err != nil {
		t.Fatal(err)
	}
	draft := drama.BatchDraft{Characters: []drama.AssetDraft{{Name: "张三", Description: "主角"}}, Scenes: []drama.AssetDraft{{Name: "竹林", Description: "竹林"}}, Props: []drama.AssetDraft{{Name: "竹杖", Description: "竹制手杖"}}, Episodes: []drama.EpisodeDraft{{Title: "入林", Synopsis: "进入竹林", ScriptText: "【竹林 外 日】张三握住竹杖。", Characters: []drama.EpisodeAssetDraft{{Name: "张三"}}, Scenes: []drama.EpisodeAssetDraft{{Name: "竹林"}}, Props: []drama.EpisodeAssetDraft{{Name: "竹杖"}}}}}
	batch1, err = repo.QueueBatchAnalysis(ctx, org, owner, project, batch1.ID, drama.BatchRunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	job := drama.BatchAnalysisJob{OrganizationID: org, OwnerID: owner, Batch: batch1}
	if err = repo.SaveGeneratedBatchName(ctx, job, "AI生成名称"); err != nil {
		t.Fatal(err)
	}
	current, err := repo.GetBatch(ctx, org, owner, project, batch1.ID)
	if err != nil || current.Name != "AI生成名称" || current.NameManuallyEdited {
		t.Fatalf("AI命名未保存：%v %+v", err, current)
	}
	batch1, err = repo.RenameBatch(ctx, org, owner, project, batch1.ID, "雨夜来信")
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveGeneratedBatchName(ctx, job, "自动覆盖名称"); err != nil {
		t.Fatal(err)
	}
	current, err = repo.GetBatch(ctx, org, owner, project, batch1.ID)
	if err != nil || current.Name != "雨夜来信" || !current.NameManuallyEdited {
		t.Fatal("AI不得覆盖手动改名")
	}
	if err = repo.SaveBatchResult(ctx, org, owner, project, batch1.ID, batch1.AnalysisRevision, draft); err != nil {
		t.Fatal(err)
	}
	batch2, err = repo.QueueBatchAnalysis(ctx, org, owner, project, batch2.ID, drama.BatchRunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.RenameBatch(ctx, org, owner, project, batch2.ID, "重返竹林"); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveBatchResult(ctx, org, owner, project, batch2.ID, batch2.AnalysisRevision, draft); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.RenameBatch(ctx, org, owner, project, batch1.ID, "改名后的起点"); err != nil {
		t.Fatal(err)
	}
	p, err := repo.Get(ctx, org, owner, project)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{p.Characters, p.Scenes, p.Props} {
		var assets []drama.Asset
		if err = json.Unmarshal(raw, &assets); err != nil {
			t.Fatal(err)
		}
		if len(assets) != 1 || assets[0].SourceBatch == nil || assets[0].SourceBatch.ID != batch1.ID || assets[0].SourceBatch.Name != "改名后的起点" || len(assets[0].UsedInBatches) != 2 || assets[0].UsedInBatches[0].Name != "改名后的起点" || assets[0].UsedInBatches[1].Name != "重返竹林" {
			t.Fatalf("来源或应用未按ID关联最新批次名称：%+v", assets)
		}
	}
	assets, err := repo.ListProjectAssets(ctx, org, owner, project, "prop", "")
	if err != nil || len(assets) != 1 || len(assets[0].UsedInBatches) != 2 {
		t.Fatal("目录查询应返回相同来源与应用")
	}
	result, err := repo.GetBatchResult(ctx, org, owner, project, batch2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.RemoveEpisodeAsset(ctx, org, owner, project, result.Episodes[0].ID, result.Episodes[0].Props[0].ID); err != nil {
		t.Fatal(err)
	}
	assets, err = repo.ListProjectAssets(ctx, org, owner, project, "prop", "")
	if err != nil || len(assets[0].UsedInBatches) != 1 || assets[0].SourceBatch.ID != batch1.ID {
		t.Fatal("应用应随实际引用变化，来源应保留")
	}
	if _, err = repo.RenameBatch(ctx, org, uuid.NewString(), project, batch1.ID, "越权名称"); !errors.Is(err, drama.ErrNotFound) {
		t.Fatal("不可修改其他用户批次")
	}
}
