package postgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"harness-admin/internal/drama"
	"os"
	"testing"
	"time"
)

func TestStoryboardPersistenceScopeResumeVersionsAndImages(t *testing.T) {
	url := os.Getenv("HARNESS_TEST_WRITE_DATABASE_URL")
	if url == "" {
		t.Skip("设置测试库后验证分镜持久化")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var org, user string
	if err = pool.QueryRow(ctx, `SELECT organization_id,id FROM users WHERE organization_id IS NOT NULL LIMIT 1`).Scan(&org, &user); err != nil {
		t.Fatal(err)
	}
	repo := NewDramaRepository(pool)
	project, batch, episode, asset := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	_, err = repo.Create(ctx, drama.Project{ID: project, OrganizationID: org, OwnerID: user, Title: "分镜临时测试", SourceText: "人物进门。\n林夏：你好。", SourceLength: 13, SourceMode: "upload", Style: "国风水墨", StyleID: "ink-wash", AspectRatio: "9:16", ModelCode: "writer", Status: "ready", Phase: "overview_done"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, e := pool.Exec(context.Background(), `DELETE FROM drama_projects WHERE id=$1`, project); e != nil {
			t.Error(e)
		}
	}()
	if _, err = pool.Exec(ctx, `INSERT INTO drama_batches(id,project_id,batch_number,start_offset,end_offset,selection_mode,boundary,status,revision,analysis_revision) VALUES($1,$2,1,0,13,'beginning','end','ready',1,1)`, batch, project); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO drama_script_episodes(id,project_id,batch_id,number,title,script_text) VALUES($1,$2,$3,1,'开门','人物进门。'||E'\n'||'林夏：你好。')`, episode, project, batch); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO drama_assets(id,project_id,kind,name,description) VALUES($1,$2,'character','林夏','主角')`, asset, project); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.AddEpisodeAsset(ctx, org, user, project, episode, asset, ""); err != nil {
		t.Fatal(err)
	}
	canvas, err := repo.ReadCanvas(ctx, org, user, project)
	if err != nil || len(canvas.Forms) != 1 {
		t.Fatal(err)
	}
	form := canvas.Forms[0].ID
	in := drama.StoryboardRun{ID: uuid.NewString(), EpisodeID: episode, ProjectID: project, ScriptText: "人物进门。\n林夏：你好。", SourceHash: "hash", ModelCode: "writer", StyleID: "ink-wash", AspectRatio: "9:16"}
	run, err := repo.QueueStoryboard(ctx, org, user, in, false)
	if err != nil {
		t.Fatal(err)
	}
	in.ID = uuid.NewString()
	if _, err = repo.QueueStoryboard(ctx, org, user, in, false); !errors.Is(err, drama.ErrStoryboardConflict) {
		t.Fatal("重复任务未拒绝", err)
	}
	if ok, err := repo.ClaimStoryboard(ctx, run.ID); err != nil || !ok {
		t.Fatal(err)
	}

	ticket := drama.RepairTicket{ID: uuid.NewString(), ProjectID: project, Kind: "storyboard", RunID: run.ID, Item: 1, Reporter: "分镜检查", Problem: "时长不足", StateHash: "draft-hash", Node: "timing", TargetItem: 1, Reason: "台词完整而时长不足", Instructions: "延长时长", ReturnTo: "分镜检查"}
	if err = repo.CreateRepairTicket(ctx, ticket); err != nil {
		t.Fatal(err)
	}
	if err = repo.FinishRepairTicket(ctx, ticket.ID, "resolved", "复核通过"); err != nil {
		t.Fatal(err)
	}
	tickets, err := repo.ListRepairTickets(ctx, drama.RepairTicket{ProjectID: project, Kind: "storyboard", RunID: run.ID, Item: -1})
	if err != nil || len(tickets) != 1 || tickets[0].Status != "resolved" {
		t.Fatal("中心派工未持久化", err)
	}
	wrong, err := repo.ListRepairTickets(ctx, drama.RepairTicket{ProjectID: uuid.NewString(), Kind: "storyboard", RunID: run.ID, Item: -1})
	if err != nil || len(wrong) != 0 {
		t.Fatal("派工范围泄漏", err)
	}
	plan := []drama.StoryboardSegment{{Title: "进门", StartLine: 1, EndLine: 1}, {Title: "问好", StartLine: 2, EndLine: 2}}
	if err = repo.SaveStoryboardPlan(ctx, run.ID, plan); err != nil {
		t.Fatal(err)
	}
	run, err = repo.GetStoryboard(ctx, org, user, project, episode, run.ID)
	if err != nil {
		t.Fatal(err)
	}

	image1, image2 := uuid.NewString(), uuid.NewString()
	for _, id := range []string{image1, image2} {
		if _, err = pool.Exec(ctx, `INSERT INTO drama_image_jobs(id,form_id,model_code,form_revision,prompt,aspect_ratio,status) VALUES($1,$2,'painter',1,'私有提示词','9:16','succeeded')`, id, form); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `UPDATE drama_asset_forms SET current_image_id=$2 WHERE id=$1`, form, image1); err != nil {
		t.Fatal(err)
	}
	first := run.Segments[0]
	first.FormIDs = []string{form}
	first.Shots = []drama.StoryboardShot{{Description: "林夏推门进入", ShotSize: "中景", Camera: "固定", Duration: 4, SourceLines: []int{1}, Utterances: []drama.StoryboardUtterance{}}}

	if err = repo.SaveStoryboardCandidate(ctx, run.ID, first); err != nil {
		t.Fatal(err)
	}
	candidate, e := repo.GetStoryboard(ctx, org, user, project, episode, run.ID)
	if e != nil || candidate.Segments[0].Status != "pending" || len(candidate.Segments[0].Shots) != 1 {
		t.Fatal("待修正候选未保存", e)
	}
	if err = repo.SaveStoryboardSegment(ctx, run.ID, first); err != nil {
		t.Fatal(err)
	}
	if err = repo.StoryboardProgress(ctx, run.ID, "片段1", "片段1已保存"); err != nil {
		t.Fatal(err)
	}
	if err = repo.FinishStoryboard(ctx, run.ID, "failed", "片段2测试失败"); err != nil {
		t.Fatal(err)
	}
	partial, err := repo.GetStoryboard(ctx, org, user, project, episode, run.ID)
	if err != nil || len(partial.Segments[0].Shots) != 1 || partial.Segments[1].Status != "pending" || len(partial.Events) != 2 {
		t.Fatal("部分成功结果或真实事件丢失", err)
	}
	for _, scope := range [][2]string{{org, uuid.NewString()}, {uuid.NewString(), user}} {
		if _, err = repo.GetStoryboard(ctx, scope[0], scope[1], project, episode, run.ID); !errors.Is(err, drama.ErrNotFound) {
			t.Fatal("越权读取未拒绝", err)
		}
	}
	run, err = repo.QueueStoryboard(ctx, org, user, partial, true)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.ClaimStoryboard(ctx, run.ID); err != nil || !ok {
		t.Fatal(err)
	}
	second := run.Segments[1]
	second.FormIDs = []string{form}
	second.Shots = []drama.StoryboardShot{{Description: "林夏开口问好", ShotSize: "近景", Camera: "固定", Duration: 3, SourceLines: []int{2}, Utterances: []drama.StoryboardUtterance{{Kind: "dialogue", Speaker: "林夏", Text: "你好。", SourceLine: 2}}}}
	if err = repo.SaveStoryboardSegment(ctx, run.ID, second); err != nil {
		t.Fatal(err)
	}
	if err = repo.FinishStoryboard(ctx, run.ID, "ready", "完成"); err != nil {
		t.Fatal(err)
	}
	run, err = repo.GetStoryboard(ctx, org, user, project, episode, run.ID)
	if err != nil {
		t.Fatal(err)
	}

	// 换了资产当前图片，人工修改分镜也不能悄悄替换旧采用图片。
	if _, err = pool.Exec(ctx, `UPDATE drama_asset_forms SET current_image_id=$2 WHERE id=$1`, form, image2); err != nil {
		t.Fatal(err)
	}
	run.Segments[0].Shots[0].Description = "人工编辑后的进门动作"
	edited, err := repo.EditStoryboard(ctx, org, user, run, run.Revision)
	if err != nil || edited.Version != 2 {
		t.Fatal("编辑没有创建新版本", err)
	}

	if edited.Segments[0].Bindings[0].ImageID != image1 {
		t.Fatal("已采用的图片版本被当前图片替换")
	}
	if _, err = repo.EditStoryboard(ctx, org, user, run, run.Revision); !errors.Is(err, drama.ErrStoryboardConflict) {
		t.Fatal("并发旧版本编辑未拒绝", err)
	}
	original, err := repo.GetStoryboard(ctx, org, user, project, episode, run.ID)
	if err != nil || original.Segments[0].Shots[0].Description == edited.Segments[0].Shots[0].Description {
		t.Fatal("旧版本被覆盖", err)
	}
	if err = repo.ConfirmStoryboard(ctx, org, user, project, episode, edited.ID, edited.Revision); err != nil {
		t.Fatal(err)
	}
	if err = repo.ConfirmStoryboard(ctx, org, user, project, episode, edited.ID, edited.Revision); !errors.Is(err, drama.ErrStoryboardConflict) {
		t.Fatal("重复确认未拒绝", err)
	}
	items, err := repo.ListVideoEpisodes(ctx, org, user, project)
	if err != nil || items[0].StoryboardStatus != "confirmed" {
		t.Fatal("列表没有显示真实状态", err)
	}
	// 新运行失败仍可读取上一版；服务恢复不重复调用模型。
	in.ID = uuid.NewString()
	next, err := repo.QueueStoryboard(ctx, org, user, in, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.FailInterruptedStoryboards(ctx); err != nil {
		t.Fatal(err)
	}
	failed, err := repo.GetStoryboard(ctx, org, user, project, episode, next.ID)
	if err != nil || failed.Status != "failed" {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE drama_script_episodes SET script_text='已经改动' WHERE id=$1`, episode); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.QueueStoryboard(ctx, org, user, failed, true); !errors.Is(err, drama.ErrStoryboardConflict) {
		t.Fatal("来源变化仍能续跑", err)
	}
}
