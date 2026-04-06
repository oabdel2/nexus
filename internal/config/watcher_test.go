package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func writeTestConfig(t *testing.T, path string, port int) {
	t.Helper()
	content := []byte("server:\n  port: " + itoa(port) + "\n")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

func TestWatcher_DetectsChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nexus.yaml")
	writeTestConfig(t, path, 8080)

	var mu sync.Mutex
	var got *Config

	w := NewConfigWatcher(path, 50*time.Millisecond, func(cfg *Config) {
		mu.Lock()
		got = cfg
		mu.Unlock()
	})
	w.Start()
	defer w.Stop()

	// Wait for initial poll to settle
	time.Sleep(100 * time.Millisecond)

	// Modify file
	writeTestConfig(t, path, 9090)

	// Wait for detection
	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		if got != nil && got.Server.Port == 9090 {
			mu.Unlock()
			return // success
		}
		mu.Unlock()
		select {
		case <-deadline:
			t.Fatal("timed out waiting for config change detection")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func TestWatcher_NoFalsePositive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nexus.yaml")
	writeTestConfig(t, path, 8080)

	callCount := 0
	var mu sync.Mutex

	w := NewConfigWatcher(path, 50*time.Millisecond, func(cfg *Config) {
		mu.Lock()
		callCount++
		mu.Unlock()
	})
	w.Start()
	defer w.Stop()

	// Let several poll cycles pass without changing the file
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if callCount != 0 {
		t.Fatalf("expected 0 callbacks on unchanged file, got %d", callCount)
	}
}

func TestWatcher_InvalidConfigKeepsOld(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nexus.yaml")
	writeTestConfig(t, path, 8080)

	var mu sync.Mutex
	var callbackCfg *Config

	w := NewConfigWatcher(path, 50*time.Millisecond, func(cfg *Config) {
		mu.Lock()
		callbackCfg = cfg
		mu.Unlock()
	})
	w.Start()
	defer w.Stop()

	time.Sleep(100 * time.Millisecond)

	// Write invalid YAML
	os.WriteFile(path, []byte("{{{{invalid yaml!!!!"), 0644)

	time.Sleep(300 * time.Millisecond)

	// Callback should NOT have been called with invalid config
	mu.Lock()
	defer mu.Unlock()
	if callbackCfg != nil {
		t.Fatal("callback should not fire for invalid config")
	}

	// Current() should still return the original config
	cur := w.Current()
	if cur == nil || cur.Server.Port != 8080 {
		t.Fatal("old config should be preserved after invalid update")
	}
}

func TestWatcher_CallbackFires(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nexus.yaml")
	writeTestConfig(t, path, 8080)

	fired := make(chan *Config, 1)

	w := NewConfigWatcher(path, 50*time.Millisecond, func(cfg *Config) {
		select {
		case fired <- cfg:
		default:
		}
	})
	w.Start()
	defer w.Stop()

	time.Sleep(100 * time.Millisecond)
	writeTestConfig(t, path, 3000)

	select {
	case cfg := <-fired:
		if cfg.Server.Port != 3000 {
			t.Fatalf("expected port 3000, got %d", cfg.Server.Port)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("callback did not fire")
	}
}

func TestWatcher_StopIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nexus.yaml")
	writeTestConfig(t, path, 8080)

	w := NewConfigWatcher(path, 50*time.Millisecond, func(cfg *Config) {})
	w.Start()
	w.Stop()
	w.Stop() // should not panic
}
