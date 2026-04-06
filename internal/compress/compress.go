package compress

import (
	"fmt"
	"regexp"
	"strings"
)

// Message mirrors the chat message structure used throughout Nexus.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CompressorConfig controls which compression strategies are applied.
type CompressorConfig struct {
	EnableWhitespace   bool
	EnableCodeStrip    bool
	EnableHistoryTrunc bool
	MaxHistoryTurns    int // total messages to keep (system + last N user/assistant)
	PreserveLastN      int // number of recent turn-pairs to always keep
}

// CompressionResult captures metrics about a compression operation.
type CompressionResult struct {
	OriginalTokens   int      // estimated via len(text)/4
	CompressedTokens int      // estimated via len(text)/4
	Ratio            float64  // CompressedTokens / OriginalTokens
	StrategiesUsed   []string // names of strategies that were applied
}

// Compressor applies configurable prompt compression strategies.
type Compressor struct {
	cfg CompressorConfig
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() CompressorConfig {
	return CompressorConfig{
		EnableWhitespace:   true,
		EnableCodeStrip:    true,
		EnableHistoryTrunc: true,
		MaxHistoryTurns:    20,
		PreserveLastN:      5,
	}
}

// New creates a Compressor with the given configuration.
func New(cfg CompressorConfig) *Compressor {
	return &Compressor{cfg: cfg}
}

// estimateTokens returns a rough token count (≈ 4 chars per token).
func estimateTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	t := len(text) / 4
	if t == 0 {
		t = 1
	}
	return t
}

// ---- Strategy 1: Whitespace Compression ----

var (
	reMultipleNewlines = regexp.MustCompile(`\n{3,}`)
	reMultipleSpaces   = regexp.MustCompile(`[^\S\n]{2,}`) // 2+ horizontal whitespace chars
	reTrailingSpaces   = regexp.MustCompile(`(?m)[ \t]+$`)
)

// WhitespaceCompress collapses redundant whitespace.
func WhitespaceCompress(text string) string {
	// Trim trailing whitespace per line
	text = reTrailingSpaces.ReplaceAllString(text, "")
	// Collapse 3+ newlines to double newline
	text = reMultipleNewlines.ReplaceAllString(text, "\n\n")
	// Collapse multiple horizontal whitespace to single space
	text = reMultipleSpaces.ReplaceAllString(text, " ")
	return text
}

// ---- Strategy 2: Code Block Compression ----

var (
	reCodeBlock      = regexp.MustCompile("(?s)```(\\w*)\\n(.*?)```")
	reLineComment    = regexp.MustCompile(`(?m)^\s*//.*$`)
	rePythonComment  = regexp.MustCompile(`(?m)^\s*#[^!].*$|^\s*#\s*$`)
	reBlockComment   = regexp.MustCompile(`(?s)/\*.*?\*/`)
	reBlankLines     = regexp.MustCompile(`\n{2,}`)
	reImportBlock    = regexp.MustCompile(`(?m)^import \(\n([\s\S]*?)\n\)`)
)

// compressCodeContent strips comments, blank lines, and collapses imports.
func compressCodeContent(code string, lang string) string {
	// Remove block comments (/* ... */)
	code = reBlockComment.ReplaceAllString(code, "")

	// Remove line comments based on language
	switch lang {
	case "python", "py", "bash", "sh", "yaml", "yml", "ruby", "rb":
		code = rePythonComment.ReplaceAllString(code, "")
	default:
		// Default: C-style line comments
		code = reLineComment.ReplaceAllString(code, "")
	}

	// Collapse import blocks: import (\n  "a"\n  "b"\n) → import ("a"; "b")
	code = reImportBlock.ReplaceAllStringFunc(code, func(match string) string {
		inner := reImportBlock.FindStringSubmatch(match)
		if len(inner) < 2 {
			return match
		}
		lines := strings.Split(strings.TrimSpace(inner[1]), "\n")
		var imports []string
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if l != "" {
				imports = append(imports, l)
			}
		}
		return "import (" + strings.Join(imports, "; ") + ")"
	})

	// Remove blank lines
	code = reBlankLines.ReplaceAllString(code, "\n")

	return strings.TrimSpace(code)
}

// CodeBlockCompress detects fenced code blocks and compresses their contents.
func CodeBlockCompress(text string) string {
	return reCodeBlock.ReplaceAllStringFunc(text, func(block string) string {
		matches := reCodeBlock.FindStringSubmatch(block)
		if len(matches) < 3 {
			return block
		}
		lang := matches[1]
		code := matches[2]
		compressed := compressCodeContent(code, lang)
		return "```" + lang + "\n" + compressed + "\n```"
	})
}

// ---- Strategy 3: History Truncation ----

