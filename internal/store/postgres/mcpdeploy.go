package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/identity"
	"harness-admin/internal/mcpadmin"
	"harness-admin/internal/mcpdeploy"
)

type MCPDeploymentRepository struct{ pool *pgxpool.Pool }

func NewMCPDeploymentRepository(pool *pgxpool.Pool) *MCPDeploymentRepository {
	return &MCPDeploymentRepository{pool: pool}
}

// 构建任务只在当前进程内运行；进程重启后所有残留 building 均已中断。
func (r *MCPDeploymentRepository) RecoverInterruptedBuilds(ctx context.Context) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `WITH recovered AS (
		UPDATE mcp_service_versions SET build_status='failed',
			error_message='构建进程中断，请重新上传或选择固定提交构建新版本', finished_at=NOW()
			WHERE build_status='building'
			RETURNING service_id
		)
		UPDATE mcp_services s SET deployment_status='failed',
			deployment_error='上次构建进程中断，请重新构建', updated_at=NOW()
		WHERE s.deployment_status='building' AND s.id IN (SELECT service_id FROM recovered)`)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *MCPDeploymentRepository) AcquireLock(ctx context.Context, serviceID string) (func() error, error) {
	connection, err := r.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	var locked bool
	err = connection.QueryRow(ctx, `SELECT pg_try_advisory_lock(20261021, hashtext($1))`, serviceID).Scan(&locked)
	if err != nil {
		connection.Release()
		return nil, err
	}
	if !locked {
		connection.Release()
		return nil, mcpdeploy.ErrBusy
	}
	return func() error {
		defer connection.Release()
		unlockCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var unlocked bool
		if err := connection.QueryRow(unlockCtx, `SELECT pg_advisory_unlock(20261021, hashtext($1))`, serviceID).Scan(&unlocked); err != nil {
			return err
		}
		if !unlocked {
			return fmt.Errorf("MCP 部署锁未持有")
		}
		return nil
	}, nil
}

const mcpVersionColumns = "id, service_id, source_type, source_ref, revision, runtime_type, entry_file, image_ref, build_status, build_log, error_message, created_at, finished_at"

func scanMCPVersion(row pgx.Row) (mcpdeploy.Version, error) {
	var v mcpdeploy.Version
	err := row.Scan(&v.ID, &v.ServiceID, &v.SourceType, &v.SourceRef, &v.Revision,
		&v.RuntimeType, &v.EntryFile, &v.ImageRef, &v.BuildStatus, &v.BuildLog,
		&v.ErrorMessage, &v.CreatedAt, &v.FinishedAt)
	return v, err
}

func (r *MCPDeploymentRepository) Create(ctx context.Context, input mcpdeploy.Input) (mcpdeploy.Result, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mcpdeploy.Result{}, err
	}
	defer tx.Rollback(ctx)
	serviceID, versionID := uuid.NewString(), uuid.NewString()
	code := "mcp_s_" + uuid.NewString()[:8] + uuid.NewString()[:8]
	container := "hadmin-mcp-" + serviceID
	_, err = tx.Exec(ctx, `INSERT INTO mcp_services
		(id, organization_id, code, name, description, transport, upstream_url, created_by,
		 source_type, runtime_type, entry_file, container_name, deployment_status)
		VALUES ($1,$2,$3,$4,$5,'stdio','',$6,$7,$8,$9,$10,'building')`,
		serviceID, input.OrganizationID, code, input.Name, input.Description, input.UserID,
		input.SourceType, input.RuntimeType, input.EntryFile, container)
	if err != nil {
		return mcpdeploy.Result{}, err
	}
	if err := insertMCPVersion(ctx, tx, versionID, serviceID, input); err != nil {
		return mcpdeploy.Result{}, err
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{OrganizationID: input.OrganizationID, ActorID: input.UserID,
		RequestID: input.RequestID, Action: "mcp.deployment.import", ResourceType: "mcp_service", ResourceID: serviceID,
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"sourceType": input.SourceType, "sourceRef": input.SourceRef, "revision": input.Revision}}); err != nil {
		return mcpdeploy.Result{}, err
	}
	result, err := getMCPDeploymentResult(ctx, tx, input.OrganizationID, serviceID, versionID)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (r *MCPDeploymentRepository) AddVersion(ctx context.Context, input mcpdeploy.Input) (mcpdeploy.Result, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mcpdeploy.Result{}, err
	}
	defer tx.Rollback(ctx)
	var sourceType, status string
	err = tx.QueryRow(ctx, `SELECT source_type, deployment_status FROM mcp_services WHERE id=$1 AND organization_id=$2 FOR UPDATE`,
		input.ServiceID, input.OrganizationID).Scan(&sourceType, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return mcpdeploy.Result{}, mcpadmin.ErrNotFound
	}
	if err != nil {
		return mcpdeploy.Result{}, err
	}
	if sourceType == "remote" || status == "building" || status == "running" {
		return mcpdeploy.Result{}, mcpdeploy.ErrInvalidInput
	}
	versionID := uuid.NewString()
	if err := insertMCPVersion(ctx, tx, versionID, input.ServiceID, input); err != nil {
		return mcpdeploy.Result{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE mcp_services SET deployment_status='building', deployment_error='', updated_at=NOW() WHERE id=$1`, input.ServiceID)
	if err != nil {
		return mcpdeploy.Result{}, err
	}
	result, err := getMCPDeploymentResult(ctx, tx, input.OrganizationID, input.ServiceID, versionID)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func insertMCPVersion(ctx context.Context, tx pgx.Tx, id, serviceID string, input mcpdeploy.Input) error {
	_, err := tx.Exec(ctx, `INSERT INTO mcp_service_versions
		(id, service_id, source_type, source_ref, revision, runtime_type, entry_file, description, build_status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'building')`, id, serviceID,
		input.SourceType, input.SourceRef, input.Revision, input.RuntimeType, input.EntryFile, input.Description)
	return err
}

func (r *MCPDeploymentRepository) UpdateBuildSource(ctx context.Context, actor mcpadmin.Actor, serviceID, versionID, revision, entryFile, description string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE mcp_service_versions v SET revision=$4, entry_file=$5, description=$6
		FROM mcp_services s WHERE v.id=$1 AND v.service_id=$2 AND v.build_status='building'
		AND s.id=v.service_id AND s.organization_id=$3`, versionID, serviceID, actor.OrganizationID, revision, entryFile, description)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return mcpdeploy.ErrNotFound
	}
	return nil
}

