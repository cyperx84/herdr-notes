// Package notes is the application core.
//
// Every front end — the CLI, the TUI pane, an agent shelling out, anything
// added later — goes through this package. That is the structural bet of the
// project: if one path can do everything, a new front end costs nothing and
// nothing gets trapped behind the TUI.
package notes

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cyperx84/herdr-notes/internal/bundle"
	"github.com/cyperx84/herdr-notes/internal/okf"
	"github.com/cyperx84/herdr-notes/internal/scope"
)

// App wires a store to a resolved scope. Construct it once, then call.
type App struct {
	Store      bundle.Store
	Resolution scope.Resolution
	EditorArgv []string
	Now        func() time.Time
}

// New builds an App from configuration values.
func New(store bundle.Store, res scope.Resolution, editorArgv []string) *App {
	return &App{
		Store:      store,
		Resolution: res,
		EditorArgv: editorArgv,
		Now:        time.Now,
	}
}

func (a *App) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// Current returns the page the active scope resolves to.
func (a *App) Current() string { return a.Resolution.Page }

// pageOrCurrent lets every command take an optional explicit page, defaulting
// to the scope's page. That is what makes `append "x"` and
// `append other-page "x"` the same command.
func (a *App) pageOrCurrent(name string) string {
	if strings.TrimSpace(name) == "" {
		return a.Current()
	}
	return name
}

// List returns every page in the bundle.
func (a *App) List() ([]bundle.Page, error) { return a.Store.List() }

// Read returns one page.
func (a *App) Read(name string) (bundle.Page, error) {
	return a.Store.Read(a.pageOrCurrent(name))
}

// Append adds a line to a page, creating it if needed.
//
// This is the agent-facing primitive: one command, no format knowledge, safe
// on a page that does not exist yet.
func (a *App) Append(name, line string) error {
	if strings.TrimSpace(line) == "" {
		return errors.New("nothing to append")
	}
	return a.Store.Append(a.pageOrCurrent(name), line, a.now())
}

// Log appends a timestamped entry to the §9 log that covers the current
// scope.
//
// The log lives beside the page it describes only when that directory is the
// subject — a project directory is, but "workspaces/" is just a container of
// unrelated pages, so a workspace-scoped log there would mix every workspace's
// history into one file. In that case the bundle root log is the honest place.
func (a *App) Log(line string) error {
	if strings.TrimSpace(line) == "" {
		return errors.New("nothing to log")
	}
	return a.Store.AppendLog(a.logDir(), line, a.now())
}

// logDir returns the directory whose §9 log describes the current scope.
func (a *App) logDir() string {
	if a.Resolution.Kind != scope.Project {
		return ""
	}
	dir := filepath.ToSlash(filepath.Dir(a.Store.Resolve(a.Current())))
	if dir == "." {
		return ""
	}
	return dir
}

