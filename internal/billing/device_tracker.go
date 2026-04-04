package billing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Device represents a tracked device for a user.
type Device struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	Fingerprint  string    `json:"fingerprint"`
	FirstSeen    time.Time `json:"first_seen"`
	LastSeen     time.Time `json:"last_seen"`
	RequestCount int64     `json:"request_count"`
}

// DeviceLimitResult contains the result of a device limit check.
type DeviceLimitResult struct {
	Allowed bool `json:"allowed"`
	Count   int  `json:"count"`
	Max     int  `json:"max"`
}

// DeviceTracker tracks unique devices per user.
type DeviceTracker struct {
	mu      sync.RWMutex
	devices map[string]*Device // keyed by Device.ID
	dataDir string
	logger  *slog.Logger
}

// NewDeviceTracker creates a new device tracker.
func NewDeviceTracker(dataDir string, logger *slog.Logger) *DeviceTracker {
	dt := &DeviceTracker{
		devices: make(map[string]*Device),
		dataDir: dataDir,
		logger:  logger,
	}
	_ = dt.load()
	return dt
}

// RecordDevice extracts a device fingerprint from a request and tracks it.
func (dt *DeviceTracker) RecordDevice(userID string, r *http.Request) string {
	fp := buildFingerprint(r)
	hash := sha256.Sum256([]byte(fp))
	deviceID := hex.EncodeToString(hash[:])

	dt.mu.Lock()
	defer dt.mu.Unlock()

	now := time.Now()
	if dev, ok := dt.devices[deviceID]; ok {
		dev.LastSeen = now
		dev.RequestCount++
	} else {
		dt.devices[deviceID] = &Device{
			ID:           deviceID,
			UserID:       userID,
			Fingerprint:  fp,
			FirstSeen:    now,
			LastSeen:     now,
			RequestCount: 1,
		}
	}

	// Periodic save
	if dt.devices[deviceID].RequestCount%50 == 0 {
		_ = dt.saveLocked()
	}

	return deviceID
}

// GetDeviceCount returns the number of unique devices for a user seen in the last 30 days.
func (dt *DeviceTracker) GetDeviceCount(userID string) int {
	dt.mu.RLock()
	defer dt.mu.RUnlock()
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	count := 0
	for _, dev := range dt.devices {
		if dev.UserID == userID && dev.LastSeen.After(cutoff) {
			count++
		}
	}
	return count
}

// CheckDeviceLimit checks if a user is within their device limit.
func (dt *DeviceTracker) CheckDeviceLimit(userID string, maxDevices int) DeviceLimitResult {
	if maxDevices == 0 {
		return DeviceLimitResult{Allowed: true, Count: dt.GetDeviceCount(userID), Max: 0}
	}
	count := dt.GetDeviceCount(userID)
	return DeviceLimitResult{
		Allowed: count <= maxDevices,
		Count:   count,
		Max:     maxDevices,
	}
}

// ListByUser returns all devices for a user.
func (dt *DeviceTracker) ListByUser(userID string) []*Device {
	dt.mu.RLock()
	defer dt.mu.RUnlock()
	var result []*Device
	for _, dev := range dt.devices {
		if dev.UserID == userID {
			copy := *dev
			result = append(result, &copy)
		}
	}
	return result
}

// CleanStale removes devices not seen in the given duration.
func (dt *DeviceTracker) CleanStale(olderThan time.Duration) int {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	cutoff := time.Now().Add(-olderThan)
	removed := 0
	for id, dev := range dt.devices {
		if dev.LastSeen.Before(cutoff) {
			delete(dt.devices, id)
			removed++
		}
	}
	if removed > 0 {
		_ = dt.saveLocked()
	}
	return removed
}

// Save forces a persist to disk.
func (dt *DeviceTracker) Save() error {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	return dt.saveLocked()
}

func buildFingerprint(r *http.Request) string {
	ua := r.UserAgent()
	ip := extractIP(r)
	// Use first 3 octets of IP (ignore last for DHCP)
	ipPrefix := truncateIP(ip)
	return fmt.Sprintf("%s|%s", ua, ipPrefix)
}

func extractIP(r *http.Request) string {
	// Check forwarded headers first
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func truncateIP(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ip
	}
	if v4 := parsed.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.0", v4[0], v4[1], v4[2])
	}
	// For IPv6, use first 6 groups
	return ip
}

func (dt *DeviceTracker) load() error {
	path := filepath.Join(dt.dataDir, "devices.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var devices []*Device
	if err := json.Unmarshal(data, &devices); err != nil {
		return err
	}
	for _, dev := range devices {
		dt.devices[dev.ID] = dev
	}
	return nil
}

func (dt *DeviceTracker) saveLocked() error {
	devices := make([]*Device, 0, len(dt.devices))
	for _, dev := range dt.devices {
		devices = append(devices, dev)
	}
	data, err := json.MarshalIndent(devices, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dt.dataDir, "devices.json"), data)
}
