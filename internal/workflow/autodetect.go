package workflow

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

// AutoDetector infers workflow boundaries from request patterns
type AutoDetector struct {
	mu       sync.RWMutex
	sessions map[string]*session // keyed by fingerprint
	window   time.Duration       // clustering window (default 30s)
	maxAge   time.Duration       // session expiry (default 5min)
}

type session struct {
	WorkflowID  string
	Step        int
	LastSeen    time.Time
	Fingerprint string
}

// NewAutoDetector creates a new auto-detector with the given window
func NewAutoDetector(window, maxAge time.Duration) *AutoDetector {
	if window == 0 {
		window = 30 * time.Second
	}
	if maxAge == 0 {
		maxAge = 5 * time.Minute
	}
	ad := &AutoDetector{
		sessions: make(map[string]*session),
		window:   window,
		maxAge:   maxAge,
	}
	go ad.cleanup()
	return ad
}

// Detect tries to match a request to an existing workflow or creates a new one.
// It uses a fingerprint derived from the API key, system prompt, and source IP.
// Returns the inferred workflow ID and step number.
func (ad *AutoDetector) Detect(apiKey, systemPrompt, sourceIP, userAgent string) (workflowID string, step int) {
	fp := ad.fingerprint(apiKey, systemPrompt, sourceIP)

	ad.mu.Lock()
	defer ad.mu.Unlock()

	s, exists := ad.sessions[fp]
	now := time.Now()

	if exists && now.Sub(s.LastSeen) <= ad.window {
		// Same workflow — increment step
		s.Step++
		s.LastSeen = now
		return s.WorkflowID, s.Step
	}

	// New workflow
	wfID := fmt.Sprintf("auto-%s", fp[:12])
	ad.sessions[fp] = &session{
		WorkflowID:  wfID,
		Step:        1,
		LastSeen:    now,
		Fingerprint: fp,
	}
	return wfID, 1
}

// fingerprint creates a stable hash from request metadata
func (ad *AutoDetector) fingerprint(apiKey, systemPrompt, sourceIP string) string {
	h := sha256.New()
	h.Write([]byte(apiKey))
	h.Write([]byte("|"))
	// Use first 200 chars of system prompt (captures intent without being too specific)
	if len(systemPrompt) > 200 {
		systemPrompt = systemPrompt[:200]
	}
	h.Write([]byte(systemPrompt))
	h.Write([]byte("|"))
	h.Write([]byte(sourceIP))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// cleanup removes expired sessions periodically
func (ad *AutoDetector) cleanup() {
	ticker := time.NewTicker(ad.maxAge)
	defer ticker.Stop()
	for range ticker.C {
		ad.mu.Lock()
		now := time.Now()
		for fp, s := range ad.sessions {
			if now.Sub(s.LastSeen) > ad.maxAge {
				delete(ad.sessions, fp)
			}
		}
		ad.mu.Unlock()
	}
}

// Stats returns current session tracking stats
func (ad *AutoDetector) Stats() (activeSessions int, totalDetected int) {
	ad.mu.RLock()
	defer ad.mu.RUnlock()
	total := 0
	for _, s := range ad.sessions {
		total += s.Step
	}
	return len(ad.sessions), total
}