// Edit opens a page in the configured editor, creating it first if absent so
// the editor never opens on a nonexistent path.
func (a *App) Edit(name string) error {
	page := a.pageOrCurrent(name)
	if _, err := a.Store.Read(page); errors.Is(err, bundle.ErrNotFound) {
		title := strings.TrimSuffix(filepath.Base(a.Store.Resolve(page)), ".md")
		doc := okf.New(defaultType(a.Store), title)
		if err := a.Store.Write(bundle.Page{Path: page, Doc: doc}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	argv := a.EditorArgv
	if len(argv) == 0 {
		return errors.New("no editor configured")
	}
	target := filepath.Join(a.Store.Root(), filepath.FromSlash(a.Store.Resolve(page)))
	cmd := exec.Command(argv[0], append(append([]string(nil), argv[1:]...), target)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func defaultType(s bundle.Store) string {
	if d, ok := s.(*bundle.Dir); ok && d.DefaultType != "" {
		return d.DefaultType
	}
	return "Note"
}

// Match is one search hit.
type Match struct {
	Path string
	Line int
	Text string
}

// Search does a case-insensitive substring scan across page bodies.
//
// Deliberately not an index: a bundle is a directory of markdown, and for the
// sizes this handles a scan is fast, has no state to invalidate, and cannot
// disagree with what is on disk. If that stops being true, this is one
// function to replace.
func (a *App) Search(query string) ([]Match, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, errors.New("empty query")
	}
	pages, err := a.List()
	if err != nil {
		return nil, err
	}
	var out []Match
	for _, p := range pages {
		for i, line := range strings.Split(p.Doc.Body, "\n") {
			if strings.Contains(strings.ToLower(line), q) {
				out = append(out, Match{Path: p.Path, Line: i + 1, Text: strings.TrimSpace(line)})
			}
		}
	}
	return out, nil
}

// linkPattern matches a standard markdown link target.
//
// Standard links only. The vault doctrine this format serves is explicit that
// [[wikilinks]] resolve only in Obsidian and must never be emitted, so links
// are read and written in the one form that works in Obsidian, nvim, GitHub,
// and any future consumer.
func linkTargets(body string) []string {
	var out []string
	for i := 0; i < len(body); i++ {
		if body[i] != ']' || i+1 >= len(body) || body[i+1] != '(' {
			continue
		}
		end := strings.IndexByte(body[i+2:], ')')
		if end < 0 {
			continue
		}
		target := strings.TrimSpace(body[i+2 : i+2+end])
		if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "#") {
			continue
		}
		if idx := strings.IndexByte(target, '#'); idx > 0 {
			target = target[:idx]
		}
		if strings.HasSuffix(strings.ToLower(target), ".md") {
			out = append(out, target)
		}
	}
	return out
}

// Links returns the pages a page links to.
func (a *App) Links(name string) ([]string, error) {
	page, err := a.Read(name)
	if err != nil {
		return nil, err
	}
	return dedupe(linkTargets(page.Doc.Body)), nil
}

// Backlinks returns the pages that link to a page. Cheap to compute from a
// full scan and far more useful than forward links for finding where
// something was discussed.
func (a *App) Backlinks(name string) ([]string, error) {
	target := a.Store.Resolve(a.pageOrCurrent(name))
	pages, err := a.List()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range pages {
		if p.Path == target {
			continue
		}
		for _, l := range linkTargets(p.Doc.Body) {
			if a.Store.Resolve(l) == target || a.Store.Resolve(resolveRelative(p.Path, l)) == target {
				out = append(out, p.Path)
				break
			}
		}
	}
	return dedupe(out), nil
}

// resolveRelative interprets a link relative to the linking page's directory,
// so a link inside projects/x/ can point at a sibling.
func resolveRelative(from, link string) string {
	dir := filepath.ToSlash(filepath.Dir(from))
	if dir == "." {
		return link
	}
	return filepath.ToSlash(filepath.Join(dir, link))
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// Health is what `doctor` reports: the resolved state, so that diagnosing a
// surprise is one command rather than reading source.
type Health struct {
	Scope          string
	ScopeLabel     string
	BundleRoot     string
	CurrentPage    string
	PageExists     bool
	PageCount      int
	NonConformant  []string
	EditorArgv     []string
	BundleWritable bool
}

// Doctor inspects the resolved configuration and bundle.
func (a *App) Doctor() (Health, error) {
	h := Health{
		Scope:       string(a.Resolution.Kind),
		ScopeLabel:  a.Resolution.Label,
		BundleRoot:  a.Store.Root(),
		CurrentPage: a.Store.Resolve(a.Current()),
		EditorArgv:  a.EditorArgv,
	}
	if _, err := a.Store.Read(a.Current()); err == nil {
		h.PageExists = true
	}
	pages, err := a.List()
	if err != nil {
		return h, err
	}
	h.PageCount = len(pages)
	for _, p := range pages {
		if !p.Doc.Conformant() {
			h.NonConformant = append(h.NonConformant, p.Path)
		}
	}

	probe := filepath.Join(a.Store.Root(), ".herdr-notes-write-probe")
	if err := os.WriteFile(probe, []byte("x"), 0o600); err == nil {
		h.BundleWritable = true
		_ = os.Remove(probe)
	}
	return h, nil
}

// String renders Health for a terminal.
func (h Health) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "scope:        %s (%s)\n", h.Scope, h.ScopeLabel)
	fmt.Fprintf(&b, "bundle:       %s\n", h.BundleRoot)
	fmt.Fprintf(&b, "writable:     %t\n", h.BundleWritable)
	fmt.Fprintf(&b, "current page: %s (exists: %t)\n", h.CurrentPage, h.PageExists)
	fmt.Fprintf(&b, "pages:        %d\n", h.PageCount)
	fmt.Fprintf(&b, "editor:       %s\n", strings.Join(h.EditorArgv, " "))
	if len(h.NonConformant) > 0 {
		fmt.Fprintf(&b, "non-conformant (readable, missing type): %s\n", strings.Join(h.NonConformant, ", "))
	}
	return b.String()
}
