// 本文件管理主密钥的私有文件存储；只为没有加密状态的新数据库生成密钥。
package securebox

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LoadKeyFile 从文件读取 Base64 编码的 32 字节主密钥；文件缺失且允许创建时才生成新密钥，绝不覆盖已有文件。
// 调用方必须先检查数据库状态，已有加密数据时须将 allowCreate 设为 false，避免误用新密钥。
func LoadKeyFile(path string, allowCreate bool) ([]byte, error) {
	dir := filepath.Dir(path)
	if allowCreate {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("master key directory must be private (mode 0700)")
	}
	key, err := readKeyFile(path)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if !allowCreate {
		return nil, errors.New("data/master.key is missing; restore the original key before starting")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	// 先同步临时文件，再发布完整内容；崩溃不会留下被当作有效密钥的半写入文件。
	file, err := os.CreateTemp(dir, ".master-key-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(0600); err != nil {
		return nil, err
	}
	if _, err := io.WriteString(file, base64.StdEncoding.EncodeToString(raw)+"\n"); err != nil {
		return nil, err
	}
	if err := file.Sync(); err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	// Link 不覆盖已存在的文件；并发创建时读取胜出的文件，而不是继续使用自己的随机值。
	if err := os.Link(file.Name(), path); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return nil, err
	}
	return readKeyFile(path)
}

// readKeyFile 读取主密钥文件，拒绝符号链接、不是 0600 的权限、过长内容和错误格式；错误信息不包含密钥值。
func readKeyFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return nil, errors.New("master key must be a regular file with mode 0600, not a symlink")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	current, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, current) {
		return nil, errors.New("master key file changed while opening")
	}
	if current.Size() > 128 {
		return nil, errors.New("master key file is too large")
	}
	value, err := io.ReadAll(io.LimitReader(file, 128))
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(value)))
	if err != nil || len(key) != 32 {
		return nil, errors.New("master key file must contain base64-encoded 32 random bytes")
	}
	return key, nil
}
