package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/drama"
)

func TestDramaSaveAnalysisSQLAcceptsAllStages(t *testing.T) {
	url := os.Getenv("HARNESS_TEST_WRITE_DATABASE_URL")
	if url == "" {
		t.Skip("设置 HARNESS_TEST_WRITE_DATABASE_URL 后验证 PostgreSQL 解析保存语句")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := NewDramaRepository(pool)
	missingID := "00000000-0000-0000-0000-000000000000"
	for _, item := range []struct {
		name  string
		phase string
		data  json.RawMessage
	}{
		{name: "开始故事解析", phase: "story"},
		{name: "保存故事概览", phase: "story", data: json.RawMessage(`{"synopsis":"测试"}`)},
		{name: "保存资产", phase: "assets", data: json.RawMessage(`{"characters":[],"scenes":[],"props":[]}`)},
		{name: "保存分集", phase: "episodes", data: json.RawMessage(`{"episodes":[]}`)},
		{name: "完成解析", phase: "complete"},
	} {
		t.Run(item.name, func(t *testing.T) {
			err := repository.SaveAnalysis(ctx, missingID, missingID, missingID, "running", item.phase, item.data)
			if !errors.Is(err, drama.ErrNotFound) {
				t.Fatalf("预期语句成功执行且未找到项目，实际错误: %v", err)
			}
		})
	}
}
