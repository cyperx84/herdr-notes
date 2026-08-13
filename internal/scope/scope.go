// Package scope resolves "which notes am I looking at right now".
//
// This is the second seam. It changed repeatedly during design — workspace,
// then project, then fleet — which is exactly the signal that it deserves an
// interface rather than an if-statement.
//
// A scope answers two questions: which bundle directory, and which page
// inside it. Everything else in the program takes those two strings and stops
// caring where they came from.
package scope

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Kind is the selector a user configures.
type Kind string

const (
	// Workspace is one page per herdr workspace: the original behaviour.
	Workspace Kind = "workspace"
	// Project is one page shared by every workspace, tab, and agent working
	// on the same repository. This is the "fleet" case — several agents on
	// one project reading and writing one shared page.
	Project Kind = "project"
	// Global is a single page everywhere.
	Global Kind = "global"
)

// Valid reports whether k is a known scope.
func (k Kind) Valid() bool {
	switch k {
	case Workspace, Project, Global:
		return true
	}
	return false
}

// Context is the environment a resolution runs against. It is a plain struct
// rather than direct env access so that resolution is testable without
// mutating global state.
type Context struct {
	WorkspaceID string
	CWD         string
	// RepoRoot, when set, short-circuits git discovery. Mainly for tests.
	RepoRoot string
}

// FromEnv builds a Context from herdr's injected environment.
func FromEnv() Context {
	cwd, _ := os.Getwd()
	return Context{
		WorkspaceID: strings.TrimSpace(os.Getenv("HERDR_WORKSPACE_ID")),
		CWD:         cwd,
	}
}

// Resolution is the outcome: which page, and a human-readable explanation of
// why. The label exists because a shared page that silently points somewhere
// unexpected is worse than no shared page at all — `doctor` prints this.
type Resolution struct {
	Kind  Kind
	Page  string
	Label string
}

// Resolve maps a scope and context onto a page path inside the bundle.
//
// Project scope deliberately keys on the repository, not the workspace: two
// worktrees of one repo are the same project, which is what makes notes
// shared across a fleet of agents instead of fragmented per pane.
func Resolve(k Kind, ctx Context) Resolution {
	switch k {
	case Global:
		return Resolution{Kind: Global, Page: "global.md", Label: "global"}

	case Project:
		if slug := projectSlug(ctx); slug != "" {
			return Resolution{
				Kind:  Project,
				Page:  "projects/" + slug + "/scratch.md",
				Label: "project " + slug,
			}
		}
		// No repository: fall back rather than silently sharing one page
		// between unrelated directories.
		if id := safeSegment(ctx.WorkspaceID); id != "" {
			return Resolution{
				Kind:  Workspace,
				Page:  "workspaces/" + id + ".md",
				Label: "workspace " + ctx.WorkspaceID + " (no repository found)",
			}
		}
		return Resolution{Kind: Global, Page: "global.md", Label: "global (no repository or workspace)"}

	default:
		if id := safeSegment(ctx.WorkspaceID); id != "" {
			return Resolution{
				Kind:  Workspace,
				Page:  "workspaces/" + id + ".md",
				Label: "workspace " + ctx.WorkspaceID,
			}
		}
		return Resolution{Kind: Global, Page: "global.md", Label: "global (no workspace id)"}
	}
}

// projectSlug derives a stable project identifier from the git repository
// containing ctx.CWD.
//
// The slug comes from the repository's canonical directory name rather than
// the current path, so a worktree at ~/github/herdr-loop-worktrees/fix-x and
// the main checkout at ~/github/herdr-loop resolve to the same project. That
// is the property that makes this useful for agents working in worktrees.
func projectSlug(ctx Context) string {
	root := ctx.RepoRoot
	if root == "" {
		root = gitCommonRoot(ctx.CWD)
	}
	if root == "" {
		return ""
	}
	return safeSegment(filepath.Base(root))
}

// gitCommonRoot returns the main repository directory for cwd, resolving
// worktrees back to their origin.
func gitCommonRoot(cwd string) string {
	if cwd == "" {
		return ""
	}
	// --git-common-dir points at the shared .git directory even from inside a
	// linked worktree, which is precisely how a worktree is mapped back to
	// its parent repository.
	out, err := runGit(cwd, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil || out == "" {
		return ""
	}
	common := filepath.Clean(out)
	// A normal checkout gives <repo>/.git; a bare repo gives <repo>.git.
	if filepath.Base(common) == ".git" {
		return filepath.Dir(common)
	}
	return strings.TrimSuffix(common, ".git")
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stderr = nil
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// safeSegment reduces an arbitrary identifier to something safe to embed in a
// path: lowercase, alphanumerics and dashes. An identifier that reduces to
// nothing returns "", so callers fall back rather than writing to a path made
// entirely of separators.
func safeSegment(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		default:
			// Every other rune — separators, punctuation, path characters —
			// collapses to a single dash rather than being deleted. Dropping
			// them would let "w49:p1" and "w49p1" collide onto one page, which
			// is exactly the accidental sharing this function exists to stop.
			if b.Len() > 0 && !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
