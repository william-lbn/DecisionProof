package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/william-lbn/DecisionProof/migrations"
)

const migrationLockID int64 = 0x4450524f4f46 // ASCII "DPROOF"

// ApplyMigrations 串行执行尚未应用的嵌入式迁移。
// 已执行迁移的内容摘要一旦变化会 Fail-closed，防止不同 Pod 使用同一版本号运行不同 DDL。
func ApplyMigrations(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("数据库连接不能为空")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("获取迁移数据库连接失败: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return fmt.Errorf("获取数据库迁移锁失败: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationLockID)
	}()

	if _, err := conn.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version BIGINT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    sha256 TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`); err != nil {
		return fmt.Errorf("初始化迁移版本表失败: %w", err)
	}

	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		return fmt.Errorf("读取嵌入式迁移失败: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		if err := applyOne(ctx, conn, entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func applyOne(ctx context.Context, conn *sql.Conn, name string) error {
	versionText, _, ok := strings.Cut(name, "_")
	if !ok {
		return fmt.Errorf("迁移文件名缺少版本前缀: %s", name)
	}
	version, err := strconv.ParseInt(versionText, 10, 64)
	if err != nil || version <= 0 {
		return fmt.Errorf("迁移文件版本无效: %s", name)
	}
	body, err := migrations.Files.ReadFile(name)
	if err != nil {
		return fmt.Errorf("读取迁移 %s 失败: %w", name, err)
	}
	sum := sha256.Sum256(body)
	digest := "sha256:" + hex.EncodeToString(sum[:])

	var existing string
	err = conn.QueryRowContext(ctx, `SELECT sha256 FROM schema_migrations WHERE version=$1`, version).Scan(&existing)
	switch {
	case err == nil:
		if existing != digest {
			return fmt.Errorf("迁移 %s 的内容摘要已变化，拒绝继续", name)
		}
		return nil
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("查询迁移 %s 状态失败: %w", name, err)
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启迁移 %s 事务失败: %w", name, err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		return fmt.Errorf("执行迁移 %s 失败: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations(version,name,sha256) VALUES ($1,$2,$3)`,
		version, name, digest); err != nil {
		return fmt.Errorf("记录迁移 %s 失败: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交迁移 %s 失败: %w", name, err)
	}
	return nil
}
