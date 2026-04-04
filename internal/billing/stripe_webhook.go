package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"
)

// StripeWebhookHandler processes Stripe webhook events.
type StripeWebhookHandler struct {
	subStore  *SubscriptionStore
	keyStore  *APIKeyStore
	logger    *slog.Logger
	secret    string
}

// NewStripeWebhookHandler creates a new webhook handler.
func NewStripeWebhookHandler(subStore *SubscriptionStore, keyStore *APIKeyStore, secret string, logger *slog.Logger) *StripeWebhookHandler {
	return &StripeWebhookHandler{
		subStore: subStore,
		keyStore: keyStore,
		logger:   logger,
		secret:   secret,
	}
}

// StripeEvent represents a parsed Stripe webhook event.
type StripeEvent struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Data    json.RawMessage `json:"data"`
	Created int64           `json:"created"`
}

// StripeEventData wraps the event data object.
type StripeEventData struct {
	Object json.RawMessage `json:"object"`
}

// StripeSubscription represents relevant fields from a Stripe subscription object.
type StripeSubscription struct {
	ID                 string `json:"id"`
	Customer           string `json:"customer"`
	Status             string `json:"status"`
	CurrentPeriodStart int64  `json:"current_period_start"`
	CurrentPeriodEnd   int64  `json:"current_period_end"`
	CanceledAt         *int64 `json:"canceled_at"`
	Plan               *struct {
		ID string `json:"id"`
	} `json:"plan"`
	Items *struct {
		Data []struct {
			Price struct {
				ID string `json:"id"`
			} `json:"price"`
		} `json:"data"`
	} `json:"items"`
	Metadata map[string]string `json:"metadata"`
}

// StripeInvoice represents relevant fields from a Stripe invoice object.
type StripeInvoice struct {
	ID           string `json:"id"`
	Customer     string `json:"customer"`
	Subscription string `json:"subscription"`
	Status       string `json:"status"`
	Paid         bool   `json:"paid"`
}

// StripeCheckoutSession represents a Stripe checkout session.
type StripeCheckoutSession struct {
	ID           string `json:"id"`
	Customer     string `json:"customer"`
	Subscription string `json:"subscription"`
	CustomerEmail string `json:"customer_email"`
	Metadata     map[string]string `json:"metadata"`
}

// VerifyStripeSignature verifies a Stripe webhook signature.
// Stripe uses HMAC-SHA256 with a versioned signature scheme (v1).
func VerifyStripeSignature(payload []byte, sigHeader, secret string) error {
	if sigHeader == "" {
		return fmt.Errorf("missing Stripe-Signature header")
	}
	if secret == "" {
		return fmt.Errorf("webhook secret not configured")
	}

	parts := strings.Split(sigHeader, ",")
	var timestamp string
	var signatures []string

	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			timestamp = kv[1]
		case "v1":
			signatures = append(signatures, kv[1])
		}
	}

	if timestamp == "" {
		return fmt.Errorf("missing timestamp in signature")
	}
	if len(signatures) == 0 {
		return fmt.Errorf("missing v1 signature")
	}

	// Check timestamp tolerance (5 minutes)
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}
	if math.Abs(float64(time.Now().Unix()-ts)) > 300 {
		return fmt.Errorf("timestamp too old")
	}

	// Compute expected signature
	signedPayload := timestamp + "." + string(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signedPayload))
	expected := hex.EncodeToString(mac.Sum(nil))

	for _, sig := range signatures {
		if hmac.Equal([]byte(sig), []byte(expected)) {
			return nil
		}
	}

	return fmt.Errorf("signature verification failed")
}

// HandleEvent processes a parsed Stripe event.
func (h *StripeWebhookHandler) HandleEvent(event StripeEvent) error {
	h.logger.Info("processing stripe event", "type", event.Type, "id", event.ID)

	var eventData StripeEventData
	if err := json.Unmarshal(event.Data, &eventData); err != nil {
		return fmt.Errorf("failed to parse event data: %w", err)
	}

	switch event.Type {
	case "customer.subscription.created":
		return h.handleSubscriptionCreated(eventData.Object)
	case "customer.subscription.updated":
		return h.handleSubscriptionUpdated(eventData.Object)
	case "customer.subscription.deleted":
		return h.handleSubscriptionDeleted(eventData.Object)
	case "invoice.paid":
		return h.handleInvoicePaid(eventData.Object)
	case "invoice.payment_failed":
		return h.handleInvoicePaymentFailed(eventData.Object)
	case "checkout.session.completed":
		return h.handleCheckoutCompleted(eventData.Object)
	default:
		h.logger.Info("unhandled stripe event type", "type", event.Type)
		return nil
	}
}

