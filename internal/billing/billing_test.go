package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func testDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("nexus_billing_test_%d", time.Now().UnixNano()))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// --- API Key Tests ---

func TestGenerateKeyFormat(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	subStore := NewSubscriptionStore(dir, logger)
	store := NewAPIKeyStore(dir, subStore, logger)

	key, rawKey, err := store.GenerateKey("user1", "sub1", "test key", nil, false)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(rawKey, "nxs_live_") {
		t.Errorf("live key should start with nxs_live_, got %s", rawKey[:12])
	}
	// nxs_live_ (9 chars) + 32 hex chars = 41 total
	if len(rawKey) != 41 {
		t.Errorf("key length should be 41, got %d", len(rawKey))
	}
	if key.KeyPrefix != rawKey[:12] {
		t.Errorf("key prefix mismatch: %s vs %s", key.KeyPrefix, rawKey[:12])
	}
}

func TestGenerateTestKeyFormat(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	subStore := NewSubscriptionStore(dir, logger)
	store := NewAPIKeyStore(dir, subStore, logger)

	_, rawKey, err := store.GenerateKey("user1", "sub1", "test", nil, true)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(rawKey, "nxs_test_") {
		t.Errorf("test key should start with nxs_test_, got %s", rawKey[:12])
	}
	if len(rawKey) != 41 {
		t.Errorf("key length should be 41, got %d", len(rawKey))
	}
}

func TestValidateKey(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	subStore := NewSubscriptionStore(dir, logger)

	// Create a subscription first
	sub := &Subscription{
		ID:     "sub1",
		UserID: "user1",
		PlanID: "starter",
		Status: "active",
		CurrentPeriodEnd: time.Now().Add(30 * 24 * time.Hour),
	}
	if err := subStore.Create(sub); err != nil {
		t.Fatal(err)
	}

	store := NewAPIKeyStore(dir, subStore, logger)
	_, rawKey, err := store.GenerateKey("user1", "sub1", "key", nil, false)
	if err != nil {
		t.Fatal(err)
	}

	// Valid key
	validKey, err := store.ValidateKey(rawKey)
	if err != nil {
		t.Fatalf("expected valid key, got error: %v", err)
	}
	if validKey.UserID != "user1" {
		t.Errorf("expected user1, got %s", validKey.UserID)
	}

	// Invalid key
	_, err = store.ValidateKey("nxs_live_0000000000000000000000000000dead")
	if err == nil {
		t.Error("expected error for invalid key")
	}
}

func TestValidateRevokedKey(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	subStore := NewSubscriptionStore(dir, logger)
	sub := &Subscription{ID: "sub1", UserID: "user1", PlanID: "free", Status: "active", CurrentPeriodEnd: time.Now().Add(30 * 24 * time.Hour)}
	subStore.Create(sub)

	store := NewAPIKeyStore(dir, subStore, logger)
	key, rawKey, _ := store.GenerateKey("user1", "sub1", "key", nil, false)

	store.RevokeKey(key.KeyHash)

	_, err := store.ValidateKey(rawKey)
	if err == nil {
		t.Error("expected error for revoked key")
	}
	if !strings.Contains(err.Error(), "revoked") {
		t.Errorf("expected 'revoked' in error, got: %s", err.Error())
	}
}

func TestValidateExpiredKey(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	subStore := NewSubscriptionStore(dir, logger)
	sub := &Subscription{ID: "sub1", UserID: "user1", PlanID: "free", Status: "active", CurrentPeriodEnd: time.Now().Add(30 * 24 * time.Hour)}
	subStore.Create(sub)

	store := NewAPIKeyStore(dir, subStore, logger)
	key, rawKey, _ := store.GenerateKey("user1", "sub1", "key", nil, false)

	// Set expiry in the past
	past := time.Now().Add(-1 * time.Hour)
	store.mu.Lock()
	store.keys[key.KeyHash].ExpiresAt = &past
	store.mu.Unlock()

	_, err := store.ValidateKey(rawKey)
	if err == nil {
		t.Error("expected error for expired key")
	}
}

