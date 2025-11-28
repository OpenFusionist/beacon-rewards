package main

import (
	_ "beacon-rewards/docs"
	"beacon-rewards/internal/config"
	"beacon-rewards/internal/dora"
	"beacon-rewards/internal/rewards"
	"beacon-rewards/internal/server"
	"beacon-rewards/internal/utils"
	"os"
	"os/signal"
	"syscall"

	"log/slog"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

func setupLoggerFromEnv() {
	levelStr := os.Getenv("LOG_LEVEL")
	var level slog.Level
	switch levelStr {
	case "debug", "DEBUG":
		level = slog.LevelDebug
	case "warn", "WARN", "warning", "WARNING":
		level = slog.LevelWarn
	case "error", "ERROR":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	format := os.Getenv("LOG_FORMAT")
	var handler slog.Handler
	if format == "json" || format == "JSON" {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	} else {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	}

	slog.SetDefault(slog.New(handler))
}

func main() {
	// Load .env file (ignore error if file doesn't exist)
	_ = godotenv.Load()

	// Setup logging
	setupLoggerFromEnv()
	slog.Info("Starting Beacon Rewards Service")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Configure genesis timestamp for epoch conversions.
	utils.SetGenesisTimestamp(cfg.GenesisTimestamp)
	logConfig(cfg)

	var doraDB *dora.DB
	if db, err := dora.New(cfg); err != nil {
		slog.Error("Failed to connect to Dora Postgres", "error", err)
	} else {
		doraDB = db
	}

	// Create rewards service
	rewardsService := rewards.NewService(cfg)
	// Attach Dora DB so service can sum effective balances
	rewardsService.SetDoraDB(doraDB)
	if err := rewardsService.Start(); err != nil {
		slog.Error("Failed to start rewards service", "error", err)
		os.Exit(1)
	}

	// Create and start HTTP server
	httpServer := server.NewServer(cfg, rewardsService, doraDB)
	if err := httpServer.Start(); err != nil {
		slog.Error("Failed to start HTTP server", "error", err)
		os.Exit(1)
	}

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	slog.Info("Shutting down gracefully...")

	// Shutdown services
	if err := httpServer.Stop(); err != nil {
		slog.Error("Error stopping HTTP server", "error", err)
	}

	rewardsService.Stop()
	if doraDB != nil {
		doraDB.Close()
	}

	slog.Info("Shutdown complete")
}

func logConfig(cfg *config.Config) {
	args := []any{
		"listen_address", cfg.ListenAddress(),
		"beacon_node", cfg.BeaconNodeURL,
		"execution_node", cfg.ExecutionNodeURL,
		"cache_reset_interval", cfg.CacheResetInterval,
		"epoch_check_interval", cfg.EpochCheckInterval,
		"backfill_concurrency", cfg.BackfillConcurrency,
		"backfill_lookback", cfg.BackfillLookback,
		"request_timeout", cfg.RequestTimeout,
		"default_api_limit", cfg.DefaultAPILimit,
		"depositor_labels_file", cfg.DepositorLabelsFile,
		"frontend_enabled", cfg.EnableFrontend,
		"genesis_timestamp", cfg.GenesisTimestamp,
	}

	slog.Info("Configuration loaded", args...)
}
