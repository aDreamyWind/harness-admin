package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

type checker struct{ err error }

func (c checker) Ping(context.Context) error { return c.err }

func TestHealthEndpoint(t *testing.T) {
	router := newTestRouter(checker{}, checker{})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 %d", response.Code, http.StatusOK)
	}
	if response.Header().Get("X-Request-ID") == "" {
		t.Fatal("响应缺少 X-Request-ID")
	}
}

func TestReadinessEndpointReportsDependencyFailure(t *testing.T) {
	router := newTestRouter(checker{err: errors.New("db down")}, checker{})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("状态码 = %d，期望 %d", response.Code, http.StatusServiceUnavailable)
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if body["error"] == nil {
		t.Fatal("失败响应缺少 error")
	}
}

func newTestRouter(database, cache HealthChecker) http.Handler {
	return New(Dependencies{
		AppName:     "Harness Admin",
		Environment: "test",
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Database:    database,
		Cache:       cache,
	})
}
