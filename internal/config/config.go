// Package config 读取并验证监听地址、数据库路径和 Cookie 设置。
package config

import (
	"fmt"
	"net"
	"strconv"
)

// Config 是非敏感运行配置；主密钥固定从工作目录 data/master.key 加载。
type Config struct {
	Addr         string
	DatabasePath string
	CookieSecure bool
}

// Load 从工作目录的 .env 和进程环境读取配置，并检查配置是否合法；已有环境变量优先。
func Load() (Config, error) {
	values, err := readEnvironment()
	if err != nil {
		return Config{}, err
	}
	return loadDatabase(values)
}

// loadDatabase 读取监听地址、数据库路径和 Cookie 设置，并检查端口范围及非安全 Cookie 的使用条件。
func loadDatabase(values environment) (Config, error) {
	addr := values.get("EPAY_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return Config{}, fmt.Errorf("invalid EPAY_ADDR: %w", err)
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return Config{}, fmt.Errorf("EPAY_ADDR port must be between 1 and 65535")
	}
	databasePath := values.get("EPAY_DB_PATH")
	if databasePath == "" {
		databasePath = "./data/epay.db"
	}
	cookieSecure := true
	if value := values.get("EPAY_COOKIE_SECURE"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("EPAY_COOKIE_SECURE must be a boolean")
		}
		cookieSecure = parsed
	}
	if !cookieSecure {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return Config{}, fmt.Errorf("insecure cookies require a numeric loopback EPAY_ADDR")
		}
	}
	return Config{Addr: addr, DatabasePath: databasePath, CookieSecure: cookieSecure}, nil
}
