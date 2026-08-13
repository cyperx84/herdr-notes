// Package bundle is the storage seam.
//
// Every design iteration of this plugin has changed one of two things: where
// notes live, or what a "scope" resolves to. Those are therefore the two
// places worth a real interface, and close to the only ones — abstracting
// link resolution or search before a second implementation exists would be
// guessing, and each guess becomes a constraint.
//
// A Bundle is a directory tree of OKF documents. That is the whole contract:
// no database, no index, no lock file. If this program disappears, what is
// left on disk is a directory of markdown that Obsidian, nvim, ripgrep, and
// git all read today.
package bundle

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cyperx84/herdr-notes/internal/okf"
)

// ErrNotFound is returned by Read for a page that does not exist.
var ErrNotFound = errors.New("page not found")

// Page is one concept document plus its location in the bundle.
type Page struct {
	// Path is the bundle-relative slug, always forward-slashed and always
	// ending in .md — the same string used in links, so a page's identity and
	// its link target never diverge.
	Path string
	Doc  okf.Doc
}

// Title returns the page's title, falling back to its path.
func (p Page) Title() string {
	if t := p.Doc.Title(); t != "" {
		return t
	}
	return strings.TrimSuffix(p.Path, ".md")
}

// Store is the storage seam. One implementation lives in this package
// (a plain directory); the interface exists so that a second — a remote
// bundle, a git-backed store, something not yet imagined — costs nothing
// elsewhere in the program.
type Store interface {
	// Root is the absolute path of the bundle on disk, for display and for
	// handing to an external editor.
	Root() string
	// List returns every concept document, excluding reserved files (§3.1).
	List() ([]Page, error)
	// Read returns one page, or ErrNotFound.
	Read(path string) (Page, error)
	// Write persists a page, creating parent directories as needed.
	Write(p Page) error
	// Append adds a line to a page's body, creating the page if absent.
	Append(path, line string, now time.Time) error
	// AppendLog adds a line to the §9 log document for a directory.
	AppendLog(dir, line string, now time.Time) error
	// Resolve turns a user-supplied name into a bundle path.
	Resolve(name string) string
}

// Dir is a Store backed by a plain directory tree.
type Dir struct {
	root string
	// DefaultType is the `type` written into newly created documents. The
	// useful vocabulary belongs to the bundle, not to this tool, so it is
	// configuration rather than a constant.
	DefaultType string
}

// Open returns a Dir rooted at path, creating it if necessary.
func Open(root, defaultType string) (*Dir, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("bundle: root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("bundle: create %s: %w", abs, err)
	}
	if defaultType == "" {
		defaultType = "Note"
	}
	return &Dir{root: abs, DefaultType: defaultType}, nil
}

// Root implements Store.
func (d *Dir) Root() string { return d.root }

// Resolve maps a user-supplied page name to a bundle-relative path.
//
// Names are accepted with or without the .md extension so that "deploy" and
// "deploy.md" are the same page — a link target and a CLI argument should not
// be two different vocabularies. Path traversal is rejected by construction:
// the result is always inside the bundle.
func (d *Dir) Resolve(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimPrefix(name, "./")
	name = strings.TrimPrefix(name, "/")
	if name == "" {
		return okf.IndexFile
	}
	if !strings.HasSuffix(strings.ToLower(name), ".md") {
		name += ".md"
	}
	// Clean resolves any ".." segments, then any remaining escape is clamped
	// to the bundle root rather than silently reading someone's ~/.ssh.
	clean := filepath.ToSlash(filepath.Clean("/" + name))
	return strings.TrimPrefix(clean, "/")
}

func (d *Dir) abs(path string) string {
	return filepath.Join(d.root, filepath.FromSlash(path))
}

// List implements Store, skipping reserved files and unreadable entries.
//
// A single malformed document must not make the whole bundle unlistable:
// §11 requires consumers to tolerate what they do not understand.
func (d *Dir) List() ([]Page, error) {
	var pages []Page
	err := filepath.WalkDir(d.root, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") && p != d.root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".md") || okf.IsReserved(entry.Name()) {
			return nil
		}
		rel, relErr := filepath.Rel(d.root, p)
		if relErr != nil {
			return nil
		}
		data, readErr := os.ReadFile(p)
		if readErr != nil {
			return nil
		}
		pages = append(pages, Page{Path: filepath.ToSlash(rel), Doc: okf.Parse(data)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(pages, func(i, j int) bool { return pages[i].Path < pages[j].Path })
	return pages, nil
}

// Read implements Store.
func (d *Dir) Read(path string) (Page, error) {
	rel := d.Resolve(path)
	data, err := os.ReadFile(d.abs(rel))
	if errors.Is(err, os.ErrNotExist) {
		return Page{}, fmt.Errorf("%w: %s", ErrNotFound, rel)
	}
	if err != nil {
		return Page{}, err
	}
	return Page{Path: rel, Doc: okf.Parse(data)}, nil
}

// Write implements Store with an atomic replace, so a crash mid-write can
// never truncate an existing page.
func (d *Dir) Write(p Page) error {
	rel := d.Resolve(p.Path)
	data, err := p.Doc.Bytes()
	if err != nil {
		return err
	}
	target := d.abs(rel)
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".herdr-notes-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)

	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		return err
	}
	return os.Rename(name, target)
}

// Append implements Store.
//
// This is the command agents call, so it has to be safe to call from anywhere
// with no knowledge of the format: a missing page is created conformant, and
// an existing page keeps its frontmatter untouched.
func (d *Dir) Append(path, line string, now time.Time) error {
	rel := d.Resolve(path)
	page, err := d.Read(rel)
	switch {
	case errors.Is(err, ErrNotFound):
		title := strings.TrimSuffix(filepath.Base(rel), ".md")
		page = Page{Path: rel, Doc: okf.New(d.DefaultType, title)}
	case err != nil:
		return err
	}
	body := strings.TrimRight(page.Doc.Body, "\n")
	entry := strings.TrimRight(line, "\n")
	if body == "" {
		body = entry
	} else {
		body += "\n" + entry
	}
	page.Doc.Body = body + "\n"
	return d.Write(page)
}

// AppendLog implements Store for §9 log documents. Log files are reserved and
// carry no frontmatter, so they are written as raw text.
func (d *Dir) AppendLog(dir, line string, now time.Time) error {
	rel := okf.LogFile
	if dir = strings.Trim(strings.TrimSpace(dir), "/"); dir != "" {
		rel = dir + "/" + okf.LogFile
	}
	target := d.abs(rel)
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	existing, err := os.ReadFile(target)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.WriteFile(target, []byte(okf.LogEntry(string(existing), line, now)), 0o600)
}

// WriteIndex regenerates a §8 index for the bundle root. It is not part of
// Store because it is a maintenance operation, not a storage primitive.
func (d *Dir) WriteIndex(heading, version string) error {
	pages, err := d.List()
	if err != nil {
		return err
	}
	entries := make([]okf.IndexEntry, 0, len(pages))
	for _, p := range pages {
		entries = append(entries, okf.IndexEntry{
			Path:        p.Path,
			Title:       p.Title(),
			Description: p.Doc.Fields.Get("description"),
		})
	}
	return os.WriteFile(d.abs(okf.IndexFile), []byte(okf.Index(heading, version, entries)), 0o600)
}

var _ Store = (*Dir)(nil)
