// Package okf implements the parts of Open Knowledge Format v0.2 that a
// producer and consumer of a bundle actually needs.
//
// The spec is deliberately small. Conformance (§11) is three rules:
//
//  1. every non-reserved .md file has a parseable YAML frontmatter block;
//  2. every frontmatter block has a non-empty `type`;
//  3. reserved filenames (index.md, log.md) follow §8/§9 when present.
//
// Everything else is soft guidance, and §11 is explicit that a consumer MUST
// NOT reject a bundle for unknown types, unknown keys, missing optional
// fields, broken cross-links, or missing index files. This package is
// therefore permissive on read and strict only on write: we never emit a
// non-conformant document, and we never refuse to show someone else's.
package okf

import (
	"bufio"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Reserved filenames (§3.1). These have structural meaning at any level of
// the tree and MUST NOT be used for concept documents.
const (
	IndexFile = "index.md"
	LogFile   = "log.md"
)

// IsReserved reports whether a base filename is structural rather than a
// concept document.
func IsReserved(name string) bool {
	return name == IndexFile || name == LogFile
}

// Doc is a parsed OKF concept document: frontmatter plus a markdown body.
//
// Fields carries every key in document order so that a round trip through
// this package never silently drops a producer's keys — §11 requires
// consumers to tolerate unknown keys, and quietly deleting them on rewrite
// would be a worse failure than rejecting them outright.
type Doc struct {
	Fields Fields
	Body   string
}

// Type returns the required `type` field (§4.1).
func (d Doc) Type() string { return d.Fields.Get("type") }

// Title returns the conventional title, falling back to empty.
func (d Doc) Title() string { return d.Fields.Get("title") }

// Conformant reports whether the document satisfies §11.2 — the only
// frontmatter rule the spec actually enforces.
func (d Doc) Conformant() bool { return strings.TrimSpace(d.Type()) != "" }

// Fields is an ordered set of frontmatter key/value pairs.
//
// OKF frontmatter is YAML, but a full YAML dependency buys very little here:
// the spec's own examples are flat scalars, lists, and the occasional nested
// mapping, and §11 requires us to tolerate anything we do not understand
// rather than fail. Values are therefore kept as raw YAML text and only
// interpreted when a caller asks for a specific shape. That keeps unknown
// structures intact byte-for-byte instead of round-tripping them through a
// lossy model.
type Fields struct {
	keys   []string
	values map[string]string
}

// NewFields returns an empty ordered field set.
func NewFields() Fields {
	return Fields{values: map[string]string{}}
}

// Get returns a scalar value with surrounding quotes removed, or "".
func (f Fields) Get(key string) string {
	return unquote(f.values[key])
}

// Raw returns the value exactly as it appeared, including any block or list
// continuation lines.
func (f Fields) Raw(key string) string { return f.values[key] }

// Has reports whether a key is present, even if its value is empty.
func (f *Fields) Has(key string) bool {
	if f.values == nil {
		return false
	}
	_, ok := f.values[key]
	return ok
}

// Keys returns the keys in document order.
func (f Fields) Keys() []string { return append([]string(nil), f.keys...) }

// Set adds or replaces a key, preserving first-insertion order.
func (f *Fields) Set(key, value string) {
	if f.values == nil {
		f.values = map[string]string{}
	}
	if _, exists := f.values[key]; !exists {
		f.keys = append(f.keys, key)
	}
	f.values[key] = value
}

// SetIfAbsent adds a key only when it is not already present, so that a
// producer never overwrites a value a human or another tool set deliberately.
func (f *Fields) SetIfAbsent(key, value string) {
	if !f.Has(key) {
		f.Set(key, value)
	}
}

// Delete removes a key.
func (f *Fields) Delete(key string) {
	if f.values == nil {
		return
	}
	if _, ok := f.values[key]; !ok {
		return
	}
	delete(f.values, key)
	for i, k := range f.keys {
		if k == key {
			f.keys = append(f.keys[:i], f.keys[i+1:]...)
			break
		}
	}
}

// List interprets a value as a YAML sequence, accepting both the inline form
// (`tags: [a, b]`) and the block form. Returns nil for an absent key.
func (f Fields) List(key string) []string {
	raw, ok := f.values[key]
	if !ok {
		return nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		inner := strings.TrimSpace(raw[1 : len(raw)-1])
		if inner == "" {
			return nil
		}
		parts := strings.Split(inner, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if v := unquote(strings.TrimSpace(p)); v != "" {
				out = append(out, v)
			}
		}
		return out
	}
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") {
			if v := unquote(strings.TrimSpace(line[2:])); v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}

// Parse reads a concept document.
//
// A document with no frontmatter block is not conformant, but per §11 that is
// reported rather than treated as an error: the body is still returned so a
// consumer can display it. Callers that care use Doc.Conformant.
func Parse(data []byte) Doc {
	text := strings.TrimPrefix(string(data), "\ufeff")
	doc := Doc{Fields: NewFields()}

	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		doc.Body = text
		return doc
	}
	end := strings.Index(rest, "\n---")
	if end < 0 {
		// An unterminated block is unparseable frontmatter (§11.1). Treat the
		// whole file as body so nothing is lost or misread as metadata.
		doc.Body = text
		return doc
	}
	front := rest[:end]
	body := rest[end+len("\n---"):]
	body = strings.TrimPrefix(body, "\n")

	doc.Fields = parseFields(front)
	doc.Body = body
	return doc
}

