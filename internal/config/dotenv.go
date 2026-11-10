// 本文件加载启动工作目录的 .env，作为进程环境变量的后备配置，不执行 shell 或修改全局环境。
package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxEnvFileBytes = 64 << 10

type environment map[string]string

// get 保留显式环境变量的优先级，包括显式设置的空值。
func (e environment) get(name string) string {
	if value, exists := os.LookupEnv(name); exists {
		return value
	}
	return e[name]
}

// readEnvironment 每次加载独立配置快照；不存在 .env 时仍可纯环境变量部署。
func readEnvironment() (environment, error) {
	file, err := os.Open(".env")
	if errors.Is(err, os.ErrNotExist) {
		return environment{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read .env: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect .env: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New(".env must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxEnvFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read .env: %w", err)
	}
	if len(data) > maxEnvFileBytes || !utf8.Valid(data) || strings.ContainsRune(string(data), '\x00') {
		return nil, errors.New(".env must be valid UTF-8 without NUL and no larger than 64 KiB")
	}
	values := environment{}
	for i, line := range strings.Split(strings.TrimPrefix(string(data), "\ufeff"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") || strings.HasPrefix(line, "export\t") {
			line = strings.TrimSpace(line[len("export"):])
		}
		name, raw, found := strings.Cut(line, "=")
		name = strings.TrimSpace(name)
		if !found || !validEnvName(name) {
			return nil, fmt.Errorf(".env line %d: expected NAME=value", i+1)
		}
		if _, duplicate := values[name]; duplicate {
			return nil, fmt.Errorf(".env line %d: duplicate variable", i+1)
		}
		value, err := parseEnvValue(strings.TrimSpace(raw))
		if err != nil || strings.ContainsRune(value, '\x00') {
			// 不把原行、变量值或解析器错误写入日志，避免泄漏主密钥。
			return nil, fmt.Errorf(".env line %d: invalid value or unmatched quotes", i+1)
		}
		values[name] = value
	}
	return values, nil
}

func validEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, c := range name {
		if c != '_' && !(c >= 'A' && c <= 'Z') && !(c >= 'a' && c <= 'z') && !(i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// parseEnvValue 支持单行单双引号和行尾注释；美元符号与命令替换保持为普通文本。
func parseEnvValue(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	quote := raw[0]
	if quote == '\'' || quote == '"' {
		for i := 1; i < len(raw); i++ {
			if quote == '"' && raw[i] == '\\' {
				i++
				continue
			}
			if raw[i] != quote {
				continue
			}
			tail := strings.TrimSpace(raw[i+1:])
			if tail != "" && !strings.HasPrefix(tail, "#") {
				return "", errors.New("unexpected suffix")
			}
			if quote == '\'' {
				return raw[1:i], nil
			}
			return strconv.Unquote(raw[:i+1])
		}
		return "", errors.New("unterminated value")
	}
	for i, c := range raw {
		if c == '#' && (i == 0 || raw[i-1] == ' ' || raw[i-1] == '\t') {
			return strings.TrimSpace(raw[:i]), nil
		}
	}
	return raw, nil
}
