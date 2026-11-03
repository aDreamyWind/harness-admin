package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"harness-admin/internal/drama"
	"slices"
)

const storyboardColumns = `r.id,r.source_episode_id,r.project_id,r.version,r.revision,r.status,r.phase,r.message,r.error_message,r.script_text,r.source_hash,r.model_code,r.style_id,r.aspect_ratio,r.created_at,r.updated_at,r.pacing_target`

func scanStoryboard(row pgx.Row) (drama.StoryboardRun, error) {
	r := drama.StoryboardRun{Repairs: []drama.RepairTicket{}, Segments: []drama.StoryboardSegment{}, Events: []drama.StoryboardEvent{}}
	err := row.Scan(&r.ID, &r.EpisodeID, &r.ProjectID, &r.Version, &r.Revision, &r.Status, &r.Phase, &r.Message, &r.Error, &r.ScriptText, &r.SourceHash, &r.ModelCode, &r.StyleID, &r.AspectRatio, &r.CreatedAt, &r.UpdatedAt, &r.PacingTarget)
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
	// 正文、镜头和引用必须来自同一个快照，避免轮询恰好跨过保存事务。
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return drama.StoryboardRun{}, err
	}
	defer tx.Rollback(ctx)
	out, err := scanStoryboard(tx.QueryRow(ctx, `SELECT `+storyboardColumns+` FROM drama_storyboard_runs r JOIN drama_projects p ON p.id=r.project_id WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3 AND r.source_episode_id=$4 AND r.id=$5`, org, user, project, episode, id))
	if err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT id,number,title,scene,purpose,start_line,end_line,start_state,end_state,status,notes,lineage_id,segment_version FROM drama_storyboard_segments WHERE run_id=$1 ORDER BY number`, id)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		v := drama.StoryboardSegment{Shots: []drama.StoryboardShot{}, FormIDs: []string{}, Bindings: []drama.StoryboardBinding{}}
		if err = rows.Scan(&v.ID, &v.Number, &v.Title, &v.Scene, &v.Purpose, &v.StartLine, &v.EndLine, &v.StartState, &v.EndState, &v.Status, &v.Notes, &v.LineageID, &v.Version); err != nil {
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
	rows, err = tx.Query(ctx, `
		SELECT DISTINCT ON (anchor.id, history.segment_version) anchor.id, history.segment_version, history.run_id, history.id
		FROM drama_storyboard_segments anchor
		JOIN drama_storyboard_segments history ON history.lineage_id=anchor.lineage_id
		JOIN drama_storyboard_runs snapshot ON snapshot.id=history.run_id
		WHERE anchor.run_id=$1 AND snapshot.project_id=$2 AND snapshot.source_episode_id=$3 AND history.status='ready'
		ORDER BY anchor.id, history.segment_version DESC, snapshot.version DESC`, id, project, episode)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var anchor string
		var version drama.StoryboardSegmentVersion
		if err = rows.Scan(&anchor, &version.Version, &version.RunID, &version.SegmentID); err != nil {
			rows.Close()
			return out, err
		}
		seg := &out.Segments[positions[anchor]]
		seg.Versions = append(seg.Versions, version)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}

	rows, err = tx.Query(ctx, `SELECT b.segment_id,b.form_id,COALESCE(b.image_id::text,'') FROM drama_storyboard_bindings b JOIN drama_storyboard_segments s ON s.id=b.segment_id WHERE s.run_id=$1 ORDER BY s.number,b.form_id`, id)
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
	rows, err = tx.Query(ctx, `SELECT x.id,x.segment_id,x.number,x.description,x.shot_size,x.camera,x.sound,x.duration,ARRAY(SELECT line_number FROM drama_storyboard_shot_sources WHERE shot_id=x.id ORDER BY line_number) FROM drama_storyboard_shots x JOIN drama_storyboard_segments s ON s.id=x.segment_id WHERE s.run_id=$1 ORDER BY s.number,x.number`, id)
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
	rows, err = tx.Query(ctx, `SELECT u.shot_id,u.kind,u.speaker,u.text,u.source_line,u.source_text,u.delivery,u.adaptation_reason FROM drama_storyboard_utterances u JOIN drama_storyboard_shots x ON x.id=u.shot_id JOIN drama_storyboard_segments s ON s.id=x.segment_id WHERE s.run_id=$1 ORDER BY s.number,x.number,u.number`, id)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var shot string
		var u drama.StoryboardUtterance
		if err = rows.Scan(&shot, &u.Kind, &u.Speaker, &u.Text, &u.SourceLine, &u.SourceText, &u.Delivery, &u.AdaptationReason); err != nil {
			rows.Close()
			return out, err
		}
		if u.Delivery == "spoken" {
			u.Delivery = ""
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

	rows, err = tx.Query(ctx, `SELECT m.segment_id,COALESCE(m.shot_id::text,''),m.field,m.utterance_number,m.start_offset,m.end_offset,m.form_id FROM drama_storyboard_text_mentions m JOIN drama_storyboard_segments s ON s.id=m.segment_id WHERE s.run_id=$1 ORDER BY s.number,m.field,m.utterance_number,m.start_offset`, id)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var segment, shot, field string
		var number int
		var mention drama.StoryboardMention
		if err = rows.Scan(&segment, &shot, &field, &number, &mention.Start, &mention.End, &mention.FormID); err != nil {
			rows.Close()
			return out, err
		}
		seg := &out.Segments[positions[segment]]
		switch field {
		case "scene":
			seg.SceneMentions = append(seg.SceneMentions, mention)
		case "purpose":
			seg.PurposeMentions = append(seg.PurposeMentions, mention)
		default:
			at, ok := shots[shot]
			if !ok {
				rows.Close()
				return out, fmt.Errorf("文字引用的镜头不存在")
			}
			target := &out.Segments[at.segment].Shots[at.index]
			switch field {
			case "description":
				target.DescriptionMentions = append(target.DescriptionMentions, mention)
			case "sound":
				target.SoundMentions = append(target.SoundMentions, mention)
			case "utterance", "speaker":
				if number < 1 || number > len(target.Utterances) {
					rows.Close()
					return out, fmt.Errorf("文字引用的声音条目不存在")
				}
				if field == "utterance" {
					target.Utterances[number-1].Mentions = append(target.Utterances[number-1].Mentions, mention)
				} else {
					target.Utterances[number-1].SpeakerMentions = append(target.Utterances[number-1].SpeakerMentions, mention)
				}
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}

	rows, err = tx.Query(ctx, `SELECT id,message,created_at FROM (SELECT id,message,created_at FROM drama_storyboard_events WHERE run_id=$1 ORDER BY id DESC LIMIT 150) x ORDER BY id`, id)
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
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
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
		tag, e := tx.Exec(ctx, `UPDATE drama_storyboard_runs SET status='queued',pacing_target=0,error_message='',message='等待继续拆分',updated_at=now() WHERE id=$1 AND status IN ('failed','partial') AND source_hash=$2`, in.ID, in.SourceHash)
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
		err := tx.QueryRow(ctx, `SELECT f.current_image_id::text FROM drama_asset_forms f JOIN drama_assets a ON a.id=f.asset_id WHERE f.id=$1 AND a.project_id=$2`, form, run.ProjectID).Scan(&image)
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
	if err := writeStoryboardMentions(ctx, tx, seg.ID, "", "scene", 0, seg.SceneMentions); err != nil {
		return err
	}
	if err := writeStoryboardMentions(ctx, tx, seg.ID, "", "purpose", 0, seg.PurposeMentions); err != nil {
		return err
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
		if err := writeStoryboardMentions(ctx, tx, seg.ID, id, "description", 0, shot.DescriptionMentions); err != nil {
			return err
		}
		if err := writeStoryboardMentions(ctx, tx, seg.ID, id, "sound", 0, shot.SoundMentions); err != nil {
			return err
		}
		for j, u := range shot.Utterances {
			delivery := u.Delivery
			if delivery == "" {
				delivery = "spoken"
			}
			if _, err := tx.Exec(ctx, `INSERT INTO drama_storyboard_utterances(id,shot_id,number,kind,speaker,text,source_line,source_text,delivery,adaptation_reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, uuid.NewString(), id, j+1, u.Kind, u.Speaker, u.Text, u.SourceLine, u.SourceText, delivery, u.AdaptationReason); err != nil {
				return err
			}
			if err := writeStoryboardMentions(ctx, tx, seg.ID, id, "utterance", j+1, u.Mentions); err != nil {
				return err
			}
			if err := writeStoryboardMentions(ctx, tx, seg.ID, id, "speaker", j+1, u.SpeakerMentions); err != nil {
				return err
			}
		}
	}
	return nil
}