func parseFields(front string) Fields {
	fields := NewFields()
	var key string
	var buf []string

	flush := func() {
		if key == "" {
			return
		}
		fields.Set(key, strings.TrimRight(strings.Join(buf, "\n"), "\n"))
		key, buf = "", nil
	}

	scanner := bufio.NewScanner(strings.NewReader(front))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" && key == "" {
			continue
		}
		// A continuation line is indented or starts a sequence item; anything
		// else at column zero with a colon begins a new key.
		if key != "" && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.HasPrefix(trimmed, "- ")) {
			buf = append(buf, line)
			continue
		}
		name, value, found := strings.Cut(line, ":")
		if !found || strings.TrimSpace(name) == "" || strings.ContainsAny(strings.TrimSpace(name), " \t") {
			if key != "" {
				buf = append(buf, line)
			}
			continue
		}
		flush()
		key = strings.TrimSpace(name)
		if v := strings.TrimSpace(value); v != "" {
			buf = []string{v}
		}
	}
	flush()
	return fields
}

// Bytes serializes a document back to markdown.
//
// Write is where conformance is enforced: a document without a non-empty
// `type` is not emitted, because producing a non-conformant file is the one
// thing that would break every other consumer of the bundle.
func (d Doc) Bytes() ([]byte, error) {
	if !d.Conformant() {
		return nil, fmt.Errorf("okf: refusing to write a document without a non-empty type (§11.2)")
	}
	var b strings.Builder
	b.WriteString("---\n")
	for _, k := range d.Fields.keys {
		v := d.Fields.values[k]
		if strings.Contains(v, "\n") {
			b.WriteString(k)
			b.WriteString(":\n")
			for _, line := range strings.Split(v, "\n") {
				b.WriteString(line)
				b.WriteString("\n")
			}
			continue
		}
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteString("\n")
	}
	b.WriteString("---\n")
	body := d.Body
	if body != "" && !strings.HasPrefix(body, "\n") {
		b.WriteString("\n")
	}
	b.WriteString(body)
	out := b.String()
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return []byte(out), nil
}

// New builds a minimally conformant document. Callers supply the vocabulary;
// this package does not invent a `type` value, because the useful vocabulary
// belongs to the bundle, not to the tool writing into it.
func New(docType, title string) Doc {
	f := NewFields()
	f.Set("type", docType)
	if title != "" {
		f.Set("title", quoteIfNeeded(title))
	}
	return Doc{Fields: f, Body: ""}
}

// LogEntry appends a line to a §9 log document under today's date heading.
//
// §9 specifies newest-first with `## YYYY-MM-DD` headings. Log files are
// reserved (§3.1) and carry no frontmatter, so this operates on raw text.
func LogEntry(existing, line string, now time.Time) string {
	heading := "## " + now.Format("2006-01-02")
	entry := "* " + strings.TrimSpace(line)

	if strings.TrimSpace(existing) == "" {
		return "# Update Log\n\n" + heading + "\n" + entry + "\n"
	}
	lines := strings.Split(strings.TrimRight(existing, "\n"), "\n")

	// Insert under an existing heading for today, keeping newest entries
	// first within the day.
	for i, l := range lines {
		if strings.TrimSpace(l) == heading {
			out := append([]string{}, lines[:i+1]...)
			out = append(out, entry)
			out = append(out, lines[i+1:]...)
			return strings.Join(out, "\n") + "\n"
		}
	}

	// No heading for today: insert a new one above the newest existing day so
	// the file stays newest-first.
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "## ") {
			out := append([]string{}, lines[:i]...)
			out = append(out, heading, entry, "")
			out = append(out, lines[i:]...)
			return strings.Join(out, "\n") + "\n"
		}
	}
	return strings.Join(lines, "\n") + "\n\n" + heading + "\n" + entry + "\n"
}

// IndexEntry is one line of a §8 directory listing.
type IndexEntry struct {
	Path        string
	Title       string
	Description string
}

// Index renders a §8 index document. Index files are reserved and carry no
// frontmatter; §8 allows a bundle-root index to declare okf_version, which
// callers pass through as version.
func Index(heading, version string, entries []IndexEntry) string {
	var b strings.Builder
	if version != "" {
		b.WriteString("---\nokf_version: ")
		b.WriteString(version)
		b.WriteString("\n---\n\n")
	}
	b.WriteString("# ")
	b.WriteString(heading)
	b.WriteString("\n\n")

	sorted := append([]IndexEntry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	for _, e := range sorted {
		label := e.Title
		if label == "" {
			label = e.Path
		}
		b.WriteString("* [")
		b.WriteString(label)
		b.WriteString("](")
		b.WriteString(e.Path)
		b.WriteString(")")
		if e.Description != "" {
			b.WriteString(" — ")
			b.WriteString(e.Description)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func quoteIfNeeded(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, ":#[]{},&*!|>'\"%@`") || strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}
