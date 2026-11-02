package postgres

import (
	"context"
	"harness-admin/internal/drama"
)

func (r *DramaRepository) ListRepairTickets(ctx context.Context, scope drama.RepairTicket) ([]drama.RepairTicket, error) {
	rows, err := r.db.Query(ctx, `SELECT id,reporter,problem,state_hash,node,target_item,reason,instructions,return_to,status,outcome,created_at FROM drama_repair_tickets WHERE project_id=$1 AND kind=$2 AND run_id=$3 AND ($4<0 OR item=$4) ORDER BY created_at,id`, scope.ProjectID, scope.Kind, scope.RunID, scope.Item)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []drama.RepairTicket{}
	for rows.Next() {
		t := scope
		if err = rows.Scan(&t.ID, &t.Reporter, &t.Problem, &t.StateHash, &t.Node, &t.TargetItem, &t.Reason, &t.Instructions, &t.ReturnTo, &t.Status, &t.Outcome, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (r *DramaRepository) CreateRepairTicket(ctx context.Context, t drama.RepairTicket) error {
	// 仅执行中的真实运行可派工。入口服务已校验租户/所有者，运行主键不可由模型更改。
	tag, err := r.db.Exec(ctx, `INSERT INTO drama_repair_tickets(id,project_id,kind,run_id,item,reporter,problem,state_hash,node,target_item,reason,instructions,return_to,status)
 SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'assigned'
 WHERE ($3='storyboard' AND EXISTS(SELECT 1 FROM drama_storyboard_runs WHERE id=$4 AND project_id=$2 AND status='running')) OR ($3='batch' AND EXISTS(SELECT 1 FROM drama_batches WHERE analysis_run_id=$4 AND project_id=$2 AND status='running'))`, t.ID, t.ProjectID, t.Kind, t.RunID, t.Item, t.Reporter, t.Problem, t.StateHash, t.Node, t.TargetItem, t.Reason, t.Instructions, t.ReturnTo)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return drama.ErrStoryboardConflict
	}
	return nil
}
func (r *DramaRepository) FinishRepairTicket(ctx context.Context, id, status, outcome string) error {
	tag, err := r.db.Exec(ctx, `UPDATE drama_repair_tickets SET status=$2,outcome=$3,updated_at=now() WHERE id=$1 AND status='assigned'`, id, status, outcome)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return drama.ErrStoryboardConflict
	}
	return nil
}
