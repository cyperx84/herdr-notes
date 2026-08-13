package notes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cyperx84/herdr-notes/internal/bundle"
	"github.com/cyperx84/herdr-notes/internal/scope"
)

func testApp(t *testing.T) *App {
	t.Helper()
	store, err := bundle.Open(t.TempDir(), "Note")
	if err != nil {
		t.Fatal(err)
	}
	res := scope.Resolve(scope.Project, scope.Context{RepoRoot: "/src/demo"})
	app := New(store, res, []string{"true"})
	app.Now = func() time.Time { return time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC) }
	return app
}

// Every command defaults to the scope's page, so an agent can call
// `append "x"` without knowing which page it is writing to.
func TestAppendDefaultsToScopePage(t *testing.T) {
	app := testApp(t)
	if err := app.Append("", "from an agent"); err != nil {
		t.Fatal(err)
	}
	page, err := app.Read("")
	if err != nil {
		t.Fatal(err)
	}
	if page.Path != "projects/demo/scratch.md" {
		t.Errorf("path = %q, want the project scratch page", page.Path)
	}
	if !strings.Contains(page.Doc.Body, "from an agent") {
		t.Errorf("body = %q", page.Doc.Body)
	}
}

func TestAppendToExplicitPage(t *testing.T) {
	app := testApp(t)
	if err := app.Append("handoff", "context for the next agent"); err != nil {
		t.Fatal(err)
	}
	page, err := app.Read("handoff")
	if err != nil {
		t.Fatal(err)
	}
	if !page.Doc.Conformant() {
		t.Error("explicit page is not conformant")
	}
	if !strings.Contains(page.Doc.Body, "context for the next agent") {
		t.Errorf("body = %q", page.Doc.Body)
	}
}

func TestAppendRejectsEmpty(t *testing.T) {
	app := testApp(t)
	if err := app.Append("", "   "); err == nil {
		t.Fatal("expected an error for an empty append")
	}
}

func TestSearchAcrossPages(t *testing.T) {
	app := testApp(t)
	mustAppend(t, app, "a", "the deploy runbook lives here")
	mustAppend(t, app, "b", "unrelated content")
	mustAppend(t, app, "c", "DEPLOY in caps")

	hits, err := app.Search("deploy")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2 (case-insensitive): %#v", len(hits), hits)
	}
	for _, h := range hits {
		if h.Line < 1 {
			t.Errorf("line numbers should be 1-based: %#v", h)
		}
	}
}

func TestLinksAndBacklinks(t *testing.T) {
	app := testApp(t)
	mustAppend(t, app, "hub", "see [runbook](runbook.md) and [external](https://example.com)")
	mustAppend(t, app, "runbook", "the runbook")
	mustAppend(t, app, "other", "also links to [runbook](runbook.md)")

	links, err := app.Links("hub")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0] != "runbook.md" {
		t.Errorf("links = %#v, want only the internal markdown link", links)
	}

	back, err := app.Backlinks("runbook")
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 2 {
		t.Fatalf("backlinks = %#v, want hub and other", back)
	}
}

// A page must not count as its own backlink.
func TestBacklinksExcludesSelf(t *testing.T) {
	app := testApp(t)
	mustAppend(t, app, "self", "a link to [self](self.md)")
	back, err := app.Backlinks("self")
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 0 {
		t.Errorf("backlinks = %#v, want none", back)
	}
}

func TestLogWritesToScopeDirectory(t *testing.T) {
	app := testApp(t)
	if err := app.Log("agent finished a task"); err != nil {
		t.Fatal(err)
	}
	// The scope page is projects/demo/scratch.md, so §9's log sits beside it.
	data, err := os.ReadFile(filepath.Join(app.Store.Root(), "projects", "demo", "log.md"))
	if err != nil {
		t.Fatalf("log not written next to the scope page: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "## 2026-08-13") || !strings.Contains(got, "agent finished a task") {
		t.Errorf("log contents:\n%s", got)
	}
	if strings.HasPrefix(got, "---") {
		t.Error("log.md is reserved and must not carry frontmatter")
	}
}

func TestDoctorReportsResolvedState(t *testing.T) {
	app := testApp(t)
	mustAppend(t, app, "", "content")

	h, err := app.Doctor()
	if err != nil {
		t.Fatal(err)
	}
	if h.Scope != string(scope.Project) {
		t.Errorf("scope = %q", h.Scope)
	}
	if !h.PageExists {
		t.Error("page should exist after an append")
	}
	if !h.BundleWritable {
		t.Error("temp bundle should be writable")
	}
	if h.PageCount < 1 {
		t.Errorf("page count = %d", h.PageCount)
	}
	if !strings.Contains(h.String(), "scope:") {
		t.Errorf("doctor output missing fields:\n%s", h.String())
	}
}

// §11: a non-conformant page is reported, never hidden and never fatal.
func TestDoctorReportsNonConformantWithoutFailing(t *testing.T) {
	app := testApp(t)
	dir := app.Store.(*bundle.Dir)
	if err := os.WriteFile(filepath.Join(dir.Root(), "loose.md"), []byte("no frontmatter\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := app.Doctor()
	if err != nil {
		t.Fatalf("doctor should not fail on a non-conformant page: %v", err)
	}
	if len(h.NonConformant) != 1 || h.NonConformant[0] != "loose.md" {
		t.Errorf("non-conformant = %#v", h.NonConformant)
	}
}

// A workspace-scoped log must not land in "workspaces/log.md", which would
// merge unrelated workspaces into one history.
func TestWorkspaceScopeLogsAtBundleRoot(t *testing.T) {
	store, err := bundle.Open(t.TempDir(), "Note")
	if err != nil {
		t.Fatal(err)
	}
	app := New(store, scope.Resolve(scope.Workspace, scope.Context{WorkspaceID: "w49"}), []string{"true"})
	app.Now = func() time.Time { return time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC) }

	if err := app.Log("workspace event"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.Root(), "workspaces", "log.md")); err == nil {
		t.Error("workspace log written into the shared workspaces/ container")
	}
	data, err := os.ReadFile(filepath.Join(store.Root(), "log.md"))
	if err != nil {
		t.Fatalf("expected a root log: %v", err)
	}
	if !strings.Contains(string(data), "workspace event") {
		t.Errorf("root log:\n%s", data)
	}
}

func mustAppend(t *testing.T, app *App, page, line string) {
	t.Helper()
	if err := app.Append(page, line); err != nil {
		t.Fatal(err)
	}
}
