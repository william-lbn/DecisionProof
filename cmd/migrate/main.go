package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/william-lbn/DecisionProof/internal/database"
)

// main 只执行数据库迁移，不启动 API。
// 部署系统应把它作为受控的单次 Job 或 Init Container 运行。
func main() {
	dsn := strings.TrimSpace(os.Getenv("SDLC_DATABASE_URL"))
	if dsn == "" {
		log.Fatal("SDLC_DATABASE_URL 不能为空")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("创建 PostgreSQL 连接失败: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("PostgreSQL 不可用: %v", err)
	}
	if err := database.ApplyMigrations(ctx, db); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}
	log.Print("数据库迁移完成")
}