func TestQuotaEnforcementAllow(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	subStore := NewSubscriptionStore(dir, logger)
	sub := &Subscription{ID: "sub1", UserID: "user1", PlanID: "free", Status: "active", CurrentPeriodEnd: time.Now().Add(30 * 24 * time.Hour)}
	subStore.Create(sub)

	store := NewAPIKeyStore(dir, subStore, logger)
	key, _, _ := store.GenerateKey("user1", "sub1", "key", nil, false)

	result := store.CheckQuota(key.KeyHash)
	if !result.Allowed {
		t.Error("expected allowed under quota")
	}
	if result.Remaining != 1000 { // free plan
		t.Errorf("expected 1000 remaining, got %d", result.Remaining)
	}
}

func TestQuotaEnforcementDeny(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	subStore := NewSubscriptionStore(dir, logger)
	sub := &Subscription{ID: "sub1", UserID: "user1", PlanID: "free", Status: "active", CurrentPeriodEnd: time.Now().Add(30 * 24 * time.Hour)}
	subStore.Create(sub)

	store := NewAPIKeyStore(dir, subStore, logger)
	key, _, _ := store.GenerateKey("user1", "sub1", "key", nil, false)

	// Exhaust the quota
	store.mu.Lock()
	store.keys[key.KeyHash].MonthlyUsage = 1000
	store.mu.Unlock()

	result := store.CheckQuota(key.KeyHash)
	if result.Allowed {
		t.Error("expected denied when quota exhausted")
	}
	if result.Remaining != 0 {
		t.Errorf("expected 0 remaining, got %d", result.Remaining)
	}
}

func TestMonthlyUsageReset(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	subStore := NewSubscriptionStore(dir, logger)
	store := NewAPIKeyStore(dir, subStore, logger)

	key, _, _ := store.GenerateKey("user1", "sub1", "key", nil, false)

	// Record some usage
	for i := 0; i < 10; i++ {
		store.RecordUsage(key.KeyHash)
	}

	store.mu.RLock()
	usage := store.keys[key.KeyHash].MonthlyUsage
	store.mu.RUnlock()
	if usage != 10 {
		t.Errorf("expected 10 monthly usage, got %d", usage)
	}

	store.ResetMonthlyUsage()

	store.mu.RLock()
	usage = store.keys[key.KeyHash].MonthlyUsage
	store.mu.RUnlock()
	if usage != 0 {
		t.Errorf("expected 0 after reset, got %d", usage)
	}
}

func TestListByUser(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	subStore := NewSubscriptionStore(dir, logger)
	store := NewAPIKeyStore(dir, subStore, logger)

	store.GenerateKey("user1", "sub1", "key1", nil, false)
	store.GenerateKey("user1", "sub1", "key2", nil, false)
	store.GenerateKey("user2", "sub2", "key3", nil, false)

	keys := store.ListByUser("user1")
	if len(keys) != 2 {
		t.Errorf("expected 2 keys for user1, got %d", len(keys))
	}
}

func TestRevokeBySubscription(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	subStore := NewSubscriptionStore(dir, logger)
	sub := &Subscription{ID: "sub1", UserID: "user1", PlanID: "free", Status: "active", CurrentPeriodEnd: time.Now().Add(30 * 24 * time.Hour)}
	subStore.Create(sub)

	store := NewAPIKeyStore(dir, subStore, logger)
	_, rawKey1, _ := store.GenerateKey("user1", "sub1", "key1", nil, false)
	_, rawKey2, _ := store.GenerateKey("user1", "sub1", "key2", nil, false)

	store.RevokeBySubscription("sub1")

	_, err1 := store.ValidateKey(rawKey1)
	_, err2 := store.ValidateKey(rawKey2)
	if err1 == nil || err2 == nil {
		t.Error("expected both keys revoked")
	}
}

// --- Device Tracker Tests ---

