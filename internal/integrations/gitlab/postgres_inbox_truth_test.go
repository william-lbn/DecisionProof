package gitlab

import (
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresInboxTruth(t *testing.T) {
	dsn := os.Getenv("DECISIONPROOF_TEST_APP_DATABASE_URL")
	if dsn == "" {
		t.Skip("未配置真实 PostgreSQL Webhook Inbox Truth Test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tenant := "webhook-truth-" + time.Now().UTC().Format("20060102150405.000000000")
	inbox, err := NewPostgresInbox(db, tenant)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	record := InboxRecord{
		ID: "WH-truth", IntegrationID: "gitlab-truth", EventUUID: "event-truth",
		EventType: "Merge Request Hook", PayloadDigest: "sha256:truth",
		Payload: json.RawMessage(`{"project_id":1}`), Status: InboxPending, ReceivedAt: now, UpdatedAt: now,
	}
	stored, first, err := inbox.PutIfAbsent(record)
	if err != nil || !stored || first.ID != record.ID {
		t.Fatalf("首次写入失败: stored=%v record=%#v err=%v", stored, first, err)
	}
	stored, duplicate, err := inbox.PutIfAbsent(record)
	if err != nil || stored || duplicate.ID != first.ID {
		t.Fatalf("重复事件去重失败: stored=%v record=%#v err=%v", stored, duplicate, err)
	}
	if err := MarkProcessing(inbox, first, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	processing, err := inbox.List(InboxProcessing, 10)
	if err != nil || len(processing) != 1 || processing[0].Attempts != 1 {
		t.Fatalf("处理状态读取失败: records=%#v err=%v", processing, err)
	}
	other, err := NewPostgresInbox(db, tenant+"-other")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Get(first.ID); err == nil {
		t.Fatal("其他租户不得读取 Webhook Inbox 记录")
	}
}
