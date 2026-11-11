// Package sqlite 负责单实例 SQLite 连接、迁移和管理员/通道配置存储。
package sqlite

import (
	"context"
	"database/sql" // 统一的数据库访问 api
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	//  _ 表示空白导入，让 SQLite 驱动完成注册，而不引入可以访问他的包名
	// 包的初始化发生在 main() 执行之前，先初始化依赖包，再初始化依赖它的包
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

const (
	loginWindow = 15 * time.Minute
	loginLimit  = 5
	loginLock   = 15 * time.Minute
)

// Store 持有单进程串行使用的 SQLite 连接。
type Store struct{ db *sql.DB }

// Open 打开数据库，限制数据目录和文件的访问权限，设置连接参数，并执行尚未应用的结构迁移。
func Open(ctx context.Context, path string) (*Store, error) {
	// 拒绝空路径，并统一为绝对路径，供后续权限检查和连接配置使用。
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("database path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	// 创建专用数据目录；0700 仅允许所有者读、写和进入目录，不修改已有目录的权限。
	if err := os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	// 在 SQLite 创建 WAL/SHM 之前限制权限；数据目录须为专用目录。
	// Lstat 不跟随路径最后一级的符号链接；0077 掩码检查组和其他用户是否具有权限。
	info, err := os.Lstat(filepath.Dir(abs))
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("database directory must be a private directory (mode 0700)")
	}
	// 已有数据库必须是普通文件，拒绝符号链接等类型；文件不存在时允许后续创建。
	if info, err := os.Lstat(abs); err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("database must be a regular file, not a symlink")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	// 预先创建或打开数据库文件；0600 仅允许所有者读写。
	file, err := os.OpenFile(abs, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	// 创建时的权限参数不影响已有文件，因此还需显式收紧文件权限。
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return nil, err
	}
	// 关闭预检查使用的文件句柄；实际数据库连接由 SQLite 驱动另行管理。
	if err := file.Close(); err != nil {
		return nil, err
	}

	// 用 file URL 编码路径中的特殊字符，再拼接驱动识别的连接参数（DSN）。
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	// 锁冲突最多等待 5 秒，启用外键约束和 WAL 日志，并使用 FULL 同步策略加强提交持久性。
	dsn := u.String() + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)"
	// 创建数据库连接池；Open 成功不代表已经建立连接，后续通过 PingContext 验证。
	db, err := sql.Open("sqlite", dsn) // "sqlite" 是注册的驱动名
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	// 连接池最多打开一个连接以串行使用，并最多保留一个空闲连接供后续请求复用。
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	// 实际建立或检查连接。
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect to SQLite database: %w", err)
	}

	// 按版本应用嵌入的 SQL 迁移。
	store := &Store{db: db}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

// Close 关闭数据库连接，释放占用的资源。
func (s *Store) Close() error { return s.db.Close() }

// migrate 按版本顺序执行尚未应用的数据库结构迁移，每个版本单独提交事务。
// 遇到程序不认识的已执行版本时返回错误，避免旧程序使用新版数据库结构。
func (s *Store) migrate(ctx context.Context) error {
	// 创建迁移台账；版本名唯一，applied_at 记录成功应用时的 Unix 秒级时间戳。
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	// 从编译进程序的嵌入文件系统中查找脚本。
	entries, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	// 按文件名字典序执行；0001、0002 等补零前缀使排序结果与版本顺序一致。
	sort.Strings(entries)
	// 用 map 作为已知版本集合，版本名取自去掉目录和 .sql 后缀的文件名。
	known := make(map[string]bool, len(entries))
	for _, name := range entries {
		known[strings.TrimSuffix(filepath.Base(name), ".sql")] = true
	}
	// 先核对数据库已有版本，拒绝当前程序不认识的版本，避免旧程序误用较新的结构。
	rows, err := s.db.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return err
	}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			rows.Close()
			return err
		}
		if !known[version] {
			rows.Close()
			return fmt.Errorf("unsupported database migration: %s", version)
		}
	}
	// Next 返回 false 也可能是读取失败，因此遍历结束后仍需检查 Err。
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	// 释放结果集占用的连接；连接池仅有一个连接，后续查询和事务需要复用它。
	rows.Close()
	for _, name := range entries {
		version := strings.TrimSuffix(filepath.Base(name), ".sql")
		// 已记录的版本直接跳过；只有 ErrNoRows 表示尚未执行，其他查询错误必须返回。
		var exists int
		err := s.db.QueryRowContext(ctx, `SELECT 1 FROM schema_migrations WHERE version = ?`, version).Scan(&exists)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		// 读取待执行脚本，读取成功后才开启事务，缩短事务持有连接的时间。
		script, err := fs.ReadFile(migrations, name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", version, err)
		}
		// 每个版本使用独立事务：该版本失败不会撤销之前已经提交的迁移。
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", version, err)
		}
		// 脚本执行和台账记录使用同一事务，保证结构变更与“已执行”标记原子提交。
		if _, err = tx.ExecContext(ctx, string(script)); err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`, version, time.Now().Unix())
		}
		if err != nil {
			// 脚本或台账写入失败时回滚本版本，并保留原始执行错误供上层定位。
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", version, err)
		}
		// 提交成功后才继续下一个版本；下次启动依据台账跳过已提交版本。
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", version, err)
		}
	}
	return nil
}