func (r *MCPDeploymentRepository) CompleteBuild(ctx context.Context, actor mcpadmin.Actor, serviceID, versionID, imageRef, log, message string) (mcpdeploy.Result, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mcpdeploy.Result{}, err
	}
	defer tx.Rollback(ctx)
	var lockedID string
	err = tx.QueryRow(ctx, `SELECT id FROM mcp_services WHERE id=$1 AND organization_id=$2 FOR UPDATE`, serviceID, actor.OrganizationID).Scan(&lockedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return mcpdeploy.Result{}, mcpadmin.ErrNotFound
	}
	if err != nil {
		return mcpdeploy.Result{}, err
	}
	if len(log) > 64*1024 {
		log = log[len(log)-64*1024:]
	}
	log = strings.ReplaceAll(strings.ToValidUTF8(log, "�"), "\x00", "")
	buildStatus, deploymentStatus := "succeeded", "stopped"
	if message != "" {
		buildStatus, deploymentStatus = "failed", "failed"
	}
	auditResult := "success"
	if message != "" {
		auditResult = "failed"
	}
	tag, err := tx.Exec(ctx, `UPDATE mcp_service_versions SET image_ref=$3, build_status=$4,
		build_log=$5, error_message=$6, finished_at=NOW() WHERE id=$1 AND service_id=$2 AND build_status='building'`,
		versionID, serviceID, imageRef, buildStatus, log, message)
	if err != nil {
		return mcpdeploy.Result{}, err
	}
	if tag.RowsAffected() == 0 {
		return mcpdeploy.Result{}, mcpdeploy.ErrNotFound
	}
	if message == "" {
		_, err = tx.Exec(ctx, `UPDATE mcp_services s SET source_type=v.source_type, runtime_type=v.runtime_type,
			entry_file=v.entry_file, image_ref=v.image_ref, current_revision=v.revision,
			description=COALESCE(NULLIF(v.description, ''), s.description),
			deployment_status=$3, deployment_error='', updated_at=NOW()
			FROM mcp_service_versions v WHERE s.id=$1 AND s.organization_id=$2 AND v.id=$4 AND v.service_id=s.id`,
			serviceID, actor.OrganizationID, deploymentStatus, versionID)
	} else {
		_, err = tx.Exec(ctx, `UPDATE mcp_services SET deployment_status=$3, deployment_error=$4, updated_at=NOW()
			WHERE id=$1 AND organization_id=$2`, serviceID, actor.OrganizationID, deploymentStatus, message)
	}
	if err != nil {
		return mcpdeploy.Result{}, err
	}
	if message == "" {
		if _, err := tx.Exec(ctx, `DELETE FROM mcp_service_tools WHERE service_id=$1`, serviceID); err != nil {
			return mcpdeploy.Result{}, err
		}
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{OrganizationID: actor.OrganizationID, ActorID: actor.UserID,
		RequestID: actor.RequestID, Action: "mcp.deployment.build", ResourceType: "mcp_service", ResourceID: serviceID,
		Result: auditResult, SourceIP: actor.SourceIP, Detail: map[string]any{"versionId": versionID}}); err != nil {
		return mcpdeploy.Result{}, err
	}
	result, err := getMCPDeploymentResult(ctx, tx, actor.OrganizationID, serviceID, versionID)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func getMCPDeploymentResult(ctx context.Context, tx pgx.Tx, organizationID, serviceID, versionID string) (mcpdeploy.Result, error) {
	service, err := getMCPService(ctx, tx, organizationID, serviceID)
	if err != nil {
		return mcpdeploy.Result{}, err
	}
	version, err := scanMCPVersion(tx.QueryRow(ctx, `SELECT `+mcpVersionColumns+` FROM mcp_service_versions WHERE id=$1 AND service_id=$2`, versionID, serviceID))
	return mcpdeploy.Result{Service: service, Version: version}, err
}

