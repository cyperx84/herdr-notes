// Package render turns markdown into terminal output.
//
// This exists as its own package for one measured reason. A Glamour render of
// a moderate note costs ~2.3ms and ~2.4MB across ~30k allocations, while
// building the renderer costs ~22µs — so the expensive thing is rendering,
// not construction, and the only real fix is to not render when nothing has
// changed. Benchmarks live beside this package so that claim stays honest.
package render

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
)

// Renderer renders markdown, reusing both the underlying Glamour renderer and
// the last result. It is safe for concurrent use.
type Renderer struct {
	mu sync.Mutex

	width int
	tr    *glamour.TermRenderer

	// Memo of the last render. A TUI re-renders on every resize, mode change,
	// and refresh tick, and the overwhelming majority of those calls have
	// identical input.
	lastSource string
	lastWidth  int
	lastOut    string
	haveLast   bool

	hits, misses int
}

// New returns a Renderer. Width may be zero; it is set on first use.
func New(width int) *Renderer {
	return &Renderer{width: normalizeWidth(width)}
}

func normalizeWidth(w int) int {
	if w < 20 {
		return 20
	}
	if w > 500 {
		return 500
	}
	return w
}

// Render returns the rendered form of source at the given width.
//
// Identical (source, width) pairs return the memoized result without touching
// Glamour at all, which is what removes the per-keystroke and per-resize cost.
func (r *Renderer) Render(source string, width int) string {
	width = normalizeWidth(width)

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.haveLast && r.lastWidth == width && r.lastSource == source {
		r.hits++
		return r.lastOut
	}
	r.misses++

	if r.tr == nil || r.width != width {
		tr, err := glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			// Falling back to the raw source keeps the pane usable; a styling
			// failure should never hide the note's content.
			r.lastSource, r.lastWidth, r.lastOut, r.haveLast = source, width, source, true
			return source
		}
		r.tr = tr
		r.width = width
	}

	out, err := r.tr.Render(source)
	if err != nil {
		out = source
	} else {
		out = strings.TrimSpace(out)
	}

	r.lastSource, r.lastWidth, r.lastOut, r.haveLast = source, width, out, true
	return out
}

// Stats reports cache hits and misses, for `doctor` and for tests that need to
// prove the memo is actually doing something.
func (r *Renderer) Stats() (hits, misses int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hits, r.misses
}

// Invalidate drops the memo, forcing the next Render to do real work.
func (r *Renderer) Invalidate() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.haveLast = false
}
