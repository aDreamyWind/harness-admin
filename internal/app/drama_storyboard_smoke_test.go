package app

import (
	"context"
	"github.com/google/uuid"
	"harness-admin/internal/cache"
	"harness-admin/internal/config"
	"harness-admin/internal/database"
	"harness-admin/internal/drama"
	"harness-admin/internal/modelgateway"
	"harness-admin/internal/modelprovider"
	"harness-admin/internal/secret"
	pgstore "harness-admin/internal/store/postgres"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// 只在显式指定参考项目时调用真实文本模型，临时数据独立创建和清理，不生成图片或视频。
func TestStoryboardConfiguredModelSmoke(t *testing.T) { configuredStoryboardSample(t, false) }
func TestStoryboardPacingConfiguredModelSmoke(t *testing.T) {
	configuredStoryboardSample(t, false, true)
}

// 同一虚构小剧本，比较供应商默认思考与按角色策略；不是完整历史版本的性能基准。
func TestStoryboardReasoningPolicyComparison(t *testing.T) {
	t.Run("供应商默认", func(t *testing.T) { configuredStoryboardSample(t, true) })
	t.Run("按角色策略", func(t *testing.T) { configuredStoryboardSample(t, false) })
}

type meteredDramaWriter struct {
	*dramaWriter
	baseline  bool
	calls     int
	reasoning int
}

func (w *meteredDramaWriter) CompleteObserved(ctx context.Context, org, user, model, prompt string, o drama.ModelCallOptions) (string, error) {
	w.calls++
	if w.baseline {
		o.Stage = ""
	}
	next := o.OnProgress
	o.OnProgress = func(p drama.ModelProgress) error {
		if p.Phase == "finished" {
			if p.ReasoningTokens != nil {
				w.reasoning += *p.ReasoningTokens
			}
		}
		if next != nil {
			return next(p)
		}
		return nil
	}
	// 临时对比不无限等待，超时也记为失败；不会恢复用户的正式运行。
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	return w.dramaWriter.CompleteObserved(ctx, org, user, model, prompt, o)
}
func configuredStoryboardSample(t *testing.T, baseline bool, pacingCase ...bool) {
	pacingSample := len(pacingCase) > 0 && pacingCase[0]
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
	writer := &meteredDramaWriter{dramaWriter: &dramaWriter{gateway: gateway, forwarder: modelprovider.NewGatewayForwarder(cfg.ModelPrivateHosts)}, baseline: baseline}

	service := drama.NewService(repo, writer).WithVisuals(repo, nil, nil).WithStyles(repo).WithStoryboards(repo).WithRepairCoordinator(repo)
	project, batch, episode, asset := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	script := "【旧书店 内 夜】\n林夏推开店门，停在门边。\n林夏：门怎么开着？\n她转头看向桌上的密封信，没有拆开。"
	if pacingSample {
		script = "【旧书店 内 夜】\n林夏进门拿起抹布。\n林夏走到书桌。\n林夏擦好桌面，整理好纸笔。\n林夏：准备好了。"
	}
	_, err = repo.Create(ctx, drama.Project{ID: project, OrganizationID: org, OwnerID: owner, Title: "分镜真实联调（临时）", SourceText: script, SourceLength: len([]rune(script)), SourceMode: "upload", Style: "日系动漫", StyleID: "anime-2d", AspectRatio: "9:16", ModelCode: "writer", Status: "ready", Phase: "overview_done"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, e := db.Exec(context.Background(), `DELETE FROM drama_projects WHERE id=$1`, project); e != nil {
			t.Error(e)
		}
	}()
	if _, err = db.Exec(ctx, `INSERT INTO drama_batches(id,project_id,batch_number,start_offset,end_offset,selection_mode,boundary,status,revision,analysis_revision) VALUES($1,$2,1,0,$3,'beginning','end','ready',1,1)`, batch, project, len([]rune(script))); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO drama_script_episodes(id,project_id,batch_id,number,title,script_text) VALUES($1,$2,$3,1,'雨夜归店',$4)`, episode, project, batch, script); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO drama_assets(id,project_id,kind,name,description) VALUES($1,$2,'character','林夏','二十五岁，齐肩黑发，灰色风衣')`, asset, project); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.AddEpisodeAsset(ctx, org, owner, project, episode, asset, ""); err != nil {
		t.Fatal(err)
	}
	job, err := service.StartStoryboard(ctx, org, owner, project, episode, false)
	if err != nil {
		t.Fatal(err)
	}
	if pacingSample {
		if ok, e := repo.ClaimStoryboard(ctx, job.Run.ID); e != nil || !ok {
			t.Fatal(e)
		}
		if err = repo.SaveStoryboardPlan(ctx, job.Run.ID, []drama.StoryboardSegment{{Title: "整理书店", StartLine: 1, EndLine: 5, StartState: "书店门口", EndState: "书桌前准备完毕"}}); err != nil {
			t.Fatal(err)
		}
		original, e := repo.GetStoryboard(ctx, org, owner, project, episode, job.Run.ID)
		if e != nil {
			t.Fatal(e)
		}
		seg := original.Segments[0]
		seg.FormIDs = []string{}
		seg.Shots = []drama.StoryboardShot{
			{Description: "林夏进门拿起抹布。", ShotSize: "中景", Camera: "固定", Duration: 6, SourceLines: []int{1, 2}, Utterances: []drama.StoryboardUtterance{}},
			{Description: "林夏走到书桌。", ShotSize: "中景", Camera: "固定", Duration: 6, SourceLines: []int{3}, Utterances: []drama.StoryboardUtterance{}},
			{Description: "林夏擦好桌面，整理好纸笔。", ShotSize: "中景", Camera: "固定", Duration: 6, SourceLines: []int{4}, Utterances: []drama.StoryboardUtterance{}},
			{Description: "林夏开口。", ShotSize: "近景", Camera: "固定", Duration: 3, SourceLines: []int{5}, Utterances: []drama.StoryboardUtterance{{Kind: "dialogue", Speaker: "林夏", Text: "准备好了。", SourceLine: 5}}},
		}
		if err = repo.SaveStoryboardSegment(ctx, original.ID, seg); err != nil {
			t.Fatal(err)
		}
		if err = repo.FinishStoryboard(ctx, original.ID, "ready", "临时原稿21秒"); err != nil {
			t.Fatal(err)
		}
		original, e = repo.GetStoryboard(ctx, org, owner, project, episode, original.ID)
		if e != nil {
			t.Fatal(e)
		}
		job, err = service.StartStoryboardPacing(ctx, org, owner, project, episode, original.ID, drama.StoryboardPacingRequest{Revision: original.Revision, SegmentID: original.Segments[0].ID})
		if err != nil {
			t.Fatal(err)
		}
	}
	started := time.Now()
	t.Logf("开始真实分镜拆分；临时运行 %s", job.Run.ID)
	if err = service.GenerateStoryboard(job); err != nil {
		t.Fatal(err)
	}
	page, err := service.Storyboards(ctx, org, owner, project, episode, "")
	if err != nil {
		t.Fatal(err)
	}
	r := page.Current
	if r == nil || r.Status != "ready" || len(r.Segments) == 0 {
		t.Fatalf("分镜未完成: %+v", r)
	}
	if pacingSample {
		applied := false
		for _, ticket := range r.Repairs {
			if ticket.Node == "pacing" && ticket.Status == "resolved" && ticket.BeforeDuration != nil && ticket.AfterDuration != nil && *ticket.AfterDuration < *ticket.BeforeDuration {
				applied = true
				t.Logf("真实节奏优化已采纳：%d→%d镜头，%.1f→%.1f秒", *ticket.BeforeShotCount, *ticket.AfterShotCount, *ticket.BeforeDuration, *ticket.AfterDuration)
			}
		}
		if !applied {
			t.Fatalf("真实模型没有完成可采纳的节奏优化：%+v", r.Repairs)
		}
	}
	shots := 0
	for _, s := range r.Segments {
		shots += len(s.Shots)
		if s.Status != "ready" {
			t.Fatal("片段未通过")
		}
	}
	if err = service.ConfirmStoryboard(ctx, org, owner, project, episode, r.ID, r.Revision); err != nil {
		t.Fatal(err)
	}
	t.Logf("真实模型联调通过：默认策略=%t；%d片段，%d镜头；请求%d次，返工%d次；思考%d Token；耗时%s；检查与确认通过", baseline, len(r.Segments), shots, writer.calls, len(r.Repairs), writer.reasoning, time.Since(started).Round(time.Millisecond))
}

func TestStoryboardRepairConfiguredModelSmoke(t *testing.T) {
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

	service := drama.NewService(repo, writer).WithVisuals(repo, nil, nil).WithStyles(repo).WithStoryboards(repo).WithRepairCoordinator(repo)
	project, batch, episode, asset := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	script := "【旧书店 内 夜】\n旁白：我们必须完整保留这段台词，让每一个字都清楚地传达给观众，不能为了凑镜头时长而省略关键的信息。"

	_, err = repo.Create(ctx, drama.Project{ID: project, OrganizationID: org, OwnerID: owner, Title: "分镜真实联调（临时）", SourceText: script, SourceLength: len([]rune(script)), SourceMode: "upload", Style: "日系动漫", StyleID: "anime-2d", AspectRatio: "9:16", ModelCode: "writer", Status: "ready", Phase: "overview_done"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, e := db.Exec(context.Background(), `DELETE FROM drama_projects WHERE id=$1`, project); e != nil {
			t.Error(e)
		}
	}()
	if _, err = db.Exec(ctx, `INSERT INTO drama_batches(id,project_id,batch_number,start_offset,end_offset,selection_mode,boundary,status,revision,analysis_revision) VALUES($1,$2,1,0,$3,'beginning','end','ready',1,1)`, batch, project, len([]rune(script))); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO drama_script_episodes(id,project_id,batch_id,number,title,script_text) VALUES($1,$2,$3,1,'雨夜归店',$4)`, episode, project, batch, script); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO drama_assets(id,project_id,kind,name,description) VALUES($1,$2,'character','林夏','二十五岁，齐肩黑发，灰色风衣')`, asset, project); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.AddEpisodeAsset(ctx, org, owner, project, episode, asset, ""); err != nil {
		t.Fatal(err)
	}

	job, err := service.StartStoryboard(ctx, org, owner, project, episode, false)
	if err != nil {
		t.Fatal(err)
	}
	if ok, e := repo.ClaimStoryboard(ctx, job.Run.ID); e != nil || !ok {
		t.Fatal(e)
	}
	if err = repo.SaveStoryboardPlan(ctx, job.Run.ID, []drama.StoryboardSegment{{Title: "旁白", StartLine: 1, EndLine: 2, StartState: "书店空镜", EndState: "停在书桌"}}); err != nil {
		t.Fatal(err)
	}
	page, e := service.Storyboards(ctx, org, owner, project, episode, job.Run.ID)
	if e != nil {
		t.Fatal(e)
	}
	segment := page.Current.Segments[0]
	segment.FormIDs = []string{}
	segment.Shots = []drama.StoryboardShot{{Description: "旧书店内景，镜头缓慢推进到桌面，雨声在窗外延续。", ShotSize: "中景", Camera: "缓慢推进", Duration: 1.5, SourceLines: []int{1, 2}, Utterances: []drama.StoryboardUtterance{{Kind: "vo", Speaker: "旁白", Text: strings.Split(script, "旁白：")[1], SourceLine: 2}}}}
	if err = repo.SaveStoryboardCandidate(ctx, job.Run.ID, segment); err != nil {
		t.Fatal(err)
	}
	if err = repo.FinishStoryboard(ctx, job.Run.ID, "failed", "模拟时长不足"); err != nil {
		t.Fatal(err)
	}
	job, err = service.StartStoryboard(ctx, org, owner, project, episode, true)
	if err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	t.Logf("开始真实分镜拆分；临时运行 %s", job.Run.ID)
	if err = service.GenerateStoryboard(job); err != nil {
		t.Fatal(err)
	}
	page, err = service.Storyboards(ctx, org, owner, project, episode, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Current.Repairs) == 0 {
		t.Fatal("没有经过中心派工")
	}
	r := page.Current
	if r == nil || r.Status != "ready" || len(r.Segments) == 0 {
		t.Fatalf("分镜未完成: %+v", r)
	}
	shots := 0
	for _, s := range r.Segments {
		shots += len(s.Shots)
		if s.Status != "ready" {
			t.Fatal("片段未通过")
		}
	}
	if err = service.ConfirmStoryboard(ctx, org, owner, project, episode, r.ID, r.Revision); err != nil {
		t.Fatal(err)
	}
	t.Logf("真实模型联调通过：%d片段，%d镜头，耗时%s", len(r.Segments), shots, time.Since(started).Round(time.Second))
}
