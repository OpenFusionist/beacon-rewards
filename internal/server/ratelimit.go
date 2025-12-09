package server

import (
	"math"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// ipRateLimiter enforces a token bucket limiter per client IP.
type ipRateLimiter struct {
	rate     rate.Limit
	burst    int
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
}

func newIPRateLimiter(rps float64, burst int) *ipRateLimiter {
	if burst <= 0 {
		burst = max(int(math.Ceil(rps)), 1)
	}

	return &ipRateLimiter{
		rate:     rate.Limit(rps),
		burst:    burst,
		limiters: make(map[string]*rate.Limiter),
	}
}

func (l *ipRateLimiter) Burst() int {
	return l.burst
}

func (l *ipRateLimiter) getLimiter(ip string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()

	limiter, ok := l.limiters[ip]
	if !ok {
		limiter = rate.NewLimiter(l.rate, l.burst)
		l.limiters[ip] = limiter
	}
	return limiter
}

func (l *ipRateLimiter) allow(ip string) bool {
	if ip == "" {
		ip = "unknown"
	}
	return l.getLimiter(ip).Allow()
}

func (l *ipRateLimiter) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !l.allow(c.ClientIP()) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
			})
			return
		}
		c.Next()
	}
}
