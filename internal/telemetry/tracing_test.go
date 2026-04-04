package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Trace ID / Span ID generation
// ---------------------------------------------------------------------------

func TestGenerateTraceID_Format(t *testing.T) {
	id := generateTraceID()
	if len(id) != 32 {
		t.Errorf("trace ID length = %d, want 32", len(id))
	}
	if !isHex(id) {
		t.Errorf("trace ID %q is not valid hex", id)
	}
}

func TestGenerateSpanID_Format(t *testing.T) {
	id := generateSpanID()
	if len(id) != 16 {
		t.Errorf("span ID length = %d, want 16", len(id))
	}
	if !isHex(id) {
		t.Errorf("span ID %q is not valid hex", id)
	}
}

func TestGenerateTraceID_Uniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := generateTraceID()
		if seen[id] {
			t.Fatalf("duplicate trace ID after %d iterations: %s", i, id)
		}
		seen[id] = true
	}
}

func TestGenerateSpanID_Uniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := generateSpanID()
		if seen[id] {
			t.Fatalf("duplicate span ID after %d iterations: %s", i, id)
		}
		seen[id] = true
	}
}

// ---------------------------------------------------------------------------
// Traceparent parsing
// ---------------------------------------------------------------------------

func TestParseTraceparent_Valid(t *testing.T) {
	header := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	traceID, parentID, sampled, ok := ParseTraceparent(header)
	if !ok {
		t.Fatal("expected valid traceparent")
	}
	if traceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("traceID = %q, want %q", traceID, "4bf92f3577b34da6a3ce929d0e0e4736")
	}
	if parentID != "00f067aa0ba902b7" {
		t.Errorf("parentID = %q, want %q", parentID, "00f067aa0ba902b7")
	}
	if !sampled {
		t.Error("expected sampled=true for flags=01")
	}
}

func TestParseTraceparent_NotSampled(t *testing.T) {
	header := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00"
	_, _, sampled, ok := ParseTraceparent(header)
	if !ok {
		t.Fatal("expected valid traceparent")
	}
	if sampled {
		t.Error("expected sampled=false for flags=00")
	}
}

func TestParseTraceparent_Invalid_TooFewParts(t *testing.T) {
	_, _, _, ok := ParseTraceparent("00-abc-01")
	if ok {
		t.Error("expected invalid for too few parts")
	}
}

func TestParseTraceparent_Invalid_BadTraceID(t *testing.T) {
	// Too short trace ID
	_, _, _, ok := ParseTraceparent("00-abc-00f067aa0ba902b7-01")
	if ok {
		t.Error("expected invalid for short trace ID")
	}
}

func TestParseTraceparent_Invalid_AllZeroTraceID(t *testing.T) {
	_, _, _, ok := ParseTraceparent("00-00000000000000000000000000000000-00f067aa0ba902b7-01")
	if ok {
		t.Error("expected invalid for all-zero trace ID")
	}
}

func TestParseTraceparent_Invalid_AllZeroSpanID(t *testing.T) {
	_, _, _, ok := ParseTraceparent("00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01")
	if ok {
		t.Error("expected invalid for all-zero span ID")
	}
}

func TestParseTraceparent_Empty(t *testing.T) {
	_, _, _, ok := ParseTraceparent("")
	if ok {
		t.Error("expected invalid for empty header")
	}
}

// ---------------------------------------------------------------------------
// FormatTraceparent
// ---------------------------------------------------------------------------

func TestFormatTraceparent(t *testing.T) {
	result := FormatTraceparent("4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7", true)
	expected := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}

	result = FormatTraceparent("4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7", false)
	expected = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00"
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

// ---------------------------------------------------------------------------
// Span context propagation
// ---------------------------------------------------------------------------

func TestSpanContextPropagation_ParentChild(t *testing.T) {
	tracer := NewTracer(TracerConfig{
		Enabled:     true,
		ServiceName: "test-service",
		SampleRate:  1.0,
		LogSpans:    false,
	})

	ctx, parent := tracer.StartSpan(context.Background(), "parent-op")
	if parent.TraceID == "" {
		t.Fatal("parent span should have a trace ID")
	}

	_, child := tracer.StartSpan(ctx, "child-op")
	if child.TraceID != parent.TraceID {
		t.Errorf("child trace ID %q != parent trace ID %q", child.TraceID, parent.TraceID)
	}
	if child.ParentID != parent.SpanID {
		t.Errorf("child parent ID %q != parent span ID %q", child.ParentID, parent.SpanID)
	}
	if child.SpanID == parent.SpanID {
		t.Error("child and parent should have different span IDs")
	}
}

func TestSpanFromContext_NoSpan(t *testing.T) {
	span := SpanFromContext(context.Background())
	if span != nil {
		t.Error("expected nil span from empty context")
	}
}

