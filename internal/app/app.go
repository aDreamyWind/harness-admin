package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/access"
	"harness-admin/internal/auditadmin"
	"harness-admin/internal/cache"
	"harness-admin/internal/config"
	"harness-admin/internal/database"
	"harness-admin/internal/departmentadmin"
	"harness-admin/internal/drama"
	"harness-admin/internal/httpserver"
	"harness-admin/internal/identity"
	"harness-admin/internal/mcpadmin"
	"harness-admin/internal/mcpclient"
	"harness-admin/internal/mcpconnection"
	"harness-admin/internal/mcpdeploy"
	"harness-admin/internal/mcpruntime"
	"harness-admin/internal/mcpsource"
	"harness-admin/internal/modeladmin"
	"harness-admin/internal/modelgateway"
	"harness-admin/internal/modelprovider"
	"harness-admin/internal/oidc"
	"harness-admin/internal/organizationadmin"
	"harness-admin/internal/platformadmin"
	"harness-admin/internal/roleadmin"
	"harness-admin/internal/secret"
	"harness-admin/internal/skilladmin"
	pgstore "harness-admin/internal/store/postgres"
	"harness-admin/internal/useradmin"
)

type Application struct {
	Server       *http.Server
	DB           *pgxpool.Pool
	Redis        *cache.Client
	alertService *modelgateway.Service
}

