package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nexus-gateway/nexus/internal/billing"
)

// billingContextKey is used to store billing info in request context.
type billingContextKey string

const billingKeyHashCtx billingContextKey = "billing_key_hash"
const billingUserIDCtx billingContextKey = "billing_user_id"

func (s *Server) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	sigHeader := r.Header.Get("Stripe-Signature")
	if err := billing.VerifyStripeSignature(body, sigHeader, s.cfg.Billing.StripeWebhookSecret); err != nil {
		s.logger.Warn("stripe webhook signature verification failed", "error", err)
		http.Error(w, "signature verification failed", http.StatusUnauthorized)
		return
	}

	var event billing.StripeEvent
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if err := s.webhookHandler.HandleEvent(event); err != nil {
		s.logger.Error("stripe webhook handling failed", "error", err, "event_type", event.Type)
		http.Error(w, "webhook processing failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleAdminSubscriptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	subs := s.subStore.ListAll()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(subs)
}

func (s *Server) handleAdminKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	userID := strings.TrimPrefix(r.URL.Path, "/api/admin/keys/")
	if userID == "" {
		http.Error(w, "user ID required", http.StatusBadRequest)
		return
	}
	keys := s.keyStore.ListByUser(userID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keys)
}

func (s *Server) handleAdminDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	userID := strings.TrimPrefix(r.URL.Path, "/api/admin/devices/")
	if userID == "" {
		http.Error(w, "user ID required", http.StatusBadRequest)
		return
	}
	devices := s.deviceTracker.ListByUser(userID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(devices)
}

func (s *Server) handleKeyGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		UserID         string   `json:"user_id"`
		SubscriptionID string   `json:"subscription_id"`
		Name           string   `json:"name"`
		Scopes         []string `json:"scopes"`
		IsTest         bool     `json:"is_test"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Use context user if available
	if ctxUser, ok := r.Context().Value(billingUserIDCtx).(string); ok && ctxUser != "" {
		// Authenticated user: enforce ownership — callers cannot forge
		// key generation for a different user.
		if req.UserID == "" {
			req.UserID = ctxUser
		} else if req.UserID != ctxUser {
			http.Error(w, `{"error":"cannot generate keys for another user"}`, http.StatusForbidden)
			return
		}
	}

	if req.UserID == "" || req.SubscriptionID == "" {
		http.Error(w, "user_id and subscription_id required", http.StatusBadRequest)
		return
	}

	key, rawKey, err := s.keyStore.GenerateKey(req.UserID, req.SubscriptionID, req.Name, req.Scopes, req.IsTest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"key":      rawKey,
		"key_hash": key.KeyHash,
		"name":     key.Name,
		"scopes":   key.Scopes,
		"message":  "Save this key — it will not be shown again.",
	})
}

func (s *Server) handleKeyRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		KeyHash string `json:"key_hash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if req.KeyHash == "" {
		http.Error(w, "key_hash required", http.StatusBadRequest)
		return
	}

	// Verify ownership
	key, found := s.keyStore.GetByHash(req.KeyHash)
	if !found {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}

	if ctxUser, ok := r.Context().Value(billingUserIDCtx).(string); ok {
		if key.UserID != ctxUser {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	if err := s.keyStore.RevokeKey(req.KeyHash); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "revoked"})
}

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}

	keyHash, _ := r.Context().Value(billingKeyHashCtx).(string)
	if keyHash == "" {
		http.Error(w, "API key required", http.StatusUnauthorized)
		return
	}

	key, found := s.keyStore.GetByHash(keyHash)
	if !found {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}

	quota := s.keyStore.CheckQuota(keyHash)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"key_prefix":      key.KeyPrefix,
		"request_count":   key.RequestCount,
		"monthly_usage":   key.MonthlyUsage,
		"monthly_reset":   key.MonthlyReset.Format(time.RFC3339),
		"quota_allowed":   quota.Allowed,
		"quota_remaining": quota.Remaining,
	})
}