// classifyTopics extracts simple topic keywords from messages for the summary.
func classifyTopics(messages []Message) string {
	wordFreq := make(map[string]int)
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "shall": true, "can": true, "to": true,
		"of": true, "in": true, "for": true, "on": true, "with": true,
		"at": true, "by": true, "from": true, "it": true, "this": true,
		"that": true, "and": true, "or": true, "but": true, "not": true,
		"i": true, "you": true, "he": true, "she": true, "we": true,
		"they": true, "me": true, "my": true, "your": true, "what": true,
		"how": true, "if": true, "so": true, "as": true, "just": true,
		"": true,
	}

	for _, m := range messages {
		words := strings.Fields(strings.ToLower(m.Content))
		for _, w := range words {
			w = strings.Trim(w, ".,!?;:\"'()[]{}/-")
			if len(w) > 2 && !stopWords[w] {
				wordFreq[w]++
			}
		}
	}

	// Get top 3 keywords by frequency
	type kv struct {
		word  string
		count int
	}
	var sorted []kv
	for w, c := range wordFreq {
		sorted = append(sorted, kv{w, c})
	}
	// Simple selection sort for top 3
	for i := 0; i < len(sorted) && i < 3; i++ {
		maxIdx := i
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].count > sorted[maxIdx].count {
				maxIdx = j
			}
		}
		sorted[i], sorted[maxIdx] = sorted[maxIdx], sorted[i]
	}

	var topics []string
	for i := 0; i < len(sorted) && i < 3; i++ {
		topics = append(topics, sorted[i].word)
	}
	if len(topics) == 0 {
		return "various topics"
	}
	return strings.Join(topics, ", ")
}

// HistoryTruncate keeps the system message, last N turns, and summarizes the middle.
func HistoryTruncate(messages []Message, preserveLastN int) []Message {
	if len(messages) == 0 {
		return messages
	}

	// If preserveLastN covers all messages, nothing to do
	if len(messages) <= preserveLastN+1 {
		return messages
	}

	var result []Message

	// Always keep system message if first message is system
	startIdx := 0
	if messages[0].Role == "system" {
		result = append(result, messages[0])
		startIdx = 1
	}

	remaining := messages[startIdx:]
	if len(remaining) <= preserveLastN {
		return append(result, remaining...)
	}

	// Middle messages to summarize
	middleEnd := len(remaining) - preserveLastN
	middle := remaining[:middleEnd]
	kept := remaining[middleEnd:]

	topics := classifyTopics(middle)
	summary := Message{
		Role:    "system",
		Content: fmt.Sprintf("[Previous context: %d messages about %s]", len(middle), topics),
	}
	result = append(result, summary)
	result = append(result, kept...)

	return result
}

// ---- Strategy 4: Combined Compression ----

// CompressMessages applies all enabled strategies and returns metrics.
func (c *Compressor) CompressMessages(messages []Message) ([]Message, CompressionResult) {
	result := CompressionResult{}

	// Calculate original token count
	var originalText strings.Builder
	for _, m := range messages {
		originalText.WriteString(m.Content)
	}
	result.OriginalTokens = estimateTokens(originalText.String())

	compressed := make([]Message, len(messages))
	copy(compressed, messages)

	// Apply history truncation first (reduces message count)
	if c.cfg.EnableHistoryTrunc && len(compressed) > c.cfg.PreserveLastN+1 {
		compressed = HistoryTruncate(compressed, c.cfg.PreserveLastN)
		result.StrategiesUsed = append(result.StrategiesUsed, "history_truncate")
	}

	// Apply per-message strategies
	for i := range compressed {
		content := compressed[i].Content

		if c.cfg.EnableCodeStrip {
			newContent := CodeBlockCompress(content)
			if newContent != content {
				content = newContent
				if !contains(result.StrategiesUsed, "code_strip") {
					result.StrategiesUsed = append(result.StrategiesUsed, "code_strip")
				}
			}
		}

		if c.cfg.EnableWhitespace {
			newContent := WhitespaceCompress(content)
			if newContent != content {
				content = newContent
				if !contains(result.StrategiesUsed, "whitespace") {
					result.StrategiesUsed = append(result.StrategiesUsed, "whitespace")
				}
			}
		}

		compressed[i].Content = content
	}

	// Calculate compressed token count
	var compressedText strings.Builder
	for _, m := range compressed {
		compressedText.WriteString(m.Content)
	}
	result.CompressedTokens = estimateTokens(compressedText.String())

	if result.OriginalTokens > 0 {
		result.Ratio = float64(result.CompressedTokens) / float64(result.OriginalTokens)
	}

	return compressed, result
}

func contains(s []string, v string) bool {
	for _, item := range s {
		if item == v {
			return true
		}
	}
	return false
}
