// Package migrations 提供按内容摘要校验的嵌入式 PostgreSQL 迁移。
package migrations

import "embed"

// Files 嵌入发布二进制对应的精确数据库迁移，避免运行时依赖外部目录漂移。
//
//go:embed *.sql
var Files embed.FS