func TestDeviceFingerprint(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	dt := NewDeviceTracker(dir, logger)

	req1 := &http.Request{
		Header:     http.Header{"User-Agent": {"Mozilla/5.0"}},
		RemoteAddr: "192.168.1.100:1234",
	}
	req2 := &http.Request{
		Header:     http.Header{"User-Agent": {"Mozilla/5.0"}},
		RemoteAddr: "192.168.1.200:5678", // same /24, different host
	}

	id1 := dt.RecordDevice("user1", req1)
	id2 := dt.RecordDevice("user1", req2)

	// Same UA + same first 3 octets → same device ID
	if id1 != id2 {
		t.Errorf("same UA + same /24 should produce same device ID: %s vs %s", id1, id2)
	}
}

func TestDeviceFingerprintDifferent(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	dt := NewDeviceTracker(dir, logger)

	req1 := &http.Request{
		Header:     http.Header{"User-Agent": {"Mozilla/5.0"}},
		RemoteAddr: "192.168.1.100:1234",
	}
	req2 := &http.Request{
		Header:     http.Header{"User-Agent": {"Chrome/120"}},
		RemoteAddr: "10.0.0.50:5678",
	}

	id1 := dt.RecordDevice("user1", req1)
	id2 := dt.RecordDevice("user1", req2)

	if id1 == id2 {
		t.Error("different UA + different network should produce different device IDs")
	}
}

func TestDeviceLimitEnforcement(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	dt := NewDeviceTracker(dir, logger)

	// Record 3 different devices
	for i := 0; i < 3; i++ {
		req := &http.Request{
			Header:     http.Header{"User-Agent": {fmt.Sprintf("Agent-%d", i)}},
			RemoteAddr: fmt.Sprintf("10.0.%d.1:1234", i),
		}
		dt.RecordDevice("user1", req)
	}

	// Limit of 3 → allowed (count = 3, max = 3)
	result := dt.CheckDeviceLimit("user1", 3)
	if !result.Allowed {
		t.Error("expected allowed at limit")
	}

	// Add one more
	req := &http.Request{
		Header:     http.Header{"User-Agent": {"Agent-extra"}},
		RemoteAddr: "172.16.0.1:1234",
	}
	dt.RecordDevice("user1", req)

	// Now over limit
	result = dt.CheckDeviceLimit("user1", 3)
	if result.Allowed {
		t.Error("expected denied over limit")
	}
	if result.Count != 4 {
		t.Errorf("expected 4 devices, got %d", result.Count)
	}
}

func TestDeviceUnlimited(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	dt := NewDeviceTracker(dir, logger)

	result := dt.CheckDeviceLimit("user1", 0)
	if !result.Allowed {
		t.Error("unlimited should always be allowed")
	}
}

func TestDeviceCleanStale(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	dt := NewDeviceTracker(dir, logger)

	req := &http.Request{
		Header:     http.Header{"User-Agent": {"old-browser"}},
		RemoteAddr: "10.0.0.1:1234",
	}
	dt.RecordDevice("user1", req)

	// Manually set LastSeen to 31 days ago
	dt.mu.Lock()
	for _, dev := range dt.devices {
		dev.LastSeen = time.Now().Add(-31 * 24 * time.Hour)
	}
	dt.mu.Unlock()

	removed := dt.CleanStale(30 * 24 * time.Hour)
	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}

	count := dt.GetDeviceCount("user1")
	if count != 0 {
		t.Errorf("expected 0 devices after cleanup, got %d", count)
	}
}

// --- Subscription Lifecycle Tests ---

func TestSubscriptionLifecycle(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	subStore := NewSubscriptionStore(dir, logger)
	keyStore := NewAPIKeyStore(dir, subStore, logger)

	// Create subscription
	sub := &Subscription{
		ID:                 "sub_lifecycle",
		UserID:             "user_lc",
		Email:              "test@example.com",
		PlanID:             "starter",
		Status:             "active",
		CurrentPeriodStart: time.Now(),
		CurrentPeriodEnd:   time.Now().Add(30 * 24 * time.Hour),
	}
	if err := subStore.Create(sub); err != nil {
		t.Fatal(err)
	}

	// Generate key
	_, rawKey, _ := keyStore.GenerateKey("user_lc", "sub_lifecycle", "main", nil, false)

	// Verify active
	_, err := keyStore.ValidateKey(rawKey)
	if err != nil {
		t.Fatalf("key should be valid: %v", err)
	}

	// Expire subscription
	sub.Status = "expired"
	subStore.Update(sub)

	// Key should now fail
	_, err = keyStore.ValidateKey(rawKey)
	if err == nil {
		t.Error("key should be invalid after subscription expired")
	}

	// Re-activate
	sub.Status = "active"
	subStore.Update(sub)

	// Key should work again
	_, err = keyStore.ValidateKey(rawKey)
	if err != nil {
		t.Errorf("key should be valid after reactivation: %v", err)
	}
}

