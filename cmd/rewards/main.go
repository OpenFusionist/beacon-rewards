package main

import (
	"endurance-rewards/internal/config"
	"endurance-rewards/internal/dora"
	"endurance-rewards/internal/rewards"
	"endurance-rewards/internal/server"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

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
	slog.Info("Starting Endurance Rewards Service")

	// Load configuration
	cfg := loadConfig()
	// Create rewards service
	rewardsService := rewards.NewService(cfg)
	if err := rewardsService.Start(); err != nil {
		slog.Error("Failed to start rewards service", "error", err)
		os.Exit(1)
	}

	var doraDB *dora.DB
	if db, err := dora.New(cfg); err != nil {
		slog.Error("Failed to connect to Dora Postgres", "error", err)
	} else {
		doraDB = db
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

// loadConfig loads the application configuration
func loadConfig() *config.Config {
	cfg := config.DefaultConfig()

	// Override with environment variables if present
	if addr := os.Getenv("SERVER_ADDRESS"); addr != "" {
		cfg.ServerAddress = addr
	}
	if port := os.Getenv("SERVER_PORT"); port != "" {
		cfg.ServerPort = port
	}
	// Dora PG single URL
	if v := os.Getenv("DORA_PG_URL"); v != "" {
		cfg.DoraPGURL = v
	}
	if beaconURL := os.Getenv("BEACON_NODE_URL"); beaconURL != "" {
		cfg.BeaconNodeURL = beaconURL
	}
	if execURL := os.Getenv("EXECUTION_NODE_URL"); execURL != "" {
		cfg.ExecutionNodeURL = execURL
	}
	if startEpoch := os.Getenv("START_EPOCH"); startEpoch != "" {
		startEpochInt, err := strconv.Atoi(startEpoch)
		if err != nil {
			slog.Error("Invalid START_EPOCH value", "error", err)
			os.Exit(1)
		}
		cfg.StartEpoch = uint64(startEpochInt)
	}
	if bfConc := os.Getenv("BACKFILL_CONCURRENCY"); bfConc != "" {
		bfConcInt, err := strconv.Atoi(bfConc)
		if err != nil {
			slog.Error("Invalid BACKFILL_CONCURRENCY value", "error", err)
			os.Exit(1)
		}
		if bfConcInt <= 0 {
			bfConcInt = 1
		}
		cfg.BackfillConcurrency = bfConcInt
	}
	if epochInterval := os.Getenv("EPOCH_UPDATE_INTERVAL"); epochInterval != "" {
		dur, err := time.ParseDuration(epochInterval)
		if err != nil {
			slog.Error("Invalid EPOCH_UPDATE_INTERVAL value", "error", err)
			os.Exit(1)
		}
		cfg.EpochUpdateInterval = dur
	}

	// Log configuration
	slog.Info("Configuration loaded", "server_address", cfg.ServerAddress, "server_port", cfg.ServerPort, "dora_pg_url", cfg.DoraPGURL, "beacon_node", cfg.BeaconNodeURL, "execution_node", cfg.ExecutionNodeURL, "cache_reset_interval", cfg.CacheResetInterval, "epoch_update_interval", cfg.EpochUpdateInterval, "backfill_concurrency", cfg.BackfillConcurrency, "start_epoch", cfg.StartEpoch)

	return cfg
}
