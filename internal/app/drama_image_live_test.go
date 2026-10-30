package app

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"harness-admin/internal/cache"
	"harness-admin/internal/config"
	"harness-admin/internal/drama"
	"harness-admin/internal/modelgateway"
	"harness-admin/internal/modelprovider"
	"harness-admin/internal/secret"
	pgstore "harness-admin/internal/store/postgres"
	"os"
	"testing"
	"time"
)

// 此测试会真实调用当前用户已配置的图片模型，必须显式开启，不在常规测试中扣费。
func TestDramaImageLiveSmoke(t *testing.T) {
	if os.Getenv("HARNESS_TEST_LIVE_DRAMA_IMAGE") != "1" {
		t.Skip("设置 HARNESS_TEST_LIVE_DRAMA_IMAGE=1 后执行真实图片生成")
	}
	seedProject := os.Getenv("HARNESS_TEST_DRAMA_PROJECT_ID")
	if seedProject == "" {
		t.Skip("需指定 HARNESS_TEST_DRAMA_PROJECT_ID，显式选择测试使用的账号项目")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	db, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var org, user string
	if err = db.QueryRow(ctx, `SELECT organization_id,owner_id FROM drama_projects WHERE id=$1`, seedProject).Scan(&org, &user); err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher(cfg.ModelCredentialKey)
	if err != nil {
		t.Fatal(err)
	}
	redis, err := cache.Open(ctx, cfg.RedisAddress, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		t.Fatal(err)
	}
	defer redis.Close()
	gateway := modelgateway.NewService(pgstore.NewModelGatewayRepository(db, cipher), cache.NewGatewayRequestLimiter(redis), pgstore.NewModelPolicyRepository(db))
	forwarder := modelprovider.NewGatewayForwarder(cfg.ModelPrivateHosts)
	repo := pgstore.NewDramaRepository(db)
	service := drama.NewService(repo, &dramaWriter{gateway: gateway, forwarder: forwarder}).WithVisuals(repo, &dramaImageEngine{gateway: gateway, forwarder: forwarder}, &dramaMediaFiles{directory: t.TempDir()})
	project, asset := uuid.NewString(), uuid.NewString()
	_, err = repo.Create(ctx, drama.Project{ID: project, OrganizationID: org, OwnerID: user, Title: "临时图片生成验证", SourceText: "陶瓷杯", SourceLength: 3, SourceMode: "upload", Style: "写实产品摄影", AspectRatio: "16:9", ModelCode: "test", Status: "ready", Phase: "overview_done"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, e := db.Exec(context.Background(), `DELETE FROM drama_projects WHERE id=$1`, project); e != nil {
			t.Errorf("清理临时项目失败: %v", e)
		}
	}()
	if _, err = db.Exec(ctx, `INSERT INTO drama_assets(id,project_id,kind,name,description) VALUES($1,$2,'prop','白色陶瓷杯','单个白色陶瓷杯，纯色背景，清晰完整，无文字。')`, asset, project); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Canvas(ctx, org, user, project); err != nil {
		t.Fatal(err)
	}
	job, err := service.StartImage(ctx, org, user, project, asset, drama.ImageInput{})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err = service.GenerateImage(job); err != nil {
		t.Fatal(err)
	}
	board, err := service.Canvas(ctx, org, user, project)
	if err != nil || len(board.Forms) != 1 || board.Forms[0].ImageStatus != "succeeded" || board.Forms[0].ImageURL == "" {
		t.Fatal("真实图片未保存", err)
	}
	data, mime, err := service.ImageContent(ctx, org, user, project, job.ID)
	if err != nil || len(data) < 100 {
		t.Fatal("图片内容无法读取", err)
	}
	t.Logf("真实模型图片生成完成，用时 %s，%s，%d 字节", time.Since(started).Round(time.Second), mime, len(data))
}