func TestSubscriptionExpiring(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	subStore := NewSubscriptionStore(dir, logger)

	// Sub expiring in 2 days
	sub := &Subscription{
		ID:               "sub_exp",
		UserID:           "user_exp",
		PlanID:           "free",
		Status:           "active",
		CurrentPeriodEnd: time.Now().Add(2 * 24 * time.Hour),
	}
	subStore.Create(sub)

	// Should appear in 3-day window
	expiring := subStore.GetExpiring(3 * 24 * time.Hour)
	if len(expiring) != 1 {
		t.Errorf("expected 1 expiring, got %d", len(expiring))
	}

	// Should NOT appear in 1-day window
	expiring = subStore.GetExpiring(24 * time.Hour)
	if len(expiring) != 0 {
		t.Errorf("expected 0 expiring in 1-day window, got %d", len(expiring))
	}
}

func TestSubscriptionGetExpired(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	subStore := NewSubscriptionStore(dir, logger)

	sub := &Subscription{
		ID:               "sub_past",
		UserID:           "user_past",
		PlanID:           "free",
		Status:           "active",
		CurrentPeriodEnd: time.Now().Add(-1 * time.Hour), // already past
	}
	subStore.Create(sub)

	expired := subStore.GetExpired()
	if len(expired) != 1 {
		t.Errorf("expected 1 expired, got %d", len(expired))
	}
}

// --- Stripe Webhook Tests ---

func TestStripeSignatureVerification(t *testing.T) {
	secret := "whsec_test_secret_123"
	payload := []byte(`{"id":"evt_1","type":"test"}`)
	timestamp := fmt.Sprintf("%d", time.Now().Unix())

	signedPayload := timestamp + "." + string(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signedPayload))
	sig := hex.EncodeToString(mac.Sum(nil))

	sigHeader := fmt.Sprintf("t=%s,v1=%s", timestamp, sig)

	err := VerifyStripeSignature(payload, sigHeader, secret)
	if err != nil {
		t.Fatalf("expected valid signature, got: %v", err)
	}
}

func TestStripeSignatureInvalid(t *testing.T) {
	err := VerifyStripeSignature([]byte("payload"), "t=123,v1=badsig", "secret")
	if err == nil {
		t.Error("expected error for invalid signature")
	}
}

func TestStripeSignatureMissing(t *testing.T) {
	err := VerifyStripeSignature([]byte("payload"), "", "secret")
	if err == nil {
		t.Error("expected error for missing signature")
	}
}

func TestStripeHandleSubscriptionCreated(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	subStore := NewSubscriptionStore(dir, logger)
	keyStore := NewAPIKeyStore(dir, subStore, logger)
	handler := NewStripeWebhookHandler(subStore, keyStore, "secret", logger)

	subObj := map[string]interface{}{
		"id":                   "sub_stripe_1",
		"customer":             "cus_123",
		"status":               "active",
		"current_period_start": time.Now().Unix(),
		"current_period_end":   time.Now().Add(30 * 24 * time.Hour).Unix(),
		"metadata":             map[string]string{"plan_id": "starter", "user_id": "user_stripe", "email": "stripe@test.com"},
	}
	eventData := map[string]interface{}{"object": subObj}
	dataJSON, _ := json.Marshal(eventData)

	event := StripeEvent{
		ID:   "evt_1",
		Type: "customer.subscription.created",
		Data: dataJSON,
	}

	if err := handler.HandleEvent(event); err != nil {
		t.Fatal(err)
	}

	sub, found := subStore.GetByStripeSubID("sub_stripe_1")
	if !found {
		t.Fatal("subscription not created")
	}
	if sub.PlanID != "starter" {
		t.Errorf("expected starter plan, got %s", sub.PlanID)
	}
	if sub.Status != "active" {
		t.Errorf("expected active, got %s", sub.Status)
	}
}

