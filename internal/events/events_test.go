package events

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEventCreation(t *testing.T) {
	hooks := []WebhookConfig{{URL: "http://localhost", Events: []string{"*"}}}
	eb := NewEventBus(hooks)
	defer eb.Close()

	eb.Emit(RequestCompleted, map[string]interface{}{"model": "gpt-4"})
	// Allow event to be processed
	time.Sleep(200 * time.Millisecond)
}

func TestEventTypes(t *testing.T) {
	types := []EventType{
		RequestCompleted, RequestCached, RequestError,
		BudgetWarning, BudgetCritical, BudgetExhausted,
		WorkflowStarted, WorkflowCompleted,
		CostAnomaly, TierDowngrade,
		ProviderUnhealthy, ProviderRecovered,
	}
	for _, et := range types {
		if string(et) == "" {
			t.Fatalf("empty event type string")
		}
	}
}

func TestWebhookDelivery(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hooks := []WebhookConfig{{URL: srv.URL, Events: []string{"*"}}}
	eb := NewEventBus(hooks)
	defer eb.Close()

	eb.Emit(RequestCompleted, map[string]interface{}{"model": "gpt-4"})
	time.Sleep(500 * time.Millisecond)

	if received.Load() != 1 {
		t.Fatalf("expected 1 webhook delivery, got %d", received.Load())
	}
}

func TestWebhookHeaders(t *testing.T) {
	var gotEventType, gotEventID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEventType = r.Header.Get("X-Nexus-Event")
		gotEventID = r.Header.Get("X-Nexus-Event-ID")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hooks := []WebhookConfig{{URL: srv.URL, Events: []string{"*"}}}
	eb := NewEventBus(hooks)
	defer eb.Close()

	eb.Emit(RequestCompleted, nil)
	time.Sleep(500 * time.Millisecond)

	if gotEventType != string(RequestCompleted) {
		t.Fatalf("expected event type %s, got %s", RequestCompleted, gotEventType)
	}
	if gotEventID == "" {
		t.Fatal("expected non-empty event ID header")
	}
}

func TestWebhookCustomHeaders(t *testing.T) {
	var gotCustom string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCustom = r.Header.Get("X-Custom")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hooks := []WebhookConfig{
		{URL: srv.URL, Events: []string{"*"}, Headers: map[string]string{"X-Custom": "hello"}},
	}
	eb := NewEventBus(hooks)
	defer eb.Close()

	eb.Emit(RequestCompleted, nil)
	time.Sleep(500 * time.Millisecond)

	if gotCustom != "hello" {
		t.Fatalf("expected custom header 'hello', got %q", gotCustom)
	}
}

func TestHMACSignature(t *testing.T) {
	secret := "my-webhook-secret"
	var gotSig string
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Nexus-Signature")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hooks := []WebhookConfig{{URL: srv.URL, Events: []string{"*"}, Secret: secret}}
	eb := NewEventBus(hooks)
	defer eb.Close()

	eb.Emit(RequestCompleted, map[string]interface{}{"test": true})
	time.Sleep(500 * time.Millisecond)

	if gotSig == "" {
		t.Fatal("expected HMAC signature header")
	}

	// Verify the signature
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(gotBody)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if gotSig != expected {
		t.Fatalf("HMAC mismatch: got %s, expected %s", gotSig, expected)
	}
}

func TestNoSignatureWithoutSecret(t *testing.T) {
	var gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Nexus-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hooks := []WebhookConfig{{URL: srv.URL, Events: []string{"*"}}}
	eb := NewEventBus(hooks)
	defer eb.Close()

	eb.Emit(RequestCompleted, nil)
	time.Sleep(500 * time.Millisecond)

	if gotSig != "" {
		t.Fatalf("expected no signature when secret is empty, got %s", gotSig)
	}
}

func TestEventFiltering_MatchSpecific(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hooks := []WebhookConfig{
		{URL: srv.URL, Events: []string{string(BudgetWarning)}},
	}
	eb := NewEventBus(hooks)
	defer eb.Close()

	eb.Emit(RequestCompleted, nil) // should NOT match
	eb.Emit(BudgetWarning, nil)    // should match
	time.Sleep(500 * time.Millisecond)

	if received.Load() != 1 {
		t.Fatalf("expected 1 delivery (filtered), got %d", received.Load())
	}
}

