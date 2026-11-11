// Command epay 启动单用户收款服务，负责进程信号和退出状态。
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

// main 处理程序入口；执行失败时输出错误，并以非零状态退出。
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("service stopped", "error", err)
		os.Exit(1)
	}
}

// run 读取配置并启动服务，收到退出信号后关闭服务；发生错误时交给调用方处理。
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
