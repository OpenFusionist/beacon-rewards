package server

import (
	"context"
	"endurance-rewards/internal/config"
	"endurance-rewards/internal/rewards"
	"fmt"
	"net/http"
	"time"

	"log/slog"

	"github.com/gin-gonic/gin"
)

// Server represents the HTTP server
type Server struct {
	config         *config.Config
	rewardsService *rewards.Service
	router         *gin.Engine
	httpServer     *http.Server
}

// NewServer creates a new HTTP server
func NewServer(cfg *config.Config, rewardsService *rewards.Service) *Server {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(loggingMiddleware())

	s := &Server{
		config:         cfg,
		rewardsService: rewardsService,
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