func TestEventFiltering_WildcardMatchesAll(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hooks := []WebhookConfig{{URL: srv.URL, Events: []string{"*"}}}
	eb := NewEventBus(hooks)
	defer eb.Close()

	eb.Emit(RequestCompleted, nil)
	eb.Emit(BudgetWarning, nil)
	eb.Emit(WorkflowStarted, nil)
	time.Sleep(500 * time.Millisecond)

	if received.Load() != 3 {
		t.Fatalf("expected 3 deliveries with wildcard, got %d", received.Load())
	}
}

func TestEventFiltering_NoMatch(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hooks := []WebhookConfig{
		{URL: srv.URL, Events: []string{string(ProviderUnhealthy)}},
	}
	eb := NewEventBus(hooks)
	defer eb.Close()

	eb.Emit(RequestCompleted, nil)
	time.Sleep(300 * time.Millisecond)

	if received.Load() != 0 {
		t.Fatalf("expected 0 deliveries for non-matching filter, got %d", received.Load())
	}
}

func TestWebhookBodyIsValidJSON(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hooks := []WebhookConfig{{URL: srv.URL, Events: []string{"*"}}}
	eb := NewEventBus(hooks)
	defer eb.Close()

	eb.Emit(RequestCompleted, map[string]interface{}{"key": "value"})
	time.Sleep(500 * time.Millisecond)

	var evt Event
	if err := json.Unmarshal(gotBody, &evt); err != nil {
		t.Fatalf("webhook body is not valid JSON: %v", err)
	}
	if evt.Type != RequestCompleted {
		t.Fatalf("expected type %s, got %s", RequestCompleted, evt.Type)
	}
	if evt.Data["key"] != "value" {
		t.Fatalf("expected data key=value, got %v", evt.Data)
	}
}

func TestConcurrentEmit(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hooks := []WebhookConfig{{URL: srv.URL, Events: []string{"*"}}}
	eb := NewEventBus(hooks)
	defer eb.Close()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			eb.Emit(RequestCompleted, nil)
		}()
	}
	wg.Wait()
	time.Sleep(time.Second)

	if received.Load() != 20 {
		t.Fatalf("expected 20 deliveries, got %d", received.Load())
	}
}

func TestEventIDsAreUnique(t *testing.T) {
	ids := make(map[string]bool)
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Nexus-Event-ID")
		mu.Lock()
		ids[id] = true
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hooks := []WebhookConfig{{URL: srv.URL, Events: []string{"*"}}}
	eb := NewEventBus(hooks)
	defer eb.Close()

	for i := 0; i < 10; i++ {
		eb.Emit(RequestCompleted, nil)
	}
	time.Sleep(time.Second)

	mu.Lock()
	defer mu.Unlock()
	if len(ids) != 10 {
		t.Fatalf("expected 10 unique IDs, got %d", len(ids))
	}
}

func TestWebhookFailureDoesNotPanic(t *testing.T) {
	// Point to a server that immediately closes
	hooks := []WebhookConfig{{URL: "http://127.0.0.1:1", Events: []string{"*"}}}
	eb := NewEventBus(hooks)
	defer eb.Close()

	eb.Emit(RequestCompleted, nil)
	time.Sleep(300 * time.Millisecond)
	// No panic = pass
}

func TestMultipleHooksIndependent(t *testing.T) {
	var count1, count2 atomic.Int32
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count1.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv1.Close()
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count2.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv2.Close()

	hooks := []WebhookConfig{
		{URL: srv1.URL, Events: []string{string(RequestCompleted)}},
		{URL: srv2.URL, Events: []string{string(BudgetWarning)}},
	}
	eb := NewEventBus(hooks)
	defer eb.Close()

	eb.Emit(RequestCompleted, nil)
	eb.Emit(BudgetWarning, nil)
	time.Sleep(500 * time.Millisecond)

	if count1.Load() != 1 {
		t.Fatalf("srv1 expected 1, got %d", count1.Load())
	}
	if count2.Load() != 1 {
		t.Fatalf("srv2 expected 1, got %d", count2.Load())
	}
}
