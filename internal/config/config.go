// Package config 读取进程配置；支付凭证由后续加密配置存储管理。
package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
)

// Config 是启动配置，默认仅监听本机，避免框架阶段意外暴露公网。
type Config struct{ Addr string }

// Load 从环境读取配置并在启动前验证监听地址。
func Load() (Config, error) {
	addr := os.Getenv("EPAY_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return Config{}, fmt.Errorf("invalid EPAY_ADDR: %w", err)
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return Config{}, fmt.Errorf("EPAY_ADDR port must be between 1 and 65535")
	}
	return Config{Addr: addr}, nil
}
