package notification

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// EventLogEntry records a sent (or attempted) notification.
type EventLogEntry struct {
	ID        string            `json:"id"`
	EventType EventType         `json:"event_type"`
	UserID    string            `json:"user_id,omitempty"`
	Email     string            `json:"email"`
	Data      map[string]string `json:"data,omitempty"`
	SentAt    time.Time         `json:"sent_at"`
	Status    string            `json:"status"` // sent, failed, logged
}

const maxEventLogSize = 10000

// EventLog stores notification event history.
type EventLog struct {
	mu      sync.RWMutex
	entries []EventLogEntry
	dataDir string
}

// NewEventLog creates a new event log, loading existing entries from disk.
func NewEventLog(dataDir string) *EventLog {
	el := &EventLog{
		entries: make([]EventLogEntry, 0),
		dataDir: dataDir,
	}
	if dataDir != "" {
		_ = os.MkdirAll(dataDir, 0o755)
		_ = el.load()
	}
	return el
}

// Record adds an event to the log.
func (el *EventLog) Record(entry EventLogEntry) {
	el.mu.Lock()
	defer el.mu.Unlock()

	el.entries = append(el.entries, entry)

	// Trim to max size
	if len(el.entries) > maxEventLogSize {
		el.entries = el.entries[len(el.entries)-maxEventLogSize:]
	}

	// Periodic save
	if len(el.entries)%100 == 0 && el.dataDir != "" {
		_ = el.saveLocked()
	}
}

// GetByUser returns events for a specific user.
func (el *EventLog) GetByUser(userID string) []EventLogEntry {
	el.mu.RLock()
	defer el.mu.RUnlock()
	var result []EventLogEntry
	for _, e := range el.entries {
		if e.UserID == userID {
			result = append(result, e)
		}
	}
	return result
}

// GetByType returns events of a specific type.
func (el *EventLog) GetByType(eventType EventType) []EventLogEntry {
	el.mu.RLock()
	defer el.mu.RUnlock()
	var result []EventLogEntry
	for _, e := range el.entries {
		if e.EventType == eventType {
			result = append(result, e)
		}
	}
	return result
}

// GetRecent returns the most recent N events.
func (el *EventLog) GetRecent(n int) []EventLogEntry {
	el.mu.RLock()
	defer el.mu.RUnlock()
	if n > len(el.entries) {
		n = len(el.entries)
	}
	result := make([]EventLogEntry, n)
	copy(result, el.entries[len(el.entries)-n:])
	return result
}

// Len returns the number of events.
func (el *EventLog) Len() int {
	el.mu.RLock()
	defer el.mu.RUnlock()
	return len(el.entries)
}

// Save forces a persist to disk.
func (el *EventLog) Save() error {
	el.mu.Lock()
	defer el.mu.Unlock()
	return el.saveLocked()
}

func (el *EventLog) load() error {
	path := filepath.Join(el.dataDir, "event_log.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(data, &el.entries)
}

func (el *EventLog) saveLocked() error {
	data, err := json.MarshalIndent(el.entries, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(el.dataDir, "event_log.json"), data)
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp := filepath.Join(dir, ".tmp_"+filepath.Base(path))
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
