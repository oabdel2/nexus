package config

import (
	"crypto/sha256"
	"log/slog"
	"os"
	"reflect"
	"sync"
	"time"
)

// ConfigWatcher polls a config file for changes and triggers a callback.
type ConfigWatcher struct {
	path     string
	lastMod  time.Time
	lastHash [32]byte
	onChange func(*Config)
	interval time.Duration
	stop     chan struct{}
	mu       sync.RWMutex
	current  *Config
	logger   *slog.Logger
}

// NewConfigWatcher creates a watcher that polls path every interval.
func NewConfigWatcher(path string, interval time.Duration, onChange func(*Config)) *ConfigWatcher {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	w := &ConfigWatcher{
		path:     path,
		onChange: onChange,
		interval: interval,
		stop:     make(chan struct{}),
		logger:   slog.Default(),
	}
	// Snapshot initial hash
	if data, err := os.ReadFile(path); err == nil {
		w.lastHash = sha256.Sum256(data)
		if info, err := os.Stat(path); err == nil {
			w.lastMod = info.ModTime()
		}
		if cfg, err := Load(path); err == nil {
			w.current = cfg
		}
	}
	return w
}

// Current returns the most recently loaded config (thread-safe).
func (w *ConfigWatcher) Current() *Config {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.current
}

// Start begins polling in a goroutine. Returns immediately.
func (w *ConfigWatcher) Start() {
	go w.poll()
}

// Stop signals the watcher to stop.
func (w *ConfigWatcher) Stop() {
	select {
	case <-w.stop:
		// already stopped
	default:
		close(w.stop)
	}
}

func (w *ConfigWatcher) poll() {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
			w.check()
		}
	}
}

func (w *ConfigWatcher) check() {
	// Quick modtime check to skip reads when nothing changed
	info, err := os.Stat(w.path)
	if err != nil {
		w.logger.Warn("config watcher: cannot stat file", "path", w.path, "error", err)
		return
	}
	if info.ModTime().Equal(w.lastMod) {
		return
	}

	data, err := os.ReadFile(w.path)
	if err != nil {
		w.logger.Warn("config watcher: cannot read file", "path", w.path, "error", err)
		return
	}

	hash := sha256.Sum256(data)
	if hash == w.lastHash {
		w.lastMod = info.ModTime()
		return
	}

	// Content changed — parse and validate
	newCfg, err := Load(w.path)
	if err != nil {
		w.logger.Error("config watcher: invalid config, keeping old", "path", w.path, "error", err)
		return
	}

	// Log what sections changed
	w.mu.RLock()
	old := w.current
	w.mu.RUnlock()
	if old != nil {
		w.logChanges(old, newCfg)
	}

	w.mu.Lock()
	w.current = newCfg
	w.lastHash = hash
	w.lastMod = info.ModTime()
	w.mu.Unlock()

	w.logger.Info("config watcher: config reloaded", "path", w.path)

	if w.onChange != nil {
		w.onChange(newCfg)
	}
}

func (w *ConfigWatcher) logChanges(old, new *Config) {
	oldV := reflect.ValueOf(*old)
	newV := reflect.ValueOf(*new)
	t := oldV.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !reflect.DeepEqual(oldV.Field(i).Interface(), newV.Field(i).Interface()) {
			w.logger.Info("config watcher: section changed", "section", field.Name)
		}
	}
}
