package observability

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// HealthCheck represents a health check function
type HealthCheck func(ctx context.Context) error

// HealthChecker manages health checks for dependencies
type HealthChecker struct {
	checks map[string]HealthCheck
	mu     sync.RWMutex
}

// HealthStatus represents the health status of the service
type HealthStatus struct {
	Status    string            `json:"status"`
	Timestamp time.Time         `json:"timestamp"`
	Checks    map[string]string `json:"checks,omitempty"`
}

// NewHealthChecker creates a new health checker
func NewHealthChecker() *HealthChecker {
	return &HealthChecker{
		checks: make(map[string]HealthCheck),
	}
}

// AddCheck adds a health check
func (h *HealthChecker) AddCheck(name string, check HealthCheck) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checks[name] = check
}

// CheckHealth runs all health checks
func (h *HealthChecker) CheckHealth(ctx context.Context) HealthStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()

	status := HealthStatus{
		Status:    "healthy",
		Timestamp: time.Now(),
		Checks:    make(map[string]string),
	}

	for name, check := range h.checks {
		if err := check(ctx); err != nil {
			status.Status = "unhealthy"
			status.Checks[name] = "error: " + err.Error()
		} else {
			status.Checks[name] = "ok"
		}
	}

	return status
}

// LivenessHandler returns HTTP handler for liveness probe
// Liveness: Is the service running?
func (h *HealthChecker) LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "alive",
			"timestamp": time.Now(),
		})
	}
}

// ReadinessHandler returns HTTP handler for readiness probe
// Readiness: Is the service ready to accept traffic?
func (h *HealthChecker) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		status := h.CheckHealth(ctx)

		w.Header().Set("Content-Type", "application/json")
		if status.Status == "healthy" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		json.NewEncoder(w).Encode(status)
	}
}

// HealthHandler returns HTTP handler for detailed health check
func (h *HealthChecker) HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		status := h.CheckHealth(ctx)

		w.Header().Set("Content-Type", "application/json")
		if status.Status == "healthy" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		json.NewEncoder(w).Encode(status)
	}
}
