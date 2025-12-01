package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

type HealthHandler struct {
	logger *zap.SugaredLogger
}

func NewHealthHandler(logger *zap.SugaredLogger) *HealthHandler {
	return &HealthHandler{
		logger: logger,
	}
}

type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Service string `json:"service"`
}

func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	response := HealthResponse{
		Status:  "healthy",
		Version: "1.0.0",
		Service: "goshort",
	}

	respondJSON(w, response, http.StatusOK)
}

func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	// TODO: Add actual readiness checks (DB, Redis, etc.)
	response := HealthResponse{
		Status:  "ready",
		Version: "1.0.0",
		Service: "goshort",
	}

	respondJSON(w, response, http.StatusOK)
}

// Helper functions for all handlers
func respondJSON(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		// Log encoding error but response is already sent
		return
	}
}

func respondError(w http.ResponseWriter, message string, status int) {
	respondJSON(w, map[string]string{"error": message}, status)
}

// getClientIP extracts the real client IP from request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// Take first IP from list
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			ip := strings.TrimSpace(ips[0])
			// Validate IP format
			if ip != "" && !strings.Contains(ip, ":") {
				return ip
			}
		}
	}

	// Check X-Real-IP header
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fallback to RemoteAddr
	ip := r.RemoteAddr
	// Remove port if present
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	// Remove brackets from IPv6
	ip = strings.TrimPrefix(ip, "[")
	ip = strings.TrimSuffix(ip, "]")

	return ip
}

