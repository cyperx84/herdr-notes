package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/glamour"
)

func benchDoc(paragraphs int) string {
	var b strings.Builder
	b.WriteString("# Benchmark note\n\n")
	for i := 0; i < paragraphs; i++ {
		b.WriteString("## Section\n\nSome **bold** and `code` and a [link](https://example.com).\n\n")
		b.WriteString("```go\nfunc main() { println(\"hello\") }\n```\n\n")
		b.WriteString("- item one\n- item two\n- item three\n\n")
	}
	return b.String()
}

// BenchmarkRendererConstruction isolates the cost paid on every preview render
// today, because renderPreview builds a new glamour renderer each call.
func BenchmarkRendererConstruction(b *testing.B) {
	for i := 0; i < b.N; i++ {
		r, err := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(80))
		if err != nil {
			b.Fatal(err)
		}
		_ = r
	}
}

// BenchmarkRenderOnly measures rendering with a renderer that is reused.
func BenchmarkRenderOnly(b *testing.B) {
	r, err := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(80))
	if err != nil {
		b.Fatal(err)
	}
	doc := benchDoc(40)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Render(doc); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkConstructAndRender is the current per-render cost.
func BenchmarkConstructAndRender(b *testing.B) {
	doc := benchDoc(40)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(80))
		if err != nil {
			b.Fatal(err)
		}
		if _, err := r.Render(doc); err != nil {
			b.Fatal(err)
		}
	}
}
