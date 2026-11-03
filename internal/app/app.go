// Package app 是组合根：注册插件、装配 HTTP 服务并管理进程生命周期。
package app

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"epay/internal/config"
	"epay/internal/httpapi"
	"epay/internal/plugin"
	"epay/internal/plugin/alipaycode"
)

// App 持有已装配的服务；依赖通过构造函数传入，不使用可变全局变量。
type App struct {
	server  *http.Server
	logger  *slog.Logger
	plugins *plugin.Registry
}

// New 只装配已存在的能力；框架阶段的支付接口明确返回未实现。
func New(cfg config.Config, logger *slog.Logger) (*App, error) {
	registry, err := plugin.NewRegistry(alipaycode.New())
	if err != nil {
		return nil, err
	}
	return &App{
		server: &http.Server{
			Addr:              cfg.Addr,
			Handler:           httpapi.NewHandler().Routes(),
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      15 * time.Second,
			IdleTimeout:       60 * time.Second,
			MaxHeaderBytes:    16 << 10,
		},
		logger:  logger,
		plugins: registry,
	}, nil
}

// Run 在收到终止信号时停止接收请求，给予已进入的请求退出时间。
func (a *App) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", a.server.Addr)
	if err != nil {
		return err
	}
	a.logger.Info("service started", "addr", listener.Addr().String(),
		"stage", "scaffold", "registered_plugins", a.plugins.IDs())
	stopped := make(chan error, 1)
	go func() { stopped <- a.server.Serve(listener) }()
	select {
	case err := <-stopped:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := a.server.Shutdown(shutdownCtx); err != nil {
			_ = a.server.Close()
			return err
		}
		err := <-stopped
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
