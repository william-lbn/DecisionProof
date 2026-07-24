package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/william-lbn/DecisionProof/internal/ai"
	"github.com/william-lbn/DecisionProof/internal/authn"
	"github.com/william-lbn/DecisionProof/internal/database"
	"github.com/william-lbn/DecisionProof/internal/executor"
	"github.com/william-lbn/DecisionProof/internal/httpapi"
	gitlabintegration "github.com/william-lbn/DecisionProof/internal/integrations/gitlab"
	"github.com/william-lbn/DecisionProof/internal/product"
	"github.com/william-lbn/DecisionProof/internal/productapi"
	"github.com/william-lbn/DecisionProof/internal/security"
	"github.com/william-lbn/DecisionProof/internal/service"
	"github.com/william-lbn/DecisionProof/internal/store"
	"github.com/william-lbn/DecisionProof/internal/telemetry"
	"github.com/william-lbn/DecisionProof/internal/ui"
)

// main 组装最小可运行控制平面。
// 参考二进制默认使用单节点原子快照存储；生产多副本部署应替换为 PostgreSQL Repository 适配器。
func main() {
	address := envOr("SDLC_LISTEN_ADDR", ":8080")
	dataPath := envOr("SDLC_DATA_FILE", "./data/snapshot.json")
	production := strings.EqualFold(os.Getenv("SDLC_ENV"), "production")

	var (
		repo        store.Repository
		productRepo product.Repository
		db          *sql.DB
		err         error
		readiness   = func(context.Context) error { return nil }
	)
	databaseURL := strings.TrimSpace(os.Getenv("SDLC_DATABASE_URL"))
	if databaseURL != "" {
		db, err = sql.Open("pgx", databaseURL)
		if err != nil {
			log.Fatalf("初始化 PostgreSQL 连接失败: %v", err)
		}
		defer db.Close()
		db.SetMaxOpenConns(20)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(30 * time.Minute)
		startupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := db.PingContext(startupCtx); err != nil {
			cancel()
			log.Fatalf("PostgreSQL 启动检查失败: %v", err)
		}
		if strings.EqualFold(strings.TrimSpace(os.Getenv("SDLC_AUTO_MIGRATE")), "true") {
			if err := database.ApplyMigrations(startupCtx, db); err != nil {
				cancel()
				log.Fatalf("PostgreSQL 自动迁移失败: %v", err)
			}
		}
		cancel()
		repo, err = store.NewPostgresStore(db)
		if err != nil {
			log.Fatalf("初始化核心 PostgreSQL Store 失败: %v", err)
		}
		productRepo, err = product.NewPostgresRepository(db)
		if err != nil {
			log.Fatalf("初始化产品 PostgreSQL Repository 失败: %v", err)
		}
		readiness = db.PingContext
		log.Print("运行时存储：PostgreSQL")
	} else {
		repo, err = store.NewFileStore(dataPath)
		if err != nil {
			log.Fatalf("初始化持久化存储失败: %v", err)
		}
		productRepo, err = product.NewFileRepository(envOr("SDLC_PRODUCT_DATA_FILE", "./data/product-snapshot.json"))
		if err != nil {
			log.Fatalf("初始化产品决策存储失败: %v", err)
		}
		log.Print("运行时存储：单节点文件快照")
	}
	signer, err := security.NewSignerFromEnv("SDLC_SIGNING_SEED_B64", !production)
	if err != nil {
		log.Fatalf("初始化发布签名器失败: %v", err)
	}

	clock := service.RealClock{}
	transitionPolicy := service.NewTransitionPolicy()
	actionPolicy := service.NewActionPolicy()
	localExecutor := executor.NewLocalExecutor()
	registry := executor.StaticRegistry{Executor: localExecutor}
	metrics := telemetry.NewMetrics()

	changeService := service.NewChangeService(repo, clock, transitionPolicy)
	evidenceService := service.NewEvidenceService(repo, clock)
	actionGateway := service.NewActionGateway(repo, clock, actionPolicy, registry)
	releaseService := service.NewReleaseService(repo, clock, transitionPolicy, signer)
	coreHandler := httpapi.NewServer(changeService, evidenceService, actionGateway, releaseService, metrics, readiness)
	productService := product.NewService(productRepo, product.RealClock{})
	productHandler := productapi.NewHTTPHandler(productService, envOr("SDLC_OBJECT_DIR", "./data/objects"), buildExtractor())
	root := http.NewServeMux()
	root.Handle("/product/v1/", productHandler)
	root.Handle("/app/", ui.Handler())
	runtimeCtx, runtimeCancel := context.WithCancel(context.Background())
	defer runtimeCancel()

	// GitLab API Token 只从部署 Secret 读取；Base URL 同样由运维固定，API 调用者不能把平台变成 SSRF 代理。
	gitlabBaseURL := strings.TrimSpace(os.Getenv("SDLC_GITLAB_BASE_URL"))
	gitlabAPIToken := strings.TrimSpace(os.Getenv("SDLC_GITLAB_API_TOKEN"))
	var (
		gitlabRuntimeRepo *gitlabintegration.PostgresRuntimeRepository
		gitlabAPIClient   *gitlabintegration.Client
	)
	if (gitlabBaseURL == "") != (gitlabAPIToken == "") {
		log.Fatal("SDLC_GITLAB_BASE_URL 与 SDLC_GITLAB_API_TOKEN 必须同时配置")
	}
	if gitlabBaseURL != "" {
		if db == nil {
			log.Fatal("GitLab Runtime API 当前要求 PostgreSQL 模式")
		}
		gitlabRuntimeRepo, err = gitlabintegration.NewPostgresRuntimeRepository(db)
		if err != nil {
			log.Fatalf("初始化 GitLab Runtime Repository 失败: %v", err)
		}
		gitlabAuthMode := gitlabintegration.AuthPrivateToken
		if strings.EqualFold(strings.TrimSpace(os.Getenv("SDLC_GITLAB_API_AUTH_MODE")), "bearer") {
			gitlabAuthMode = gitlabintegration.AuthBearer
		}
		secretReference := envOr("SDLC_GITLAB_API_SECRET_REFERENCE", "gitlab-primary")
		gitlabAPIClient, err = gitlabintegration.NewClient(gitlabBaseURL, gitlabAPIToken, gitlabAuthMode)
		if err != nil {
			log.Fatalf("初始化 GitLab API Client 失败: %v", err)
		}
		gitlabRuntime := &gitlabintegration.RuntimeService{
			Repository: gitlabRuntimeRepo, Product: productService, BaseURL: gitlabBaseURL,
			SecretReference: secretReference,
			Credentials: gitlabintegration.StaticCredentialResolver{
				Reference: secretReference,
				Credential: gitlabintegration.Credential{
					Token: gitlabAPIToken, AuthMode: gitlabAuthMode,
				},
			},
		}
		root.Handle("/integrations/v1/gitlab/", gitlabintegration.RuntimeHTTPHandler{Service: gitlabRuntime})
	}

	// GitLab Webhook 接收器只有在配置签名密钥或旧版 Token 时才启用。
	// 当前 FileInbox 用于单节点真实性验证；生产多副本必须切换 PostgreSQL Inbox。
	gitlabSigningSecret := strings.TrimSpace(os.Getenv("SDLC_GITLAB_WEBHOOK_SIGNING_SECRET"))
	gitlabLegacyToken := strings.TrimSpace(os.Getenv("SDLC_GITLAB_WEBHOOK_LEGACY_TOKEN"))
	if gitlabSigningSecret != "" || gitlabLegacyToken != "" {
		gitlabTenantID := strings.TrimSpace(os.Getenv("SDLC_GITLAB_TENANT_ID"))
		var inbox gitlabintegration.InboxStore
		if db != nil {
			if gitlabTenantID == "" {
				log.Fatal("PostgreSQL Webhook Inbox 必须配置 SDLC_GITLAB_TENANT_ID")
			}
			inbox, err = gitlabintegration.NewPostgresInbox(db, gitlabTenantID)
		} else {
			if gitlabTenantID == "" {
				gitlabTenantID = "tenant-demo"
			}
			inbox, err = gitlabintegration.NewFileInbox(envOr("SDLC_GITLAB_INBOX_FILE", "./data/gitlab-webhook-inbox.json"))
		}
		if err != nil {
			log.Fatalf("初始化 GitLab Webhook Inbox 失败: %v", err)
		}
		root.Handle("/webhooks/gitlab", gitlabintegration.WebhookHandler{
			TenantID:      gitlabTenantID,
			IntegrationID: envOr("SDLC_GITLAB_INTEGRATION_ID", "gitlab-default"),
			Verifier: gitlabintegration.WebhookVerifier{
				SigningSecret: gitlabSigningSecret,
				LegacyToken:   gitlabLegacyToken,
			},
			Inbox: inbox,
		})
		if gitlabAPIClient != nil && gitlabRuntimeRepo != nil {
			reconciler := &gitlabintegration.Reconciler{
				Inbox: inbox,
				Processor: gitlabintegration.GitLabReadbackProcessor{
					API: gitlabAPIClient, Integrations: gitlabRuntimeRepo,
				},
				MaxAttempts: 5,
				OnError: func(err error) {
					log.Printf("GitLab Webhook Reconciler 失败: %v", err)
				},
			}
			go reconciler.Run(runtimeCtx)
		}
	}
	root.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/app/", http.StatusTemporaryRedirect)
			return
		}
		coreHandler.ServeHTTP(w, r)
	})
	authMode := strings.TrimSpace(os.Getenv("SDLC_AUTH_MODE"))
	if authMode == "" {
		if production {
			log.Fatal("生产环境必须显式配置 SDLC_AUTH_MODE")
		}
		authMode = string(authn.ModeDevelopmentHeaders)
	}
	authCtx, authCancel := context.WithTimeout(context.Background(), 10*time.Second)
	authMiddleware, err := authn.NewMiddleware(authCtx, authn.Config{
		Mode:                            authn.Mode(authMode),
		Production:                      production,
		AllowInsecureDevelopmentHeaders: strings.EqualFold(strings.TrimSpace(os.Getenv("SDLC_ALLOW_INSECURE_DEV_AUTH")), "true"),
		IssuerURL:                       strings.TrimSpace(os.Getenv("SDLC_OIDC_ISSUER_URL")),
		Audience:                        strings.TrimSpace(os.Getenv("SDLC_OIDC_AUDIENCE")),
		TenantClaim:                     envOr("SDLC_OIDC_TENANT_CLAIM", "tenant_id"),
		RolesClaim:                      envOr("SDLC_OIDC_ROLES_CLAIM", "roles"),
	})
	authCancel()
	if err != nil {
		log.Fatalf("初始化身份认证失败: %v", err)
	}
	handler := securityHeaders(authn.SameOrigin(authMiddleware(root)))

	server := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	go func() {
		log.Printf("SDLCProof 控制 API 正在监听 %s", address)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP 服务异常退出: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	runtimeCancel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("优雅停止失败: %v", err)
	}
}

// buildExtractor 根据环境变量选择确定性规则提取器或企业 HTTP JSON AI 服务。
// 远端 AI 只能返回结构化候选决策，不能直接形成 APPROVED 对象。
func buildExtractor() product.Extractor {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SDLC_AI_EXTRACTOR"))) {
	case "http-json":
		return &ai.HTTPJSONExtractor{
			Endpoint:  strings.TrimSpace(os.Getenv("SDLC_AI_ENDPOINT")),
			APIKey:    strings.TrimSpace(os.Getenv("SDLC_AI_API_KEY")),
			Provider:  envOr("SDLC_AI_PROVIDER", "enterprise-ai-gateway"),
			ModelName: envOr("SDLC_AI_MODEL", "configured-model"),
		}
	default:
		return ai.HeuristicExtractor{}
	}
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
