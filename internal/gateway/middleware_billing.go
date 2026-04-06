package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nexus-gateway/nexus/internal/security"
)

// billingAuthMiddleware validates API keys on protected routes.
func (s *Server) billingAuthMiddleware() security.Middleware {
	skipPaths := map[string]bool{
		"/health":          true,
		"/health/live":     true,
		"/health/ready":    true,
		"/metrics":         true,
		"/dashboard":       true,
		"/":                true,
		"/webhooks/stripe": true,
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			if skipPaths[path] || strings.HasPrefix(path, "/dashboard/") {
				next.ServeHTTP(w, r)
				return
			}

			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer nxs_") {
				// No billing key — block admin endpoints instead
				// of letting unauthenticated requests through.
				if strings.HasPrefix(path, "/api/admin/") || strings.HasPrefix(path, "/api/keys/") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnauthorized)
					json.NewEncoder(w).Encode(map[string]string{"error": "API key required"})
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			rawKey := strings.TrimPrefix(auth, "Bearer ")
			apiKey, err := s.keyStore.ValidateKey(rawKey)
			if err != nil {
				if strings.Contains(err.Error(), "subscription") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusPaymentRequired)
					json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid API key"})
				return
			}

			// Check scope for admin endpoints
			if strings.HasPrefix(path, "/api/admin/") || strings.HasPrefix(path, "/api/keys/") {
				hasAdmin := false
				for _, scope := range apiKey.Scopes {
					if scope == "admin" || scope == "*" {
						hasAdmin = true
						break
					}
				}
				if !hasAdmin {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					json.NewEncoder(w).Encode(map[string]string{"error": "admin scope required"})
					return
				}
			}

			// Check quota
			quota := s.keyStore.CheckQuota(apiKey.KeyHash)
			if !quota.Allowed {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", quota.ResetAt.Format(time.RFC3339))
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]string{
					"error": "monthly quota exceeded",
					"reset": quota.ResetAt.Format(time.RFC3339),
				})
				return
			}

			// Check device limit
			sub, found := s.subStore.Get(apiKey.SubscriptionID)
			if found {
				plan, planFound := s.subStore.GetPlan(sub.PlanID)
				if planFound && plan.MaxDevices > 0 {
					s.deviceTracker.RecordDevice(apiKey.UserID, r)
					devResult := s.deviceTracker.CheckDeviceLimit(apiKey.UserID, plan.MaxDevices)
					if !devResult.Allowed {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusForbidden)
						json.NewEncoder(w).Encode(map[string]string{
							"error": fmt.Sprintf("Device limit reached. Your plan allows %d devices. Upgrade or remove inactive devices.", devResult.Max),
						})
						return
					}
				} else if planFound {
					s.deviceTracker.RecordDevice(apiKey.UserID, r)
				}
			}

			// Record usage
			s.keyStore.RecordUsage(apiKey.KeyHash)

			// Store key info in context
			ctx := context.WithValue(r.Context(), billingKeyHashCtx, apiKey.KeyHash)
			ctx = context.WithValue(ctx, billingUserIDCtx, apiKey.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
