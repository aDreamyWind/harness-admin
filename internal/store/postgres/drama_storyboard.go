package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"harness-admin/internal/drama"
)

const storyboardColumns = `r.id,r.source_episode_id,r.project_id,r.version,r.revision,r.status,r.phase,r.message,r.error_message,r.script_text,r.source_hash,r.model_code,r.style_id,r.aspect_ratio,r.created_at,r.updated_at`

func scanStoryboard(row pgx.Row) (drama.StoryboardRun, error) {
	r := drama.StoryboardRun{Repairs: []drama.RepairTicket{}, Segments: []drama.StoryboardSegment{}, Events: []drama.StoryboardEvent{}}
	err := row.Scan(&r.ID, &r.EpisodeID, &r.ProjectID, &r.Version, &r.Revision, &r.Status, &r.Phase, &r.Message, &r.Error, &r.ScriptText, &r.SourceHash, &r.ModelCode, &r.StyleID, &r.AspectRatio, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, drama.ErrNotFound
	}
	return r, err
}
func (r *DramaRepository) ListStoryboards(ctx context.Context, org, user, project, episode string) ([]drama.StoryboardRun, error) {
	rows, err := r.db.Query(ctx, `SELECT `+storyboardColumns+` FROM drama_storyboard_runs r JOIN drama_projects p ON p.id=r.project_id WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3 AND r.source_episode_id=$4 ORDER BY r.version DESC`, org, user, project, episode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []drama.StoryboardRun{}
	for rows.Next() {
		v, e := scanStoryboard(rows)
		if e != nil {
			return nil, e
		}
		v.ScriptText = ""
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *DramaRepository) GetStoryboard(ctx context.Context, org, user, project, episode, id string) (drama.StoryboardRun, error) {
	out, err := scanStoryboard(r.db.QueryRow(ctx, `SELECT `+storyboardColumns+` FROM drama_storyboard_runs r JOIN drama_projects p ON p.id=r.project_id WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3 AND r.source_episode_id=$4 AND r.id=$5`, org, user, project, episode, id))
	if err != nil {
		return out, err
	}
	rows, err := r.db.Query(ctx, `SELECT id,number,title,scene,purpose,start_line,end_line,start_state,end_state,status,notes FROM drama_storyboard_segments WHERE run_id=$1 ORDER BY number`, id)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		v := drama.StoryboardSegment{Shots: []drama.StoryboardShot{}, FormIDs: []string{}, Bindings: []drama.StoryboardBinding{}}
		if err = rows.Scan(&v.ID, &v.Number, &v.Title, &v.Scene, &v.Purpose, &v.StartLine, &v.EndLine, &v.StartState, &v.EndState, &v.Status, &v.Notes); err != nil {
			rows.Close()
			return out, err
		}
		out.Segments = append(out.Segments, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}

	// 统一按run_id批量读取，轮询成本不随片段数增长为逐镜头查询。
	positions := map[string]int{}
	for i, seg := range out.Segments {
		positions[seg.ID] = i
	}
	rows, err = r.db.Query(ctx, `SELECT b.segment_id,b.form_id,COALESCE(b.image_id::text,'') FROM drama_storyboard_bindings b JOIN drama_storyboard_segments s ON s.id=b.segment_id WHERE s.run_id=$1 ORDER BY s.number,b.form_id`, id)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var segment string
		var binding drama.StoryboardBinding
		if err = rows.Scan(&segment, &binding.FormID, &binding.ImageID); err != nil {
			rows.Close()
			return out, err
		}
		v := &out.Segments[positions[segment]]
		v.FormIDs = append(v.FormIDs, binding.FormID)
		v.Bindings = append(v.Bindings, binding)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	type shotPos struct{ segment, index int }
	shots := map[string]shotPos{}
	rows, err = r.db.Query(ctx, `SELECT x.id,x.segment_id,x.number,x.description,x.shot_size,x.camera,x.sound,x.duration,ARRAY(SELECT line_number FROM drama_storyboard_shot_sources WHERE shot_id=x.id ORDER BY line_number) FROM drama_storyboard_shots x JOIN drama_storyboard_segments s ON s.id=x.segment_id WHERE s.run_id=$1 ORDER BY s.number,x.number`, id)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var segment string
		x := drama.StoryboardShot{Utterances: []drama.StoryboardUtterance{}}
		if err = rows.Scan(&x.ID, &segment, &x.Number, &x.Description, &x.ShotSize, &x.Camera, &x.Sound, &x.Duration, &x.SourceLines); err != nil {
			rows.Close()
			return out, err
		}
		i := positions[segment]
		shots[x.ID] = shotPos{i, len(out.Segments[i].Shots)}
		out.Segments[i].Shots = append(out.Segments[i].Shots, x)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = r.db.Query(ctx, `SELECT u.shot_id,u.kind,u.speaker,u.text,u.source_line FROM drama_storyboard_utterances u JOIN drama_storyboard_shots x ON x.id=u.shot_id JOIN drama_storyboard_segments s ON s.id=x.segment_id WHERE s.run_id=$1 ORDER BY s.number,x.number,u.number`, id)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var shot string
		var u drama.StoryboardUtterance
		if err = rows.Scan(&shot, &u.Kind, &u.Speaker, &u.Text, &u.SourceLine); err != nil {
			rows.Close()
			return out, err
		}
		at := shots[shot]
		v := &out.Segments[at.segment].Shots[at.index]
		v.Utterances = append(v.Utterances, u)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}

	rows, err = r.db.Query(ctx, `SELECT id,message,created_at FROM (SELECT id,message,created_at FROM drama_storyboard_events WHERE run_id=$1 ORDER BY id DESC LIMIT 150) x ORDER BY id`, id)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var e drama.StoryboardEvent
		if err = rows.Scan(&e.ID, &e.Message, &e.CreatedAt); err != nil {
			return out, err
		}
		out.Events = append(out.Events, e)
	}
	return out, rows.Err()
}
func (r *DramaRepository) lockStoryboardEpisode(ctx context.Context, tx pgx.Tx, org, user, project, episode string) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT e.id FROM drama_script_episodes e JOIN drama_projects p ON p.id=e.project_id WHERE p.id=$3 AND e.id=$4 AND p.organization_id=$1 AND p.owner_id=$2 FOR UPDATE OF e`, org, user, project, episode).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return drama.ErrNotFound
	}
	return err
}
func (r *DramaRepository) QueueStoryboard(ctx context.Context, org, user string, in drama.StoryboardRun, resume bool) (drama.StoryboardRun, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return in, err
	}
	defer tx.Rollback(ctx)
	if err = r.lockStoryboardEpisode(ctx, tx, org, user, in.ProjectID, in.EpisodeID); err != nil {
		return in, err
	}
	var sourceCurrent bool
	if err = tx.QueryRow(ctx, `SELECT e.script_text=$2 AND b.status='ready' AND b.revision=b.analysis_revision FROM drama_script_episodes e JOIN drama_batches b ON b.id=e.batch_id WHERE e.id=$1`, in.EpisodeID, in.ScriptText).Scan(&sourceCurrent); err != nil {
		return in, err
	}
	if !sourceCurrent {
		return in, drama.ErrStoryboardConflict
	}

	var active bool
	var latest string
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM drama_storyboard_runs WHERE source_episode_id=$1 AND status IN ('queued','running')),COALESCE((SELECT id::text FROM drama_storyboard_runs WHERE source_episode_id=$1 ORDER BY version DESC LIMIT 1),'')`, in.EpisodeID).Scan(&active, &latest); err != nil {
		return in, err
	}
	if active {
		return in, drama.ErrStoryboardConflict
	}
	if resume {
		if latest != in.ID {
			return in, drama.ErrStoryboardConflict
		}
		tag, e := tx.Exec(ctx, `UPDATE drama_storyboard_runs SET status='queued',error_message='',message='等待继续拆分',updated_at=now() WHERE id=$1 AND status='failed' AND source_hash=$2`, in.ID, in.SourceHash)
		if e != nil {
			return in, e
		}
		if tag.RowsAffected() != 1 {
			return in, drama.ErrStoryboardConflict
		}
	} else {
		if err = tx.QueryRow(ctx, `SELECT COALESCE(max(version),0)+1 FROM drama_storyboard_runs WHERE source_episode_id=$1`, in.EpisodeID).Scan(&in.Version); err != nil {
			return in, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO drama_storyboard_runs(id,episode_id,source_episode_id,project_id,version,status,script_text,source_hash,model_code,style_id,aspect_ratio,message) VALUES($1,$2,$2,$3,$4,'queued',$5,$6,$7,$8,$9,'等待导演规划')`, in.ID, in.EpisodeID, in.ProjectID, in.Version, in.ScriptText, in.SourceHash, in.ModelCode, in.StyleID, in.AspectRatio); err != nil {
			return in, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return in, err
	}
	return r.GetStoryboard(ctx, org, user, in.ProjectID, in.EpisodeID, in.ID)
}
func (r *DramaRepository) ClaimStoryboard(ctx context.Context, id string) (bool, error) {
	tag, err := r.db.Exec(ctx, `UPDATE drama_storyboard_runs SET status='running',updated_at=now() WHERE id=$1 AND status='queued'`, id)
	return tag.RowsAffected() == 1, err
}
func (r *DramaRepository) SaveStoryboardPlan(ctx context.Context, id string, segments []drama.StoryboardSegment) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM drama_storyboard_runs WHERE id=$1 FOR UPDATE`, id).Scan(&status); err != nil {
		return err
	}
	if status != "running" {
		return drama.ErrStoryboardConflict
	}
	for i, seg := range segments {
		if _, err = tx.Exec(ctx, `INSERT INTO drama_storyboard_segments(id,run_id,number,title,scene,purpose,start_line,end_line,start_state,end_state,status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'pending')`, uuid.NewString(), id, i+1, seg.Title, seg.Scene, seg.Purpose, seg.StartLine, seg.EndLine, seg.StartState, seg.EndState); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (r *DramaRepository) writeStoryboardSegment(ctx context.Context, tx pgx.Tx, run drama.StoryboardRun, seg drama.StoryboardSegment, old []drama.StoryboardBinding, adoptMissing bool) error {
	// 图片必须来自对应形象；编辑同一引用时沿用原来采用的图片版本。
	for _, form := range seg.FormIDs {
		var image *string
		err := tx.QueryRow(ctx, `SELECT f.current_image_id::text FROM drama_asset_forms f JOIN drama_assets a ON a.id=f.asset_id JOIN drama_episode_asset_links l ON l.asset_id=a.id WHERE f.id=$1 AND a.project_id=$2 AND l.episode_id=$3`, form, run.ProjectID, run.EpisodeID).Scan(&image)
		if errors.Is(err, pgx.ErrNoRows) {
			return drama.ErrInvalidInput
		}
		if err != nil {
			return err
		}
		for _, b := range old {
			if b.FormID == form && (!adoptMissing || b.ImageID != "") {
				image = nil
				if b.ImageID != "" {
					v := b.ImageID
					image = &v
				}
			}
		}
		if image != nil {
			var valid bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM drama_image_jobs WHERE id=$1 AND form_id=$2 AND status='succeeded')`, *image, form).Scan(&valid); err != nil {
				return err
			}
			if !valid {
				return drama.ErrInvalidInput
			}
		}
		if _, err = tx.Exec(ctx, `INSERT INTO drama_storyboard_bindings(segment_id,form_id,image_id) VALUES($1,$2,$3)`, seg.ID, form, image); err != nil {
			return err
		}
	}
	for i, shot := range seg.Shots {
		id := uuid.NewString()
		if _, err := tx.Exec(ctx, `INSERT INTO drama_storyboard_shots(id,segment_id,number,description,shot_size,camera,sound,duration) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, seg.ID, i+1, shot.Description, shot.ShotSize, shot.Camera, shot.Sound, shot.Duration); err != nil {
			return err
		}
		for _, n := range shot.SourceLines {
			if _, err := tx.Exec(ctx, `INSERT INTO drama_storyboard_shot_sources(shot_id,line_number) VALUES($1,$2)`, id, n); err != nil {
				return err
			}
		}
		for j, u := range shot.Utterances {
			if _, err := tx.Exec(ctx, `INSERT INTO drama_storyboard_utterances(id,shot_id,number,kind,speaker,text,source_line) VALUES($1,$2,$3,$4,$5,$6,$7)`, uuid.NewString(), id, j+1, u.Kind, u.Speaker, u.Text, u.SourceLine); err != nil {
				return err
			}
		}
	}
	return nil
}
func (r *DramaRepository) SaveStoryboardSegment(ctx context.Context, runID string, seg drama.StoryboardSegment) error {
	return r.persistStoryboardSegment(ctx, runID, seg, true)
}
func (r *DramaRepository) SaveStoryboardCandidate(ctx context.Context, runID string, seg drama.StoryboardSegment) error {
	return r.persistStoryboardSegment(ctx, runID, seg, false)
}
func (r *DramaRepository) persistStoryboardSegment(ctx context.Context, runID string, seg drama.StoryboardSegment, ready bool) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var run drama.StoryboardRun
	if err = tx.QueryRow(ctx, `SELECT r.project_id,r.source_episode_id FROM drama_storyboard_runs r JOIN drama_storyboard_segments s ON s.run_id=r.id WHERE r.id=$1 AND s.id=$2 AND r.status='running' AND s.status='pending' FOR UPDATE OF r,s`, runID, seg.ID).Scan(&run.ProjectID, &run.EpisodeID); errors.Is(err, pgx.ErrNoRows) {
		return drama.ErrStoryboardConflict
	}
	if err != nil {
		return err
	}

	if _, err = tx.Exec(ctx, `DELETE FROM drama_storyboard_shots WHERE segment_id=$1`, seg.ID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM drama_storyboard_bindings WHERE segment_id=$1`, seg.ID); err != nil {
		return err
	}
	if err = r.writeStoryboardSegment(ctx, tx, run, seg, seg.Bindings, false); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE drama_storyboard_segments SET status=$3,notes=$2 WHERE id=$1`, seg.ID, seg.Notes, map[bool]string{true: "ready", false: "pending"}[ready]); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *DramaRepository) StoryboardProgress(ctx context.Context, id, phase, message string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE drama_storyboard_runs SET phase=$2,message=$3,updated_at=now() WHERE id=$1 AND status='running'`, id, phase, message)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return drama.ErrStoryboardConflict
	}
	if _, err = tx.Exec(ctx, `INSERT INTO drama_storyboard_events(run_id,message) VALUES($1,$2)`, id, message); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *DramaRepository) FinishStoryboard(ctx context.Context, id, status, message string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	failure := ""
	if status == "failed" {
		failure = message
	}
	tag, err := tx.Exec(ctx, `UPDATE drama_storyboard_runs SET status=$2,message=$3,error_message=$4,updated_at=now() WHERE id=$1 AND status='running'`, id, status, message, failure)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return drama.ErrStoryboardConflict
	}
	if _, err = tx.Exec(ctx, `INSERT INTO drama_storyboard_events(run_id,message) VALUES($1,$2)`, id, message); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *DramaRepository) EditStoryboard(ctx context.Context, org, user string, in drama.StoryboardRun, revision int) (drama.StoryboardRun, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return in, err
	}
	defer tx.Rollback(ctx)
	if err = r.lockStoryboardEpisode(ctx, tx, org, user, in.ProjectID, in.EpisodeID); err != nil {
		return in, err
	}
	var sourceCurrent bool
	if err = tx.QueryRow(ctx, `SELECT e.script_text=$2 AND b.status='ready' AND b.revision=b.analysis_revision FROM drama_script_episodes e JOIN drama_batches b ON b.id=e.batch_id WHERE e.id=$1`, in.EpisodeID, in.ScriptText).Scan(&sourceCurrent); err != nil {
		return in, err
	}
	if !sourceCurrent {
		return in, drama.ErrStoryboardConflict
	}

	var latestID, status string
	var rev, version int
	if err = tx.QueryRow(ctx, `SELECT id,status,revision,version FROM drama_storyboard_runs WHERE source_episode_id=$1 ORDER BY version DESC LIMIT 1 FOR UPDATE`, in.EpisodeID).Scan(&latestID, &status, &rev, &version); err != nil {
		return in, err
	}
	if latestID != in.ID || rev != revision || status == "running" || status == "queued" {
		return in, drama.ErrStoryboardConflict
	}
	// 每次人工保存生成完整独立版本，旧镜头、绑定和事件保持原样。
	old, err := r.GetStoryboard(ctx, org, user, in.ProjectID, in.EpisodeID, in.ID)
	if err != nil {
		return in, err
	}
	in.ID, in.Version, in.Status = uuid.NewString(), version+1, "ready"
	if _, err = tx.Exec(ctx, `INSERT INTO drama_storyboard_runs(id,episode_id,source_episode_id,project_id,version,status,phase,message,script_text,source_hash,model_code,style_id,aspect_ratio) VALUES($1,$2,$2,$3,$4,'ready','人工编辑','人工编辑已保存，待确认',$5,$6,$7,$8,$9)`, in.ID, in.EpisodeID, in.ProjectID, in.Version, in.ScriptText, in.SourceHash, in.ModelCode, in.StyleID, in.AspectRatio); err != nil {
		return in, err
	}
	for i, seg := range in.Segments {
		var bindings []drama.StoryboardBinding
		for _, prior := range old.Segments {
			if prior.ID == seg.ID {
				bindings = prior.Bindings
			}
		}
		seg.ID = uuid.NewString()
		if _, err = tx.Exec(ctx, `INSERT INTO drama_storyboard_segments(id,run_id,number,title,scene,purpose,start_line,end_line,start_state,end_state,status,notes) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'ready',$11)`, seg.ID, in.ID, i+1, seg.Title, seg.Scene, seg.Purpose, seg.StartLine, seg.EndLine, seg.StartState, seg.EndState, seg.Notes); err != nil {
			return in, err
		}
		if err = r.writeStoryboardSegment(ctx, tx, in, seg, bindings, true); err != nil {
			return in, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return in, err
	}
	return r.GetStoryboard(ctx, org, user, in.ProjectID, in.EpisodeID, in.ID)
}
func (r *DramaRepository) ConfirmStoryboard(ctx context.Context, org, user, project, episode, id string, revision int) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = r.lockStoryboardEpisode(ctx, tx, org, user, project, episode); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE drama_storyboard_runs SET status='confirmed',revision=revision+1,message='分镜已确认，等待素材与视频制作',updated_at=now() WHERE id=$1 AND source_episode_id=$2 AND revision=$3 AND status='ready' AND script_text=(SELECT script_text FROM drama_script_episodes WHERE id=$2) AND version=(SELECT max(version) FROM drama_storyboard_runs WHERE source_episode_id=$2)`, id, episode, revision)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return drama.ErrStoryboardConflict
	}
	return tx.Commit(ctx)
}
func (r *DramaRepository) FailInterruptedStoryboards(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `UPDATE drama_storyboard_runs SET status='failed',error_message='服务重启，分镜任务已中断。已保存片段保留，可从失败处继续。',message='任务中断',updated_at=now() WHERE status IN ('queued','running')`)
	if err != nil {
		return fmt.Errorf("恢复分镜中断任务失败: %w", err)
	}
	return nil
}

func (r *DramaRepository) ReopenStoryboardFrom(ctx context.Context, run string, number int) error {
	tag, err := r.db.Exec(ctx, `UPDATE drama_storyboard_segments s SET status='pending' FROM drama_storyboard_runs r WHERE s.run_id=r.id AND r.id=$1 AND r.status='running' AND s.number>=$2`, run, number)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return drama.ErrStoryboardConflict
	}
	return nil
}
func (r *DramaRepository) ReviseStoryboardPlan(ctx context.Context, run string, s drama.StoryboardSegment) error {
	tag, err := r.db.Exec(ctx, `UPDATE drama_storyboard_segments s SET title=$3,scene=$4,purpose=$5,start_state=$6,end_state=$7 FROM drama_storyboard_runs r WHERE s.run_id=r.id AND r.id=$1 AND s.id=$2 AND r.status='running' AND s.status='pending'`, run, s.ID, s.Title, s.Scene, s.Purpose, s.StartState, s.EndState)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return drama.ErrStoryboardConflict
	}
	return nil
}
