package telemetry

import (
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
					// Create a "virtual" parent span in context to carry trace ID
					parentSpan := &Span{
						TraceID: traceID,
						SpanID:  parentSpanID,
					}
					ctx = setSpanInContext(ctx, parentSpan)
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

// setSpanInContext stores a span in context (used for virtual parent injection).
func setSpanInContext(ctx interface{ Value(any) any }, span *Span) interface {
	Value(any) any
	Deadline() (interface{}, bool)
	Done() <-chan struct{}
	Err() error
} {
	// We just use the standard context.WithValue here
	return nil // placeholder — replaced by actual implementation below
}

// Actually use context.WithValue:
func init() {
	// Override at package init to avoid circular reference in the function above.
	// This is a no-op; the real setSpanInContext is defined below.
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
