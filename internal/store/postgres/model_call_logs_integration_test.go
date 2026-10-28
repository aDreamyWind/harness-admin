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
		WHERE l.call_id IS NULL LIMIT 1`).Scan(&callID, &organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		t.Skip("测试数据库没有可关联的历史模型调用")
	}
	if err != nil {
		t.Fatal(err)
	}
	request := []byte(`{"messages":[{"role":"user","content":"测试原文"}]}`)
	response := []byte(`{"choices":[{"message":{"content":"测试结果"}}]}`)
	if err := insertModelCallLog(ctx, tx, callID, modelgateway.RecordCallInput{
		OrganizationID: organizationID, Source: "ai_drama", Operation: "chat",
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
	if string(savedRequest) != string(request) || string(savedResponse) != string(response) || source != "ai_drama" || operation != "chat" {
		t.Fatal("模型调用原始请求、响应或来源未正确保存")
	}
}