func TestStripeHandleInvoicePaid(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	subStore := NewSubscriptionStore(dir, logger)
	keyStore := NewAPIKeyStore(dir, subStore, logger)

	// Create existing subscription
	sub := &Subscription{
		ID:               "sub_inv",
		UserID:           "user_inv",
		PlanID:           "starter",
		Status:           "past_due",
		StripeSubID:      "stripe_sub_inv",
		CurrentPeriodEnd: time.Now().Add(30 * 24 * time.Hour),
	}
	subStore.Create(sub)

	handler := NewStripeWebhookHandler(subStore, keyStore, "secret", logger)

	invoiceObj := map[string]interface{}{
		"id":           "inv_1",
		"customer":     "cus_inv",
		"subscription": "stripe_sub_inv",
		"status":       "paid",
		"paid":         true,
	}
	eventData := map[string]interface{}{"object": invoiceObj}
	dataJSON, _ := json.Marshal(eventData)

	event := StripeEvent{
		ID:   "evt_inv",
		Type: "invoice.paid",
		Data: dataJSON,
	}

	if err := handler.HandleEvent(event); err != nil {
		t.Fatal(err)
	}

	updated, _ := subStore.Get("sub_inv")
	if updated.Status != "active" {
		t.Errorf("expected active after invoice.paid, got %s", updated.Status)
	}
}

func TestStripeHandleSubscriptionDeleted(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	subStore := NewSubscriptionStore(dir, logger)
	keyStore := NewAPIKeyStore(dir, subStore, logger)

	sub := &Subscription{
		ID:               "sub_del",
		UserID:           "user_del",
		PlanID:           "starter",
		Status:           "active",
		StripeSubID:      "stripe_sub_del",
		CurrentPeriodEnd: time.Now().Add(30 * 24 * time.Hour),
	}
	subStore.Create(sub)

	// Create a key for this subscription
	_, rawKey, _ := keyStore.GenerateKey("user_del", "sub_del", "key", nil, false)

	handler := NewStripeWebhookHandler(subStore, keyStore, "secret", logger)

	subObj := map[string]interface{}{
		"id":                   "stripe_sub_del",
		"customer":             "cus_del",
		"status":               "canceled",
		"current_period_start": time.Now().Unix(),
		"current_period_end":   time.Now().Unix(),
	}
	eventData := map[string]interface{}{"object": subObj}
	dataJSON, _ := json.Marshal(eventData)

	event := StripeEvent{
		ID:   "evt_del",
		Type: "customer.subscription.deleted",
		Data: dataJSON,
	}

	if err := handler.HandleEvent(event); err != nil {
		t.Fatal(err)
	}

	updated, _ := subStore.Get("sub_del")
	if updated.Status != "expired" {
		t.Errorf("expected expired, got %s", updated.Status)
	}

	// Key should be revoked
	_, err := keyStore.ValidateKey(rawKey)
	if err == nil {
		t.Error("key should be revoked after subscription deleted")
	}
}

// --- Persistence Tests ---

func TestSubscriptionPersistence(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()

	store1 := NewSubscriptionStore(dir, logger)
	sub := &Subscription{
		ID:               "sub_persist",
		UserID:           "user_p",
		PlanID:           "team",
		Status:           "active",
		CurrentPeriodEnd: time.Now().Add(30 * 24 * time.Hour),
	}
	store1.Create(sub)

	// Load in new store instance
	store2 := NewSubscriptionStore(dir, logger)
	loaded, found := store2.Get("sub_persist")
	if !found {
		t.Fatal("subscription not persisted")
	}
	if loaded.PlanID != "team" {
		t.Errorf("expected team plan, got %s", loaded.PlanID)
	}
}

