package render

import (
	"strings"
	"testing"
)

func doc(sections int) string {
	var b strings.Builder
	b.WriteString("# Note\n\n")
	for i := 0; i < sections; i++ {
		b.WriteString("## Section\n\ntext with **bold** and `code`.\n\n")
		b.WriteString("```go\nfunc main() {}\n```\n\n- a\n- b\n\n")
	}
	return b.String()
}

func TestRenderProducesOutput(t *testing.T) {
	r := New(80)
	out := r.Render("# Hello\n\nworld\n", 80)
	if !strings.Contains(out, "Hello") || !strings.Contains(out, "world") {
		t.Fatalf("render lost content: %q", out)
	}
}

// The whole point of the package: repeated identical renders must not reach
// Glamour again.
func TestRepeatedRenderIsMemoized(t *testing.T) {
	r := New(80)
	src := doc(10)
	first := r.Render(src, 80)
	for i := 0; i < 50; i++ {
		if got := r.Render(src, 80); got != first {
			t.Fatal("memoized render returned different output")
		}
	}
	hits, misses := r.Stats()
	if misses != 1 {
		t.Errorf("misses = %d, want 1 real render", misses)
	}
	if hits != 50 {
		t.Errorf("hits = %d, want 50 cached renders", hits)
	}
}

func TestChangedSourceInvalidates(t *testing.T) {
	r := New(80)
	r.Render("# One\n", 80)
	r.Render("# Two\n", 80)
	_, misses := r.Stats()
	if misses != 2 {
		t.Errorf("misses = %d, want a real render per distinct source", misses)
	}
}

// A resize must re-render, since wrapping changes.
func TestWidthChangeInvalidates(t *testing.T) {
	r := New(80)
	src := doc(3)
	a := r.Render(src, 80)
	b := r.Render(src, 40)
	if a == b {
		t.Error("different widths produced identical output")
	}
	_, misses := r.Stats()
	if misses != 2 {
		t.Errorf("misses = %d, want one render per width", misses)
	}
}

func TestWidthIsClamped(t *testing.T) {
	r := New(0)
	if out := r.Render("# t\n", -5); out == "" {
		t.Error("a nonsensical width should still render something")
	}
}

func TestInvalidateForcesRerender(t *testing.T) {
	r := New(80)
	src := doc(2)
	r.Render(src, 80)
	r.Invalidate()
	r.Render(src, 80)
	_, misses := r.Stats()
	if misses != 2 {
		t.Errorf("misses = %d, want the memo dropped", misses)
	}
}

// --- benchmarks: these are the numbers the design decision rests on --------

func BenchmarkRenderCold(b *testing.B) {
	src := doc(40)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := New(80)
		r.Render(src, 80)
	}
}

func BenchmarkRenderMemoized(b *testing.B) {
	src := doc(40)
	r := New(80)
	r.Render(src, 80)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Render(src, 80)
	}
}
