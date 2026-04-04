package telemetry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const (
	exportBatchSize  = 100
	exportFlushDelay = 5 * time.Second
	exportChanSize   = 1024
)

// SpanExporter handles exporting completed spans.
type SpanExporter struct {
	config  TracerConfig
	spans   chan *Span
	done    chan struct{}
	wg      sync.WaitGroup
	client  *http.Client
}

// NewSpanExporter creates a span exporter that logs and optionally sends to OTLP.
func NewSpanExporter(cfg TracerConfig) *SpanExporter {
	e := &SpanExporter{
		config: cfg,
		spans:  make(chan *Span, exportChanSize),
		done:   make(chan struct{}),
		client: &http.Client{Timeout: 5 * time.Second},
	}

	if cfg.ExportURL != "" {
		e.wg.Add(1)
		go e.batchExportLoop()
	}

	return e
}

// Export sends a completed span to the exporter pipeline.
func (e *SpanExporter) Export(span *Span) {
	if e == nil || span == nil {
		return
	}

	// Always log spans when configured
	if e.config.LogSpans {
		e.logSpan(span)
	}

	// Queue for OTLP export if URL is set
	if e.config.ExportURL != "" {
		select {
		case e.spans <- span:
		default:
			// Channel full — drop span rather than block request path
			slog.Warn("tracing: span export channel full, dropping span",
				"trace_id", span.TraceID,
				"span_name", span.Name,
			)
		}
	}
}

// Shutdown flushes pending spans and stops the exporter.
func (e *SpanExporter) Shutdown() {
	if e == nil {
		return
	}
	close(e.done)
	e.wg.Wait()
}

// logSpan writes a structured JSON log entry for the span.
func (e *SpanExporter) logSpan(span *Span) {
	attrs := make([]slog.Attr, 0, 8+len(span.Attributes))
	attrs = append(attrs,
		slog.String("trace_id", span.TraceID),
		slog.String("span_id", span.SpanID),
		slog.String("parent_id", span.ParentID),
		slog.String("name", span.Name),
		slog.Float64("duration_ms", float64(span.Duration.Microseconds())/1000.0),
		slog.String("status", span.Status),
		slog.String("service", span.Service),
	)

	if len(span.Attributes) > 0 {
		attrPairs := make([]any, 0, len(span.Attributes)*2)
		for k, v := range span.Attributes {
			attrPairs = append(attrPairs, k, v)
		}
		attrs = append(attrs, slog.Group("attributes", attrPairs...))
	}

	slogAttrs := make([]any, len(attrs))
	for i, a := range attrs {
		slogAttrs[i] = a
	}
	slog.Info("span.completed", slogAttrs...)
}

// batchExportLoop collects spans and sends them in batches to the OTLP endpoint.
func (e *SpanExporter) batchExportLoop() {
	defer e.wg.Done()

	batch := make([]*Span, 0, exportBatchSize)
	ticker := time.NewTicker(exportFlushDelay)
	defer ticker.Stop()

	for {
		select {
		case span := <-e.spans:
			batch = append(batch, span)
			if len(batch) >= exportBatchSize {
				e.sendBatch(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				e.sendBatch(batch)
				batch = batch[:0]
			}
		case <-e.done:
			// Drain remaining spans
			for {
				select {
				case span := <-e.spans:
					batch = append(batch, span)
				default:
					if len(batch) > 0 {
						e.sendBatch(batch)
					}
					return
				}
			}
		}
	}
}

// sendBatch posts a batch of spans to the OTLP HTTP endpoint.
func (e *SpanExporter) sendBatch(batch []*Span) {
	payload := e.buildOTLPPayload(batch)

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("tracing: failed to marshal OTLP payload", "error", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, e.config.ExportURL, bytes.NewReader(body))
	if err != nil {
		slog.Warn("tracing: failed to create OTLP request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		slog.Warn("tracing: OTLP export failed", "error", err, "spans", len(batch))
		return
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		slog.Warn("tracing: OTLP export returned error",
			"status", resp.StatusCode,
			"spans", len(batch),
		)
	}
}

// buildOTLPPayload constructs a simplified OTLP JSON traces payload.
func (e *SpanExporter) buildOTLPPayload(batch []*Span) map[string]any {
	otlpSpans := make([]map[string]any, 0, len(batch))
	for _, s := range batch {
		otlpSpan := map[string]any{
			"traceId":            s.TraceID,
			"spanId":             s.SpanID,
			"parentSpanId":       s.ParentID,
			"name":               s.Name,
			"startTimeUnixNano":  fmt.Sprintf("%d", s.StartTime.UnixNano()),
			"endTimeUnixNano":    fmt.Sprintf("%d", s.EndTime.UnixNano()),
			"kind":               2, // SPAN_KIND_SERVER
			"status":             buildOTLPStatus(s.Status),
			"attributes":         buildOTLPAttributes(s.Attributes),
		}

		if len(s.Events) > 0 {
			events := make([]map[string]any, 0, len(s.Events))
			for _, ev := range s.Events {
				events = append(events, map[string]any{
					"name":              ev.Name,
					"timeUnixNano":     fmt.Sprintf("%d", ev.Timestamp.UnixNano()),
					"attributes":        buildOTLPAttributes(ev.Attributes),
				})
			}
			otlpSpan["events"] = events
		}

		otlpSpans = append(otlpSpans, otlpSpan)
	}

	return map[string]any{
		"resourceSpans": []map[string]any{
			{
				"resource": map[string]any{
					"attributes": []map[string]any{
						{
							"key":   "service.name",
							"value": map[string]any{"stringValue": e.config.ServiceName},
						},
					},
				},
				"scopeSpans": []map[string]any{
					{
						"scope": map[string]any{
							"name":    "nexus-tracing",
							"version": "0.1.0",
						},
						"spans": otlpSpans,
					},
				},
			},
		},
	}
}

func buildOTLPStatus(status string) map[string]any {
	code := 1 // STATUS_CODE_OK
	if status == "error" {
		code = 2 // STATUS_CODE_ERROR
	}
	return map[string]any{
		"code": code,
	}
}

func buildOTLPAttributes(attrs map[string]string) []map[string]any {
	if len(attrs) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(attrs))
	for k, v := range attrs {
		result = append(result, map[string]any{
			"key":   k,
			"value": map[string]any{"stringValue": v},
		})
	}
	return result
}
