package compress

import (
	"strings"
	"testing"
)

func generateText(size int) string {
	base := "This is a sample line with some text content.\n"
	var sb strings.Builder
	for sb.Len() < size {
		sb.WriteString(base)
	}
	return sb.String()[:size]
}

func generateCodeText(size int) string {
	base := `Here is an explanation with code:
` + "```go" + `
package main

import (
	"fmt"
	"os"
	"strings"
)

// Main entry point
func main() {
	// Print greeting
	fmt.Println("hello")

	// Exit cleanly
	os.Exit(0)
}
` + "```" + `

The above code demonstrates basic Go patterns.

`
	var sb strings.Builder
	for sb.Len() < size {
		sb.WriteString(base)
	}
	return sb.String()[:size]
}

func BenchmarkWhitespace_1KB(b *testing.B) {
	text := generateText(1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		WhitespaceCompress(text)
	}
}

func BenchmarkWhitespace_10KB(b *testing.B) {
	text := generateText(10 * 1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		WhitespaceCompress(text)
	}
}

func BenchmarkWhitespace_100KB(b *testing.B) {
	text := generateText(100 * 1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		WhitespaceCompress(text)
	}
}

func BenchmarkCodeBlockCompress_1KB(b *testing.B) {
	text := generateCodeText(1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CodeBlockCompress(text)
	}
}

func BenchmarkCodeBlockCompress_10KB(b *testing.B) {
	text := generateCodeText(10 * 1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CodeBlockCompress(text)
	}
}

func BenchmarkCompressMessages_Combined(b *testing.B) {
	msgs := make([]Message, 20)
	msgs[0] = Message{Role: "system", Content: "You are a helpful coding assistant."}
	for i := 1; i < 20; i++ {
		if i%2 == 1 {
			msgs[i] = Message{Role: "user", Content: generateCodeText(512)}
		} else {
			msgs[i] = Message{Role: "assistant", Content: generateText(512)}
		}
	}
	c := New(DefaultConfig())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.CompressMessages(msgs)
	}
}

func BenchmarkHistoryTruncate(b *testing.B) {
	msgs := make([]Message, 30)
	msgs[0] = Message{Role: "system", Content: "You are a helpful assistant."}
	for i := 1; i < 30; i++ {
		msgs[i] = Message{Role: "user", Content: generateText(200)}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		HistoryTruncate(msgs, 5)
	}
}
