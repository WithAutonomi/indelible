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

	"github.com/maidsafe/indelible/internal/config"
	"github.com/maidsafe/indelible/internal/database"
	"github.com/maidsafe/indelible/internal/handlers"
	"github.com/maidsafe/indelible/internal/worker"
)

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

	// Start upload worker
	uploadWorker := worker.NewUploadWorker(db, cfg)
	uploadWorker.Start()
	defer uploadWorker.Stop()

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
