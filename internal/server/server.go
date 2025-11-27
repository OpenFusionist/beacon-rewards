package server

import (
	"context"
	"endurance-rewards/internal/config"
	"endurance-rewards/internal/dora"
	"endurance-rewards/internal/rewards"
	"endurance-rewards/internal/utils"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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
	templates       map[string]*template.Template
	frontendEnabled bool
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

	var templates map[string]*template.Template
	frontendEnabled := cfg.EnableFrontend
	if cfg.EnableFrontend {
		templates, err = loadTemplates()
		if err != nil {
			slog.Warn("Failed to load templates", "error", err)
		}
		if len(templates) == 0 {
			slog.Warn("Disabling frontend: no templates found")
			frontendEnabled = false
		}
	} else {
		slog.Info("Frontend disabled via configuration")
	}

	s := &Server{
		config:          cfg,
		rewardsService:  rewardsService,
		doraDB:          doraDB,
		router:          router,
		depositorLabels: depositorLabels,
		templates:       templates,
		frontendEnabled: frontendEnabled,
	}

	// Set HTML renderer
	if s.frontendEnabled && templates != nil {
		s.router.HTMLRender = &HTMLRenderer{templates: templates}
	}

	s.setupRoutes()

	return s
}

// setupRoutes configures the API routes
func (s *Server) setupRoutes() {
	if s.frontendEnabled {
		// Static files
		s.router.Static("/static", "./internal/server/static")

		// Page routes (HTML pages) - order matters, more specific routes first
		s.router.GET("/", func(c *gin.Context) {
			slog.Info("Root path accessed, redirecting to top-deposits")
			c.Redirect(http.StatusFound, "/deposits/top-deposits")
		})

		slog.Info("Registering routes with frontend enabled",
			"top-deposits", "/deposits/top-deposits",
			"network-rewards", "/rewards/network",
			"address-rewards", "/rewards/by-address")
	} else {
		slog.Info("Registering API-only routes; frontend disabled")
	}

	s.router.GET("/deposits/top-deposits", s.topDepositsPageOrAPIHandler)
	s.router.GET("/rewards/network", s.networkRewardsPageOrAPIHandler)
	if s.frontendEnabled {
		s.router.GET("/rewards/by-address", s.addressRewardsPageHandler)
	}

	// Health check endpoint
	s.router.GET("/health", s.healthHandler)

	// API endpoints
	s.router.POST("/rewards", s.rewardsHandler)
	s.router.POST("/rewards/by-address", s.addressRewardsHandler)

	// get top deposits by witrdraw address
	s.router.GET("/deposits/top-withdrawals", s.topWithdrawalsHandler)

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
// @Param        sort_by  query     string  false  "Sort field (total_deposit,depositor_address,withdrawal_address,validators_total, slashed, voluntary_exited, active, total_active_effective_balance)"  default(total_deposit)
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
// @Param        sort_by  query     string  false  "Sort field (total_deposit,withdrawal_address,validators_total, slashed, voluntary_exited, active, total_active_effective_balance)"  default(total_deposit)
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
	historyEntries, err := s.rewardsService.NetworkRewardHistory()
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
	Address string `json:"address" form:"address" binding:"required"`
}

// AddressRewardsResult captures the aggregated rewards per depositor or withdrawal address.
type AddressRewardsResult struct {
	Address                        string    `json:"address"`
	DepositorLabel                 string    `json:"depositor_label,omitempty"`
	ActiveValidatorCount           int       `json:"active_validator_count"`
	ValidatorIndices               []uint64  `json:"validator_indices,omitempty"`
	ClRewardsGwei                  int64     `json:"cl_rewards_gwei"`
	ElRewardsGwei                  int64     `json:"el_rewards_gwei"`
	TotalRewardsGwei               int64     `json:"total_rewards_gwei"`
	TotalEffectiveBalanceGwei      int64     `json:"total_effective_balance_gwei"`
	EstimatedHistoryRewards31dGwei float64   `json:"estimated_history_rewards_31d_gwei"`
	WeightedAverageStakeTime       int64     `json:"weighted_average_stake_time(seconds)"`
	WindowStart                    time.Time `json:"window_start"`
	WindowEnd                      time.Time `json:"window_end"`
}

