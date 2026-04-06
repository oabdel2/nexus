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

type OpenAIProvider struct {
	name    string
	baseURL string
	apiKey  string
	headers map[string]string
	client  *http.Client
}

func NewOpenAI(name, baseURL, apiKey string, headers map[string]string) *OpenAIProvider {
	return &OpenAIProvider{
		name:    name,
		baseURL: baseURL,
		apiKey:  apiKey,
		headers: headers,
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

func (p *OpenAIProvider) Name() string {
	return p.name
}

func (p *OpenAIProvider) Send(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	for k, v := range p.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("provider error (status %d): %s", resp.StatusCode, string(respBody))
	}

	// Decode into raw map first to extract prompt_tokens_details.cached_tokens
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Extract OpenAI's cached_tokens from prompt_tokens_details
	chatResp.Usage.CachedTokens = extractCachedTokens(respBody)

	return &chatResp, nil
}

// extractCachedTokens parses OpenAI's usage.prompt_tokens_details.cached_tokens.
func extractCachedTokens(body []byte) int {
	var raw struct {
		Usage struct {
			PromptTokensDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &raw) == nil {
		return raw.Usage.PromptTokensDetails.CachedTokens
	}
	return 0
}

func (p *OpenAIProvider) SendStream(ctx context.Context, req ChatRequest, w io.Writer) (*Usage, error) {
	req.Stream = true
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	for k, v := range p.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("provider error (status %d): %s", resp.StatusCode, string(respBody))
	}

	// Buffer and forward SSE stream, extracting usage from final chunk
	usage := &Usage{}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		// Forward the line to client
		fmt.Fprintf(w, "%s\n", line)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// Parse usage from SSE data lines
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				continue
			}
			var chunk struct {
				Usage *Usage `json:"usage"`
			}
			if json.Unmarshal([]byte(data), &chunk) == nil && chunk.Usage != nil {
				usage = chunk.Usage
			}
		}
	}

	return usage, scanner.Err()
}

func (p *OpenAIProvider) HealthCheck(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/models", nil)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	for k, v := range p.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed: status %d", resp.StatusCode)
	}
	return nil
}
