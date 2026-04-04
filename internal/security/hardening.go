package security

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

// IPAllowlistConfig configures IP-based access control for admin endpoints.
type IPAllowlistConfig struct {
	Enabled    bool
	AllowedIPs []string // CIDR notation: "10.0.0.0/8", "192.168.1.0/24"
	Paths      []string // paths to protect: "/api/admin/", "/api/synonyms/"
}

// BodySizeLimit prevents DDoS via oversized payloads.
// Returns 413 Request Entity Too Large if the body exceeds maxBytes.
func BodySizeLimit(maxBytes int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > maxBytes {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				w.Write([]byte(`{"error":"request body too large"}`))
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// RequestTimeout prevents slowloris and hung requests by applying a
// context deadline to every inbound request.
func RequestTimeout(timeout time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// PanicRecovery catches panics in downstream handlers, logs the full stack
// trace at ERROR level, and returns a generic 500 to the client.
func PanicRecovery(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					stack := debug.Stack()
					logger.Error("panic recovered",
						"error", fmt.Sprintf("%v", err),
						"stack", string(stack),
						"method", r.Method,
						"path", r.URL.Path,
					)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(`{"error":"internal server error"}`))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// IPAllowlist protects specified path prefixes with IP-based access control.
// CIDRs are parsed once at init time for efficiency.
func IPAllowlist(cfg IPAllowlistConfig) Middleware {
	var networks []*net.IPNet
	if cfg.Enabled {
		for _, cidr := range cfg.AllowedIPs {
			_, network, err := net.ParseCIDR(cidr)
			if err != nil {
				// Try as bare IP address.
				ip := net.ParseIP(cidr)
				if ip != nil {
					mask := net.CIDRMask(32, 32)
					if ip.To4() == nil {
						mask = net.CIDRMask(128, 128)
					}
					network = &net.IPNet{IP: ip.Mask(mask), Mask: mask}
				}
			}
			if network != nil {
				networks = append(networks, network)
			}
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			protected := false
			for _, prefix := range cfg.Paths {
				if strings.HasPrefix(r.URL.Path, prefix) {
					protected = true
					break
				}
			}

			if !protected {
				next.ServeHTTP(w, r)
				return
			}

			clientIP := extractClientIP(r)
			ip := net.ParseIP(clientIP)
			if ip != nil {
				for _, network := range networks {
					if network.Contains(ip) {
						next.ServeHTTP(w, r)
						return
					}
				}
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"forbidden"}`))
		})
	}
}

// InputValidator validates the chat request JSON schema.
// Only applies to POST /v1/chat/completions; all other endpoints pass through.
func InputValidator() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
				next.ServeHTTP(w, r)
				return
			}

			body, err := io.ReadAll(r.Body)
			r.Body.Close()
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":"failed to read request body"}`))
				return
			}

			var payload map[string]interface{}
			if err := json.Unmarshal(body, &payload); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":"invalid JSON in request body"}`))
				return
			}

			msgs, ok := payload["messages"]
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":"missing required field: messages"}`))
				return
			}

			msgArray, ok := msgs.([]interface{})
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":"messages must be an array"}`))
				return
			}

			for i, msg := range msgArray {
				m, ok := msg.(map[string]interface{})
				if !ok {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					w.Write([]byte(fmt.Sprintf(`{"error":"messages[%d] must be an object"}`, i)))
					return
				}

				role, exists := m["role"]
				if !exists {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					w.Write([]byte(fmt.Sprintf(`{"error":"messages[%d] missing required field: role"}`, i)))
					return
				}
				if _, ok := role.(string); !ok {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					w.Write([]byte(fmt.Sprintf(`{"error":"messages[%d].role must be a string"}`, i)))
					return
				}

				content, exists := m["content"]
				if !exists {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					w.Write([]byte(fmt.Sprintf(`{"error":"messages[%d] missing required field: content"}`, i)))
					return
				}
				if _, ok := content.(string); !ok {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					w.Write([]byte(fmt.Sprintf(`{"error":"messages[%d].content must be a string"}`, i)))
					return
				}
			}

			if model, ok := payload["model"]; ok {
				if _, ok := model.(string); !ok {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					w.Write([]byte(`{"error":"model must be a string"}`))
					return
				}
			}

			// Re-set body for downstream handlers.
			r.Body = io.NopCloser(bytes.NewReader(body))
			next.ServeHTTP(w, r)
		})
	}
}

// responseCapture wraps ResponseWriter to capture the status code for logging.
type responseCapture struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (rc *responseCapture) WriteHeader(code int) {
	if !rc.wroteHeader {
		rc.status = code
		rc.wroteHeader = true
	}
	rc.ResponseWriter.WriteHeader(code)
}

func (rc *responseCapture) Write(b []byte) (int, error) {
	if !rc.wroteHeader {
		rc.status = http.StatusOK
		rc.wroteHeader = true
	}
	return rc.ResponseWriter.Write(b)
}

// RequestLogger provides structured request/response logging with
// sanitized authorization headers.
func RequestLogger(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rc := &responseCapture{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rc, r)

			latency := time.Since(start)
			reqID, _ := r.Context().Value(ContextKeyReqID).(string)
			clientIP := extractClientIP(r)

			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", rc.status,
				"latency_ms", latency.Milliseconds(),
				"client_ip", clientIP,
				"user_agent", r.UserAgent(),
				"request_id", reqID,
			}

			if auth := r.Header.Get("Authorization"); auth != "" {
				if strings.HasPrefix(auth, "Bearer ") {
					attrs = append(attrs, "authorization", "Bearer ***")
				} else {
					attrs = append(attrs, "authorization", "***")
				}
			}

			switch {
			case rc.status >= 500:
				logger.Error("request completed", attrs...)
			case rc.status >= 400:
				logger.Warn("request completed", attrs...)
			default:
				logger.Info("request completed", attrs...)
			}
		})
	}
}

// extractClientIP gets the client IP from X-Forwarded-For or RemoteAddr.
func extractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
