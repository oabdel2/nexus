package notification

import (
	"fmt"
	"log/slog"
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
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("nexus_notif_test_%d", time.Now().UnixNano()))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestEventLogRecord(t *testing.T) {
	dir := testDir(t)
	log := NewEventLog(dir)

	log.Record(EventLogEntry{
		ID:        "evt_1",
		EventType: EventWelcome,
		UserID:    "user1",
		Email:     "test@test.com",
		SentAt:    time.Now(),
		Status:    "sent",
	})

	if log.Len() != 1 {
		t.Errorf("expected 1 event, got %d", log.Len())
	}
}

func TestEventLogGetByUser(t *testing.T) {
	log := NewEventLog("")

	log.Record(EventLogEntry{ID: "e1", EventType: EventWelcome, UserID: "user1", SentAt: time.Now(), Status: "sent"})
	log.Record(EventLogEntry{ID: "e2", EventType: EventPaymentFailed, UserID: "user2", SentAt: time.Now(), Status: "sent"})
	log.Record(EventLogEntry{ID: "e3", EventType: EventSubExpired, UserID: "user1", SentAt: time.Now(), Status: "sent"})

	entries := log.GetByUser("user1")
	if len(entries) != 2 {
		t.Errorf("expected 2 events for user1, got %d", len(entries))
	}
}

func TestEventLogGetByType(t *testing.T) {
	log := NewEventLog("")

	log.Record(EventLogEntry{ID: "e1", EventType: EventWelcome, SentAt: time.Now(), Status: "sent"})
	log.Record(EventLogEntry{ID: "e2", EventType: EventWelcome, SentAt: time.Now(), Status: "sent"})
	log.Record(EventLogEntry{ID: "e3", EventType: EventSubExpired, SentAt: time.Now(), Status: "sent"})

	entries := log.GetByType(EventWelcome)
	if len(entries) != 2 {
		t.Errorf("expected 2 welcome events, got %d", len(entries))
	}
}

func TestEventLogGetRecent(t *testing.T) {
	log := NewEventLog("")

	for i := 0; i < 5; i++ {
		log.Record(EventLogEntry{
			ID:        fmt.Sprintf("e%d", i),
			EventType: EventWelcome,
			SentAt:    time.Now(),
			Status:    "sent",
		})
	}

	recent := log.GetRecent(3)
	if len(recent) != 3 {
		t.Errorf("expected 3 recent, got %d", len(recent))
	}
	// Should be the last 3
	if recent[0].ID != "e2" {
		t.Errorf("expected e2, got %s", recent[0].ID)
	}
}

func TestEventLogPersistence(t *testing.T) {
	dir := testDir(t)
	log1 := NewEventLog(dir)

	// Insert 100 to trigger save (every 100)
	for i := 0; i < 100; i++ {
		log1.Record(EventLogEntry{
			ID:        fmt.Sprintf("e%d", i),
			EventType: EventWelcome,
			SentAt:    time.Now(),
			Status:    "sent",
		})
	}

	log2 := NewEventLog(dir)
	if log2.Len() != 100 {
		t.Errorf("expected 100 persisted events, got %d", log2.Len())
	}
}

func TestTemplateRenderWelcome(t *testing.T) {
	log := NewEventLog("")
	n := NewNotifier(NotifierConfig{}, log, testLogger())
	defer n.Stop()

	subject, body := n.RenderTemplate(EventWelcome, map[string]string{
		"name": "Alice",
		"plan": "Starter",
	})

	if !strings.Contains(subject, "Welcome") {
		t.Errorf("expected Welcome in subject, got: %s", subject)
	}
	if !strings.Contains(body, "Alice") {
		t.Errorf("expected Alice in body, got: %s", body)
	}
	if !strings.Contains(body, "Starter") {
		t.Errorf("expected Starter in body, got: %s", body)
	}
}

func TestTemplateRenderUsageWarning(t *testing.T) {
	log := NewEventLog("")
	n := NewNotifier(NotifierConfig{}, log, testLogger())
	defer n.Stop()

	subject, body := n.RenderTemplate(EventUsageWarning80, map[string]string{
		"usage": "800",
		"limit": "1000",
	})

	if !strings.Contains(subject, "80%") {
		t.Errorf("expected 80%% in subject, got: %s", subject)
	}
	if !strings.Contains(body, "800") {
		t.Errorf("expected 800 in body")
	}
}

