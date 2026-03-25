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
		configPath string
		showVer    bool
	)
	flag.StringVar(&configPath, "config", "", "path to indelible.toml config file")
	flag.BoolVar(&showVer, "version", false, "print version and exit")
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
		defer antdMgr.Stop()
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
		os.Exit(1)
	}
	defer db.Close()

	// Run migrations
	if err := database.Migrate(db, cfg.DBDriver()); err != nil {
		slog.Error("failed to run migrations", "error", err)
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

	// Start server
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 300 * time.Second, // long for file uploads
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