func TestAPIKeyPersistence(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	subStore := NewSubscriptionStore(dir, logger)

	store1 := NewAPIKeyStore(dir, subStore, logger)
	key, _, _ := store1.GenerateKey("user_p", "sub_p", "key", nil, false)

	store2 := NewAPIKeyStore(dir, subStore, logger)
	loaded, found := store2.GetByHash(key.KeyHash)
	if !found {
		t.Fatal("key not persisted")
	}
	if loaded.UserID != "user_p" {
		t.Errorf("expected user_p, got %s", loaded.UserID)
	}
}

func TestDefaultPlans(t *testing.T) {
	plans := DefaultPlans()
	if len(plans) != 4 {
		t.Errorf("expected 4 default plans, got %d", len(plans))
	}
	free := plans["free"]
	if free.MaxRequests != 1000 || free.MaxRPM != 10 || free.MaxDevices != 1 {
		t.Errorf("free plan has wrong limits: %+v", free)
	}
	ent := plans["enterprise"]
	if ent.MaxRequests != 0 {
		t.Error("enterprise should have unlimited requests")
	}
}

func TestKeyHashConsistency(t *testing.T) {
	raw := "nxs_live_0123456789abcdef0123456789abcdef"
	h1 := HashKey(raw)
	h2 := HashKey(raw)
	if h1 != h2 {
		t.Error("hash should be deterministic")
	}
	if len(h1) != 64 {
		t.Errorf("expected 64-char hex hash, got %d", len(h1))
	}
}

func TestKeyScopes(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	subStore := NewSubscriptionStore(dir, logger)
	store := NewAPIKeyStore(dir, subStore, logger)

	key, _, _ := store.GenerateKey("user1", "sub1", "admin key", []string{"chat", "admin", "dashboard"}, false)
	if len(key.Scopes) != 3 {
		t.Errorf("expected 3 scopes, got %d", len(key.Scopes))
	}

	// Default scopes
	key2, _, _ := store.GenerateKey("user1", "sub1", "basic", nil, false)
	if len(key2.Scopes) != 1 || key2.Scopes[0] != "chat" {
		t.Errorf("expected default [chat] scope, got %v", key2.Scopes)
	}
}

func TestRecordUsageCounters(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	subStore := NewSubscriptionStore(dir, logger)
	store := NewAPIKeyStore(dir, subStore, logger)

	key, _, _ := store.GenerateKey("user1", "sub1", "key", nil, false)

	for i := 0; i < 5; i++ {
		store.RecordUsage(key.KeyHash)
	}

	store.mu.RLock()
	k := store.keys[key.KeyHash]
	if k.RequestCount != 5 {
		t.Errorf("expected 5 total requests, got %d", k.RequestCount)
	}
	if k.MonthlyUsage != 5 {
		t.Errorf("expected 5 monthly usage, got %d", k.MonthlyUsage)
	}
	if k.LastUsedAt == nil {
		t.Error("expected LastUsedAt to be set")
	}
	store.mu.RUnlock()
}

func TestSubscriptionInactiveKeyValidation(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	subStore := NewSubscriptionStore(dir, logger)

	sub := &Subscription{
		ID:               "sub_inactive",
		UserID:           "user_ia",
		PlanID:           "free",
		Status:           "canceled",
		CurrentPeriodEnd: time.Now().Add(30 * 24 * time.Hour),
	}
	subStore.Create(sub)

	keyStore := NewAPIKeyStore(dir, subStore, logger)
	_, rawKey, _ := keyStore.GenerateKey("user_ia", "sub_inactive", "key", nil, false)

	_, err := keyStore.ValidateKey(rawKey)
	if err == nil {
		t.Error("expected error for canceled subscription")
	}
	if !strings.Contains(err.Error(), "canceled") {
		t.Errorf("expected 'canceled' in error, got: %s", err.Error())
	}
}

// --- Webhook Signature Verification Tests ---