func TestTemplateRenderPaymentFailed(t *testing.T) {
	log := NewEventLog("")
	n := NewNotifier(NotifierConfig{}, log, testLogger())
	defer n.Stop()

	subject, body := n.RenderTemplate(EventPaymentFailed, map[string]string{
		"plan": "Team",
	})

	if !strings.Contains(subject, "Payment Failed") {
		t.Errorf("expected 'Payment Failed' in subject, got: %s", subject)
	}
	if !strings.Contains(body, "Team") {
		t.Errorf("expected Team in body")
	}
}

func TestTemplateRenderDeviceLimit(t *testing.T) {
	log := NewEventLog("")
	n := NewNotifier(NotifierConfig{}, log, testLogger())
	defer n.Stop()

	subject, body := n.RenderTemplate(EventDeviceLimitHit, map[string]string{
		"max":   "3",
		"count": "3",
		"plan":  "Starter",
	})

	if !strings.Contains(subject, "Device Limit") {
		t.Errorf("expected 'Device Limit' in subject, got: %s", subject)
	}
	if !strings.Contains(body, "3") {
		t.Errorf("expected '3' in body")
	}
}

func TestNotificationQueuing(t *testing.T) {
	dir := testDir(t)
	log := NewEventLog(dir)
	n := NewNotifier(NotifierConfig{Enabled: false}, log, testLogger())
	defer n.Stop()

	// Non-blocking: should return immediately
	done := make(chan struct{})
	go func() {
		for i := 0; i < 10; i++ {
			n.SendNotification(EventWelcome, fmt.Sprintf("user%d@test.com", i), map[string]string{
				"name": fmt.Sprintf("User%d", i),
				"plan": "Free",
			})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SendNotification should be non-blocking")
	}

	// Wait for worker to process
	time.Sleep(500 * time.Millisecond)

	if log.Len() < 10 {
		t.Errorf("expected at least 10 logged events, got %d", log.Len())
	}
}

func TestBulkMarketing(t *testing.T) {
	log := NewEventLog("")
	n := NewNotifier(NotifierConfig{Enabled: false}, log, testLogger())
	defer n.Stop()

	emails := []string{"a@test.com", "b@test.com", "c@test.com"}
	n.SendBulkMarketing("New Feature!", "<p>Check out our new feature</p>", emails)

	time.Sleep(500 * time.Millisecond)

	entries := log.GetByType(EventMarketingUpdate)
	if len(entries) < 3 {
		t.Errorf("expected 3 marketing events, got %d", len(entries))
	}
}

func TestSMTPDisabledLogMode(t *testing.T) {
	log := NewEventLog("")
	n := NewNotifier(NotifierConfig{Enabled: false}, log, testLogger())
	defer n.Stop()

	n.SendNotification(EventWelcome, "test@test.com", map[string]string{"name": "Test", "plan": "Free"})
	time.Sleep(300 * time.Millisecond)

	entries := log.GetRecent(1)
	if len(entries) != 1 {
		t.Fatal("expected 1 event")
	}
	if entries[0].Status != "logged" {
		t.Errorf("expected 'logged' status in dev mode, got: %s", entries[0].Status)
	}
}

func TestTemplateRenderAllEventTypes(t *testing.T) {
	log := NewEventLog("")
	n := NewNotifier(NotifierConfig{}, log, testLogger())
	defer n.Stop()

	events := []EventType{
		EventWelcome, EventUsageWarning80, EventUsageWarning90,
		EventSubExpiring7d, EventSubExpiring3d, EventSubExpiring1d,
		EventSubExpired, EventKeyRevoked, EventPaymentFailed,
		EventPaymentSucceeded, EventDeviceLimitHit, EventMarketingUpdate,
	}

	data := map[string]string{
		"name": "Test", "plan": "Starter", "usage": "100", "limit": "1000",
		"expires": "2025-01-01", "key_prefix": "nxs_live_abc",
		"max": "3", "count": "3", "subject": "Update", "body": "<p>Hi</p>",
	}

	for _, event := range events {
		subject, body := n.RenderTemplate(event, data)
		if subject == "" {
			t.Errorf("empty subject for event %s", event)
		}
		if body == "" {
			t.Errorf("empty body for event %s", event)
		}
	}
}
