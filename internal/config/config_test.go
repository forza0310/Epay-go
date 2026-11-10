// 本文件验证 .env 的工作目录限定、配置优先级、错误脱敏和安全解析边界。
package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func clearConfigEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"EPAY_ADDR", "EPAY_DB_PATH", "EPAY_MASTER_KEY", "EPAY_COOKIE_SECURE"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
}

func writeConfigFixture(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

// TestDotEnvWorkingDirectoryAndPrecedence 验证只读取工作目录的 .env，已有环境变量优先且加载不修改进程环境。
func TestDotEnvWorkingDirectoryAndPrecedence(t *testing.T) {
	clearConfigEnvironment(t)
	root := t.TempDir()
	writeConfigFixture(t, filepath.Join(root, "go.mod"), "module epay\n")
	key := base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901"))
	writeConfigFixture(t, filepath.Join(root, ".env"), "EPAY_ADDR=127.0.0.1:9999\n")
	child := filepath.Join(root, "cmd", "epay")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	// 即使父目录包含 go.mod，也只能读取实际工作目录里的 .env。
	writeConfigFixture(t, filepath.Join(child, ".env"), "\ufeff# fixture\r\nexport EPAY_MASTER_KEY = '"+key+"'\r\nEPAY_ADDR=127.0.0.1:9090 # local\nEPAY_COOKIE_SECURE=\"false\"\nEPAY_DB_PATH='data/with space.db'\n")
	t.Chdir(child)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	values, err := readEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:9090" || values.get("EPAY_COOKIE_SECURE") != "false" || values.get("EPAY_DB_PATH") != "data/with space.db" || values.get("EPAY_MASTER_KEY") != key {
		t.Fatal("working-directory .env was not loaded correctly")
	}
	if _, exists := os.LookupEnv("EPAY_MASTER_KEY"); exists {
		t.Fatal("loading .env mutated process environment")
	}
	t.Setenv("EPAY_ADDR", "127.0.0.1:8081")
	t.Setenv("EPAY_COOKIE_SECURE", "true")
	cfg, err = Load()
	if err != nil || cfg.Addr != "127.0.0.1:8081" || values.get("EPAY_COOKIE_SECURE") != "true" {
		t.Fatal("explicit environment must take precedence")
	}
	t.Setenv("EPAY_MASTER_KEY", "")
	if values.get("EPAY_MASTER_KEY") != "" {
		t.Fatal("explicit empty master key must not fall back to .env")
	}
}

// TestDotEnvAbsentDoesNotSearchParents 验证工作目录缺少 .env 时保留默认配置，不读取父目录中的文件。
func TestDotEnvAbsentDoesNotSearchParents(t *testing.T) {
	clearConfigEnvironment(t)
	// 提供独立的环境配置，避免此加载器测试依赖其他功能所需的文件配置。
	t.Setenv("EPAY_MASTER_KEY", base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901")))
	root := t.TempDir()
	t.Chdir(root)
	cfg, err := Load()
	if err != nil || cfg.Addr != "127.0.0.1:8080" {
		t.Fatal("missing .env must retain defaults")
	}
	writeConfigFixture(t, "go.mod", "module epay\n")
	writeConfigFixture(t, ".env", "EPAY_ADDR=127.0.0.1:9091\n")
	cfg, err = Load()
	if err != nil || cfg.Addr != "127.0.0.1:9091" {
		t.Fatal("deployment cwd .env not loaded")
	}
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(child)
	cfg, err = Load()
	if err != nil || cfg.Addr != "127.0.0.1:8080" {
		t.Fatal("must not read parent .env even when parent contains go.mod")
	}
}

// TestDotEnvSafeValuesAndErrors 验证配置值的安全解析，并拒绝非法内容且不在错误信息中泄露配置值。
func TestDotEnvSafeValuesAndErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	writeConfigFixture(t, "go.mod", "module fixture\n")
	writeConfigFixture(t, ".env", "LITERAL=$(echo never-execute)\nHASH=value#suffix # comment\nQUOTED=\"a\\n\\\"b\" # comment\nEMPTY=\n")
	values, err := readEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if values["LITERAL"] != "$(echo never-execute)" || values["HASH"] != "value#suffix" || values["QUOTED"] != "a\n\"b" || values["EMPTY"] != "" {
		t.Fatal("literal or quoted values were parsed incorrectly")
	}
	for _, input := range []string{
		"EPAY_MASTER_KEY='secret-marker", "secret-marker", "1BAD=secret-marker", "KEY=secret-marker\nKEY=again",
		"KEY=\"secret-marker\" trailing", "KEY=secret-marker\x00", "KEY=\"\\x00\"", "KEY=\xffsecret-marker",
		"KEY=" + strings.Repeat("x", maxEnvFileBytes),
	} {
		writeConfigFixture(t, ".env", input)
		_, err := readEnvironment()
		if err == nil {
			t.Fatal("invalid .env accepted")
		}
		if strings.Contains(err.Error(), "secret-marker") {
			t.Fatal("configuration error leaked a value")
		}
	}
}