func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Application, error) {
	startupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	db, err := database.Open(startupCtx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	if cfg.AutoMigrate {
		if err := database.Migrate(startupCtx, cfg.DatabaseURL); err != nil {
			db.Close()
			return nil, err
		}
		logger.Info("数据库迁移已完成")
	}

	redisClient, err := cache.Open(startupCtx, cfg.RedisAddress, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		db.Close()
		return nil, err
	}
	identityRepository := pgstore.NewIdentityRepository(db)
	platformRepository := pgstore.NewPlatformAdminRepository(db)
	loginLimiter := cache.NewLoginLimiter(redisClient, 5, 15*time.Minute)
	platformService := platformadmin.NewService(platformRepository, platformLimiterAdapter{limiter: loginLimiter}, identity.HashPassword, identity.VerifyPassword, cfg.SessionTTL)
	authService := identity.NewService(identityRepository, loginLimiter, cfg.SessionTTL)
	accessService := access.NewService(identityRepository)
	userAdminService := useradmin.NewService(identityRepository, identity.HashPassword, loginLimiter)
	roleAdminService := roleadmin.NewService(identityRepository)
	departmentAdminService := departmentadmin.NewService(identityRepository)
	organizationAdminService := organizationadmin.NewService(identityRepository)
	auditEventService := auditadmin.NewService(identityRepository)
	credentialCipher, err := secret.NewCipher(cfg.ModelCredentialKey)
	if err != nil {
		redisClient.Close()
		db.Close()
		return nil, err
	}
	modelRepository := pgstore.NewModelAdminRepository(db, credentialCipher)
	modelAdminService := modeladmin.NewService(modelRepository, modelprovider.NewProber(cfg.ModelPrivateHosts))
	modelGatewayRepository := pgstore.NewModelGatewayRepository(db, credentialCipher)
	modelPolicyRepository := pgstore.NewModelPolicyRepository(db)
	modelGatewayService := modelgateway.NewService(modelGatewayRepository, cache.NewGatewayRequestLimiter(redisClient), modelPolicyRepository)
	modelPolicyService := modelgateway.NewPolicyService(modelPolicyRepository)
	modelForwarder := modelprovider.NewGatewayForwarder(cfg.ModelPrivateHosts)
	dramaRepository := pgstore.NewDramaRepository(db)
	dramaMedia := &dramaMediaFiles{directory: cfg.DramaMediaDirectory}
	dramaService := drama.NewService(dramaRepository, &dramaWriter{gateway: modelGatewayService, forwarder: modelForwarder}).WithVisuals(dramaRepository, &dramaImageEngine{gateway: modelGatewayService, forwarder: modelForwarder}, dramaMedia).WithStyles(dramaRepository).WithStoryboards(dramaRepository).WithRepairCoordinator(dramaRepository).WithSegmentVideos(dramaRepository, &dramaVideoEngine{gateway: modelGatewayService, forwarder: modelForwarder}, dramaMedia)
	if err := dramaService.Recover(startupCtx); err != nil {
		redisClient.Close()
		db.Close()
		return nil, fmt.Errorf("恢复漫剧解析任务失败: %w", err)
	}
	mcpUpstream := mcpclient.New(cfg.MCPPrivateHosts)
	mcpService := mcpadmin.NewService(pgstore.NewMCPRepository(db, credentialCipher), mcpUpstream)
	mcpDeploymentRepository := pgstore.NewMCPDeploymentRepository(db)
	if cfg.MCPDeployEnabled {
		if err := mcpDeploymentRepository.RecoverInterruptedBuilds(startupCtx); err != nil {
			redisClient.Close()
			db.Close()
			return nil, fmt.Errorf("恢复中断的 MCP 构建状态失败: %w", err)
		}
	}
	mcpDeploymentService := mcpdeploy.NewService(mcpDeploymentRepository, mcpsource.NewGitHub(),
		mcpruntime.NewDocker(cfg.MCPNodeImage, cfg.MCPPythonImage, cfg.MCPBuildNetwork), mcpUpstream, cfg.MCPDeployEnabled)
	mcpConnectionService := mcpconnection.NewService(pgstore.NewMCPConnectionRepository(db, credentialCipher), cache.NewGatewayRequestLimiter(redisClient))
	skillService := skilladmin.NewService(pgstore.NewSkillRepository(db), mcpsource.NewGitHub())
	oidcRepository := pgstore.NewOIDCRepository(db, credentialCipher)
	oidcService := oidc.NewService(oidcRepository)
	oidcSigner, err := oidc.NewRS256Signer(cfg.OIDCSigningPrivateKey)
	if err != nil {
		redisClient.Close()
		db.Close()
		return nil, err
	}
	if cfg.OIDCSigningPrivateKey == "" {
		logger.Warn("未配置固定 OIDC 签名私钥，本次启动使用临时签名密钥；重启后现有 OIDC 令牌将失效")
	}
	oidcProviderService, err := oidc.NewProviderService(oidcRepository, oidcSigner, cfg.OIDCIssuer)
	if err != nil {
		redisClient.Close()
		db.Close()
		return nil, err
	}

	handler := httpserver.New(httpserver.Dependencies{
		AppName:               cfg.AppName,
		Environment:           cfg.Environment,
		Logger:                logger,
		Database:              db,
		Cache:                 redisClient,
		Auth:                  authService,
		Access:                accessService,
		UserAdmin:             userAdminService,
		RoleAdmin:             roleAdminService,
		DepartmentAdmin:       departmentAdminService,
		OrganizationAdmin:     organizationAdminService,
		AuditEvents:           auditEventService,
		ModelAdmin:            modelAdminService,
		ModelGateway:          modelGatewayService,
		ModelPolicy:           modelPolicyService,
		MCPAdmin:              mcpService,
		MCPDeploy:             mcpDeploymentService,
		MCPConnections:        mcpConnectionService,
		Skills:                skillService,
		Drama:                 dramaService,
		DramaVisual:           dramaService,
		DramaStoryboards:      dramaService,
		DramaSegmentVideos:    dramaService,
		DramaStyles:           dramaService,
		MCPToolCaller:         mcpUpstream,
		GatewayForwarder:      modelForwarder,
		OIDCAdmin:             oidcService,
		OIDCProvider:          oidcProviderService,
		ConsoleURL:            cfg.ConsoleURL,
		AuditRecorder:         identityRepository,
		PlatformAdmin:         platformService,
		SessionCookie:         cfg.SessionCookie,
		PlatformSessionCookie: cfg.PlatformSessionCookie,
	})

	return &Application{
		Server: &http.Server{
			Addr:              cfg.HTTPAddress,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       30 * time.Second,
			// 流式模型响应需要保持连接，写超时由上游响应头超时和请求上下文共同约束。
			WriteTimeout: 0,
			IdleTimeout:  60 * time.Second,
		},
		DB:           db,
		Redis:        redisClient,
		alertService: modelGatewayService,
	}, nil
}

// RunAlertRefresh 在本进程内定时计算已开启的企业预警；跨实例使用 PostgreSQL 事务锁避免重复扫描。
func (a *Application) RunAlertRefresh(ctx context.Context, logger *slog.Logger) {
	refresh := func() {
		refreshCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		conn, err := pgx.ConnectConfig(refreshCtx, a.DB.Config().ConnConfig)
		if err != nil {
			if ctx.Err() == nil {
				logger.Error("获取预警刷新数据库连接失败", "error", err)
			}
			return
		}
		defer func() {
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer closeCancel()
			if err := conn.Close(closeCtx); err != nil {
				logger.Error("关闭预警刷新锁连接失败", "error", err)
			}
		}()
		tx, err := conn.Begin(refreshCtx)
		if err != nil {
			if ctx.Err() == nil {
				logger.Error("创建预警刷新锁事务失败", "error", err)
			}
			return
		}
		defer func() {
			rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer rollbackCancel()
			if err := tx.Rollback(rollbackCtx); err != nil {
				logger.Error("释放预警刷新事务锁失败", "error", err)
			}
		}()
		var locked bool
		if err := tx.QueryRow(refreshCtx, `SELECT pg_try_advisory_xact_lock(20261020, 19)`).Scan(&locked); err != nil {
			if ctx.Err() == nil {
				logger.Error("获取预警刷新锁失败", "error", err)
			}
			return
		}
		if !locked {
			return
		}
		if err := a.alertService.RefreshEnabledAlerts(refreshCtx); err != nil && ctx.Err() == nil {
			logger.Error("后台刷新模型预警失败", "error", err)
		}
	}

	refresh()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refresh()
		}
	}
}

type platformLimiterAdapter struct {
	limiter *cache.LoginLimiter
}

func (a platformLimiterAdapter) Allow(ctx context.Context, username string) (bool, error) {
	return a.limiter.Allow(ctx, "platform", username)
}

func (a platformLimiterAdapter) RecordFailure(ctx context.Context, username string) (int64, bool, error) {
	failure, err := a.limiter.RecordFailure(ctx, "platform", username)
	return failure.Attempts, failure.LimitReached, err
}

func (a platformLimiterAdapter) Clear(ctx context.Context, username string) error {
	return a.limiter.Clear(ctx, "platform", username)
}

func (a *Application) Close() {
	if a.Redis != nil {
		if err := a.Redis.Close(); err != nil {
			fmt.Printf("关闭 Redis 连接失败: %v\n", err)
		}
	}
	if a.DB != nil {
		a.DB.Close()
	}
}
