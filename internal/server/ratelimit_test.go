package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestIPRateLimiterBlocksBurstPerIP(t *testing.T) {
	gin.SetMode(gin.TestMode)

	limiter := newIPRateLimiter(1, 1)
	router := gin.New()
	router.Use(limiter.middleware())
	router.GET("/ping", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	makeRequest := func(remoteAddr string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/ping", nil)
		req.RemoteAddr = remoteAddr
		router.ServeHTTP(w, req)
		return w
	}

	if w := makeRequest("1.1.1.1:1234"); w.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want %d", w.Code, http.StatusOK)
	}

	if w := makeRequest("1.1.1.1:1234"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}

	if w := makeRequest("2.2.2.2:1234"); w.Code != http.StatusOK {
		t.Fatalf("request from different IP status = %d, want %d", w.Code, http.StatusOK)
	}

	time.Sleep(1100 * time.Millisecond)
	if w := makeRequest("1.1.1.1:1234"); w.Code != http.StatusOK {
		t.Fatalf("request after refill status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestIPRateLimiterDefaultBurst(t *testing.T) {
	limiter := newIPRateLimiter(5.1, 0)
	if got, want := limiter.Burst(), 6; got != want {
		t.Fatalf("default burst = %d, want %d", got, want)
	}
}
