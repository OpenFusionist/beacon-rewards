package server

import (
	"beacon-rewards/internal/config"
	"beacon-rewards/internal/dora"
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestEnsureDoraDB(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig()
	s := &Server{config: cfg}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)

	if ok := s.ensureDoraDB(c); ok {
		t.Fatalf("ensureDoraDB should return false when db is nil")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	// With a non-nil db pointer, it should just return true without writing.
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)

	s.doraDB = &dora.DB{}
	if ok := s.ensureDoraDB(c); !ok {
		t.Fatalf("expected ensureDoraDB to return true when db is set")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected no error response, got status %d", w.Code)
	}
}

func TestLimitParam(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig()
	cfg.DefaultAPILimit = 50
	s := &Server{config: cfg}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/?limit=25", nil)
	if limit := s.limitParam(c); limit != 25 {
		t.Fatalf("limitParam with query = %d, want 25", limit)
	}

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/?limit=abc", nil)
	s.config.DefaultAPILimit = 0
	if limit := s.limitParam(c); limit != 100 {
		t.Fatalf("limitParam fallback = %d, want 100", limit)
	}
}

func TestRequestContextDefaultTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig()
	cfg.RequestTimeout = -1
	s := &Server{config: cfg}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)

	ctx, cancel := s.requestContext(c)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatalf("expected deadline to be set")
	}
	remaining := time.Until(deadline)
	if remaining < 9*time.Second || remaining > 11*time.Second {
		t.Fatalf("unexpected timeout fallback: %v remaining", remaining)
	}
}

func TestRespondWithTop(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig()
	cfg.DefaultAPILimit = 5
	cfg.RequestTimeout = 2 * time.Second
	s := &Server{config: cfg}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/?limit=3&sort_by=validators_total&order=asc", nil)

	called := false
	s.respondWithTop(c, func(ctx context.Context, limit int, sortBy, order string) (any, error) {
		called = true
		if limit != 3 || sortBy != "validators_total" || order != "asc" {
			t.Fatalf("unexpected args: limit=%d sortBy=%s order=%s", limit, sortBy, order)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatalf("expected deadline to be set")
		}
		return []string{"ok"}, nil
	})

	if !called {
		t.Fatalf("fetch function was not called")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp["limit"].(float64) != 3 {
		t.Fatalf("response limit = %v, want 3", resp["limit"])
	}
	if resp["sort_by"] != "validators_total" || resp["order"] != "asc" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	// Error path
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	s.respondWithTop(c, func(context.Context, int, string, string) (any, error) {
		return nil, errors.New("boom")
	})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("error status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestLoadAndApplyDepositorLabels(t *testing.T) {
	content := `
0xABCDEFabcdefABCDEFabcdefABCDEFabcdefAB: Label One
0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef: Label Two
"": should-be-ignored
`
	file := t.TempDir() + "/labels.yaml"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write labels file: %v", err)
	}

	labels, err := loadDepositorLabels(file)
	if err != nil {
		t.Fatalf("loadDepositorLabels returned error: %v", err)
	}
	if len(labels) != 2 {
		t.Fatalf("expected 2 labels, got %d", len(labels))
	}
	if labels["0xabcdefabcdefabcdefabcdefabcdefabcdefab"] != "Label One" {
		t.Fatalf("label normalization failed: %+v", labels)
	}

	stats := []dora.DepositorStat{
		{DepositorAddress: "0xABCDEFabcdefABCDEFabcdefABCDEFabcdefAB"},
		{DepositorAddress: "0x123"},
	}
	s := &Server{depositorLabels: labels}
	s.applyDepositorLabels(stats)
	if stats[0].DepositorLabel != "Label One" {
		t.Fatalf("expected label to be applied, got %q", stats[0].DepositorLabel)
	}
	if stats[1].DepositorLabel != "" {
		t.Fatalf("unexpected label applied: %q", stats[1].DepositorLabel)
	}

	if _, ok := s.lookupDepositorLabel(""); ok {
		t.Fatalf("empty address should not match a label")
	}
}

func TestAvailableTemplateNames(t *testing.T) {
	s := &Server{
		templates: map[string]*template.Template{
			"b.html": nil,
			"a.html": nil,
			"c.html": nil,
		},
	}

	if got, want := s.availableTemplateNames(), "a.html,b.html,c.html"; got != want {
		t.Fatalf("availableTemplateNames = %s, want %s", got, want)
	}
}

func TestHTMLRenderer(t *testing.T) {
	tmpl := template.Must(template.New("hello.html").Parse("Hello {{.Name}}"))
	renderer := &HTMLRenderer{
		templates: map[string]*template.Template{
			"hello.html": tmpl,
		},
	}

	w := httptest.NewRecorder()
	r := renderer.Instance("hello.html", map[string]string{"Name": "world"})
	if err := r.Render(w); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if body := w.Body.String(); body != "Hello world" {
		t.Fatalf("unexpected render output: %q", body)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("content type = %s, want text/html; charset=utf-8", ct)
	}

	// Missing template returns 500 and error message.
	w = httptest.NewRecorder()
	r = renderer.Instance("missing.html", nil)
	if err := r.Render(w); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("missing template status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if body := w.Body.String(); body == "" {
		t.Fatalf("expected error message for missing template")
	}
}
