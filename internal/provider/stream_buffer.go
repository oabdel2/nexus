package provider

import (
	"encoding/json"
	"strings"
)

// StreamBuffer captures SSE chunks while forwarding them to the client,
// then assembles the full response for caching.
type StreamBuffer struct {
	chunks       []string
	content      strings.Builder
	usage        *Usage
	model        string
	id           string
	finishReason string
}

func NewStreamBuffer() *StreamBuffer {
	return &StreamBuffer{
		usage: &Usage{},
	}
}

// WriteChunk captures a single SSE line and extracts content/metadata.
func (sb *StreamBuffer) WriteChunk(line string) {
	sb.chunks = append(sb.chunks, line)

	if !strings.HasPrefix(line, "data: ") {
		return
	}
	data := strings.TrimPrefix(line, "data: ")
	if data == "[DONE]" {
		return
	}

	var chunk struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *Usage `json:"usage"`
	}

	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return
	}

	if chunk.ID != "" {
		sb.id = chunk.ID
	}
	if chunk.Model != "" {
		sb.model = chunk.Model
	}
	if chunk.Usage != nil {
		sb.usage = chunk.Usage
	}
	if len(chunk.Choices) > 0 {
		if chunk.Choices[0].Delta.Content != "" {
			sb.content.WriteString(chunk.Choices[0].Delta.Content)
		}
		if chunk.Choices[0].FinishReason != "" {
			sb.finishReason = chunk.Choices[0].FinishReason
		}
	}
}

// AssembleResponse builds a full ChatResponse from the captured stream chunks.
func (sb *StreamBuffer) AssembleResponse() *ChatResponse {
	finishReason := sb.finishReason
	if finishReason == "" {
		finishReason = "stop"
	}

	return &ChatResponse{
		ID:     sb.id,
		Object: "chat.completion",
		Model:  sb.model,
		Choices: []Choice{
			{
				Index: 0,
				Message: Message{
					Role:    "assistant",
					Content: sb.content.String(),
				},
				FinishReason: finishReason,
			},
		},
		Usage: *sb.usage,
	}
}

// CacheableJSON returns the assembled response as JSON suitable for cache storage.
// Returns false if the buffer has no content to cache.
func (sb *StreamBuffer) CacheableJSON() ([]byte, bool) {
	if sb.content.Len() == 0 {
		return nil, false
	}
	resp := sb.AssembleResponse()
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, false
	}
	return data, true
}