// RewardsResponse
type RewardsResponse struct {
	ValidatorCount int                                 `json:"validator_count"`
	Rewards        map[uint64]*rewards.ValidatorReward `json:"rewards"`
	WindowStart    time.Time                           `json:"window_start"`
	WindowEnd      time.Time                           `json:"window_end"`
}

// rewardsHandler handles reward queries
// @Summary      Get total rewards (EL+CL) for validators from Today's rewards from UTC 0:00 to the present.
// @Tags         Rewards
// @Accept       json
// @Produce      json
// @Param        request  body   RewardsRequest  true  "Validators request"
// @Success      200      {object}  RewardsResponse
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
	windowStart, windowEnd := s.rewardsService.GetRewardWindow()

	result := RewardsResponse{
		ValidatorCount: len(req.Validators),
		Rewards:        validatorRewards,
		WindowStart:    windowStart,
		WindowEnd:      windowEnd,
	}
	c.JSON(http.StatusOK, result)
}

// addressRewardsHandler aggregates validator rewards by withdrawal or deposit addresses.
// @Summary      Get aggregated validator rewards (EL+CL) per withdrawal or deposit address.
// @Description  Looks up validators funded by withdrawal or deposit address and returns the summed rewards for those validators. Set include_validator_indices query parameter to true to include active validator indices in the response.
// @Tags         Rewards
// @Accept       json
// @Produce      json
// @Param        request  body   AddressRewardsRequest  true  "Addresses request"
// @Param        include_validator_indices  query   bool  false  "Include validator indices in response"  default(false)
// @Success      200      {object}  AddressRewardsResult
// @Failure      400      {object}  map[string]string
// @Failure      503      {object}  map[string]string
// @Router       /rewards/by-address [post]
func (s *Server) addressRewardsHandler(c *gin.Context) {
	if !s.ensureDoraDB(c) {
		return
	}

	var req AddressRewardsRequest
	var err error
	contentType := c.GetHeader("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		err = c.ShouldBindJSON(&req)
	} else {
		err = c.ShouldBind(&req)
	}
	if err != nil {
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

	currentEpoch := utils.TimeToEpoch(time.Now())

	details, err := s.doraDB.ValidatorDetailsByAddress(ctx, req.Address)
	if err != nil {
		if errors.Is(err, dora.ErrInvalidAddress) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		slog.Error("Failed to load validators by address", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load validator details for addresses"})
		return
	}

	allValidatorIndices := make([]uint64, 0, len(details))
	activeValidatorIndices := make([]uint64, 0, len(details))

	effectiveBalances := make(map[uint64]int64, len(details))
	depositBalances := make(map[uint64]int64, len(details))
	lifecycles := make(map[uint64]dora.ValidatorLifecycle, len(details))

	for _, d := range details {
		idx := d.ValidatorIndex
		allValidatorIndices = append(allValidatorIndices, idx)
		if d.EffectiveBalance > 0 {
			effectiveBalances[idx] = d.EffectiveBalance
		}
		if d.TotalDepositGwei > 0 {
			depositBalances[idx] = d.TotalDepositGwei
		}
		lifecycles[idx] = dora.ValidatorLifecycle{
			ActivationEpoch: d.ActivationEpoch,
			ExitEpoch:       d.ExitEpoch,
		}
		if d.ActivationEpoch <= currentEpoch && d.ExitEpoch > currentEpoch {
			activeValidatorIndices = append(activeValidatorIndices, idx)
		}
	}

	var (
		weightedAvgStakeTime int64
		validatorRewards     map[uint64]*rewards.ValidatorReward
		windowStart          time.Time
		windowEnd            time.Time
		estimatedRewards     float64
	)

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		if len(allValidatorIndices) == 0 {
			return
		}
		if avg, err := s.doraDB.GetWeightedAverageStakeTime(ctx, allValidatorIndices); err == nil {
			weightedAvgStakeTime = avg
		} else {
			slog.Error("Failed to calculate weighted average stake time", "error", err)
		}
	}()

	go func() {
		defer wg.Done()
		validatorRewards = s.rewardsService.GetTotalRewards(activeValidatorIndices, effectiveBalances)
		windowStart, windowEnd = s.rewardsService.GetRewardWindow()
	}()

	go func() {
		defer wg.Done()
		networkSnapshot := s.rewardsService.TotalNetworkRewards()
		estimatedRewards = estimateRecentRewardsForValidators(
			allValidatorIndices,
			networkSnapshot.ProjectAprPercent,
			currentEpoch,
			estimateWindowEpochs(),
			effectiveBalances,
			depositBalances,
			lifecycles,
		)
	}()

	wg.Wait()

	result := AddressRewardsResult{
		Address:                  req.Address,
		ActiveValidatorCount:     len(activeValidatorIndices),
		WindowStart:              windowStart,
		WindowEnd:                windowEnd,
		WeightedAverageStakeTime: weightedAvgStakeTime,
	}
	includeIndices := c.Query("include_validator_indices")
	if includeIndices != "" {
		if parsed, err := strconv.ParseBool(includeIndices); err == nil && parsed {
			result.ValidatorIndices = allValidatorIndices
		}
	}

	if label, ok := s.lookupDepositorLabel(req.Address); ok {
		result.DepositorLabel = label
	}

	for _, idx := range activeValidatorIndices {
		reward, ok := validatorRewards[idx]
		if !ok {
			continue
		}
		result.ClRewardsGwei += reward.ClRewardsGwei
		result.ElRewardsGwei += reward.ElRewardsGwei
		result.TotalRewardsGwei += reward.TotalRewardsGwei
		result.TotalEffectiveBalanceGwei += reward.EffectiveBalanceGwei
	}
	result.EstimatedHistoryRewards31dGwei = estimatedRewards
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

// Page handlers

func (s *Server) topDepositsPageOrAPIHandler(c *gin.Context) {
	slog.Info("topDepositsPageOrAPIHandler called",
		"path", c.Request.URL.Path,
		"method", c.Request.Method,
		"hx-request", c.GetHeader("HX-Request"),
		"accept", c.GetHeader("Accept"))

	if !s.frontendEnabled {
		slog.Info("Frontend disabled, serving top-deposits as JSON")
		s.topDepositsAPIHandler(c)
		return
	}

	// Check if this is an HTMX request (for table fragment) or API request
	if c.GetHeader("HX-Request") == "true" {
		slog.Info("Handling as HTMX request for table fragment")
		s.topDepositsTableHandler(c)
		return
	}

	// Check if this is an API request (Accept: application/json)
	accept := c.GetHeader("Accept")
	if accept != "" && strings.Contains(accept, "application/json") {
		slog.Info("Handling as JSON API request")
		s.topDepositsAPIHandler(c)
		return
	}

	// Otherwise, render the page
	if len(s.templates) == 0 {
		slog.Error("Templates not loaded")
		c.String(http.StatusInternalServerError, "Templates not loaded")
		return
	}

	limit := s.limitParam(c)
	sortBy := strings.TrimSpace(c.Query("sort_by"))
	if sortBy == "" {
		sortBy = "total_deposit"
	}
	order := strings.ToLower(strings.TrimSpace(c.Query("order")))
	if order == "" {
		order = "desc"
	}

	slog.Info("Rendering top-deposits.html template",
		"path", c.Request.URL.Path,
		"limit", limit,
		"sortBy", sortBy,
		"order", order)

	data := gin.H{
		"Limit":       limit,
		"SortBy":      sortBy,
		"Order":       order,
		"CurrentPath": c.Request.URL.Path,
	}

	c.HTML(http.StatusOK, "top-deposits.html", data)
}

func (s *Server) topDepositsAPIHandler(c *gin.Context) {
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

func (s *Server) topDepositsTableHandler(c *gin.Context) {
	if !s.ensureDoraDB(c) {
		return
	}

	if len(s.templates) == 0 {
		c.String(http.StatusInternalServerError, "Templates not loaded")
		return
	}

	limit := s.limitParam(c)
	sortBy := strings.TrimSpace(c.Query("sort_by"))
	if sortBy == "" {
		sortBy = "total_deposit"
	}
	order := strings.ToLower(strings.TrimSpace(c.Query("order")))
	if order == "" {
		order = "desc"
	}

	ctx, cancel := s.requestContext(c)
	defer cancel()

	stats, err := s.doraDB.TopDepositorAddresses(ctx, limit, sortBy, order)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"error": err.Error()})
		return
	}
	s.applyDepositorLabels(stats)

	// Convert stats to map for template
	results := make([]map[string]interface{}, len(stats))
	for i, stat := range stats {
		results[i] = map[string]interface{}{
			"depositor_address":              stat.DepositorAddress,
			"depositor_label":                stat.DepositorLabel,
			"withdrawal_address":             stat.WithdrawalAddress,
			"total_deposit":                  stat.TotalDeposit,
			"validators_total":               stat.ValidatorsTotal,
			"active":                         stat.Active,
			"slashed":                        stat.Slashed,
			"voluntary_exited":               stat.VoluntaryExited,
			"total_active_effective_balance": stat.TotalActiveEffectiveBalance,
		}
	}

	data := gin.H{
		"results": results,
		"sort_by": sortBy,
		"order":   order,
	}

	c.HTML(http.StatusOK, "top-deposits-table.html", data)
}