func makeValidSignature(payload []byte, secret string, ts int64) string {
	timestamp := fmt.Sprintf("%d", ts)
	signedPayload := timestamp + "." + string(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signedPayload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("t=%s,v1=%s", timestamp, sig)
}

func TestWebhookValidSignaturePasses(t *testing.T) {
	secret := "whsec_test_webhook_secret"
	payload := []byte(`{"id":"evt_test","type":"customer.subscription.created"}`)
	sigHeader := makeValidSignature(payload, secret, time.Now().Unix())

	err := VerifyStripeSignature(payload, sigHeader, secret)
	if err != nil {
		t.Fatalf("valid signature should pass: %v", err)
	}
}

func TestWebhookInvalidSignatureRejected(t *testing.T) {
	secret := "whsec_test_webhook_secret"
	payload := []byte(`{"id":"evt_test","type":"test"}`)
	sigHeader := fmt.Sprintf("t=%d,v1=0000000000000000000000000000000000000000000000000000000000000000", time.Now().Unix())

	err := VerifyStripeSignature(payload, sigHeader, secret)
	if err == nil {
		t.Error("invalid signature should be rejected")
	}
	if !strings.Contains(err.Error(), "signature verification failed") {
		t.Errorf("expected 'signature verification failed', got: %s", err.Error())
	}
}

func TestWebhookMissingSignatureRejected(t *testing.T) {
	err := VerifyStripeSignature([]byte(`{}`), "", "whsec_secret")
	if err == nil {
		t.Error("missing signature header should be rejected")
	}
	if !strings.Contains(err.Error(), "missing Stripe-Signature") {
		t.Errorf("expected 'missing Stripe-Signature', got: %s", err.Error())
	}
}

func TestWebhookMissingSecretRejected(t *testing.T) {
	err := VerifyStripeSignature([]byte(`{}`), "t=123,v1=abc", "")
	if err == nil {
		t.Error("empty webhook secret should be rejected")
	}
	if !strings.Contains(err.Error(), "webhook secret not configured") {
		t.Errorf("expected 'webhook secret not configured', got: %s", err.Error())
	}
}

func TestWebhookReplayAttackRejected(t *testing.T) {
	secret := "whsec_replay_test"
	payload := []byte(`{"id":"evt_replay","type":"test"}`)
	// Signature from 10 minutes ago (exceeds 5-minute tolerance)
	oldTimestamp := time.Now().Add(-10 * time.Minute).Unix()
	sigHeader := makeValidSignature(payload, secret, oldTimestamp)

	err := VerifyStripeSignature(payload, sigHeader, secret)
	if err == nil {
		t.Error("replay attack with old timestamp should be rejected")
	}
	if !strings.Contains(err.Error(), "timestamp too old") {
		t.Errorf("expected 'timestamp too old', got: %s", err.Error())
	}
}

func TestWebhookFutureTimestampRejected(t *testing.T) {
	secret := "whsec_future_test"
	payload := []byte(`{"id":"evt_future","type":"test"}`)
	futureTimestamp := time.Now().Add(10 * time.Minute).Unix()
	sigHeader := makeValidSignature(payload, secret, futureTimestamp)

	err := VerifyStripeSignature(payload, sigHeader, secret)
	if err == nil {
		t.Error("future timestamp beyond tolerance should be rejected")
	}
}

func TestWebhookMalformedSignatureHeader(t *testing.T) {
	tests := []struct {
		name      string
		sigHeader string
		wantErr   string
	}{
		{"missing timestamp", "v1=abc123", "missing timestamp"},
		{"missing v1 sig", "t=123456", "missing v1 signature"},
		{"invalid timestamp", "t=notanumber,v1=abc", "invalid timestamp"},
		{"garbage data", "garbage", "missing timestamp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifyStripeSignature([]byte(`{}`), tt.sigHeader, "secret")
			if err == nil {
				t.Error("malformed signature should be rejected")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %s", tt.wantErr, err.Error())
			}
		})
	}
}

