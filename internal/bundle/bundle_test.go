package bundle

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testDir(t *testing.T) *Dir {
	t.Helper()
	d, err := Open(t.TempDir(), "Note")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// A page name and a link target must be the same vocabulary, so "deploy",
// "deploy.md", and "./deploy.md" are one page.
func TestResolveNormalizesNames(t *testing.T) {
	d := testDir(t)
	for _, in := range []string{"deploy", "deploy.md", "./deploy.md", "/deploy.md"} {
		if got := d.Resolve(in); got != "deploy.md" {
			t.Errorf("Resolve(%q) = %q, want deploy.md", in, got)
		}
	}
	if got := d.Resolve(""); got != "index.md" {
		t.Errorf("empty name = %q, want index.md", got)
	}
	if got := d.Resolve("proj/sub/page"); got != "proj/sub/page.md" {
		t.Errorf("nested = %q", got)
	}
}

// Escaping the bundle would let an agent's append target arbitrary files.
func TestResolveClampsTraversal(t *testing.T) {
	d := testDir(t)
	for _, in := range []string{"../../etc/passwd", "../outside", "a/../../b"} {
		got := d.Resolve(in)
		if strings.Contains(got, "..") || strings.HasPrefix(got, "/") {
			t.Errorf("Resolve(%q) = %q escaped the bundle", in, got)
		}
	}
}

// Append is the agent-facing command: it must create a conformant page
// without the caller knowing anything about OKF.
func TestAppendCreatesConformantPage(t *testing.T) {
	d := testDir(t)
	now := time.Now()
	if err := d.Append("scratch", "first line", now); err != nil {
		t.Fatal(err)
	}
	page, err := d.Read("scratch")
	if err != nil {
		t.Fatal(err)
	}
	if !page.Doc.Conformant() {
		t.Error("appended page is not OKF-conformant")
	}
	if page.Doc.Type() != "Note" {
		t.Errorf("type = %q, want the configured default", page.Doc.Type())
	}
	if !strings.Contains(page.Doc.Body, "first line") {
		t.Errorf("body = %q", page.Doc.Body)
	}

	if err := d.Append("scratch", "second line", now); err != nil {
		t.Fatal(err)
	}
	page, _ = d.Read("scratch")
	if !strings.Contains(page.Doc.Body, "first line") || !strings.Contains(page.Doc.Body, "second line") {
		t.Errorf("append lost content: %q", page.Doc.Body)
	}
	if strings.Index(page.Doc.Body, "first line") > strings.Index(page.Doc.Body, "second line") {
		t.Error("append should preserve chronological order in the body")
	}
}

// Appending must never disturb frontmatter a human or another tool set.
func TestAppendPreservesExistingFrontmatter(t *testing.T) {
	d := testDir(t)
	raw := "---\ntype: Project\ntitle: Kept\nproject: fleet-provisioning\ncustom: yes\n---\n\noriginal\n"
	if err := os.WriteFile(filepath.Join(d.Root(), "hub.md"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := d.Append("hub", "appended", time.Now()); err != nil {
		t.Fatal(err)
	}
	page, err := d.Read("hub")
	if err != nil {
		t.Fatal(err)
	}
	if page.Doc.Type() != "Project" {
		t.Errorf("type changed to %q", page.Doc.Type())
	}
	if page.Doc.Fields.Get("project") != "fleet-provisioning" {
		t.Error("append dropped an existing field")
	}
	if page.Doc.Fields.Get("custom") != "yes" {
		t.Error("append dropped an unknown field")
	}
	if !strings.Contains(page.Doc.Body, "original") || !strings.Contains(page.Doc.Body, "appended") {
		t.Errorf("body = %q", page.Doc.Body)
	}
}

// §3.1: reserved files are structural, not concept documents.
func TestListSkipsReservedAndTolerNonConformant(t *testing.T) {
	d := testDir(t)
	write := func(name, content string) {
		t.Helper()
		p := filepath.Join(d.Root(), name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("index.md", "# listing\n")
	write("log.md", "# log\n")
	write("good.md", "---\ntype: Note\n---\nok\n")
	write("nested/deep.md", "---\ntype: Note\n---\nok\n")
	write("broken.md", "no frontmatter at all\n")

	pages, err := d.List()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, p := range pages {
		got[p.Path] = true
	}
	if got["index.md"] || got["log.md"] {
		t.Error("reserved files listed as concept documents")
	}
	if !got["good.md"] || !got["nested/deep.md"] {
		t.Errorf("missing concept documents: %#v", got)
	}
	// §11: a malformed document must not be dropped or make List fail.
	if !got["broken.md"] {
		t.Error("non-conformant document was silently hidden")
	}
}

func TestReadMissingReturnsNotFound(t *testing.T) {
	d := testDir(t)
	if _, err := d.Read("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// §9 log ordering, exercised through the store.
func TestAppendLogWritesReservedFile(t *testing.T) {
	d := testDir(t)
	now := time.Date(2026, 8, 13, 9, 0, 0, 0, time.UTC)
	if err := d.AppendLog("", "root event", now); err != nil {
		t.Fatal(err)
	}
	if err := d.AppendLog("projects/eve", "project event", now); err != nil {
		t.Fatal(err)
	}
	root, err := os.ReadFile(filepath.Join(d.Root(), "log.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(root), "## 2026-08-13") || !strings.Contains(string(root), "root event") {
		t.Errorf("root log:\n%s", root)
	}
	nested, err := os.ReadFile(filepath.Join(d.Root(), "projects", "eve", "log.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(nested), "project event") {
		t.Errorf("nested log:\n%s", nested)
	}
	// A log is reserved and carries no frontmatter (§3.1/§9).
	if strings.HasPrefix(string(root), "---") {
		t.Error("log.md must not carry frontmatter")
	}
}

func TestWriteIsAtomicAndLeavesNoTempFiles(t *testing.T) {
	d := testDir(t)
	if err := d.Append("page", "content", time.Now()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(d.Root())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestWriteIndexListsPages(t *testing.T) {
	d := testDir(t)
	now := time.Now()
	if err := d.Append("alpha", "a", now); err != nil {
		t.Fatal(err)
	}
	if err := d.Append("beta", "b", now); err != nil {
		t.Fatal(err)
	}
	if err := d.WriteIndex("herdr-notes", "0.2"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(d.Root(), "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "okf_version: 0.2") {
		t.Errorf("root index should declare okf_version:\n%s", got)
	}
	for _, want := range []string{"alpha.md", "beta.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("index missing %s:\n%s", want, got)
		}
	}
}
