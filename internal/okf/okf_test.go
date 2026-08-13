package okf

import (
	"strings"
	"testing"
	"time"
)

// §11.1/§11.2: a conformant document has parseable frontmatter with a
// non-empty type. Round-tripping must not disturb the body.
func TestParseRoundTrip(t *testing.T) {
	src := "---\ntype: Note\ntitle: \"Deploy runbook\"\ntags: [ops, deploy]\n---\n\n# Body\n\ntext here\n"
	doc := Parse([]byte(src))
	if !doc.Conformant() {
		t.Fatal("expected conformant document")
	}
	if doc.Type() != "Note" {
		t.Errorf("type = %q, want Note", doc.Type())
	}
	if doc.Title() != "Deploy runbook" {
		t.Errorf("title = %q, want unquoted title", doc.Title())
	}
	if got := doc.Fields.List("tags"); len(got) != 2 || got[0] != "ops" || got[1] != "deploy" {
		t.Errorf("tags = %#v, want [ops deploy]", got)
	}
	if !strings.Contains(doc.Body, "# Body") {
		t.Errorf("body lost: %q", doc.Body)
	}
	out, err := doc.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	again := Parse(out)
	if again.Type() != "Note" || again.Title() != "Deploy runbook" {
		t.Errorf("round trip lost fields: %q / %q", again.Type(), again.Title())
	}
	if strings.TrimSpace(again.Body) != strings.TrimSpace(doc.Body) {
		t.Errorf("round trip changed body:\n%q\n%q", again.Body, doc.Body)
	}
}

// §11: consumers MUST NOT reject a bundle for unknown keys, and a rewrite
// that silently dropped them would corrupt another producer's data.
func TestUnknownKeysSurviveRoundTrip(t *testing.T) {
	src := "---\ntype: Concept\nsources:\n  - path: a.md\n    kind: derived\nweird_key: 42\n---\nbody\n"
	doc := Parse([]byte(src))
	out, err := doc.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	got := string(out)
	for _, want := range []string{"weird_key: 42", "sources:", "- path: a.md", "kind: derived"} {
		if !strings.Contains(got, want) {
			t.Errorf("round trip dropped %q:\n%s", want, got)
		}
	}
}

// §11.2 is the one rule this package enforces on write: emitting a document
// with no type would break every other consumer of the bundle.
func TestWriteRefusesMissingType(t *testing.T) {
	doc := Doc{Fields: NewFields(), Body: "orphan"}
	if _, err := doc.Bytes(); err == nil {
		t.Fatal("expected refusal to write a document without type")
	}
}

// A file with no frontmatter is not conformant, but §11 forbids rejecting it
// outright — the body must still be readable.
func TestParseNonConformantKeepsBody(t *testing.T) {
	doc := Parse([]byte("# Just markdown\n\nno frontmatter\n"))
	if doc.Conformant() {
		t.Error("expected non-conformant")
	}
	if !strings.Contains(doc.Body, "Just markdown") {
		t.Errorf("body lost: %q", doc.Body)
	}
}

func TestParseUnterminatedFrontmatterKeepsEverything(t *testing.T) {
	doc := Parse([]byte("---\ntype: Note\nnever closed\n"))
	if doc.Conformant() {
		t.Error("unterminated frontmatter is unparseable (§11.1), not conformant")
	}
	if !strings.Contains(doc.Body, "type: Note") {
		t.Errorf("content lost when frontmatter was unterminated: %q", doc.Body)
	}
}

// §3.1: reserved filenames must not be treated as concept documents.
func TestReservedFilenames(t *testing.T) {
	for _, name := range []string{"index.md", "log.md"} {
		if !IsReserved(name) {
			t.Errorf("%s should be reserved", name)
		}
	}
	if IsReserved("scratch.md") {
		t.Error("concept document treated as reserved")
	}
}

// §9: newest-first, "## YYYY-MM-DD" headings.
func TestLogEntryCreatesAndPrepends(t *testing.T) {
	now := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)
	first := LogEntry("", "did a thing", now)
	if !strings.Contains(first, "## 2026-08-13") || !strings.Contains(first, "* did a thing") {
		t.Fatalf("unexpected first entry:\n%s", first)
	}

	same := LogEntry(first, "did another", now)
	if strings.Count(same, "## 2026-08-13") != 1 {
		t.Errorf("same-day entry duplicated the heading:\n%s", same)
	}
	if strings.Index(same, "did another") > strings.Index(same, "did a thing") {
		t.Errorf("entries within a day should be newest-first:\n%s", same)
	}

	next := LogEntry(same, "next day", now.AddDate(0, 0, 1))
	if strings.Index(next, "## 2026-08-14") > strings.Index(next, "## 2026-08-13") {
		t.Errorf("days should be newest-first:\n%s", next)
	}
}

// §8: index files are a directory listing; the bundle root may declare
// okf_version.
func TestIndexRendersListing(t *testing.T) {
	got := Index("openwiki", "0.2", []IndexEntry{
		{Path: "b.md", Title: "Bee"},
		{Path: "a.md", Title: "Ay", Description: "first"},
	})
	if !strings.HasPrefix(got, "---\nokf_version: 0.2\n---") {
		t.Errorf("root index should declare okf_version:\n%s", got)
	}
	if strings.Index(got, "a.md") > strings.Index(got, "b.md") {
		t.Errorf("entries should be ordered:\n%s", got)
	}
	if !strings.Contains(got, "[Ay](a.md) — first") {
		t.Errorf("description missing:\n%s", got)
	}
}

func TestSetIfAbsentDoesNotOverride(t *testing.T) {
	doc := New("Note", "Title")
	doc.Fields.SetIfAbsent("status", "draft")
	doc.Fields.SetIfAbsent("status", "stable")
	if got := doc.Fields.Get("status"); got != "draft" {
		t.Errorf("status = %q, want the first value kept", got)
	}
}

// Titles containing YAML metacharacters must survive a round trip.
func TestTitleQuotingRoundTrip(t *testing.T) {
	doc := New("Note", "fix: the thing [urgent]")
	out, err := doc.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if got := Parse(out).Title(); got != "fix: the thing [urgent]" {
		t.Errorf("title round trip = %q", got)
	}
}