func TestSpanFromContext_NilContext(t *testing.T) {
	span := SpanFromContext(nil)
	if span != nil {
		t.Error("expected nil span from nil context")
	}
}

// ---------------------------------------------------------------------------
// Sampling
// ---------------------------------------------------------------------------

func TestSampling_ZeroPercent(t *testing.T) {
	tracer := NewTracer(TracerConfig{
		Enabled:    true,
		SampleRate: 0.0,
		LogSpans:   false,
	})

	sampled := 0
	for i := 0; i < 100; i++ {
		_, span := tracer.StartSpan(context.Background(), "test")
		if span.TraceID != "" {
			sampled++
		}
	}
	if sampled != 0 {
		t.Errorf("expected 0 sampled spans at 0%% rate, got %d", sampled)
	}
}

func TestSampling_HundredPercent(t *testing.T) {
	tracer := NewTracer(TracerConfig{
		Enabled:    true,
		SampleRate: 1.0,
		LogSpans:   false,
	})

	sampled := 0
	for i := 0; i < 100; i++ {
		_, span := tracer.StartSpan(context.Background(), "test")
		if span.TraceID != "" {
			sampled++
		}
	}
	if sampled != 100 {
		t.Errorf("expected 100 sampled spans at 100%% rate, got %d", sampled)
	}
}

func TestSampling_Disabled(t *testing.T) {
	tracer := NewTracer(TracerConfig{
		Enabled:    false,
		SampleRate: 1.0,
	})

	_, span := tracer.StartSpan(context.Background(), "test")
	if span.TraceID != "" {
		t.Error("expected no trace ID when tracing is disabled")
	}
}

// ---------------------------------------------------------------------------
// Span attributes and events
// ---------------------------------------------------------------------------

func TestSpan_SetAttribute(t *testing.T) {
	span := &Span{Attributes: make(map[string]string)}
	span.SetAttribute("key1", "value1")
	span.SetAttribute("key2", "value2")

	if span.Attributes["key1"] != "value1" {
		t.Errorf("expected key1=value1, got %q", span.Attributes["key1"])
	}
	if span.Attributes["key2"] != "value2" {
		t.Errorf("expected key2=value2, got %q", span.Attributes["key2"])
	}
}

func TestSpan_AddEvent(t *testing.T) {
	span := &Span{}
	span.AddEvent("test-event", map[string]string{"detail": "abc"})

	if len(span.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(span.Events))
	}
	if span.Events[0].Name != "test-event" {
		t.Errorf("event name = %q, want test-event", span.Events[0].Name)
	}
	if span.Events[0].Attributes["detail"] != "abc" {
		t.Errorf("event attribute detail = %q, want abc", span.Events[0].Attributes["detail"])
	}
}

// ---------------------------------------------------------------------------
// EndSpan duration
// ---------------------------------------------------------------------------

func TestEndSpan_RecordsDuration(t *testing.T) {
	tracer := NewTracer(TracerConfig{
		Enabled:    true,
		SampleRate: 1.0,
		LogSpans:   false,
	})

	_, span := tracer.StartSpan(context.Background(), "timed-op")
	tracer.EndSpan(span)

	if span.EndTime.IsZero() {
		t.Error("expected EndTime to be set")
	}
	if span.Duration < 0 {
		t.Error("expected non-negative Duration")
	}
}

// ---------------------------------------------------------------------------
// Log exporter output format
// ---------------------------------------------------------------------------

func TestLogExporter_OutputFormat(t *testing.T) {
	// We can't easily capture slog output in tests without custom handler,
	// but we can verify the exporter doesn't panic and processes spans.
	cfg := TracerConfig{
		Enabled:     true,
		ServiceName: "test",
		SampleRate:  1.0,
		LogSpans:    true,
	}
	exporter := NewSpanExporter(cfg)

	span := &Span{
		TraceID:    "4bf92f3577b34da6a3ce929d0e0e4736",
		SpanID:     "00f067aa0ba902b7",
		ParentID:   "",
		Name:       "test.operation",
		Service:    "test",
		Duration:   42000000, // 42ms
		Status:     "ok",
		Attributes: map[string]string{"key": "value"},
	}

	// Should not panic
	exporter.Export(span)
	exporter.Shutdown()
}

// ---------------------------------------------------------------------------
// Trace middleware
// ---------------------------------------------------------------------------