func (r *MCPDeploymentRepository) Get(ctx context.Context, organizationID, id string) (mcpadmin.ServiceRecord, error) {
	return getMCPService(ctx, r.pool, organizationID, id)
}

func (r *MCPDeploymentRepository) UpdateMetadata(ctx context.Context, actor mcpadmin.Actor, id, name, _ string) (mcpadmin.ServiceRecord, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE mcp_services SET name=$3, updated_at=NOW()
		WHERE id=$1 AND organization_id=$2 AND source_type <> 'remote'`, id, actor.OrganizationID, name)
	if err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	if tag.RowsAffected() == 0 {
		return mcpadmin.ServiceRecord{}, mcpadmin.ErrNotFound
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{OrganizationID: actor.OrganizationID, ActorID: actor.UserID,
		RequestID: actor.RequestID, Action: "mcp.deployment.metadata.update", ResourceType: "mcp_service", ResourceID: id,
		Result: "success", SourceIP: actor.SourceIP}); err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	item, err := getMCPService(ctx, tx, actor.OrganizationID, id)
	if err != nil {
		return item, err
	}
	return item, tx.Commit(ctx)
}

func (r *MCPDeploymentRepository) ListVersions(ctx context.Context, organizationID, id string) ([]mcpdeploy.Version, error) {
	if _, err := r.Get(ctx, organizationID, id); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT `+mcpVersionColumns+` FROM mcp_service_versions WHERE service_id=$1 ORDER BY created_at DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]mcpdeploy.Version, 0)
	for rows.Next() {
		var v mcpdeploy.Version
		if err := rows.Scan(&v.ID, &v.ServiceID, &v.SourceType, &v.SourceRef, &v.Revision,
			&v.RuntimeType, &v.EntryFile, &v.ImageRef, &v.BuildStatus, &v.BuildLog,
			&v.ErrorMessage, &v.CreatedAt, &v.FinishedAt); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (r *MCPDeploymentRepository) SetRuntimeStatus(ctx context.Context, actor mcpadmin.Actor, id, status, message string) (mcpadmin.ServiceRecord, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE mcp_services SET deployment_status=$3, deployment_error=$4, updated_at=NOW()
		WHERE id=$1 AND organization_id=$2 AND source_type <> 'remote'`, id, actor.OrganizationID, status, message)
	if err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	if tag.RowsAffected() == 0 {
		return mcpadmin.ServiceRecord{}, mcpadmin.ErrNotFound
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{OrganizationID: actor.OrganizationID, ActorID: actor.UserID,
		RequestID: actor.RequestID, Action: "mcp.deployment." + status, ResourceType: "mcp_service", ResourceID: id,
		Result: "success", SourceIP: actor.SourceIP}); err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	item, err := getMCPService(ctx, tx, actor.OrganizationID, id)
	if err != nil {
		return item, err
	}
	return item, tx.Commit(ctx)
}

func (r *MCPDeploymentRepository) ActivateVersion(ctx context.Context, actor mcpadmin.Actor, id, versionID string) (mcpadmin.ServiceRecord, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE mcp_services s SET source_type=v.source_type, runtime_type=v.runtime_type,
		entry_file=v.entry_file, image_ref=v.image_ref, current_revision=v.revision,
		description=COALESCE(NULLIF(v.description, ''), s.description),
		deployment_status='stopped', deployment_error='', updated_at=NOW()
		FROM mcp_service_versions v WHERE s.id=$1 AND s.organization_id=$2 AND v.id=$3
		AND v.service_id=s.id AND v.build_status='succeeded'`, id, actor.OrganizationID, versionID)
	if err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	if tag.RowsAffected() == 0 {
		return mcpadmin.ServiceRecord{}, mcpdeploy.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM mcp_service_tools WHERE service_id=$1`, id); err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{OrganizationID: actor.OrganizationID, ActorID: actor.UserID,
		RequestID: actor.RequestID, Action: "mcp.deployment.rollback", ResourceType: "mcp_service", ResourceID: id,
		Result: "success", SourceIP: actor.SourceIP, Detail: map[string]any{"versionId": versionID}}); err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	item, err := getMCPService(ctx, tx, actor.OrganizationID, id)
	if err != nil {
		return item, err
	}
	return item, tx.Commit(ctx)
}
