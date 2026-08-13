package scope

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The point of project scope: every workspace and agent on one repository
// shares a page, so a fleet of agents can read each other's notes.
func TestProjectScopeSharedAcrossWorkspaces(t *testing.T) {
	a := Resolve(Project, Context{WorkspaceID: "w49", RepoRoot: "/src/herdr-loop"})
	b := Resolve(Project, Context{WorkspaceID: "w7C", RepoRoot: "/src/herdr-loop"})
	if a.Page != b.Page {
		t.Fatalf("two workspaces on one repo resolved differently: %q vs %q", a.Page, b.Page)
	}
	if a.Page != "projects/herdr-loop/scratch.md" {
		t.Errorf("page = %q", a.Page)
	}
}

func TestProjectScopeSeparatesDifferentRepos(t *testing.T) {
	a := Resolve(Project, Context{RepoRoot: "/src/herdr-loop"})
	b := Resolve(Project, Context{RepoRoot: "/src/herdr-notes"})
	if a.Page == b.Page {
		t.Fatal("different repositories collapsed onto one page")
	}
}

// Workspace scope keeps its original one-page-per-workspace behaviour.
func TestWorkspaceScopeIsPerWorkspace(t *testing.T) {
	a := Resolve(Workspace, Context{WorkspaceID: "w49"})
	b := Resolve(Workspace, Context{WorkspaceID: "w7C"})
	if a.Page == b.Page {
		t.Fatal("workspace scope shared a page between workspaces")
	}
	if a.Page != "workspaces/w49.md" {
		t.Errorf("page = %q", a.Page)
	}
}

func TestGlobalScopeIsOnePage(t *testing.T) {
	a := Resolve(Global, Context{WorkspaceID: "w49"})
	b := Resolve(Global, Context{WorkspaceID: "w7C"})
	if a.Page != b.Page || a.Page != "global.md" {
		t.Fatalf("global scope not stable: %q vs %q", a.Page, b.Page)
	}
}

// Falling back to a shared page would silently merge unrelated directories,
// which is worse than admitting there is no project.
func TestProjectScopeFallsBackWithoutRepo(t *testing.T) {
	got := Resolve(Project, Context{WorkspaceID: "w49", CWD: t.TempDir()})
	if got.Kind != Workspace {
		t.Fatalf("kind = %q, want a workspace fallback", got.Kind)
	}
	if got.Page != "workspaces/w49.md" {
		t.Errorf("page = %q", got.Page)
	}
	if !strings.Contains(got.Label, "no repository") {
		t.Errorf("label should explain the fallback, got %q", got.Label)
	}
}

func TestResolveWithoutAnyContext(t *testing.T) {
	got := Resolve(Workspace, Context{})
	if got.Page != "global.md" {
		t.Fatalf("page = %q, want a global fallback", got.Page)
	}
	if !strings.Contains(got.Label, "no workspace") {
		t.Errorf("label = %q", got.Label)
	}
}

// A workspace id must never escape the bundle or produce an empty segment.
func TestWorkspaceIdsAreMadePathSafe(t *testing.T) {
	for _, id := range []string{"../../etc", "w49:p1", "  ", "///"} {
		got := Resolve(Workspace, Context{WorkspaceID: id})
		if strings.Contains(got.Page, "..") || strings.Contains(got.Page, "//") {
			t.Errorf("id %q produced unsafe page %q", id, got.Page)
		}
	}
	if got := Resolve(Workspace, Context{WorkspaceID: "w49:p1"}); got.Page != "workspaces/w49-p1.md" {
		t.Errorf("page = %q", got.Page)
	}
}

// A linked worktree must map back to its parent repository, otherwise every
// agent worktree would get its own disconnected page — the exact failure this
// scope exists to prevent.
func TestProjectScopeMapsWorktreeToParentRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	base := t.TempDir()
	repo := filepath.Join(base, "myrepo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(repo, "add", ".")
	run(repo, "commit", "-m", "init")

	wt := filepath.Join(base, "worktrees", "feature")
	run(repo, "worktree", "add", "-b", "feature", wt)

	main := Resolve(Project, Context{CWD: repo})
	linked := Resolve(Project, Context{CWD: wt})
	if main.Page != linked.Page {
		t.Fatalf("worktree did not map to parent repo: %q vs %q", main.Page, linked.Page)
	}
	if main.Page != "projects/myrepo/scratch.md" {
		t.Errorf("page = %q", main.Page)
	}
}

func TestKindValidation(t *testing.T) {
	for _, k := range []Kind{Workspace, Project, Global} {
		if !k.Valid() {
			t.Errorf("%q should be valid", k)
		}
	}
	if Kind("fleet").Valid() {
		t.Error("unknown kind reported valid")
	}
}
