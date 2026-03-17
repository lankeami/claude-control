package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

func AuthMiddleware(apiKey string, rl *RateLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip == "" {
			ip = r.RemoteAddr
		}

		// Check if IP is locked out from too many failed auth attempts
		if rl.IsLockedOut(ip) {
			http.Error(w, `{"error":"too many failed attempts, try again later"}`, http.StatusTooManyRequests)
			return
		}

		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			rl.RecordFailedAuth(ip)
			http.Error(w, `{"error":"missing authorization"}`, http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		if token != apiKey {
			rl.RecordFailedAuth(ip)
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type RateLimiter struct {
	mu             sync.Mutex
	requests       map[string][]time.Time
	failedAuths    map[string][]time.Time
	maxPerMinute   int
	maxFailedAuths int
	lockoutMinutes int
}

func NewRateLimiter(maxPerMinute, maxFailedAuths int) *RateLimiter {
	return &RateLimiter{
		requests:       make(map[string][]time.Time),
		failedAuths:    make(map[string][]time.Time),
		maxPerMinute:   maxPerMinute,
		maxFailedAuths: maxFailedAuths,
		lockoutMinutes: 15,
	}
}

func (rl *RateLimiter) RecordFailedAuth(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.failedAuths[ip] = append(rl.failedAuths[ip], time.Now())
}

func (rl *RateLimiter) IsLockedOut(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-time.Duration(rl.lockoutMinutes) * time.Minute)
	var recent []time.Time
	for _, t := range rl.failedAuths[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	rl.failedAuths[ip] = recent
	return len(recent) >= rl.maxFailedAuths
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip == "" {
			ip = r.RemoteAddr
		}

		rl.mu.Lock()
		now := time.Now()
		cutoff := now.Add(-1 * time.Minute)

		// Clean old entries
		var recent []time.Time
		for _, t := range rl.requests[ip] {
			if t.After(cutoff) {
				recent = append(recent, t)
			}
		}

		if len(recent) >= rl.maxPerMinute {
			rl.mu.Unlock()
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}

		rl.requests[ip] = append(recent, now)
		rl.mu.Unlock()

		next.ServeHTTP(w, r)
	})
}
