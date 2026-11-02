package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/modelgateway"
)

func TestModelCallLogPersistsRawPayloadInTransaction(t *testing.T) {
	url := os.Getenv("HARNESS_TEST_WRITE_DATABASE_URL")
	if url == "" {
		t.Skip("设置 HARNESS_TEST_WRITE_DATABASE_URL 后验证原始模型调用日志")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var callID, organizationID string
	err = tx.QueryRow(ctx, `SELECT c.id, c.organization_id FROM model_gateway_calls c
		LEFT JOIN model_call_logs l ON l.call_id = c.id
		LIMIT 1`).Scan(&callID, &organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		t.Skip("测试数据库没有可关联的历史模型调用")
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM model_call_logs WHERE call_id=$1`, callID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	headers, thought, content, finished := now.Add(time.Millisecond), now.Add(2*time.Millisecond), now.Add(3*time.Millisecond), now.Add(5*time.Millisecond)
	timing := &modelgateway.CallTiming{Stage: "director", ReasoningPolicy: "低强度思考", RequestedAt: now, HeadersAt: &headers, FirstReasoningAt: &thought, FirstContentAt: &content, FinishedAt: finished, ReasoningTokens: func() *int { v := 12; return &v }()}
	request := []byte(`{"messages":[{"role":"user","content":"测试原文"}]}`)
	response := []byte(`{"choices":[{"message":{"content":"测试结果"}}]}`)
	if err := insertModelCallLog(ctx, tx, callID, modelgateway.RecordCallInput{
		Timing: timing, OrganizationID: organizationID, Source: "ai_drama", Operation: "chat",
		RequestBody: request, ResponseBody: response, ErrorMessage: "",
	}); err != nil {
		t.Fatal(err)
	}
	var savedRequest, savedResponse []byte
	var source, operation string
	if err := tx.QueryRow(ctx, `SELECT request_body, response_body, source, operation
		FROM model_call_logs WHERE call_id=$1`, callID).Scan(&savedRequest, &savedResponse, &source, &operation); err != nil {
		t.Fatal(err)
	}
	var savedTiming modelgateway.CallTiming
	if err = tx.QueryRow(ctx, `SELECT workflow_stage,reasoning_policy,requested_at,headers_at,first_reasoning_at,first_content_at,finished_at,reasoning_tokens FROM model_call_logs WHERE call_id=$1`, callID).Scan(&savedTiming.Stage, &savedTiming.ReasoningPolicy, &savedTiming.RequestedAt, &savedTiming.HeadersAt, &savedTiming.FirstReasoningAt, &savedTiming.FirstContentAt, &savedTiming.FinishedAt, &savedTiming.ReasoningTokens); err != nil {
		t.Fatal(err)
	}
	if savedTiming.Stage != "director" || (savedTiming.ReasoningTokens == nil || *savedTiming.ReasoningTokens != 12) || !savedTiming.RequestedAt.Equal(now) || savedTiming.FirstContentAt == nil || !savedTiming.FirstContentAt.Equal(content) {
		t.Fatal("阶段时间或用量未正确持久化", savedTiming)
	}
	if string(savedRequest) != string(request) || string(savedResponse) != string(response) || source != "ai_drama" || operation != "chat" {
		t.Fatal("模型调用原始请求、响应或来源未正确保存")
	}
}