func (h *StripeWebhookHandler) handleSubscriptionCreated(data json.RawMessage) error {
	var stripeSub StripeSubscription
	if err := json.Unmarshal(data, &stripeSub); err != nil {
		return err
	}

	// Determine plan from metadata or default
	planID := "free"
	if stripeSub.Metadata != nil {
		if p, ok := stripeSub.Metadata["plan_id"]; ok {
			planID = p
		}
	}

	userID := stripeSub.Customer
	if stripeSub.Metadata != nil {
		if uid, ok := stripeSub.Metadata["user_id"]; ok {
			userID = uid
		}
	}

	email := ""
	if stripeSub.Metadata != nil {
		if e, ok := stripeSub.Metadata["email"]; ok {
			email = e
		}
	}

	sub := &Subscription{
		ID:                 fmt.Sprintf("sub_%s", stripeSub.ID),
		UserID:             userID,
		Email:              email,
		PlanID:             planID,
		Status:             mapStripeStatus(stripeSub.Status),
		StripeCustomerID:   stripeSub.Customer,
		StripeSubID:        stripeSub.ID,
		CurrentPeriodStart: time.Unix(stripeSub.CurrentPeriodStart, 0),
		CurrentPeriodEnd:   time.Unix(stripeSub.CurrentPeriodEnd, 0),
	}

	if stripeSub.CanceledAt != nil {
		t := time.Unix(*stripeSub.CanceledAt, 0)
		sub.CanceledAt = &t
	}

	return h.subStore.Create(sub)
}

func (h *StripeWebhookHandler) handleSubscriptionUpdated(data json.RawMessage) error {
	var stripeSub StripeSubscription
	if err := json.Unmarshal(data, &stripeSub); err != nil {
		return err
	}

	sub, found := h.subStore.GetByStripeSubID(stripeSub.ID)
	if !found {
		h.logger.Warn("subscription not found for update", "stripe_sub_id", stripeSub.ID)
		return nil
	}

	sub.Status = mapStripeStatus(stripeSub.Status)
	sub.CurrentPeriodStart = time.Unix(stripeSub.CurrentPeriodStart, 0)
	sub.CurrentPeriodEnd = time.Unix(stripeSub.CurrentPeriodEnd, 0)

	if stripeSub.CanceledAt != nil {
		t := time.Unix(*stripeSub.CanceledAt, 0)
		sub.CanceledAt = &t
	}

	if stripeSub.Metadata != nil {
		if p, ok := stripeSub.Metadata["plan_id"]; ok {
			sub.PlanID = p
		}
	}

	return h.subStore.Update(sub)
}

func (h *StripeWebhookHandler) handleSubscriptionDeleted(data json.RawMessage) error {
	var stripeSub StripeSubscription
	if err := json.Unmarshal(data, &stripeSub); err != nil {
		return err
	}

	sub, found := h.subStore.GetByStripeSubID(stripeSub.ID)
	if !found {
		h.logger.Warn("subscription not found for deletion", "stripe_sub_id", stripeSub.ID)
		return nil
	}

	sub.Status = "expired"
	now := time.Now()
	sub.CanceledAt = &now

	if err := h.subStore.Update(sub); err != nil {
		return err
	}

	// Revoke all API keys for this subscription
	return h.keyStore.RevokeBySubscription(sub.ID)
}

func (h *StripeWebhookHandler) handleInvoicePaid(data json.RawMessage) error {
	var invoice StripeInvoice
	if err := json.Unmarshal(data, &invoice); err != nil {
		return err
	}

	if invoice.Subscription == "" {
		return nil
	}

	sub, found := h.subStore.GetByStripeSubID(invoice.Subscription)
	if !found {
		h.logger.Warn("subscription not found for invoice", "stripe_sub_id", invoice.Subscription)
		return nil
	}

	sub.Status = "active"
	if err := h.subStore.Update(sub); err != nil {
		return err
	}

	// Reset monthly usage on payment
	h.keyStore.ResetMonthlyUsageBySubscription(sub.ID)
	return nil
}

func (h *StripeWebhookHandler) handleInvoicePaymentFailed(data json.RawMessage) error {
	var invoice StripeInvoice
	if err := json.Unmarshal(data, &invoice); err != nil {
		return err
	}

	if invoice.Subscription == "" {
		return nil
	}

	sub, found := h.subStore.GetByStripeSubID(invoice.Subscription)
	if !found {
		return nil
	}

	sub.Status = "past_due"
	return h.subStore.Update(sub)
}

func (h *StripeWebhookHandler) handleCheckoutCompleted(data json.RawMessage) error {
	var session StripeCheckoutSession
	if err := json.Unmarshal(data, &session); err != nil {
		return err
	}

	if session.Subscription == "" {
		return nil
	}

	// Update existing subscription with customer email if available
	sub, found := h.subStore.GetByStripeSubID(session.Subscription)
	if found && session.CustomerEmail != "" {
		sub.Email = session.CustomerEmail
		return h.subStore.Update(sub)
	}

	return nil
}

func mapStripeStatus(s string) string {
	switch s {
	case "active":
		return "active"
	case "past_due":
		return "past_due"
	case "canceled":
		return "canceled"
	case "unpaid":
		return "past_due"
	case "trialing":
		return "trialing"
	default:
		return "active"
	}
}
