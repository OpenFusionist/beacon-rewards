package server

import (
	"context"
	"endurance-rewards/internal/config"
	"endurance-rewards/internal/dora"
	"endurance-rewards/internal/rewards"
	"net/http"
	"strconv"
	"time"

	"log/slog"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// @title           Endurance Rewards API
// @version         1.0
// @description     REST API for Ethereum validator rewards and deposits analytics.
// @BasePath        /
// @schemes         http https
// @produce         json
// @consumes        json

// Server represents the HTTP server
type Server struct {
	config          *config.Config
	rewardsService  *rewards.Service
	doraDB          *dora.DB
	router          *gin.Engine
	httpServer      *http.Server
	depositorLabels map[string]string
}

// NewServer creates a new HTTP server
func NewServer(cfg *config.Config, rewardsService *rewards.Service, doraDB *dora.DB) *Server {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(loggingMiddleware())

	depositorLabels, err := loadDepositorLabels(cfg.DepositorLabelsFile)
	if err != nil {
		slog.Warn("Failed to load depositor labels", "path", cfg.DepositorLabelsFile, "error", err)
	}

	s := &Server{
		config:          cfg,
		rewardsService:  rewardsService,
		doraDB:          doraDB,
		router:          router,
		depositorLabels: depositorLabels,
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

	// get top deposits by witrdraw address
	s.router.GET("/deposits/top-withdrawals", s.topWithdrawalsHandler)

	// get top deposits by address to deposit contracts
	s.router.GET("/deposits/top-deposits", s.topDepositsHandler)

	// Swagger UI (requires generated docs; run `swag init` and import docs package in main)
	//http://localhost:8080/swagger/index.html

	s.router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
}

// Start starts the HTTP server
func (s *Server) Start() error {
	s.httpServer = &http.Server{
		Addr:    s.config.ListenAddress(),
		Handler: s.router,
	}

	slog.Info("Starting HTTP server", "address", s.httpServer.Addr)

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
// @Summary      Health check
// @Tags         Health
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Router       /health [get]
func (s *Server) healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
		"time":   time.Now().Unix(),
	})
}

// topDepositsHandler aggregates deposit amounts && validator counts by depositor (tx sender) and returns top N by validator counts.
// @Summary      aggregates deposit amounts && validator counts by depositor (tx sender) and returns top N by validator counts.
// @Tags         Deposits
// @Produce      json
// @Param        limit   query     int  false  "Number of results to return"  default(100)
// @Success      200     {object}  map[string]interface{}
// @Failure      503     {object}  map[string]string
// @Failure      500     {object}  map[string]string
// @Router       /deposits/top-deposits [get]
func (s *Server) topDepositsHandler(c *gin.Context) {
	if !s.ensureDoraDB(c) {
		return
	}

	s.respondWithTop(c, func(ctx context.Context, limit int) (any, error) {
		stats, err := s.doraDB.TopDepositorAddresses(ctx, limit)
		if err != nil {
			return nil, err
		}
		s.applyDepositorLabels(stats)
		return stats, nil
	})
}

// topWithdrawalsHandler aggregates deposit amounts && validator counts by withdrawal address and returns top N by validator counts.
// @Summary      aggregates deposit amounts && validator counts by withdrawal address and returns top N by validator counts.
// @Tags         Deposits
// @Produce      json
// @Param        limit   query     int  false  "Number of results to return"  default(100)
// @Success      200     {object}  map[string]interface{}
// @Failure      503     {object}  map[string]string
// @Failure      500     {object}  map[string]string
// @Router       /deposits/top-withdrawals [get]
func (s *Server) topWithdrawalsHandler(c *gin.Context) {
	if !s.ensureDoraDB(c) {
		return
	}

	s.respondWithTop(c, func(ctx context.Context, limit int) (any, error) {
		return s.doraDB.TopWithdrawalAddresses(ctx, limit)
	})
}

// RewardsRequest represents the request body for rewards query
type RewardsRequest struct {
	Validators []uint64 `json:"validators" binding:"required"`
}

// rewardsHandler handles reward queries
// @Summary      Get total rewards (EL+CL) for validators from Today's rewards from UTC 0:00 to the present.
// @Tags         Rewards
// @Accept       json
// @Produce      json
// @Param        request  body   RewardsRequest  true  "Validators request"
// @Success      200      {object}  map[string]interface{}
// @Failure      400      {object}  map[string]string
// @Router       /rewards [post]
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

	var effectiveBalances map[uint64]int64
	if s.doraDB != nil {
		ctx, cancel := s.requestContext(c)
		balances, err := s.doraDB.EffectiveBalances(ctx, req.Validators)
		cancel()
		if err != nil {
			slog.Error("Failed to load effective balances", "error", err)
		} else {
			effectiveBalances = balances
		}
	}

	// Get total rewards (EL+CL) for each requested validator
	validatorRewards := s.rewardsService.GetTotalRewards(req.Validators, effectiveBalances)

	c.JSON(http.StatusOK, gin.H{
		"validator_count": len(req.Validators),
		"rewards":         validatorRewards,
	})
}

func (s *Server) ensureDoraDB(c *gin.Context) bool {
	if s.doraDB != nil {
		return true
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"error": "Dora Postgres is not configured/connected",
	})
	return false
}

func (s *Server) limitParam(c *gin.Context) int {
	limit := s.config.DefaultAPILimit
	if limit <= 0 {
		limit = 100
	}

	if raw := c.Query("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	return limit
}

func (s *Server) requestContext(c *gin.Context) (context.Context, context.CancelFunc) {
	timeout := s.config.RequestTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return context.WithTimeout(c.Request.Context(), timeout)
}

func (s *Server) respondWithTop(c *gin.Context, fetch func(context.Context, int) (any, error)) {
	limit := s.limitParam(c)
	ctx, cancel := s.requestContext(c)
	defer cancel()

	results, err := fetch(ctx, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"limit":   limit,
		"results": results,
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
