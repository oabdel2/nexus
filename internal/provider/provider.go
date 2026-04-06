package provider

import (
	"context"
	"io"
)

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
	// Nexus metadata (stripped before forwarding)
	WorkflowID  string    `json:"-"`
	StepNumber  int       `json:"-"`
	AgentRole   string    `json:"-"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CachedTokens     int `json:"cached_tokens,omitempty"` // OpenAI prefix-cached tokens
}

type Provider interface {
	Name() string
	Send(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	SendStream(ctx context.Context, req ChatRequest, w io.Writer) (*Usage, error)
	HealthCheck(ctx context.Context) error
}
