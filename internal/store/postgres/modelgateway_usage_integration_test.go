package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/modelgateway"
)

func TestModelGatewayUsageQueriesAgainstDatabase(t *testing.T) {
	url := os.Getenv("HARNESS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("未配置只读数据库集成测试地址")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	organizationID := uuid.NewString()
	err = pool.QueryRow(ctx, `SELECT id::TEXT FROM organizations ORDER BY created_at LIMIT 1`).Scan(&organizationID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
	repository := NewModelGatewayRepository(pool, nil)
	for _, groupBy := range []string{"department", "user", "model"} {
		page, err := repository.ListUsage(ctx, modelgateway.UsageQuery{
			OrganizationID: organizationID, Month: "2026-10", GroupBy: groupBy, Page: 1, PageSize: 20,
		})
		if err != nil {
			t.Fatalf("%s 维度统计 SQL 执行失败: %v", groupBy, err)
		}
		if page.GroupBy != groupBy || page.Items == nil || page.CallCount < 0 || page.Total < 0 {
			t.Fatalf("%s 维度统计结果不正确: %#v", groupBy, page)
		}
	}
}
