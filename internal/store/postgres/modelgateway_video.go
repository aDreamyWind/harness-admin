package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"harness-admin/internal/identity"
	"harness-admin/internal/modelgateway"
)

func (r *ModelGatewayRepository) SaveVideoTask(ctx context.Context, input modelgateway.SaveVideoTaskInput) (modelgateway.VideoTask, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return modelgateway.VideoTask{}, err
	}
	defer tx.Rollback(ctx)
	task := modelgateway.VideoTask{
		ID: uuid.NewString(), Caller: input.Caller, Route: input.Route, UpstreamTaskID: input.UpstreamTaskID,
	}
	_, err = tx.Exec(ctx, `INSERT INTO model_gateway_video_tasks
		(id, organization_id, auth_type, api_key_id, user_id, model_catalog_entry_id, provider_connection_id, upstream_task_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		task.ID, input.Caller.OrganizationID, input.Caller.AuthType, nullableUUID(input.Caller.APIKeyID), nullableUUID(input.Caller.UserID),
		input.Route.ModelID, input.Route.ProviderID, input.UpstreamTaskID)
	if err != nil {
		return modelgateway.VideoTask{}, err
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.Caller.OrganizationID, ActorID: input.Caller.UserID, RequestID: input.RequestID,
		Action: "model.gateway.video_task_create", ResourceType: "model_video_task", ResourceID: task.ID,
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"modelCode": input.Route.ModelCode, "apiKeyId": input.Caller.APIKeyID},
	}); err != nil {
		return modelgateway.VideoTask{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return modelgateway.VideoTask{}, err
	}
	return task, nil
}

func (r *ModelGatewayRepository) FindVideoTask(ctx context.Context, caller modelgateway.Caller, taskID string) (modelgateway.VideoTask, error) {
	task := modelgateway.VideoTask{ID: taskID, Caller: caller}
	var encryptedSecret []byte
	err := r.pool.QueryRow(ctx, `
		SELECT t.upstream_task_id, m.id, m.code, m.model_type, m.status, m.upstream_model_id,
		       p.id, p.provider_kind, p.status, p.base_url, p.request_timeout_ms, p.stream_usage_enabled, c.encrypted_secret
		FROM model_gateway_video_tasks t
		JOIN model_catalog_entries m ON m.organization_id = t.organization_id AND m.id = t.model_catalog_entry_id
		JOIN model_provider_connections p ON p.organization_id = t.organization_id AND p.id = t.provider_connection_id
		JOIN model_provider_credentials c ON c.provider_connection_id = p.id
		WHERE t.id = $1 AND t.organization_id = $2 AND t.auth_type = $3
		  AND ((t.auth_type = 'api_key' AND t.api_key_id = NULLIF($4, '')::UUID)
		    OR (t.auth_type = 'user' AND t.user_id = NULLIF($5, '')::UUID))
	`, taskID, caller.OrganizationID, caller.AuthType, caller.APIKeyID, caller.UserID).Scan(
		&task.UpstreamTaskID, &task.Route.ModelID, &task.Route.ModelCode, &task.Route.ModelType,
		&task.Route.ModelStatus, &task.Route.UpstreamModelID, &task.Route.ProviderID, &task.Route.ProviderKind,
		&task.Route.ProviderStatus, &task.Route.ProviderBaseURL, &task.Route.ProviderTimeoutMS,
		&task.Route.ProviderStreamUsageEnabled, &encryptedSecret,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return modelgateway.VideoTask{}, modelgateway.ErrVideoTaskNotFound
	}
	if err != nil {
		return modelgateway.VideoTask{}, err
	}
	task.Route.ProviderAPIKey, err = r.cipher.Open(encryptedSecret)
	if err != nil {
		return modelgateway.VideoTask{}, fmt.Errorf("解密视频任务供应商凭据失败: %w", err)
	}
	return task, nil
}
