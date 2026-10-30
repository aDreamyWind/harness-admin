package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"harness-admin/internal/cache"
	"harness-admin/internal/config"
	"harness-admin/internal/database"
	"harness-admin/internal/drama"
	"harness-admin/internal/modelgateway"
	"harness-admin/internal/modelprovider"
	"harness-admin/internal/secret"
	pgstore "harness-admin/internal/store/postgres"
)

// 显式指定参考项目才会真实调用该用户配置的模型；仅创建并清理临时项目，不修改参考项目。
func TestDramaWorkflowConfiguredModelSmoke(t *testing.T) {
	reference := os.Getenv("HARNESS_TEST_DRAMA_MODEL_PROJECT_ID")
	if reference == "" {
		t.Skip("设置参考项目后才执行真实模型流式联调")
	}
	if name := os.Getenv("OIDC_SIGNING_PRIVATE_KEY_FILE"); name != "" && !filepath.IsAbs(name) {
		_, sourceFile, _, _ := runtime.Caller(0)
		t.Setenv("OIDC_SIGNING_PRIVATE_KEY_FILE", filepath.Join(filepath.Dir(sourceFile), "../..", name))
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	redis, err := cache.Open(ctx, cfg.RedisAddress, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		t.Fatal(err)
	}
	defer redis.Close()
	cipher, err := secret.NewCipher(cfg.ModelCredentialKey)
	if err != nil {
		t.Fatal(err)
	}
	var org, owner string
	if err = db.QueryRow(ctx, `SELECT organization_id,owner_id FROM drama_projects WHERE id=$1`, reference).Scan(&org, &owner); err != nil {
		t.Fatal(err)
	}
	repo := pgstore.NewDramaRepository(db)
	gateway := modelgateway.NewService(pgstore.NewModelGatewayRepository(db, cipher), cache.NewGatewayRequestLimiter(redis), pgstore.NewModelPolicyRepository(db))
	writer := &dramaWriter{gateway: gateway, forwarder: modelprovider.NewGatewayForwarder(cfg.ModelPrivateHosts)}
	service := drama.NewService(repo, writer).WithStyles(repo)
	source := "第一章 雨夜来信\n林夏二十五岁，留着齐肩黑发，穿灰色风衣。她在雨夜回到祖父留下的旧书店。店内只有一盏暖黄色台灯亮着。\n林夏：门怎么开着？\n她在柜台上发现一封密封信。信封上写着她的名字，信纸写着：明天早上八点，请带旧钥匙到北街钟楼。\n林夏从抽屉里取出祖父留下的铜钥匙，钥匙柄刻着一只燕子。\n第二章 门外脚步\n书店门外响起脚步声。林夏将信纸折好，握紧铜钥匙。穿深蓝雨衣的邻居周伯站在门口。\n周伯声音沙哑：我看见店里亮灯，过来看看。\n林夏：周伯，您知道北街钟楼吗？\n周伯盯着她手里的铜钥匙，停顿了一下。\n周伯：明早我陪你去。\n林夏点头，把钥匙收进口袋。雨声中，钟楼远远传来一声钟响。"
	source = strings.ReplaceAll(source, "\n", "\r\n")
	id := uuid.NewString()
	_, err = repo.Create(ctx, drama.Project{ID: id, OrganizationID: org, OwnerID: owner, Title: "工作流真实模型联调（临时）", SourceText: source, SourceLength: len([]rune(source)), SourceMode: "upload", Style: "日系动漫", StyleID: "anime-2d", AspectRatio: "9:16", ModelCode: "writer", Status: "ready", Phase: "overview_done"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := db.Exec(context.Background(), `DELETE FROM drama_projects WHERE id=$1`, id); err != nil {
			t.Errorf("清理临时项目失败: %v", err)
		}
	}()
	batch, err := repo.CreateBatch(ctx, org, owner, drama.Batch{ID: uuid.NewString(), ProjectID: id, EndOffset: len([]rune(source)), SelectionMode: "beginning", Boundary: "end"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := service.StartBatchAnalysis(ctx, org, owner, id, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	t.Log("开始真实模型联调：美术、编号证据和单元检查点、独立批次上下文、范围规划、命名、逐集生成和累计审校")
	if err = service.AnalyzeBatch(job); err != nil {
		t.Fatal(err)
	}
	result, err := repo.GetBatchResult(ctx, org, owner, id, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Batch.Status != "ready" || len(result.Episodes) == 0 || len(result.Assets.Characters) == 0 {
		t.Fatal("真实工作流未产生正式剧集与资产")
	}
	steps, err := repo.ListBatchSteps(ctx, org, owner, id, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	nameSteps := 0
	contextSteps := 0
	evidenceSteps := 0
	for _, step := range steps {
		if step.Stage == "evidence" {
			evidenceSteps++
			if step.Status != "completed" {
				t.Fatal("证据单元未完成")
			}
			var data struct {
				Units []struct {
					Facts []drama.SourceFact `json:"facts"`
				} `json:"units"`
			}
			if err = json.Unmarshal([]byte(step.ResultText), &data); err != nil {
				t.Fatal(err)
			}
			for _, unit := range data.Units {
				for _, fact := range unit.Facts {
					if len(fact.SourceIDs) == 0 || len(fact.References) == 0 {
						t.Fatal("真实模型没有返回编号引用")
					}
					for _, ref := range fact.References {
						if string([]rune(source)[ref.StartOffset:ref.EndOffset]) != ref.Quote {
							t.Fatal("原文位置映射错误")
						}
					}
				}
			}
		}
		if step.Stage == "context" {
			contextSteps++
		}
		if step.Stage == "name" {
			nameSteps++
		}
	}
	if result.Batch.Name == "" || result.Batch.Name == "未命名批次" || nameSteps != 1 || contextSteps != 1 || evidenceSteps == 0 {
		t.Fatalf("独立起名未成功：名称=%q，起名步骤=%d", result.Batch.Name, nameSteps)
	}
	t.Logf("真实流式链路通过：%d 个步骤、%d 集，批次名=%s，耗时 %s", len(steps), len(result.Episodes), result.Batch.Name, time.Since(start).Round(time.Second))
}
