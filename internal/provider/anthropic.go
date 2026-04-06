package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AnthropicProvider implements the Provider interface for Anthropic's Messages API.
type AnthropicProvider struct {
	name    string
	baseURL string
	apiKey  string
	version string
	client  *http.Client
}

// Anthropic request/response types (not exported — internal wire format only).

type anthropicRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system,omitempty"`
	Messages  []Message `json:"messages"`
	Stream    bool      `json:"stream,omitempty"`
}

type anthropicResponse struct {
	ID      string                 `json:"id"`
	Type    string                 `json:"type"`
	Role    string                 `json:"role"`
	Content []anthropicContentBlock `json:"content"`
	Model   string                 `json:"model"`
	Usage   anthropicUsage         `json:"usage"`
	StopReason string              `json:"stop_reason"`
}

type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func NewAnthropic(name, baseURL, apiKey string) *AnthropicProvider {
	return &AnthropicProvider{
		name:    name,
		baseURL: baseURL,
		apiKey:  apiKey,
		version: "2023-06-01",
		client: &http.Client{
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
				DisableCompression:  false,
				ForceAttemptHTTP2:   true,
			},
		},
	}
}

func (p *AnthropicProvider) Name() string {
	return p.name
}

// buildAnthropicRequest converts a generic ChatRequest into Anthropic's wire format,
// extracting system messages into the top-level field.
func buildAnthropicRequest(req ChatRequest) anthropicRequest {
	var system string
	var msgs []Message
	for _, m := range req.Messages {
		if m.Role == "system" {
			if system != "" {
				system += "\n"
			}
			system += m.Content
		} else {
			msgs = append(msgs, m)
		}
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 1024
	}

	return anthropicRequest{
		Model:     req.Model,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  msgs,
		Stream:    req.Stream,
	}
}

// toOpenAIResponse converts an Anthropic response into the unified ChatResponse format.
func toOpenAIResponse(ar *anthropicResponse) *ChatResponse {
	var content strings.Builder
	for _, block := range ar.Content {
		if block.Type == "text" {
			content.WriteString(block.Text)
		}
	}

	total := ar.Usage.InputTokens + ar.Usage.OutputTokens
	finishReason := mapStopReason(ar.StopReason)

	return &ChatResponse{
		ID:     ar.ID,
		Object: "chat.completion",
		Model:  ar.Model,
		Choices: []Choice{
			{
				Index:        0,
				Message:      Message{Role: "assistant", Content: content.String()},
				FinishReason: finishReason,
			},
		},
		Usage: Usage{
			PromptTokens:     ar.Usage.InputTokens,
			CompletionTokens: ar.Usage.OutputTokens,
			TotalTokens:      total,
		},
	}
}

func mapStopReason(reason string) string {
	switch reason {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "stop_sequence":
		return "stop"
	default:
		if reason == "" {
			return "stop"
		}
		return reason
	}
}

func (p *AnthropicProvider) Send(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	ar := buildAnthropicRequest(req)
	ar.Stream = false

	body, err := json.Marshal(ar)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	p.setHeaders(httpReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("provider error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var ar2 anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar2); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return toOpenAIResponse(&ar2), nil
}

func (p *AnthropicProvider) SendStream(ctx context.Context, req ChatRequest, w io.Writer) (*Usage, error) {
	ar := buildAnthropicRequest(req)
	ar.Stream = true

	body, err := json.Marshal(ar)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	p.setHeaders(httpReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("provider error (status %d): %s", resp.StatusCode, string(respBody))
	}

	// Parse Anthropic SSE stream and re-emit as OpenAI-compatible SSE.
	usage := &Usage{}
	var model string
	var id string
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		// Anthropic SSE uses "event:" and "data:" lines.
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var event map[string]any
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		eventType, _ := event["type"].(string)

		switch eventType {
		case "message_start":
			if msg, ok := event["message"].(map[string]any); ok {
				model, _ = msg["model"].(string)
				id, _ = msg["id"].(string)
				if u, ok := msg["usage"].(map[string]any); ok {
					if v, ok := u["input_tokens"].(float64); ok {
						usage.PromptTokens = int(v)
					}
				}
			}

		case "content_block_delta":
			delta, _ := event["delta"].(map[string]any)
			text, _ := delta["text"].(string)
			if text != "" {
				chunk := buildOpenAIStreamChunk(id, model, text, "")
				fmt.Fprintf(w, "data: %s\n\n", chunk)
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}

		case "message_delta":
			if u, ok := event["usage"].(map[string]any); ok {
				if v, ok := u["output_tokens"].(float64); ok {
					usage.CompletionTokens = int(v)
				}
			}
			// Emit final chunk with finish_reason
			chunk := buildOpenAIStreamChunk(id, model, "", "stop")
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

		case "message_stop":
			fmt.Fprintf(w, "data: [DONE]\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}

	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	return usage, scanner.Err()
}

// buildOpenAIStreamChunk builds a minimal OpenAI-compatible SSE chunk JSON string.
func buildOpenAIStreamChunk(id, model, content, finishReason string) string {
	type delta struct {
		Content string `json:"content,omitempty"`
	}
	type choice struct {
		Index        int    `json:"index"`
		Delta        delta  `json:"delta"`
		FinishReason string `json:"finish_reason,omitempty"`
	}
	type chunk struct {
		ID      string   `json:"id"`
		Object  string   `json:"object"`
		Model   string   `json:"model"`
		Choices []choice `json:"choices"`
	}

	fr := finishReason
	c := chunk{
		ID:     id,
		Object: "chat.completion.chunk",
		Model:  model,
		Choices: []choice{
			{
				Index:        0,
				Delta:        delta{Content: content},
				FinishReason: fr,
			},
		},
	}
	b, _ := json.Marshal(c)
	return string(b)
}

func (p *AnthropicProvider) HealthCheck(ctx context.Context) error {
	// Anthropic doesn't have a /models endpoint; use a lightweight message request.
	httpReq, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/v1/messages", nil)
	if err != nil {
		return err
	}
	p.setHeaders(httpReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Any non-5xx response (including 405 Method Not Allowed) indicates the API is reachable.
	if resp.StatusCode >= 500 {
		return fmt.Errorf("health check failed: status %d", resp.StatusCode)
	}
	return nil
}

func (p *AnthropicProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", p.version)
}
