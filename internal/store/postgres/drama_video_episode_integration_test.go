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

func TestDramaVideoEpisodesUseCanonicalScriptsAndAssets(t *testing.T) {
	url := os.Getenv("HARNESS_TEST_WRITE_DATABASE_URL")
	if url == "" {
		t.Skip("设置 HARNESS_TEST_WRITE_DATABASE_URL 后验证视频分集关联")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var org, owner string
	if err = pool.QueryRow(ctx, `SELECT organization_id,id FROM users WHERE organization_id IS NOT NULL LIMIT 1`).Scan(&org, &owner); err != nil {
		t.Fatal(err)
	}
	project := uuid.NewString()
	repo := NewDramaRepository(pool)
	_, err = repo.Create(ctx, drama.Project{ID: project, OrganizationID: org, OwnerID: owner, Title: "视频分集关联测试", SourceText: "林夏走入书店", SourceLength: 6, SourceMode: "upload", Style: "日系动漫", AspectRatio: "16:9", ModelCode: "writer", Status: "ready", Phase: "overview_done"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM drama_projects WHERE id=$1`, project); err != nil {
			t.Errorf("清理临时项目失败: %v", err)
		}
	}()
	if items, err := repo.ListVideoEpisodes(ctx, org, owner, project); err != nil || len(items) != 0 {
		t.Fatal("尚未生成剧本时应为空列表")
	}
	// 旧概览分集规划不能作为可拍摄剧本进入视频分集。
	if _, err = pool.Exec(ctx, `INSERT INTO drama_script_episodes(id,project_id,number,title) VALUES($1,$2,1,'旧概览规划')`, uuid.NewString(), project); err != nil {
		t.Fatal(err)
	}
	first, err := repo.CreateBatch(ctx, org, owner, drama.Batch{ID: uuid.NewString(), ProjectID: project, EndOffset: 6, SelectionMode: "beginning", Boundary: "end"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.CreateBatch(ctx, org, owner, drama.Batch{ID: uuid.NewString(), ProjectID: project, EndOffset: 6, SelectionMode: "beginning", Boundary: "end"})
	if err != nil {
		t.Fatal(err)
	}
	for i, batch := range []drama.Batch{first, second} {
		batch, err = repo.QueueBatchAnalysis(ctx, org, owner, project, batch.ID, drama.BatchRunOptions{})
		if err != nil {
			t.Fatal(err)
		}
		draft := drama.BatchDraft{Characters: []drama.AssetDraft{{Name: "林夏", Description: "主角"}, {Name: "周伯", Description: "邻居"}}, Scenes: []drama.AssetDraft{{Name: "旧书店", Description: "旧书店场景"}}, Props: []drama.AssetDraft{{Name: "铜钥匙", Description: "古旧钥匙"}}, Episodes: []drama.EpisodeDraft{{Title: "雨夜来信", Synopsis: "林夏收到来信", ScriptText: "【旧书店 内 夜】林夏拿起铜钥匙。", Characters: []drama.EpisodeAssetDraft{{Name: "林夏", StateLabel: "灰色风衣"}}, Scenes: []drama.EpisodeAssetDraft{{Name: "旧书店"}}, Props: []drama.EpisodeAssetDraft{{Name: "铜钥匙"}}}}}
		if i == 0 {
			draft.Episodes = append(draft.Episodes, drama.EpisodeDraft{Title: "门外脚步", Synopsis: "周伯敲门", ScriptText: "周伯站在门外。", Characters: []drama.EpisodeAssetDraft{{Name: "周伯"}}})
		}
		if err = repo.SaveBatchResult(ctx, org, owner, project, batch.ID, batch.AnalysisRevision, draft); err != nil {
			t.Fatal(err)
		}
	}
	items, err := repo.ListVideoEpisodes(ctx, org, owner, project)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0].Number != 1 || items[1].Number != 2 || items[2].Number != 3 || items[2].BatchEpisodeNumber != 1 {
		t.Fatalf("全项目顺序或历史规划过滤错误: %+v", items)
	}
	if items[0].StoryboardStatus != "pending" || items[0].CharacterCount != 1 || items[0].SceneCount != 1 || items[0].PropCount != 1 || items[0].ScriptNeedsReview {
		t.Fatalf("制作状态或本集资产数量错误: %+v", items[0])
	}
	result, err := repo.GetBatchResult(ctx, org, owner, project, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if items[1].ScriptEpisodeID != result.Episodes[1].ID {
		t.Fatal("视频分集必须关联原有剧本主键")
	}
	work, err := repo.GetVideoEpisodeWorkspace(ctx, org, owner, project, items[1].ScriptEpisodeID)
	if err != nil {
		t.Fatal(err)
	}
	if work.ScriptText != "周伯站在门外。" || len(work.Characters) != 1 || work.Characters[0].Name != "周伯" || len(work.Scenes) != 0 || len(work.Props) != 0 {
		t.Fatalf("工作区只能展示本集实际引用资产: %+v", work)
	}
	asset := result.Episodes[0].Characters[0]
	link, err := repo.AddEpisodeAsset(ctx, org, owner, project, items[1].ScriptEpisodeID, asset.AssetID, "")
	if err != nil {
		t.Fatal(err)
	}
	work, err = repo.GetVideoEpisodeWorkspace(ctx, org, owner, project, items[1].ScriptEpisodeID)
	if err != nil || len(work.Characters) != 2 || work.Episode.CharacterCount != 2 {
		t.Fatal("剧本关联修改必须同步体现到制作区")
	}
	if err = repo.RemoveEpisodeAsset(ctx, org, owner, project, items[1].ScriptEpisodeID, link.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.RenameBatch(ctx, org, owner, project, first.ID, "更新后的来源名"); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE drama_assets SET description='更新后的目录描述' WHERE id=$1`, asset.AssetID); err != nil {
		t.Fatal(err)
	}
	work, err = repo.GetVideoEpisodeWorkspace(ctx, org, owner, project, items[0].ScriptEpisodeID)
	if err != nil || work.Episode.Batch.Name != "更新后的来源名" || work.Characters[0].Description != "更新后的目录描述" || work.Characters[0].StateLabel != "灰色风衣" {
		t.Fatal("来源名和目录描述必须实时关联，状态说明保持独立")
	}
	if _, err = pool.Exec(ctx, `UPDATE drama_script_episodes SET title='修改后的集名',script_text='修改后的本集剧本',updated_at=NOW() WHERE id=$1`, items[0].ScriptEpisodeID); err != nil {
		t.Fatal(err)
	}
	work, err = repo.GetVideoEpisodeWorkspace(ctx, org, owner, project, items[0].ScriptEpisodeID)
	if err != nil || work.Episode.Title != "修改后的集名" || work.ScriptText != "修改后的本集剧本" {
		t.Fatal("视频入口不得保留复制的旧标题和旧剧本")
	}
	if _, err = pool.Exec(ctx, `UPDATE drama_batches SET status='stale',revision=revision+1 WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	work, err = repo.GetVideoEpisodeWorkspace(ctx, org, owner, project, items[0].ScriptEpisodeID)
	if err != nil || !work.Episode.ScriptNeedsReview {
		t.Fatal("范围变化应提示剧本需要核对，保留已有剧本入口")
	}
	for _, scope := range [][3]string{{org, uuid.NewString(), project}, {uuid.NewString(), owner, project}, {org, owner, uuid.NewString()}} {
		if _, err = repo.ListVideoEpisodes(ctx, scope[0], scope[1], scope[2]); !errors.Is(err, drama.ErrNotFound) {
			t.Fatalf("越权列表应返回不存在: %v", err)
		}
		if _, err = repo.GetVideoEpisodeWorkspace(ctx, scope[0], scope[1], scope[2], items[0].ScriptEpisodeID); !errors.Is(err, drama.ErrNotFound) {
			t.Fatalf("越权详情应返回不存在: %v", err)
		}
	}
	if _, err = repo.GetVideoEpisodeWorkspace(ctx, org, owner, project, uuid.NewString()); !errors.Is(err, drama.ErrNotFound) {
		t.Fatal("不存在的剧本应返回不存在")
	}
}
