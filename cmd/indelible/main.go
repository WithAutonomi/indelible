package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	managedantd "github.com/WithAutonomi/indelible/internal/antd"
	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/handlers"
	"github.com/WithAutonomi/indelible/internal/middleware"
	"github.com/WithAutonomi/indelible/internal/worker"

	sdk "github.com/WithAutonomi/ant-sdk/antd-go"

	_ "github.com/WithAutonomi/indelible/docs" // swagger docs
)

// @title Indelible API
// @version 2.0
// @description Enterprise gateway for Autonomi decentralized storage. Provides file upload/download, user management, API tokens, webhooks, and admin controls.
// @host localhost:8080
// @BasePath /api/v2
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Enter your JWT or API token as: Bearer <token>

var version = "dev"

func main() {
	var (
		configPath  string
		showVer     bool
		networkFlag string
	)
	flag.StringVar(&configPath, "config", "", "path to indelible.toml config file")
	flag.BoolVar(&showVer, "version", false, "print version and exit")
	flag.StringVar(&networkFlag, "network", "", `EVM network preset: "arbitrum-one" (default), "arbitrum-sepolia", or "custom" — overrides config file and INDELIBLE_NETWORK`)
	flag.Parse()

	if showVer {
		fmt.Printf("indelible %s\n", version)
		os.Exit(0)
	}

	// Load configuration (env vars override config file)
	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	// CLI --network beats env/file. Then fill EVM defaults from the preset
	// for any value not already set explicitly.
	if networkFlag != "" {
		cfg.Network = networkFlag
	}
	if err := cfg.ApplyNetworkPreset(); err != nil {
		slog.Error("invalid network configuration", "error", err)
		os.Exit(1)
	}

	// Set up structured logging
	logLevel := slog.LevelInfo
	if cfg.Debug {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	slog.Info("starting indelible", "version", version, "port", cfg.Port, "db_driver", cfg.DBDriver())

	// Managed antd
	var antdMgr *managedantd.Manager
	if cfg.AntdManaged {
		antdMgr = managedantd.NewManager(cfg)
		if err := antdMgr.Start(context.Background()); err != nil {
			slog.Error("failed to start antd", "error", err)
			os.Exit(1)
		}
		defer func() { _ = antdMgr.Stop() }()
		cfg.AntdURL = antdMgr.URL()
		slog.Info("antd managed", "url", cfg.AntdURL, "pid", antdMgr.PID())
	} else if cfg.AntdURL == "" || cfg.AntdURL == "http://localhost:8082" {
		// Not managed — try auto-discovery as convenience
		if url := sdk.DiscoverDaemonURL(); url != "" {
			cfg.AntdURL = url
			slog.Info("antd auto-discovered", "url", url)
		}
	}

	// Open database
	db, err := database.Open(cfg.DBURL)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		if antdMgr != nil {
			_ = antdMgr.Stop()
		}
		os.Exit(1)
	}
	defer db.Close()

	// Run migrations
	if err := database.Migrate(db, cfg.DBDriver()); err != nil {
		slog.Error("failed to run migrations", "error", err)
		if antdMgr != nil {
			_ = antdMgr.Stop()
		}
		os.Exit(1)
	}

	// Build router
	router := handlers.NewRouter(cfg, db)

	// Start background workers
	uploadWorker := worker.NewUploadWorker(db, cfg)
	uploadWorker.Start()
	defer uploadWorker.Stop()

	logRetentionWorker := worker.NewLogRetentionWorker(db)
	logRetentionWorker.Start()
	defer logRetentionWorker.Stop()

	diskAlertWorker := worker.NewDiskAlertWorker(db, cfg)
	diskAlertWorker.Start()
	defer diskAlertWorker.Stop()

	// System monitor: antd health, wallet balance, queue backlog, failure rate, etc.
	sysMonitor := worker.NewSystemMonitor(db, cfg)
	sysMonitor.Start()
	defer sysMonitor.Stop()

	// S15: Idempotency key cleanup (every 5 minutes instead of hourly)
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			middleware.CleanupIdempotencyKeys(db)
		}
	}()

	// Start server
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      router,
		ReadTimeout:  120 * time.Second,
		WriteTimeout: 1 * time.Hour, // long-running downloads stream antd responses through the response writer
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-done
	slog.Info("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "error", err)
	}

	slog.Info("stopped")
}
