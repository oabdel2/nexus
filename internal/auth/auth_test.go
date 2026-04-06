package auth

import (
	"sync"
	"testing"
)

func makeKey(name, key, team string, budget float64, rpm int, tiers []string) APIKey {
	return APIKey{
		Key:           key,
		Name:          name,
		Team:          team,
		MonthlyBudget: budget,
		AlertAt:       0.8,
		RPM:           rpm,
		AllowedTiers:  tiers,
		Enabled:       true,
	}
}

func TestNewKeyManager_EnabledKeys(t *testing.T) {
	keys := []APIKey{
		makeKey("k1", "secret1", "team-a", 100, 60, nil),
		makeKey("k2", "secret2", "team-b", 200, 0, nil),
	}
	km := NewKeyManager(keys)
	if !km.enabled {
		t.Fatal("expected manager to be enabled")
	}
	if len(km.keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(km.keys))
	}
}

func TestNewKeyManager_DisabledKeysSkipped(t *testing.T) {
	keys := []APIKey{
		{Key: "k1", Name: "disabled", Enabled: false},
		makeKey("k2", "secret2", "team-b", 100, 0, nil),
	}
	km := NewKeyManager(keys)
	if len(km.keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(km.keys))
	}
}

func TestNewKeyManager_Empty(t *testing.T) {
	km := NewKeyManager(nil)
	if km.enabled {
		t.Fatal("expected manager to be disabled for empty keys")
	}
}

func TestValidate_AuthDisabled(t *testing.T) {
	km := NewKeyManager(nil)
	k, err := km.Validate("anything")
	if err != nil || k != nil {
		t.Fatalf("expected nil,nil when auth disabled; got %v, %v", k, err)
	}
}

