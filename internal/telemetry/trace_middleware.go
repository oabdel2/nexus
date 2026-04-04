package telemetry

import (
	"context"
	"fmt"
	"net/http"
)

// TraceMiddleware returns an HTTP middleware that creates a root span for each
// request, propagates W3C traceparent headers, and records response status.
func TraceMiddleware(tracer *Tracer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tracer == nil || !tracer.config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			ctx := r.Context()

			// Parse incoming traceparent header
			if tp := r.Header.Get("traceparent"); tp != "" {
				traceID, parentSpanID, _, ok := ParseTraceparent(tp)
				if ok {
					// Inject a virtual parent span into context so StartSpan
					// picks up the incoming trace-id and parent-span-id.
					parentSpan := &Span{
						TraceID: traceID,
						SpanID:  parentSpanID,
					}
					ctx = context.WithValue(ctx, spanContextKey, parentSpan)
				}
			}

			// Start root span for this request
			ctx, span := tracer.StartSpan(ctx, "gateway.request")
			span.SetAttribute("http.method", r.Method)
			span.SetAttribute("http.path", r.URL.Path)
			span.SetAttribute("http.user_agent", r.UserAgent())

			// Set response headers before handler writes them
			if span.TraceID != "" {
				w.Header().Set("traceparent", FormatTraceparent(span.TraceID, span.SpanID, true))
				w.Header().Set("X-Trace-ID", span.TraceID)
			}

			// Wrap response writer to capture status code
			sw := &traceStatusWriter{ResponseWriter: w, status: 200}

			// Serve with enriched context
			next.ServeHTTP(sw, r.WithContext(ctx))

			// Record response details
			span.SetAttribute("http.status_code", fmt.Sprintf("%d", sw.status))
			if sw.status >= 400 {
				span.SetStatus("error")
			}

			tracer.EndSpan(span)
		})
	}
}

// traceStatusWriter captures the HTTP status code written by handlers.
type traceStatusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *traceStatusWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *traceStatusWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	return w.ResponseWriter.Write(b)
}

// Flush implements http.Flusher for streaming support.
func (w *traceStatusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
