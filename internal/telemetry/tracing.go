package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// contextKey is a private type for tracing context keys.
type tracingContextKey struct{}

// spanContextKey stores the current span in context.
var spanContextKey = tracingContextKey{}

// Span represents a single unit of work in a distributed trace.
type Span struct {
	TraceID    string
	SpanID     string
	ParentID   string
	Name       string
	Service    string
	StartTime  time.Time
	EndTime    time.Time
	Duration   time.Duration
	Status     string // "ok", "error"
	Attributes map[string]string
	Events     []SpanEvent
}

// SpanEvent records a notable event during a span's lifetime.
type SpanEvent struct {
	Name       string
	Timestamp  time.Time
	Attributes map[string]string
}

// AddEvent appends a timestamped event to the span.
func (s *Span) AddEvent(name string, attrs map[string]string) {
	s.Events = append(s.Events, SpanEvent{
		Name:       name,
		Timestamp:  time.Now(),
		Attributes: attrs,
	})
}

// SetAttribute sets a key-value attribute on the span.
func (s *Span) SetAttribute(key, value string) {
	if s.Attributes == nil {
		s.Attributes = make(map[string]string)
	}
	s.Attributes[key] = value
}

// SetStatus sets the span status ("ok" or "error").
func (s *Span) SetStatus(status string) {
	s.Status = status
}

// TracerConfig controls the tracing system behavior.
type TracerConfig struct {
	Enabled     bool
	ServiceName string  // default: "nexus-gateway"
	SampleRate  float64 // 0.0-1.0, default: 1.0 (sample everything)
	ExportURL   string  // OTLP HTTP endpoint (optional)
	LogSpans    bool    // log spans as structured JSON (default: true)
}

// Tracer is the lightweight tracing engine.
type Tracer struct {
	config   TracerConfig
	exporter *SpanExporter
}

// NewTracer creates a Tracer with the given configuration.
func NewTracer(cfg TracerConfig) *Tracer {
	if cfg.ServiceName == "" {
		cfg.ServiceName = "nexus-gateway"
	}
	if cfg.SampleRate < 0 {
		cfg.SampleRate = 0
	}
	if cfg.SampleRate > 1.0 {
		cfg.SampleRate = 1.0
	}

	t := &Tracer{
		config: cfg,
	}

	if cfg.Enabled {
		t.exporter = NewSpanExporter(cfg)
	}

	return t
}

// StartSpan creates a new span and stores it in the context.
// If a parent span exists in the context, the new span becomes a child.
func (t *Tracer) StartSpan(ctx context.Context, name string) (context.Context, *Span) {
	if !t.config.Enabled {
		return ctx, &Span{Name: name, Attributes: make(map[string]string)}
	}

	if !t.shouldSample() {
		return ctx, &Span{Name: name, Attributes: make(map[string]string)}
	}

	span := &Span{
		SpanID:     generateSpanID(),
		Name:       name,
		Service:    t.config.ServiceName,
		StartTime:  time.Now(),
		Status:     "ok",
		Attributes: make(map[string]string),
	}

	// Link to parent span if present
	if parent := SpanFromContext(ctx); parent != nil && parent.TraceID != "" {
		span.TraceID = parent.TraceID
		span.ParentID = parent.SpanID
	} else {
		span.TraceID = generateTraceID()
	}

	return context.WithValue(ctx, spanContextKey, span), span
}

// EndSpan marks a span as complete and sends it to the exporter.
func (t *Tracer) EndSpan(span *Span) {
	if span == nil || span.TraceID == "" {
		return
	}
	span.EndTime = time.Now()
	span.Duration = span.EndTime.Sub(span.StartTime)

	if t.exporter != nil {
		t.exporter.Export(span)
	}
}

// Shutdown cleanly shuts down the exporter.
func (t *Tracer) Shutdown() {
	if t.exporter != nil {
		t.exporter.Shutdown()
	}
}

// SpanFromContext retrieves the current span from context, or nil.
func SpanFromContext(ctx context.Context) *Span {
	if ctx == nil {
		return nil
	}
	span, _ := ctx.Value(spanContextKey).(*Span)
	return span
}

// shouldSample returns true if this request should be sampled.
func (t *Tracer) shouldSample() bool {
	if t.config.SampleRate >= 1.0 {
		return true
	}
	if t.config.SampleRate <= 0.0 {
		return false
	}
	// Use crypto/rand for unbiased sampling
	n, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return true // sample on error
	}
	return n.Int64() < int64(t.config.SampleRate*10000)
}

// generateTraceID produces a W3C-compliant 32-hex-char trace ID.
func generateTraceID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return fmt.Sprintf("%032x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// generateSpanID produces a W3C-compliant 16-hex-char span ID.
func generateSpanID() string {
	b := make([]byte, 8)
	_, err := rand.Read(b)
	if err != nil {
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// ParseTraceparent parses a W3C traceparent header.
// Format: version-traceId-parentSpanId-traceFlags  (e.g. "00-abc...def-012...789-01")
// Returns traceID, parentSpanID, sampled flag, and whether parsing succeeded.
func ParseTraceparent(header string) (traceID, parentSpanID string, sampled bool, ok bool) {
	parts := strings.Split(header, "-")
	if len(parts) != 4 {
		return "", "", false, false
	}

	version := parts[0]
	traceID = parts[1]
	parentSpanID = parts[2]
	flags := parts[3]

	// Validate version (only "00" supported)
	if len(version) != 2 {
		return "", "", false, false
	}

	// Validate trace ID (32 hex chars, not all zeros)
	if len(traceID) != 32 || !isHex(traceID) || isAllZeros(traceID) {
		return "", "", false, false
	}

	// Validate span ID (16 hex chars, not all zeros)
	if len(parentSpanID) != 16 || !isHex(parentSpanID) || isAllZeros(parentSpanID) {
		return "", "", false, false
	}

	// Validate flags (2 hex chars)
	if len(flags) != 2 || !isHex(flags) {
		return "", "", false, false
	}

	// Bit 0 of flags = sampled
	sampled = flags[1] == '1' || flags[1] == '3' || flags[1] == '5' ||
		flags[1] == '7' || flags[1] == '9' || flags[1] == 'b' ||
		flags[1] == 'd' || flags[1] == 'f'

	return traceID, parentSpanID, sampled, true
}

// FormatTraceparent produces a W3C traceparent header value.
func FormatTraceparent(traceID, spanID string, sampled bool) string {
	flags := "00"
	if sampled {
		flags = "01"
	}
	return fmt.Sprintf("00-%s-%s-%s", traceID, spanID, flags)
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func isAllZeros(s string) bool {
	for _, c := range s {
		if c != '0' {
			return false
		}
	}
	return true
}
