package server

import (
	"context"
	"endurance-rewards/internal/config"
	"endurance-rewards/internal/dora"
	"endurance-rewards/internal/rewards"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"golang.org/x/sync/errgroup"
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
	s.router.GET("/rewards/network", s.networkRewardsHandler)
	s.router.POST("/rewards", s.rewardsHandler)
	s.router.POST("/rewards/by-address", s.addressRewardsHandler)

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
// @Param        limit    query     int     false  "Number of results to return"  default(100)
// @Param        sort_by  query     string  false  "Sort field (total_deposit,depositor_address,validators_total, slashed, voluntary_exited, active)"  default(total_deposit)
// @Param        order    query     string  false  "Sort order (asc|desc)"  default(desc)
// @Success      200     {object}  map[string]interface{}
// @Failure      503     {object}  map[string]string
// @Failure      500     {object}  map[string]string
// @Router       /deposits/top-deposits [get]
func (s *Server) topDepositsHandler(c *gin.Context) {
	if !s.ensureDoraDB(c) {
		return
	}

	s.respondWithTop(c, func(ctx context.Context, limit int, sortBy string, order string) (any, error) {
		stats, err := s.doraDB.TopDepositorAddresses(ctx, limit, sortBy, order)
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
// @Param        limit    query     int     false  "Number of results to return"  default(100)
// @Param        sort_by  query     string  false  "Sort field (total_deposit,withdrawal_address,validators_total, slashed, voluntary_exited, active)"  default(total_deposit)
// @Param        order    query     string  false  "Sort order (asc|desc)"  default(desc)
// @Success      200     {object}  map[string]interface{}
// @Failure      503     {object}  map[string]string
// @Failure      500     {object}  map[string]string
// @Router       /deposits/top-withdrawals [get]
func (s *Server) topWithdrawalsHandler(c *gin.Context) {
	if !s.ensureDoraDB(c) {
		return
	}

	s.respondWithTop(c, func(ctx context.Context, limit int, sortBy string, order string) (any, error) {
		return s.doraDB.TopWithdrawalAddresses(ctx, limit, sortBy, order)
	})
}

// networkRewardsHandler returns aggregated cache rewards for all validators over the config window.
// @Summary      Get total validator rewards for the config window
// @Description  Uses cached consensus/execution rewards to calculate global CL/EL totals and a daily APR estimate.
// @Tags         Rewards
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Router       /rewards/network [get]
func (s *Server) networkRewardsHandler(c *gin.Context) {
	snapshot := s.rewardsService.TotalNetworkRewards()
	historyEntries, err := s.rewardsService.RewardHistory()
	if err != nil {
		slog.Error("Failed to load rewards history", "error", err)
	}

	response := gin.H{
		"current": snapshot,
	}
	if err != nil {
		response["history_error"] = "failed to load stored history"
	} else if len(historyEntries) > 0 {
		response["history"] = historyEntries
	}

	c.JSON(http.StatusOK, response)
}

// RewardsRequest represents the request body for rewards query
type RewardsRequest struct {
	Validators []uint64 `json:"validators" binding:"required"`
}

// AddressRewardsRequest represents the request body for reward aggregation per depositor address.
type AddressRewardsRequest struct {
	Address string `json:"address" binding:"required"`
}

// AddressRewardsResult captures the aggregated rewards per depositor or withdrawal address.
type AddressRewardsResult struct {
	Address                   string    `json:"address"`
	DepositorLabel            string    `json:"depositor_label,omitempty"`
	ActiveValidatorCount      int       `json:"active_validator_count"`
	ClRewardsGwei             int64     `json:"cl_rewards_gwei"`
	ElRewardsGwei             int64     `json:"el_rewards_gwei"`
	TotalRewardsGwei          int64     `json:"total_rewards_gwei"`
	TotalEffectiveBalanceGwei int64     `json:"total_effective_balance_gwei"`
	WeightedAverageStakeTime  int64     `json:"weighted_average_stake_time(seconds)"`
	WindowStart               time.Time `json:"window_start"`
	WindowEnd                 time.Time `json:"window_end"`
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

// addressRewardsHandler aggregates validator rewards by withdrawal or deposit addresses.
// @Summary      Get aggregated validator rewards (EL+CL) per withdrawal or deposit address.
// @Description  Looks up validators funded by withdrawal or deposit address and returns the summed rewards for those validators.
// @Tags         Rewards
// @Accept       json
// @Produce      json
// @Param        request  body   AddressRewardsRequest  true  "Addresses request"
// @Success      200      {object}  AddressRewardsResult
// @Failure      400      {object}  map[string]string
// @Failure      503      {object}  map[string]string
// @Router       /rewards/by-address [post]
func (s *Server) addressRewardsHandler(c *gin.Context) {
	if !s.ensureDoraDB(c) {
		return
	}

	var req AddressRewardsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request body: address is required",
		})
		return
	}

	if strings.TrimSpace(req.Address) == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Address cannot be empty",
		})
		return
	}

	ctx, cancel := s.requestContext(c)
	defer cancel()

	// 1) Handle withdrawal credentials: e.g.0x0100000000000000000000000988dc1554cf6877508208fff8aab4e5afa11ee3
	if strings.HasPrefix(req.Address, "0x01") || strings.HasPrefix(req.Address, "0x02") {
		// withdrawal_credentials: 0x01 (or 0x02) + 11 bytes zero + 20 bytes ETH address
		// hex: "0x01" or "0x02" (2+2) + 22 zeros (11 bytes) + 40 chars (20 bytes)
		if len(req.Address) == 66 { // "0x" + 64 hex chars for withdrawal_credentials
			req.Address = strings.ToLower("0x" + req.Address[26:])
			slog.Info("withdrawal address", "address", req.Address)
		}
	}

	currentEpoch := s.rewardsService.GetCurrentEpoch(time.Now())
	validatorIndices, err := s.doraDB.ActiveValidatorsIndexByAddress(ctx, req.Address, currentEpoch)
	if err != nil {
		if errors.Is(err, dora.ErrInvalidAddress) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		slog.Error("Failed to load validators by address", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load validator index for addresses"})
		return
	}

	var (
		effectiveBalances    map[uint64]int64
		weightedAvgStakeTime int64
	)

	g, gCtx := errgroup.WithContext(ctx)

	// 1. Load effective balances
	g.Go(func() error {
		if len(validatorIndices) > 0 {
			balances, err := s.doraDB.EffectiveBalances(gCtx, validatorIndices)
			if err != nil {
				slog.Error("Failed to load effective balances", "error", err)
				// Don't fail the request, just log error
				return nil
			}
			effectiveBalances = balances
		}
		return nil
	})

	// 2. Calculate weighted average stake time
	g.Go(func() error {
		if len(validatorIndices) > 0 {
			avg, err := s.doraDB.GetWeightedAverageStakeTime(gCtx, validatorIndices)
			if err != nil {
				slog.Error("Failed to calculate weighted average stake time", "error", err)
				// Don't fail the request, just log error
				return nil
			}
			weightedAvgStakeTime = avg
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		slog.Error("Error in parallel requests", "error", err)
	}

	validatorRewards := s.rewardsService.GetTotalRewards(validatorIndices, effectiveBalances)
	windowStart, windowEnd := s.rewardsService.GetRewardWindow()

	result := AddressRewardsResult{
		Address:                  req.Address,
		ActiveValidatorCount:     len(validatorIndices),
		WindowStart:              windowStart,
		WindowEnd:                windowEnd,
		WeightedAverageStakeTime: weightedAvgStakeTime,
	}

	if label, ok := s.lookupDepositorLabel(req.Address); ok {
		result.DepositorLabel = label
	}

	for _, idx := range validatorIndices {
		reward, ok := validatorRewards[idx]
		if !ok {
			continue
		}
		result.ClRewardsGwei += reward.ClRewardsGwei
		result.ElRewardsGwei += reward.ElRewardsGwei
		result.TotalRewardsGwei += reward.TotalRewardsGwei
		result.TotalEffectiveBalanceGwei += reward.EffectiveBalanceGwei
	}
	c.JSON(http.StatusOK, result)

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

func (s *Server) respondWithTop(c *gin.Context, fetch func(context.Context, int, string, string) (any, error)) {
	limit := s.limitParam(c)
	sortBy := strings.TrimSpace(c.Query("sort_by"))
	order := strings.ToLower(strings.TrimSpace(c.Query("order")))
	ctx, cancel := s.requestContext(c)
	defer cancel()

	results, err := fetch(ctx, limit, sortBy, order)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"limit":   limit,
		"sort_by": sortBy,
		"order":   order,
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
