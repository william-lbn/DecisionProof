package gitlab

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/william-lbn/DecisionProof/internal/database"
)

// TestPostgresRuntimeTruth 使用真实非超级用户验证 Project Integration、
// Delivery 幂等记录和跨租户 RLS；它不是 GitLab Provider Truth Test。
func TestPostgresRuntimeTruth(t *testing.T) {
	adminURL := os.Getenv("DECISIONPROOF_TEST_ADMIN_DATABASE_URL")
	appURL := os.Getenv("DECISIONPROOF_TEST_APP_DATABASE_URL")
	if adminURL == "" || appURL == "" {
		t.Skip("未配置真实 PostgreSQL GitLab Runtime Truth Test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := sql.Open("pgx", adminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if err := database.ApplyMigrations(ctx, admin); err != nil {
		t.Fatal(err)
	}
	app, err := sql.Open("pgx", appURL)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	repo, err := NewPostgresRuntimeRepository(app)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := "gitlab-runtime-" + time.Now().UTC().Format("20060102150405.000000000")
	now := time.Now().UTC()
	integration, err := repo.SaveIntegration(ctx, ProjectIntegration{
		TenantID: tenantID, IntegrationID: "gitlab", ProjectID: "project",
		ExternalProjectID: 7, BaseURL: "https://gitlab.example", PathWithNamespace: "group/project",
		DefaultBranch: "main", Visibility: "private", SecretReference: "gitlab-primary",
		Status: IntegrationActive, LastVerifiedAt: now, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetIntegration(ctx, tenantID, integration.IntegrationID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetIntegration(ctx, tenantID+"-other", integration.IntegrationID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("跨租户 Integration 读取必须为空，实际 %v", err)
	}
	run := DeliveryRun{
		TenantID: tenantID, DeliveryID: "gld-truth", IntegrationID: integration.IntegrationID,
		PackageID: "erp-truth", PackageDigest: "sha256:truth", IdempotencyKey: "delivery-truth",
		Status: DeliveryPending, CreatedBy: "delivery-manager", CreatedAt: now, UpdatedAt: now,
	}
	first, created, err := repo.BeginDelivery(ctx, run)
	if err != nil || !created {
		t.Fatalf("首次 Delivery 写入失败: created=%v run=%+v err=%v", created, first, err)
	}
	duplicate, created, err := repo.BeginDelivery(ctx, run)
	if err != nil || created || duplicate.DeliveryID != first.DeliveryID {
		t.Fatalf("Delivery 幂等回读失败: created=%v run=%+v err=%v", created, duplicate, err)
	}
	first.Status = DeliveryUnknown
	first.LastError = "write result cannot be reconciled"
	first.UpdatedAt = now.Add(time.Second)
	updated, err := repo.UpdateDelivery(ctx, first)
	if err != nil || updated.Status != DeliveryUnknown {
		t.Fatalf("Delivery UNKNOWN 持久化失败: run=%+v err=%v", updated, err)
	}
}
