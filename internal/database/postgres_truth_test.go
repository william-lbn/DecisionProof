package database_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/william-lbn/DecisionProof/internal/database"
	"github.com/william-lbn/DecisionProof/internal/domain"
	"github.com/william-lbn/DecisionProof/internal/product"
	"github.com/william-lbn/DecisionProof/internal/service"
	"github.com/william-lbn/DecisionProof/internal/store"
)

// TestPostgresTruth 使用真实 PostgreSQL 管理员和非超级用户应用账号验证迁移、RLS 和两套 Repository。
// 未显式提供连接串时跳过，避免把本地单元测试误写成 Provider Truth Test。
func TestPostgresTruth(t *testing.T) {
	adminURL := os.Getenv("DECISIONPROOF_TEST_ADMIN_DATABASE_URL")
	appURL := os.Getenv("DECISIONPROOF_TEST_APP_DATABASE_URL")
	if adminURL == "" || appURL == "" {
		t.Skip("未配置真实 PostgreSQL Truth Test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin := openDB(t, adminURL)
	defer admin.Close()
	if err := database.ApplyMigrations(ctx, admin); err != nil {
		t.Fatalf("首次幂等迁移失败: %v", err)
	}
	if err := database.ApplyMigrations(ctx, admin); err != nil {
		t.Fatalf("重复幂等迁移失败: %v", err)
	}

	app := openDB(t, appURL)
	defer app.Close()
	var visibleWithoutTenant int
	if err := app.QueryRowContext(ctx, `SELECT count(*) FROM changes`).Scan(&visibleWithoutTenant); err != nil {
		t.Fatalf("无租户查询失败: %v", err)
	}
	if visibleWithoutTenant != 0 {
		t.Fatalf("非超级用户未设置租户时不应看到数据，实际 %d", visibleWithoutTenant)
	}

	coreRepo, err := store.NewPostgresStore(app)
	if err != nil {
		t.Fatal(err)
	}
	changes := service.NewChangeService(coreRepo, service.RealClock{}, service.NewTransitionPolicy())
	tenantA := "truth-a-" + time.Now().UTC().Format("20060102150405.000000000")
	tenantB := tenantA + "-other"
	change, err := changes.Create(ctx, tenantA, "postgres-truth", "PostgreSQL RLS Truth", "", "high")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := changes.Get(ctx, tenantA, change.ID); err != nil {
		t.Fatalf("本租户读取失败: %v", err)
	}
	if _, err := changes.Get(ctx, tenantB, change.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("跨租户读取必须返回不存在，实际: %v", err)
	}

	productRepo, err := product.NewPostgresRepository(app)
	if err != nil {
		t.Fatal(err)
	}
	products := product.NewService(productRepo, product.RealClock{})
	doc, err := products.RegisterDocument(ctx, tenantA, "postgres-truth", "truth.md", "text/markdown",
		"s3://truth/object", "sha256:truth-document", "product-manager")
	if err != nil {
		t.Fatal(err)
	}
	blockID, err := domain.NewID("block")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := products.SaveParsedBlocks(ctx, tenantA, doc.ID, []product.SourceBlock{{
		ID: blockID, Kind: "markdown", Locator: "markdown:line:1", Text: "需求：必须通过真实 PostgreSQL。",
		Digest: "sha256:truth-block", CreatedAt: time.Now().UTC(),
	}}); err != nil {
		t.Fatal(err)
	}
	decision, err := products.CreateManualDecision(ctx, tenantA, "postgres-truth", product.DecisionRequirement,
		"真实数据库", "必须使用非超级用户验证 RLS。", "product-manager")
	if err != nil {
		t.Fatal(err)
	}
	items, err := products.ListDecisions(ctx, tenantA, "postgres-truth")
	if err != nil || len(items) != 1 || items[0].ID != decision.ID {
		t.Fatalf("产品 PostgreSQL Repository 读取不一致: items=%#v err=%v", items, err)
	}
	other, err := products.ListDecisions(ctx, tenantB, "postgres-truth")
	if err != nil || len(other) != 0 {
		t.Fatalf("产品决策跨租户泄漏: items=%#v err=%v", other, err)
	}
}

func openDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		t.Fatal(err)
	}
	return db
}
