package server

import (
	"context"
	"endurance-rewards/internal/config"
	"endurance-rewards/internal/dora"
	"endurance-rewards/internal/rewards"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"log/slog"

	"github.com/gin-gonic/gin"
)

// Server represents the HTTP server
type Server struct {
	config         *config.Config
	rewardsService *rewards.Service
	doraDB         *dora.DB
	router         *gin.Engine
	httpServer     *http.Server
}

// NewServer creates a new HTTP server
func NewServer(cfg *config.Config, rewardsService *rewards.Service, doraDB *dora.DB) *Server {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(loggingMiddleware())

	s := &Server{
		config:         cfg,
		rewardsService: rewardsService,
		doraDB:         doraDB,
		router:         router,
	}

	s.setupRoutes()

	return s
}

// setupRoutes configures the API routes
func (s *Server) setupRoutes() {
	// Health check endpoint
	s.router.GET("/health", s.healthHandler)

	// Rewards endpoint
	s.router.POST("/rewards", s.rewardsHandler)

	// Deposits → top withdrawals (aggregated by normalized 0x01/0x02 address)
	s.router.GET("/deposits/top-withdrawals", s.topWithdrawalsHandler)
}

// Start starts the HTTP server
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%s", s.config.ServerAddress, s.config.ServerPort)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: s.router,
	}

	slog.Info("Starting HTTP server", "address", addr)

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Failed to start HTTP server", "error", err)
		}
	}()

	return nil
}

// Stop gracefully stops the HTTP server
func (s *Server) Stop() error {
	if s.httpServer == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	slog.Info("Stopping HTTP server")
	return s.httpServer.Shutdown(ctx)
}

// healthHandler handles health check requests
func (s *Server) healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
		"time":   time.Now().Unix(),
	})
}

// topWithdrawalsHandler aggregates deposit amounts by withdrawal address and returns top N.
func (s *Server) topWithdrawalsHandler(c *gin.Context) {
	if s.doraDB == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Dora Postgres is not configured/connected",
		})
		return
	}

	limit := 100
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	stats, err := s.doraDB.TopWithdrawalAddresses(ctx, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"limit":   limit,
		"results": stats,
	})
}

// RewardsRequest represents the request body for rewards query
type RewardsRequest struct {
	Validators []uint64 `json:"validators" binding:"required"`
}

// rewardsHandler handles reward queries
func (s *Server) rewardsHandler(c *gin.Context) {
	var req RewardsRequest

	// Parse JSON request body
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request body: validators array is required",
		})
		return
	}

	if len(req.Validators) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Validators array cannot be empty",
		})
		return
	}

	// Get total rewards (EL+CL) for each requested validator
	validatorRewards := s.rewardsService.GetTotalRewards(req.Validators)

	c.JSON(http.StatusOK, gin.H{
		"validator_count": len(req.Validators),
		"rewards":         validatorRewards,
	})
}

// loggingMiddleware logs HTTP requests
func loggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		statusCode := c.Writer.Status()

		slog.Info("HTTP request", "method", c.Request.Method, "path", path, "query", query, "status", statusCode, "latency", latency, "ip", c.ClientIP())
	}
}