type pacingSettlement struct {
	TicketID string
	Result   drama.RepairResult
}

func (r *DramaRepository) SaveStoryboardPacingResult(ctx context.Context, runID string, seg drama.StoryboardSegment, ticketID string, result drama.RepairResult) error {
	return r.persistStoryboardSegment(ctx, runID, seg, false, pacingSettlement{ticketID, result})
}
func (r *DramaRepository) SaveStoryboardSegment(ctx context.Context, runID string, seg drama.StoryboardSegment) error {
	return r.persistStoryboardSegment(ctx, runID, seg, true)
}
func (r *DramaRepository) SaveStoryboardCandidate(ctx context.Context, runID string, seg drama.StoryboardSegment) error {
	return r.persistStoryboardSegment(ctx, runID, seg, false)
}
func (r *DramaRepository) persistStoryboardSegment(ctx context.Context, runID string, seg drama.StoryboardSegment, ready bool, settlements ...pacingSettlement) error {
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
	for _, settlement := range settlements {
		result := settlement.Result
		tag, e := tx.Exec(ctx, `UPDATE drama_repair_tickets SET status=$4,outcome=$5,after_shot_count=$6,after_duration=$7,updated_at=now() WHERE id=$1 AND run_id=$2 AND item=$3 AND kind='storyboard' AND node='pacing' AND status='assigned'`, settlement.TicketID, runID, seg.Number, result.Status, result.Outcome, result.AfterShotCount, result.AfterDuration)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return drama.ErrStoryboardConflict
		}
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
	return r.cloneStoryboardVersion(ctx, org, user, in, revision, "", false)
}
func (r *DramaRepository) QueueStoryboardPacing(ctx context.Context, org, user string, in drama.StoryboardRun, revision int, segmentID string) (drama.StoryboardRun, error) {
	return r.cloneStoryboardVersion(ctx, org, user, in, revision, segmentID, false)
}
func (r *DramaRepository) QueueStoryboardContinuation(ctx context.Context, org, user string, in drama.StoryboardRun, revision int) (drama.StoryboardRun, error) {
	return r.cloneStoryboardVersion(ctx, org, user, in, revision, "", true)
}
func (r *DramaRepository) cloneStoryboardVersion(ctx context.Context, org, user string, in drama.StoryboardRun, revision int, pacingSegment string, continuation bool) (drama.StoryboardRun, error) {
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
	// 编辑、节奏优化与续跑均生成完整独立版本，旧镜头、绑定和事件保持原样。
	old, err := r.GetStoryboard(ctx, org, user, in.ProjectID, in.EpisodeID, in.ID)
	if err != nil {
		return in, err
	}
	phase, message, newStatus := "人工编辑", "人工编辑已保存，待确认", "ready"
	in.PacingTarget = 0
	if continuation {
		if status != "failed" && status != "partial" {
			return in, drama.ErrStoryboardConflict
		}
		// 从库内最新快照复制，绝不采用调用方提交的镜头或更换原模型。
		in = old
		in.Segments = old.Segments
		in.PacingTarget = 0
		phase, message, newStatus = "续跑", "已保留完成片段与待修正草稿，等待继续未完成片段", "queued"
	}
	if pacingSegment != "" {
		if status != "ready" && status != "confirmed" && status != "partial" && status != "failed" {
			return in, drama.ErrStoryboardConflict
		}
		in.Segments = old.Segments
		found := false
		for _, seg := range in.Segments {
			if seg.ID == pacingSegment {
				if seg.Status != "ready" {
					return in, drama.ErrStoryboardConflict
				}
				in.PacingTarget = seg.Number
				found = true
			}
		}
		if !found {
			return in, drama.ErrNotFound
		}
		phase, message, newStatus = "节奏优化", "等待优化所选片段，其他片段保持不变", "queued"
	}
	in.ID, in.Version, in.Status = uuid.NewString(), version+1, newStatus
	if _, err = tx.Exec(ctx, `INSERT INTO drama_storyboard_runs(id,episode_id,source_episode_id,project_id,version,status,phase,message,script_text,source_hash,model_code,style_id,aspect_ratio,pacing_target) VALUES($1,$2,$2,$3,$4,$10,$11,$12,$5,$6,$7,$8,$9,$13)`, in.ID, in.EpisodeID, in.ProjectID, in.Version, in.ScriptText, in.SourceHash, in.ModelCode, in.StyleID, in.AspectRatio, newStatus, phase, message, in.PacingTarget); err != nil {
		return in, err
	}
	priorByID := make(map[string]drama.StoryboardSegment, len(old.Segments))
	for _, prior := range old.Segments {
		priorByID[prior.ID] = prior
	}
	for i, seg := range in.Segments {
		prior, inherited := priorByID[seg.ID]
		delete(priorByID, seg.ID)
		lineage, segmentVersion := uuid.NewString(), 1
		segmentStatus := "ready"
		if pacingSegment != "" || continuation {
			segmentStatus = seg.Status
		}
		if seg.ID == pacingSegment {
			segmentStatus = "pending"
		}
		if inherited {
			lineage, segmentVersion = prior.LineageID, prior.Version
			if seg.ID == pacingSegment || (continuation && seg.Status != "ready") ||
				(pacingSegment == "" && !continuation && !sameStoryboardSegmentContent(prior, seg)) {
				segmentVersion++
			}
		}
		seg.ID = uuid.NewString()
		if _, err = tx.Exec(ctx, `INSERT INTO drama_storyboard_segments(id,run_id,number,title,scene,purpose,start_line,end_line,start_state,end_state,status,notes,lineage_id,segment_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$12,$11,$13,$14)`, seg.ID, in.ID, i+1, seg.Title, seg.Scene, seg.Purpose, seg.StartLine, seg.EndLine, seg.StartState, seg.EndState, seg.Notes, segmentStatus, lineage, segmentVersion); err != nil {
			return in, err
		}
		if err = r.writeStoryboardSegment(ctx, tx, in, seg, prior.Bindings, pacingSegment == "" && !continuation); err != nil {
			return in, err
		}
	}
	if continuation {
		if _, err = tx.Exec(ctx, `INSERT INTO drama_storyboard_events(run_id,message) VALUES($1,$2)`, in.ID, fmt.Sprintf("从版本%d继续：旧版修正预算已满；已复制规划、完成镜头、待修正草稿和采用图片，旧记录保留在历史版本。", old.Version)); err != nil {
			return in, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return in, err
	}
	return r.GetStoryboard(ctx, org, user, in.ProjectID, in.EpisodeID, in.ID)
}

// 位置、数据库行主键和展示信息不影响片段自身的内容版本。
func sameStoryboardSegmentContent(a, b drama.StoryboardSegment) bool {
	normalize := func(seg drama.StoryboardSegment) drama.StoryboardSegment {
		seg.ID, seg.LineageID, seg.Number, seg.Version = "", "", 0, 0
		seg.Status, seg.Notes, seg.Versions, seg.VideoReferences, seg.Bindings = "", "", nil, nil, nil
		seg.FormIDs = append([]string{}, seg.FormIDs...)
		slices.Sort(seg.FormIDs)
		seg.Shots = slices.Clone(seg.Shots)
		for i := range seg.Shots {
			shot := &seg.Shots[i]
			shot.ID, shot.Number = "", 0
			shot.SourceLines = slices.Clone(shot.SourceLines)
			slices.Sort(shot.SourceLines)
			shot.Utterances = append([]drama.StoryboardUtterance{}, shot.Utterances...)
			for j := range shot.Utterances {
				if shot.Utterances[j].Delivery == "spoken" {
					shot.Utterances[j].Delivery = ""
				}
			}
		}
		return seg
	}
	left, leftErr := json.Marshal(normalize(a))
	right, rightErr := json.Marshal(normalize(b))
	return leftErr == nil && rightErr == nil && bytes.Equal(left, right)
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
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE drama_storyboard_segments s SET title=$3,scene=$4,purpose=$5,start_state=$6,end_state=$7 FROM drama_storyboard_runs r WHERE s.run_id=r.id AND r.id=$1 AND s.id=$2 AND r.status='running' AND s.status='pending'`, run, s.ID, s.Title, s.Scene, s.Purpose, s.StartState, s.EndState)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return drama.ErrStoryboardConflict
	}
	if _, err = tx.Exec(ctx, `DELETE FROM drama_storyboard_text_mentions WHERE segment_id=$1 AND field IN ('scene','purpose')`, s.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func writeStoryboardMentions(ctx context.Context, tx pgx.Tx, segment, shot, field string, number int, refs []drama.StoryboardMention) error {
	var shotID any
	if shot != "" {
		shotID = shot
	}
	for _, ref := range refs {
		if _, err := tx.Exec(ctx, `INSERT INTO drama_storyboard_text_mentions(segment_id,shot_id,field,utterance_number,start_offset,end_offset,form_id) VALUES($1,$2,$3,$4,$5,$6,$7)`, segment, shotID, field, number, ref.Start, ref.End, ref.FormID); err != nil {
			return err
		}
	}
	return nil
}
