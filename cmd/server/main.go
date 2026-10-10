package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"harness-admin/internal/app"
	"harness-admin/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	logger := newLogger(cfg.Environment)
	application, err := app.New(context.Background(), cfg, logger)
	if err != nil {
		logger.Error("应用初始化失败", "error", err)
		os.Exit(1)
	}
	defer application.Close()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("HTTP 服务已启动", "address", cfg.HTTPAddress, "environment", cfg.Environment)
		errCh <- application.Server.ListenAndServe()
	}()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-signalCh:
		logger.Info("收到停止信号", "signal", sig.String())
	case serveErr := <-errCh:
		if !errors.Is(serveErr, http.ErrServerClosed) {
			logger.Error("HTTP 服务异常退出", "error", serveErr)
			os.Exit(1)
		}
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := application.Server.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP 服务未能正常停止", "error", err)
		os.Exit(1)
	}
	logger.Info("HTTP 服务已停止")
}

func newLogger(environment string) *slog.Logger {
	level := slog.LevelInfo
	if environment == "development" {
		level = slog.LevelDebug
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     level,
		AddSource: environment == "development",
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.TimeKey {
				attr.Value = slog.StringValue(attr.Value.Time().Format(time.RFC3339Nano))
			}
			return attr
		},
	})
	return slog.New(handler)
}