func TestValidate_EmptyKey(t *testing.T) {
	km := NewKeyManager([]APIKey{makeKey("k1", "secret", "t", 100, 0, nil)})
	_, err := km.Validate("")
	if err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestValidate_ValidKey(t *testing.T) {
	km := NewKeyManager([]APIKey{makeKey("k1", "my-secret-key", "team-a", 100, 0, nil)})
	k, err := km.Validate("my-secret-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if k.Name != "k1" {
		t.Fatalf("expected name k1, got %s", k.Name)
	}
}

func TestValidate_InvalidKey(t *testing.T) {
	km := NewKeyManager([]APIKey{makeKey("k1", "real-key", "t", 100, 0, nil)})
	_, err := km.Validate("wrong-key")
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
}

func TestCheckRateLimit_NoRPM(t *testing.T) {
	km := NewKeyManager([]APIKey{makeKey("k1", "key1", "t", 100, 0, nil)})
	if !km.CheckRateLimit("key1") {
		t.Fatal("expected rate limit pass when RPM=0")
	}
}

func TestCheckRateLimit_UnknownKey(t *testing.T) {
	km := NewKeyManager([]APIKey{makeKey("k1", "key1", "t", 100, 60, nil)})
	if !km.CheckRateLimit("nonexistent") {
		t.Fatal("expected rate limit pass for unknown key")
	}
}

func TestCheckRateLimit_ExceedsLimit(t *testing.T) {
	km := NewKeyManager([]APIKey{makeKey("k1", "key1", "t", 100, 3, nil)})
	for i := 0; i < 3; i++ {
		if !km.CheckRateLimit("key1") {
			t.Fatalf("request %d should pass", i+1)
		}
	}
	if km.CheckRateLimit("key1") {
		t.Fatal("expected rate limit exceeded on 4th request")
	}
}

func TestCheckBudget_Unlimited(t *testing.T) {
	km := NewKeyManager([]APIKey{makeKey("k1", "key1", "t", 0, 0, nil)})
	ok, remaining := km.CheckBudget("key1")
	if !ok || remaining != -1 {
		t.Fatalf("expected unlimited budget; got ok=%v remaining=%v", ok, remaining)
	}
}

func TestCheckBudget_UnknownKey(t *testing.T) {
	km := NewKeyManager([]APIKey{makeKey("k1", "key1", "t", 100, 0, nil)})
	ok, remaining := km.CheckBudget("nonexistent")
	if !ok || remaining != -1 {
		t.Fatalf("expected unlimited for unknown key; got ok=%v remaining=%v", ok, remaining)
	}
}

func TestCheckBudget_WithUsage(t *testing.T) {
	km := NewKeyManager([]APIKey{makeKey("k1", "key1", "t", 10.0, 0, nil)})
	km.RecordUsage("key1", 7.0)
	ok, remaining := km.CheckBudget("key1")
	if !ok {
		t.Fatal("expected budget ok")
	}
	if remaining != 3.0 {
		t.Fatalf("expected remaining 3.0, got %f", remaining)
	}
}

func TestCheckBudget_Exhausted(t *testing.T) {
	km := NewKeyManager([]APIKey{makeKey("k1", "key1", "t", 5.0, 0, nil)})
	km.RecordUsage("key1", 5.0)
	ok, _ := km.CheckBudget("key1")
	if ok {
		t.Fatal("expected budget exhausted")
	}
}

func TestRecordUsage_AccumulatesCost(t *testing.T) {
	km := NewKeyManager([]APIKey{makeKey("k1", "key1", "t", 100, 0, nil)})
	km.RecordUsage("key1", 1.5)
	km.RecordUsage("key1", 2.5)
	report := km.GetUsageReport("key1")
	if report["total_cost"].(float64) != 4.0 {
		t.Fatalf("expected total_cost 4.0, got %v", report["total_cost"])
	}
	if report["requests"].(int64) != 2 {
		t.Fatalf("expected 2 requests, got %v", report["requests"])
	}
}

func TestRecordUsage_UnknownKey(t *testing.T) {
	km := NewKeyManager([]APIKey{makeKey("k1", "key1", "t", 100, 0, nil)})
	km.RecordUsage("nonexistent", 5.0) // should not panic
}

func TestIsTierAllowed_NoRestrictions(t *testing.T) {
	km := NewKeyManager([]APIKey{makeKey("k1", "key1", "t", 100, 0, nil)})
	if !km.IsTierAllowed("key1", "premium") {
		t.Fatal("expected all tiers allowed when no restrictions")
	}
}

func TestIsTierAllowed_Restricted(t *testing.T) {
	km := NewKeyManager([]APIKey{makeKey("k1", "key1", "t", 100, 0, []string{"cheap", "mid"})})
	if !km.IsTierAllowed("key1", "cheap") {
		t.Fatal("expected cheap allowed")
	}
	if km.IsTierAllowed("key1", "premium") {
		t.Fatal("expected premium not allowed")
	}
}

func TestIsTierAllowed_UnknownKey(t *testing.T) {
	km := NewKeyManager([]APIKey{makeKey("k1", "key1", "t", 100, 0, []string{"cheap"})})
	if !km.IsTierAllowed("nonexistent", "premium") {
		t.Fatal("expected allowed for unknown key")
	}
}

func TestGetUsageReport_ValidKey(t *testing.T) {
	km := NewKeyManager([]APIKey{makeKey("k1", "key1", "team-a", 50, 0, nil)})
	km.RecordUsage("key1", 25.0)
	report := km.GetUsageReport("key1")
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if report["name"] != "k1" {
		t.Fatalf("expected name k1, got %v", report["name"])
	}
	if report["budget_used"].(float64) != 0.5 {
		t.Fatalf("expected budget_used 0.5, got %v", report["budget_used"])
	}
}

func TestGetUsageReport_UnknownKey(t *testing.T) {
	km := NewKeyManager([]APIKey{makeKey("k1", "key1", "t", 100, 0, nil)})
	report := km.GetUsageReport("nonexistent")
	if report != nil {
		t.Fatal("expected nil report for unknown key")
	}
}

func TestGetUsageReport_ZeroBudget(t *testing.T) {
	km := NewKeyManager([]APIKey{makeKey("k1", "key1", "t", 0, 0, nil)})
	km.RecordUsage("key1", 10.0)
	report := km.GetUsageReport("key1")
	if report["budget_used"].(float64) != 0.0 {
		t.Fatalf("expected budget_used 0.0 for zero budget, got %v", report["budget_used"])
	}
}

func TestListKeys(t *testing.T) {
	keys := []APIKey{
		makeKey("k1", "secret1", "team-a", 100, 60, nil),
		makeKey("k2", "secret2", "team-b", 200, 0, nil),
	}
	km := NewKeyManager(keys)
	km.RecordUsage("secret1", 5.0)
	list := km.ListKeys()
	if len(list) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(list))
	}
}

func TestConcurrentValidate(t *testing.T) {
	km := NewKeyManager([]APIKey{makeKey("k1", "key1", "t", 100, 0, nil)})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			km.Validate("key1")
		}()
	}
	wg.Wait()
}

func TestConcurrentRecordAndBudget(t *testing.T) {
	km := NewKeyManager([]APIKey{makeKey("k1", "key1", "t", 1000, 0, nil)})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			km.RecordUsage("key1", 0.1)
		}()
		go func() {
			defer wg.Done()
			km.CheckBudget("key1")
		}()
	}
	wg.Wait()
}
