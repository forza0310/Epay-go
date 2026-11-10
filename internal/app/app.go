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
	"epay/internal/plugin/builtin"
)

// App 持有已装配的服务；依赖通过构造函数传入。
type App struct {
	server  *http.Server
	logger  *slog.Logger
	plugins *plugin.Registry
}

// Go 没有专门的构造方法语法。New 是一种常用命名惯例，它只是普通函数，名字也可以换成其他名称。
func New(cfg config.Config, logger *slog.Logger) (*App, error) {
	registry, err := builtin.NewRegistry()
	if err != nil {
		return nil, err
	}
	// App{...} 是结构体复合字面量，对它取地址会得到指向新变量的指针；可以把 &App{...} 理解为 Java 里的 new App(...)
	// 编译器通过“逃逸分析”，决定值存储在堆还是栈；这里的返回指针指向的对象会被后续逻辑使用，编译器会把它放在堆上
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
// (a *App) 叫做接收者，表示这个方法关联到 *App 类型。
func (a *App) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", a.server.Addr)
	if err != nil {
		return err
	}
	a.logger.Info("service started", "addr", listener.Addr().String(),
		"stage", "scaffold", "registered_plugins", a.plugins.IDs())

	// make 创建并初始化一个带同步能力、传递error值的 channel。接收时如果没有可以接收的值，goroutine 就会等待。
	// 发送值 stopped <- err
	// 接收值 err := <-stopped
	stopped := make(chan error, 1)

	// go... 启动一个新的 goroutine 并发执行
	// func() {...} 是一个匿名函数表达式。在它后面加圆括号表示调用这个函数。它是一个闭包，可以使用外层的变量【类似于c++引用捕获】。
	go func() { stopped <- a.server.Serve(listener) }()

	// 如果没有分支就绪且没有default，会阻塞
	// 如果分支同时就绪，会随机选择一个分支，写在前面的分支没有优先权
	select {
	case err := <-stopped: // 服务自行结束，这个 channel 被写入内容不再阻塞
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case _ = <-ctx.Done(): // Ctrl+C，ctx 被取消时，从这个只读 channel 接收不再继续阻塞
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := a.server.Shutdown(shutdownCtx); err != nil { // 超时时间为10s
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