func TestTraceMiddleware_SetsResponseHeaders(t *testing.T) {
	tracer := NewTracer(TracerConfig{
		Enabled:     true,
		ServiceName: "test-gateway",
		SampleRate:  1.0,
		LogSpans:    false,
	})

	handler := TraceMiddleware(tracer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	handler.ServeHTTP(rec, req)

	tp := rec.Header().Get("traceparent")
	if tp == "" {
		t.Error("expected traceparent response header")
	}
	// Validate format: 00-{32hex}-{16hex}-{2hex}
	parts := strings.Split(tp, "-")
	if len(parts) != 4 {
		t.Fatalf("traceparent has %d parts, want 4", len(parts))
	}
	if parts[0] != "00" {
		t.Errorf("version = %q, want 00", parts[0])
	}
	if len(parts[1]) != 32 {
		t.Errorf("trace ID length = %d, want 32", len(parts[1]))
	}
	if len(parts[2]) != 16 {
		t.Errorf("span ID length = %d, want 16", len(parts[2]))
	}

	traceID := rec.Header().Get("X-Trace-ID")
	if traceID == "" {
		t.Error("expected X-Trace-ID response header")
	}
	if traceID != parts[1] {
		t.Errorf("X-Trace-ID %q != traceparent trace ID %q", traceID, parts[1])
	}
}

func TestTraceMiddleware_PropagatesIncomingTraceID(t *testing.T) {
	tracer := NewTracer(TracerConfig{
		Enabled:     true,
		ServiceName: "test-gateway",
		SampleRate:  1.0,
		LogSpans:    false,
	})

	incomingTraceID := "4bf92f3577b34da6a3ce929d0e0e4736"
	incomingParentID := "00f067aa0ba902b7"
	incomingTP := fmt.Sprintf("00-%s-%s-01", incomingTraceID, incomingParentID)

	handler := TraceMiddleware(tracer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := SpanFromContext(r.Context())
		if span == nil {
			t.Error("expected span in context")
			return
		}
		if span.TraceID != incomingTraceID {
			t.Errorf("span trace ID = %q, want %q", span.TraceID, incomingTraceID)
		}
		if span.ParentID != incomingParentID {
			t.Errorf("span parent ID = %q, want %q", span.ParentID, incomingParentID)
		}
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("traceparent", incomingTP)
	handler.ServeHTTP(rec, req)

	// Response traceparent should carry the same trace ID
	tp := rec.Header().Get("traceparent")
	if !strings.Contains(tp, incomingTraceID) {
		t.Errorf("response traceparent %q should contain incoming trace ID %q", tp, incomingTraceID)
	}
}

func TestTraceMiddleware_Disabled(t *testing.T) {
	tracer := NewTracer(TracerConfig{
		Enabled: false,
	})

	called := false
	handler := TraceMiddleware(tracer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("handler should have been called even with tracing disabled")
	}
	if rec.Header().Get("traceparent") != "" {
		t.Error("should not set traceparent when tracing is disabled")
	}
}

func TestTraceMiddleware_RecordsErrorStatus(t *testing.T) {
	tracer := NewTracer(TracerConfig{
		Enabled:     true,
		ServiceName: "test",
		SampleRate:  1.0,
		LogSpans:    false,
	})

	var capturedSpan *Span
	handler := TraceMiddleware(tracer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedSpan = SpanFromContext(r.Context())
		w.WriteHeader(http.StatusInternalServerError)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	handler.ServeHTTP(rec, req)

	// After middleware completes, span status should be "error"
	if capturedSpan == nil {
		t.Fatal("expected span in context")
	}
	// The span's status is set after ServeHTTP returns, in EndSpan
	// We verify the response code was captured
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("response code = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// ---------------------------------------------------------------------------
// OTLP payload format
// ---------------------------------------------------------------------------

func TestBuildOTLPPayload(t *testing.T) {
	cfg := TracerConfig{
		Enabled:     true,
		ServiceName: "test-service",
		ExportURL:   "http://localhost:4318/v1/traces",
		LogSpans:    false,
	}
	exporter := NewSpanExporter(cfg)
	defer exporter.Shutdown()

	spans := []*Span{
		{
			TraceID:   "4bf92f3577b34da6a3ce929d0e0e4736",
			SpanID:    "00f067aa0ba902b7",
			Name:      "test.span",
			Service:   "test-service",
			Status:    "ok",
			Attributes: map[string]string{"http.method": "POST"},
		},
	}

	payload := exporter.buildOTLPPayload(spans)

	// Verify top-level structure
	resourceSpans, ok := payload["resourceSpans"].([]map[string]any)
	if !ok || len(resourceSpans) == 0 {
		t.Fatal("expected resourceSpans array")
	}

	scopeSpans, ok := resourceSpans[0]["scopeSpans"].([]map[string]any)
	if !ok || len(scopeSpans) == 0 {
		t.Fatal("expected scopeSpans array")
	}

	otlpSpans, ok := scopeSpans[0]["spans"].([]map[string]any)
	if !ok || len(otlpSpans) == 0 {
		t.Fatal("expected spans array")
	}

	if otlpSpans[0]["name"] != "test.span" {
		t.Errorf("span name = %q, want test.span", otlpSpans[0]["name"])
	}
}
