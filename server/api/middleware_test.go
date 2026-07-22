package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestAuthMiddlewareValidToken(t *testing.T) {
	rl := NewRateLimiter(60, 10)
	handler := AuthMiddleware("test-key", rl, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAuthMiddlewareInvalidToken(t *testing.T) {
	rl := NewRateLimiter(60, 10)
	handler := AuthMiddleware("test-key", rl, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddlewareMissingHeader(t *testing.T) {
	rl := NewRateLimiter(60, 10)
	handler := AuthMiddleware("test-key", rl, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestRateLimiterAllowsWithinLimit(t *testing.T) {
	rl := NewRateLimiter(5, 1)
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i, rec.Code)
		}
	}
}

func TestRateLimiterBlocksOverLimit(t *testing.T) {
	rl := NewRateLimiter(2, 100)
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if i < 2 && rec.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i, rec.Code)
		}
		if i == 2 && rec.Code != http.StatusTooManyRequests {
			t.Errorf("request %d: expected 429, got %d", i, rec.Code)
		}
	}
}

func TestClientIPUsesRemoteAddrByDefault(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:5555"
	req.Header.Set("X-Forwarded-For", "203.0.113.50")
	os.Unsetenv("TRUST_PROXY")
	if got := clientIP(req); got != "10.0.0.1" {
		t.Errorf("expected RemoteAddr IP 10.0.0.1, got %s", got)
	}
}

func TestClientIPUsesXForwardedForWhenTrusted(t *testing.T) {
	os.Setenv("TRUST_PROXY", "true")
	defer os.Unsetenv("TRUST_PROXY")
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:5555"
	req.Header.Set("X-Forwarded-For", "203.0.113.50, 10.0.0.1")
	if got := clientIP(req); got != "203.0.113.50" {
		t.Errorf("expected first XFF IP 203.0.113.50, got %s", got)
	}
}

func TestClientIPFallsBackWhenXFFEmpty(t *testing.T) {
	os.Setenv("TRUST_PROXY", "true")
	defer os.Unsetenv("TRUST_PROXY")
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:5555"
	if got := clientIP(req); got != "10.0.0.1" {
		t.Errorf("expected RemoteAddr fallback 10.0.0.1, got %s", got)
	}
}

func TestRateLimiterSeparateBucketsPerIP(t *testing.T) {
	rl := NewRateLimiter(2, 100)
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.RemoteAddr = "1.1.1.1:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if i < 2 && rec.Code != http.StatusOK {
			t.Errorf("ip1 request %d: expected 200, got %d", i, rec.Code)
		}
	}
	// Different IP should have its own bucket
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "2.2.2.2:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("ip2 request: expected 200, got %d", rec.Code)
	}
}
