// Command epay 启动单管理员收款服务，负责进程信号和退出状态。
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"epay/internal/app"
	"epay/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("service stopped", "error", err)
		os.Exit(1)
	}
}

// run 使初始化与运行错误沿调用链返回，不在业务包中退出进程。
func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	application, err := app.New(cfg, logger)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return application.Run(ctx)
}
