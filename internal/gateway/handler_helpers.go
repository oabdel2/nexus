package gateway

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nexus-gateway/nexus/internal/compress"
	"github.com/nexus-gateway/nexus/internal/provider"
	"github.com/nexus-gateway/nexus/internal/router"
)

// tryCheapFirst sends the request to the cheapest model and scores confidence.
// Returns nil if cascade is not applicable, otherwise a CascadeResult and the cheap response.
func (s *Server) tryCheapFirst(ctx context.Context, req provider.ChatRequest, original router.ModelSelection, promptText string) (*router.CascadeResult, *provider.ChatResponse) {
	cheapSel := s.cascade.CheapSelection()
	if cheapSel.Provider == "" || cheapSel.Model == "" {
		return nil, nil
	}

	p, ok := s.providers[cheapSel.Provider]
	if !ok || !s.cbPool.IsAvailable(cheapSel.Provider) {
		return nil, nil
	}

	cheapReq := req
	cheapReq.Model = cheapSel.Model

	cascadeStart := time.Now()
	cheapCtx, cancel := context.WithTimeout(ctx, s.cascade.MaxLatency())
	defer cancel()

	resp, err := p.Send(cheapCtx, cheapReq)
	latencyAdded := time.Since(cascadeStart)

	if err != nil || len(resp.Choices) == 0 {
		return &router.CascadeResult{
			UsedCheapModel: true,
			Escalated:      true,
			LatencyAdded:   latencyAdded,
		}, nil
	}

	// Score the cheap response
	var confidence float64
	if s.confidenceScorer != nil {
		evalResult := s.confidenceScorer.CombinedScore(
			resp.Choices[0].Message.Content,
			resp.Usage.PromptTokens,
			resp.Usage.CompletionTokens,
			resp.Choices[0].FinishReason,
		)
		confidence = evalResult.Score
	}

	escalated := confidence < s.cascade.Threshold()

	originalCost := float64(resp.Usage.TotalTokens) / 1000.0 * s.router.GetModelCost(original.Provider, original.Model)
	cheapCost := float64(resp.Usage.TotalTokens) / 1000.0 * s.router.GetModelCost(cheapSel.Provider, cheapSel.Model)
	costSaved := 0.0
	if !escalated {
		costSaved = originalCost - cheapCost
	}

	result := &router.CascadeResult{
		UsedCheapModel:  true,
		Escalated:       escalated,
		CheapConfidence: confidence,
		CostSaved:       costSaved,
		LatencyAdded:    latencyAdded,
	}
	if escalated {
		result.WastedTokens = resp.Usage.TotalTokens
	}
	// Return the actual response so caller can reuse it when not escalated
	if !escalated {
		return result, resp
	}
	return result, nil
}

func extractPromptText(messages []provider.Message) string {
	// Use the last user message for routing and cache key.
	// This dramatically improves cache hit rates for multi-turn conversations.
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" && messages[i].Content != "" {
			return messages[i].Content
		}
	}
	// Fallback: concatenate all messages
	var b strings.Builder
	for i, m := range messages {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(m.Content)
	}
	return b.String()
}

// fullPromptText returns the full concatenation of all messages.
// Used for prompt guard checks where the entire conversation must be scanned.
func fullPromptText(messages []provider.Message) string {
	var b strings.Builder
	for i, m := range messages {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(m.Content)
	}
	return b.String()
}

// providerToCompressMessages converts provider.Message slice to compress.Message slice.
func providerToCompressMessages(msgs []provider.Message) []compress.Message {
	out := make([]compress.Message, len(msgs))
	for i, m := range msgs {
		out[i] = compress.Message{Role: m.Role, Content: m.Content}
	}
	return out
}

// compressToProviderMessages converts compress.Message slice back to provider.Message slice.
func compressToProviderMessages(msgs []compress.Message) []provider.Message {
	out := make([]provider.Message, len(msgs))
	for i, m := range msgs {
		out[i] = provider.Message{Role: m.Role, Content: m.Content}
	}
	return out
}

// findFallbackProvider tries all other available providers when the primary is circuit-broken.
func (s *Server) findFallbackProvider(original router.ModelSelection) (provider.Provider, router.ModelSelection) {
	for name, p := range s.providers {
		if name == original.Provider {
			continue
		}
		if s.cbPool.IsAvailable(name) {
			// Use the first available provider with its first model
			original.Provider = name
			original.Reason = "circuit-breaker failover"
			return p, original
		}
	}
	return nil, original
}

// streamTeeWriter wraps an io.Writer so that each line written is also
// captured in a StreamBuffer for subsequent caching.
type streamTeeWriter struct {
	w   io.Writer
	buf *provider.StreamBuffer
}

func (tw *streamTeeWriter) Write(p []byte) (int, error) {
	// Forward to the real writer first.
	n, err := tw.w.Write(p)

	// Capture each line for the buffer using bytes operations to avoid string copy.
	remaining := p
	for len(remaining) > 0 {
		idx := bytes.IndexByte(remaining, '\n')
		var line []byte
		if idx >= 0 {
			line = remaining[:idx]
			remaining = remaining[idx+1:]
		} else {
			line = remaining
			remaining = nil
		}
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			tw.buf.WriteChunk(string(line))
		}
	}
	return n, err
}

// Flush delegates to the underlying writer if it supports http.Flusher.
func (tw *streamTeeWriter) Flush() {
	if f, ok := tw.w.(http.Flusher); ok {
		f.Flush()
	}
}