func TestWebhookTamperedPayloadRejected(t *testing.T) {
	secret := "whsec_tamper_test"
	originalPayload := []byte(`{"id":"evt_original","type":"test"}`)
	sigHeader := makeValidSignature(originalPayload, secret, time.Now().Unix())

	// Tamper with the payload
	tamperedPayload := []byte(`{"id":"evt_original","type":"test","extra":"hacked"}`)
	err := VerifyStripeSignature(tamperedPayload, sigHeader, secret)
	if err == nil {
		t.Error("tampered payload should be rejected")
	}
}

func TestWebhookWrongSecretRejected(t *testing.T) {
	payload := []byte(`{"id":"evt_test","type":"test"}`)
	sigHeader := makeValidSignature(payload, "correct_secret", time.Now().Unix())

	err := VerifyStripeSignature(payload, sigHeader, "wrong_secret")
	if err == nil {
		t.Error("wrong secret should be rejected")
	}
}

// --- API Key Hashing Verification ---

func TestAPIKeyNeverStoredPlaintext(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	subStore := NewSubscriptionStore(dir, logger)
	store := NewAPIKeyStore(dir, subStore, logger)

	key, rawKey, err := store.GenerateKey("user1", "sub1", "plaintext test", nil, false)
	if err != nil {
		t.Fatal(err)
	}

	// The Key field should be empty (never persisted)
	if key.Key != "" {
		t.Error("raw key should not be stored in APIKey struct")
	}

	// KeyHash should be SHA-256 of the raw key
	expectedHash := HashKey(rawKey)
	if key.KeyHash != expectedHash {
		t.Error("key hash should match SHA-256 of raw key")
	}

	// Verify hash length (SHA-256 = 64 hex chars)
	if len(key.KeyHash) != 64 {
		t.Errorf("key hash should be 64 hex chars, got %d", len(key.KeyHash))
	}

	// KeyHash should not contain the raw key
	if strings.Contains(key.KeyHash, rawKey) {
		t.Error("key hash should not contain the raw key")
	}
}

func TestAPIKeyPrefixValidation(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	subStore := NewSubscriptionStore(dir, logger)
	store := NewAPIKeyStore(dir, subStore, logger)

	// Live key prefix
	_, liveKey, _ := store.GenerateKey("u1", "s1", "live", nil, false)
	if !strings.HasPrefix(liveKey, "nxs_live_") {
		t.Errorf("live key should have nxs_live_ prefix, got %s", liveKey[:12])
	}

	// Test key prefix
	_, testKey, _ := store.GenerateKey("u1", "s1", "test", nil, true)
	if !strings.HasPrefix(testKey, "nxs_test_") {
		t.Errorf("test key should have nxs_test_ prefix, got %s", testKey[:12])
	}

	// Invalid prefix should fail validation
	_, err := store.ValidateKey("invalid_prefix_key_abcdef01234567890")
	if err == nil {
		t.Error("key with invalid prefix should fail validation")
	}
}

func TestAPIKeyPerKeyRateLimiting(t *testing.T) {
	dir := testDir(t)
	logger := testLogger()
	subStore := NewSubscriptionStore(dir, logger)

	sub := &Subscription{
		ID:               "sub_rl",
		UserID:           "user_rl",
		PlanID:           "free",
		Status:           "active",
		CurrentPeriodEnd: time.Now().Add(30 * 24 * time.Hour),
	}
	subStore.Create(sub)

	store := NewAPIKeyStore(dir, subStore, logger)
	key, _, _ := store.GenerateKey("user_rl", "sub_rl", "rate limit key", nil, false)

	// Under quota should be allowed
	result := store.CheckQuota(key.KeyHash)
	if !result.Allowed {
		t.Error("should be allowed under quota")
	}

	// Exhaust quota
	store.mu.Lock()
	store.keys[key.KeyHash].MonthlyUsage = 1000 // free plan limit
	store.mu.Unlock()

	result = store.CheckQuota(key.KeyHash)
	if result.Allowed {
		t.Error("should be denied when quota exhausted")
	}
	if result.Remaining != 0 {
		t.Errorf("expected 0 remaining, got %d", result.Remaining)
	}
}
