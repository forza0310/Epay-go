// 本文件提供停服后的本地 TOTP 恢复；首次管理员创建由网页完成。
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"golang.org/x/term"

	"epay/internal/admin"
	"epay/internal/config"
	"epay/internal/securebox"
	"epay/internal/storage/sqlite"
)

// command 处理本地恢复命令；首次创建管理员请使用网页初始化。
func command(args []string) error {
	if len(args) != 1 || args[0] != "reset-totp" {
		return errors.New("usage: epay [reset-totp]; initial setup is available at /admin/setup")
	}
	return resetTOTP()
}

// resetTOTP 在终端检查管理员密码和新动态码，然后更换验证器绑定并使旧登录会话失效。
func resetTOTP() error {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stderr.Fd())) {
		return errors.New("administrator setup requires an interactive terminal; do not pipe credentials")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx := context.Background()
	key, err := securebox.LoadKeyFile("data/master.key", false)
	if err != nil {
		return err
	}
	box, err := securebox.New(key)
	if err != nil {
		return err
	}
	store, err := sqlite.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err = store.VerifyKey(ctx, box); err != nil {
		return err
	}
	service, err := admin.New(store, box)
	if err != nil {
		return err
	}
	initialized, err := service.Initialized(ctx)
	if err != nil {
		return err
	}
	if !initialized {
		return errors.New("administrator is not initialized; visit /admin/setup")
	}
	password, err := readSecret("管理员密码: ")
	if err != nil {
		return err
	}
	if err = service.VerifyRecoveryPassword(ctx, password); err != nil {
		return err
	}
	secret, err := admin.NewTOTPSecret()
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "在验证器中手工添加账户 epay / admin：基于时间、SHA-1、6 位、30 秒。")
	fmt.Fprintln(os.Stderr, "绑定密钥（仅本次展示，请勿录屏、共享或记录到日志）：", secret)
	code, err := readSecret("输入验证器当前的 6 位动态码: ")
	if err != nil {
		return err
	}
	err = service.ResetTOTP(ctx, password, secret, code)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "管理员双因素配置完成；旧会话已失效。请等待下一动态码周期再登录。")
	return nil
}

// readSecret 从终端读取密码或动态码，输入内容不显示在屏幕上。
func readSecret(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("读取凭证: %w", err)
	}
	return string(value), nil
}
