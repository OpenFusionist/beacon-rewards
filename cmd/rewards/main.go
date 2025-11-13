package main

import (
	"endurance-rewards/internal/config"
	"endurance-rewards/internal/rewards"
	"endurance-rewards/internal/server"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"log/slog"

	"github.com/joho/godotenv"
)

func main() {
	// Load .env file (ignore error if file doesn't exist)
	_ = godotenv.Load()

	// Setup logging
	slog.Info("Starting Endurance Rewards Service")

	// Load configuration
	cfg := loadConfig()
	// Create rewards service
	rewardsService := rewards.NewService(cfg)
	if err := rewardsService.Start(); err != nil {
		slog.Error("Failed to start rewards service", "error", err)
		os.Exit(1)
	}

	// Create and start HTTP server
	httpServer := server.NewServer(cfg, rewardsService)
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

	// Log configuration
	slog.Info("Configuration loaded", "server_address", cfg.ServerAddress, "server_port", cfg.ServerPort, "beacon_node", cfg.BeaconNodeURL, "execution_node", cfg.ExecutionNodeURL, "cache_reset_interval", cfg.CacheResetInterval, "epoch_update_interval", cfg.EpochUpdateInterval)

	return cfg
}