func (s *Server) networkRewardsPageOrAPIHandler(c *gin.Context) {
	slog.Info("networkRewardsPageOrAPIHandler called",
		"path", c.Request.URL.Path,
		"method", c.Request.Method,
		"hx-request", c.GetHeader("HX-Request"),
		"accept", c.GetHeader("Accept"),
		"user-agent", c.GetHeader("User-Agent"))

	if !s.frontendEnabled {
		slog.Info("Frontend disabled, serving network rewards as JSON")
		s.networkRewardsHandler(c)
		return
	}

	// Check if this is an API request (Accept: application/json)
	accept := c.GetHeader("Accept")
	if accept != "" && strings.Contains(accept, "application/json") {
		slog.Info("Handling as JSON API request")
		s.networkRewardsHandler(c)
		return
	}

	// Check if this is an HTMX request - return JSON data
	if c.GetHeader("HX-Request") == "true" {
		slog.Info("Handling as HTMX request, returning JSON")
		s.networkRewardsHandler(c)
		return
	}

	// Otherwise, render the page
	if len(s.templates) == 0 {
		slog.Error("Templates not loaded")
		c.String(http.StatusInternalServerError, "Templates not loaded")
		return
	}

	if _, ok := s.templates["network-rewards.html"]; !ok {
		available := s.availableTemplateNames()
		slog.Error("Template not found", "name", "network-rewards.html", "available", available)
		c.String(http.StatusInternalServerError, "Template network-rewards.html not found. Available templates: "+available)
		return
	}

	slog.Info("Rendering network-rewards.html template", "path", c.Request.URL.Path)
	c.HTML(http.StatusOK, "network-rewards.html", gin.H{
		"CurrentPath": c.Request.URL.Path,
	})
}

func (s *Server) addressRewardsPageHandler(c *gin.Context) {
	slog.Info("addressRewardsPageHandler called",
		"path", c.Request.URL.Path,
		"method", c.Request.Method,
		"hx-request", c.GetHeader("HX-Request"),
		"accept", c.GetHeader("Accept"))

	if len(s.templates) == 0 {
		slog.Error("Templates not loaded")
		c.String(http.StatusInternalServerError, "Templates not loaded")
		return
	}

	if _, ok := s.templates["address-rewards.html"]; !ok {
		available := s.availableTemplateNames()
		slog.Error("Template not found", "name", "address-rewards.html", "available", available)
		c.String(http.StatusInternalServerError, "Template address-rewards.html not found. Available templates: "+available)
		return
	}

	slog.Info("Rendering address-rewards.html template", "path", c.Request.URL.Path)
	c.HTML(http.StatusOK, "address-rewards.html", gin.H{
		"CurrentPath": c.Request.URL.Path,
	})
}

func (s *Server) availableTemplateNames() string {
	if len(s.templates) == 0 {
		return ""
	}
	names := make([]string, 0, len(s.templates))
	for name := range s.templates {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}
