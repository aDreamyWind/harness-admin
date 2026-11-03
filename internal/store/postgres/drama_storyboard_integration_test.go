package postgres

import (
	"context"
	"errors"
	"fmt"
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
	if err = repo.FinishRepairTicket(ctx, ticket.ID, drama.RepairResult{Status: "resolved", Outcome: "复核通过"}); err != nil {
		t.Fatal(err)
	}
	tickets, err := repo.ListRepairTickets(ctx, drama.RepairTicket{ProjectID: project, Kind: "storyboard", RunID: run.ID, Item: -1})
	if err != nil || len(tickets) != 1 || tickets[0].Status != "resolved" || tickets[0].Item != 1 {
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
	// 后台保存与轮询并发时，引用位置不能与另一次保存的正文混在一起。
	writeDone := make(chan error, 1)
	go func() {
		for n := 0; n < 20; n++ {
			copy := first
			copy.Shots = append([]drama.StoryboardShot{}, first.Shots...)
			start := 0
			copy.Shots[0].Description = "林夏推门进入"
			if n%2 == 1 {
				copy.Shots[0].Description = "😀林夏推门进入"
				start = 1
			}
			copy.Shots[0].DescriptionMentions = []drama.StoryboardMention{{Start: start, End: start + 2, FormID: form}}
			if e := repo.SaveStoryboardCandidate(ctx, run.ID, copy); e != nil {
				writeDone <- e
				return
			}
		}
		writeDone <- nil
	}()
	var snapshotError error
	for n := 0; n < 20; n++ {
		snapshot, e := repo.GetStoryboard(ctx, org, user, project, episode, run.ID)
		if e != nil {
			snapshotError = e
			break
		}
		shot := snapshot.Segments[0].Shots[0]
		if len(shot.DescriptionMentions) > 0 {
			ref := shot.DescriptionMentions[0]
			chars := []rune(shot.Description)
			if ref.End > len(chars) || string(chars[ref.Start:ref.End]) != "林夏" {
				snapshotError = fmt.Errorf("轮询读到不一致的正文和文字引用")
				break
			}
		}
	}
	if e := <-writeDone; e != nil {
		t.Fatal(e)
	}
	if snapshotError != nil {
		t.Fatal(snapshotError)
	}
	if e := repo.SaveStoryboardCandidate(ctx, run.ID, first); e != nil {
		t.Fatal(e)
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
	t.Run("片段视频关联版本并防止重复生成", func(t *testing.T) {
		ready, err := repo.GetStoryboard(ctx, org, user, project, episode, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		scope := drama.SegmentVideoScope{OrganizationID: org, UserID: user, ProjectID: project, EpisodeID: episode, RunID: run.ID, SegmentID: ready.Segments[0].ID}
		job := drama.SegmentVideo{ID: uuid.NewString(), SegmentID: scope.SegmentID, Revision: ready.Revision, ModelCode: "video-test", Duration: 4, ImageIDs: []string{image1}}
		created, err := repo.CreateSegmentVideo(ctx, scope, job)
		if err != nil || created.Status != "running" {
			t.Fatal(created, err)
		}
		duplicate := job
		duplicate.ID = uuid.NewString()
		if _, err = repo.CreateSegmentVideo(ctx, scope, duplicate); !errors.Is(err, drama.ErrSegmentVideoBusy) {
			t.Fatal("重复提交未拒绝", err)
		}
		unauthorized := scope
		unauthorized.UserID = uuid.NewString()
		if _, err = repo.ReadSegmentVideo(ctx, unauthorized, job.ID); !errors.Is(err, drama.ErrNotFound) {
			t.Fatal("不能跨用户查询视频", err)
		}
		if err = repo.FinishSegmentVideo(ctx, job.ID, "succeeded", ""); err != nil {
			t.Fatal(err)
		}
		history, err := repo.ListSegmentVideos(ctx, scope)
		if err != nil || len(history) != 1 || history[0].Status != "succeeded" || len(history[0].ImageIDs) != 1 || history[0].ImageIDs[0] != image1 {
			t.Fatal(history, err)
		}
	})

	run, err = repo.GetStoryboard(ctx, org, user, project, episode, run.ID)
	if err != nil {
		t.Fatal(err)
	}

	// 换了资产当前图片，人工修改分镜也不能悄悄替换旧采用图片。
	if _, err = pool.Exec(ctx, `UPDATE drama_asset_forms SET current_image_id=$2 WHERE id=$1`, form, image2); err != nil {
		t.Fatal(err)
	}
	run.Segments[0].Shots[0].Description = "😀林夏人工编辑后的进门动作"
	run.Segments[0].Shots[0].DescriptionMentions = []drama.StoryboardMention{{Start: 1, End: 3, FormID: form}}
	run.Segments[0].Scene = "林夏所在的书店"
	run.Segments[0].SceneMentions = []drama.StoryboardMention{{Start: 0, End: 2, FormID: form}}
	run.Segments[1].Shots[0].Utterances[0].SpeakerMentions = []drama.StoryboardMention{{Start: 0, End: 2, FormID: form}}
	edited, err := repo.EditStoryboard(ctx, org, user, run, run.Revision)
	if err != nil || edited.Version != 2 {
		t.Fatal("编辑没有创建新版本", err)
	}

	if len(edited.Segments[0].SceneMentions) != 1 || edited.Segments[0].SceneMentions[0].FormID != form || len(edited.Segments[0].Shots[0].DescriptionMentions) != 1 || edited.Segments[0].Shots[0].DescriptionMentions[0].Start != 1 || len(edited.Segments[1].Shots[0].Utterances[0].SpeakerMentions) != 1 {
		t.Fatal("文字位置和真实形象引用未随新版本保存")
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

	// 局部节奏版本保留原版本、未选片段及采用的图片；采纳与镜头同一事务保存。
	confirmed, err := repo.GetStoryboard(ctx, org, user, project, episode, edited.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.QueueStoryboardPacing(ctx, org, uuid.NewString(), confirmed, confirmed.Revision, confirmed.Segments[0].ID); !errors.Is(err, drama.ErrNotFound) {
		t.Fatal("优化越权未拒绝", err)
	}
	if _, err = repo.QueueStoryboardPacing(ctx, org, user, confirmed, confirmed.Revision+1, confirmed.Segments[0].ID); !errors.Is(err, drama.ErrStoryboardConflict) {
		t.Fatal("优化旧修订号未拒绝", err)
	}
	paced, err := repo.QueueStoryboardPacing(ctx, org, user, confirmed, confirmed.Revision, confirmed.Segments[0].ID)
	if err != nil || paced.PacingTarget != 1 || paced.Segments[0].Status != "pending" || paced.Segments[1].Status != "ready" || paced.Segments[0].Bindings[0].ImageID != image1 {
		t.Fatal("没有只复制并优化目标片段", err)
	}
	if _, err = repo.QueueStoryboardPacing(ctx, org, user, paced, paced.Revision, paced.Segments[1].ID); !errors.Is(err, drama.ErrStoryboardConflict) {
		t.Fatal("运行中优化未拒绝", err)
	}
	if ok, e := repo.ClaimStoryboard(ctx, paced.ID); e != nil || !ok {
		t.Fatal(e)
	}
	// 规划节点重写场次时，旧文字位置和场次更新必须原子生效。
	planRevision := paced.Segments[0]
	planRevision.Scene = "书店窗边"
	if e := repo.ReviseStoryboardPlan(ctx, paced.ID, planRevision); e != nil {
		t.Fatal(e)
	}
	planned, e := repo.GetStoryboard(ctx, org, user, project, episode, paced.ID)
	if e != nil || planned.Segments[0].Scene != "书店窗边" || len(planned.Segments[0].SceneMentions) != 0 {
		t.Fatal("规划重写后残留旧文字引用", e)
	}
	paced.Segments[0] = planned.Segments[0]
	pacingCandidate := paced.Segments[0]
	pacingCandidate.Shots[0].Duration = 2
	count := 1
	beforeTime, afterTime := 4.0, 2.0
	settlement := drama.RepairResult{Status: "resolved", Outcome: "已采纳并修改", AfterShotCount: &count, AfterDuration: &afterTime}
	if err = repo.SaveStoryboardPacingResult(ctx, paced.ID, pacingCandidate, uuid.NewString(), settlement); !errors.Is(err, drama.ErrStoryboardConflict) {
		t.Fatal("未知派工记录不能提交采纳", err)
	}
	rollback, e := repo.GetStoryboard(ctx, org, user, project, episode, paced.ID)
	if e != nil || rollback.Segments[0].Shots[0].Duration != 4 {
		t.Fatal("无采纳记录时镜头改动必须一起回滚", e)
	}
	paceTicket := drama.RepairTicket{ID: uuid.NewString(), ProjectID: project, Kind: "storyboard", RunID: paced.ID, Item: 1, Reporter: "节奏复核", Problem: "普通动作停留过长", StateHash: "pacing-before", Node: "pacing", TargetItem: 1, Reason: "重复铺垫", Instructions: "保留进门动作并缩短", ReturnTo: "节奏复核", BeforeShotCount: &count, BeforeDuration: &beforeTime}
	if err = repo.CreateRepairTicket(ctx, paceTicket); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveStoryboardPacingResult(ctx, paced.ID, pacingCandidate, paceTicket.ID, settlement); err != nil {
		t.Fatal(err)
	}
	actual, e := repo.GetStoryboard(ctx, org, user, project, episode, paced.ID)
	records, e2 := repo.ListRepairTickets(ctx, drama.RepairTicket{ProjectID: project, Kind: "storyboard", RunID: paced.ID, Item: -1})
	if e != nil || e2 != nil || actual.Segments[0].Shots[0].Duration != 2 || actual.Segments[1].Shots[0].Duration != 3 || len(records) != 1 || records[0].Item != 1 || records[0].Status != "resolved" || *records[0].AfterDuration != 2 {
		t.Fatal("实际镜头与采纳记录不一致", e, e2, records)
	}
	if err = repo.SaveStoryboardSegment(ctx, paced.ID, pacingCandidate); err != nil {
		t.Fatal(err)
	}
	if err = repo.FinishStoryboard(ctx, paced.ID, "ready", "仅目标片段已优化"); err != nil {
		t.Fatal(err)
	}
	oldVersion, e := repo.GetStoryboard(ctx, org, user, project, episode, confirmed.ID)
	if e != nil || oldVersion.Segments[0].Shots[0].Duration != 4 || oldVersion.Status != "confirmed" {
		t.Fatal("优化改动了历史确认版", e)
	}
	// 失败版本也可只优化已完成片段，其他待处理片段保持原样。
	if _, err = pool.Exec(ctx, `UPDATE drama_storyboard_runs SET status='failed' WHERE id=$1`, paced.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE drama_storyboard_segments SET status='pending' WHERE run_id=$1 AND number=2`, paced.ID); err != nil {
		t.Fatal(err)
	}
	incomplete, e := repo.GetStoryboard(ctx, org, user, project, episode, paced.ID)
	if e != nil {
		t.Fatal(e)
	}
	pacingPartial, e := repo.QueueStoryboardPacing(ctx, org, user, incomplete, incomplete.Revision, incomplete.Segments[0].ID)
	if e != nil || pacingPartial.PacingTarget != 1 || pacingPartial.Segments[1].Status != "pending" {
		t.Fatal("未完成片段被错误发布", e)
	}
	if ok, e := repo.ClaimStoryboard(ctx, pacingPartial.ID); e != nil || !ok {
		t.Fatal(e)
	}
	if err = repo.SaveStoryboardSegment(ctx, pacingPartial.ID, pacingPartial.Segments[0]); err != nil {
		t.Fatal(err)
	}
	if err = repo.FinishStoryboard(ctx, pacingPartial.ID, "partial", "目标片段已处理，其余可继续"); err != nil {
		t.Fatal(err)
	}
	pacingPartial, e = repo.GetStoryboard(ctx, org, user, project, episode, pacingPartial.ID)
	if e != nil {
		t.Fatal(e)
	}
	resumed, e := repo.QueueStoryboard(ctx, org, user, pacingPartial, true)
	if e != nil || resumed.PacingTarget != 0 || resumed.Segments[0].Status != "ready" || resumed.Segments[1].Status != "pending" {
		t.Fatal("继续拆分未清除局部意图或丢失检查点", e)
	}
	if ok, e := repo.ClaimStoryboard(ctx, resumed.ID); e != nil || !ok {
		t.Fatal(e)
	}
	if err = repo.FinishStoryboard(ctx, resumed.ID, "failed", "临时测试结束"); err != nil {
		t.Fatal(err)
	}
	// 预算耗尽时独立续跑，保留完成状态、未完成草稿、采用图片与原模型。
	failedResume, e := repo.GetStoryboard(ctx, org, user, project, episode, resumed.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = repo.QueueStoryboardContinuation(ctx, org, uuid.NewString(), failedResume, failedResume.Revision); !errors.Is(e, drama.ErrNotFound) {
		t.Fatal("续跑越权未拒绝", e)
	}
	if _, e = repo.QueueStoryboardContinuation(ctx, org, user, failedResume, failedResume.Revision+1); !errors.Is(e, drama.ErrStoryboardConflict) {
		t.Fatal("续跑修订冲突未拒绝", e)
	}
	continued, e := repo.QueueStoryboardContinuation(ctx, org, user, failedResume, failedResume.Revision)
	if e != nil || continued.ID == failedResume.ID || continued.Version != failedResume.Version+1 || continued.ModelCode != failedResume.ModelCode || continued.PacingTarget != 0 || continued.Segments[0].Status != "ready" || continued.Segments[1].Status != "pending" || continued.Segments[0].Bindings[0].ImageID != image1 || len(continued.Segments[1].Shots) == 0 {
		t.Fatal("续跑丢失旧成果", e)
	}
	if _, e = repo.QueueStoryboardContinuation(ctx, org, user, failedResume, failedResume.Revision); !errors.Is(e, drama.ErrStoryboardConflict) {
		t.Fatal("重复或过期续跑未拒绝", e)
	}
	if ok, e := repo.ClaimStoryboard(ctx, continued.ID); e != nil || !ok {
		t.Fatal(e)
	}
	if e = repo.ReopenStoryboardFrom(ctx, continued.ID, 1); e != nil {
		t.Fatal(e)
	}
	voice := continued.Segments[0]
	voice.Shots[0].Utterances = []drama.StoryboardUtterance{{Kind: "vo", Speaker: "旁白", Text: "", SourceLine: 1, SourceText: "人物进门。", Delivery: "visual", AdaptationReason: "由推门动作呈现"}, {Kind: "vo", Speaker: "旁白", Text: "进门。", SourceLine: 1, SourceText: "人物进门。", AdaptationReason: "精简重复解释"}}
	voice.Shots[0].Utterances[0].Delivery = "unknown"
	if e = repo.SaveStoryboardSegment(ctx, continued.ID, voice); e == nil {
		t.Fatal("未知表达方式不能静默当成口播")
	}
	voice.Shots[0].Utterances[0].Delivery = "visual"
	if e = repo.SaveStoryboardSegment(ctx, continued.ID, voice); e != nil {
		t.Fatal(e)
	}
	loaded, e := repo.GetStoryboard(ctx, org, user, project, episode, continued.ID)
	if e != nil || len(loaded.Segments[0].Shots[0].Utterances) != 2 {
		t.Fatal("旁白字段未保存", e)
	}
	voices := loaded.Segments[0].Shots[0].Utterances
	if voices[0].Delivery != "visual" || voices[0].Text != "" || voices[0].SourceText != "人物进门。" || voices[0].AdaptationReason != "由推门动作呈现" || voices[1].Delivery != "" || voices[1].Text != "进门。" {
		t.Fatal("旁白往返信息不一致", voices)
	}
	if e = repo.FinishStoryboard(ctx, continued.ID, "failed", "续跑测试结束"); e != nil {
		t.Fatal(e)
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
