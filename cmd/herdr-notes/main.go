// Command herdr-notes is an OKF bundle of markdown notes for herdr.
//
// The CLI is the complete surface and the TUI is one client of it. That is
// deliberate: agents, shell, editors, and any future front end all take the
// same path, so nothing is trapped behind the pane.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cyperx84/herdr-notes/internal/bundle"
	"github.com/cyperx84/herdr-notes/internal/config"
	"github.com/cyperx84/herdr-notes/internal/launcher"
	"github.com/cyperx84/herdr-notes/internal/notes"
	"github.com/cyperx84/herdr-notes/internal/scope"
	"github.com/cyperx84/herdr-notes/internal/tui"
)

var version = "dev"

const usage = `herdr-notes — OKF markdown notes for herdr

usage:
  herdr-notes [flags]                 open the note pane (TUI)
  herdr-notes ls                      list pages in the bundle
  herdr-notes show [page]             print a page as markdown
  herdr-notes edit [page]             open a page in $EDITOR (nvim by default)
  herdr-notes append [page] <text>    append a line; creates the page if absent
  herdr-notes log <text>              append to the directory log (OKF §9)
  herdr-notes search <query>          search page bodies
  herdr-notes links [page]            pages this page links to
  herdr-notes backlinks [page]        pages that link here
  herdr-notes path [page]             print a page's absolute path
  herdr-notes index                   regenerate index.md (OKF §8)
  herdr-notes doctor                  print resolved configuration
  herdr-notes version

flags:
  --scope workspace|project|global    what a note is about
  --bundle <dir>                      OKF bundle root
  --workspace <id>                    override the herdr workspace id

Pages are plain markdown with OKF frontmatter. Nothing here is a database:
if this program disappears, a directory of markdown remains.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "herdr-notes:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("herdr-notes", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }

	var (
		scopeFlag   = fs.String("scope", "", "workspace|project|global")
		bundleFlag  = fs.String("bundle", "", "OKF bundle root")
		workspace   = fs.String("workspace", "", "herdr workspace id")
		notesDir    = fs.String("notes-dir", "", "deprecated alias for --bundle")
		showPath    = fs.Bool("path", false, "print the current page's path")
		external    = fs.Bool("external", false, "open the current page in the editor")
		toggle      = fs.Bool("toggle", false, "toggle the herdr notes pane")
		showVersion = fs.Bool("version", false, "print version")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Println(version)
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if *workspace != "" {
		cfg.Workspace = *workspace
	}
	if *scopeFlag != "" {
		cfg.Scope = *scopeFlag
	}
	if *bundleFlag != "" {
		cfg.BundleDir = *bundleFlag
	}
	if *notesDir != "" {
		cfg.BundleDir = *notesDir
	}

	app, err := buildApp(cfg)
	if err != nil {
		return err
	}

	rest := fs.Args()
	// Legacy flags remain so existing keybindings and manifests keep working.
	switch {
	case *showPath && len(rest) == 0:
		return printPath(app, "")
	case *external && len(rest) == 0:
		return app.Edit("")
	}
	if *toggle {
		return togglePane(cfg)
	}
	if len(rest) == 0 {
		return runTUI(app)
	}

	cmd, cmdArgs := rest[0], rest[1:]
	switch cmd {
	case "ls", "list":
		return list(app)
	case "show", "cat":
		return show(app, first(cmdArgs))
	case "edit":
		return app.Edit(first(cmdArgs))
	case "append":
		return appendLine(app, cmdArgs)
	case "log":
		return app.Log(strings.Join(cmdArgs, " "))
	case "search", "grep":
		return search(app, strings.Join(cmdArgs, " "))
	case "links":
		return printList(app.Links(first(cmdArgs)))
	case "backlinks":
		return printList(app.Backlinks(first(cmdArgs)))
	case "path":
		return printPath(app, first(cmdArgs))
	case "index":
		return writeIndex(app)
	case "doctor":
		return doctor(app)
	case "version":
		fmt.Println(version)
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q (try: herdr-notes help)", cmd)
	}
}

func buildApp(cfg config.Config) (*notes.App, error) {
	root, err := cfg.ResolveBundleDir(strings.TrimSpace(os.Getenv("HERDR_PLUGIN_STATE_DIR")))
	if err != nil {
		return nil, err
	}
	st, err := bundle.Open(root, cfg.ResolveDocType())
	if err != nil {
		return nil, err
	}

	kind := scope.Kind(strings.ToLower(strings.TrimSpace(cfg.Scope)))
	if !kind.Valid() {
		if cfg.Scope != "" {
			return nil, fmt.Errorf("unknown scope %q (want workspace, project, or global)", cfg.Scope)
		}
		kind = scope.Workspace
	}
	ctx := scope.FromEnv()
	if cfg.Workspace != "" {
		ctx.WorkspaceID = cfg.Workspace
	}
	return notes.New(st, scope.Resolve(kind, ctx), cfg.EditorArgv()), nil
}

func first(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func list(app *notes.App) error {
	pages, err := app.List()
	if err != nil {
		return err
	}
	current := app.Store.Resolve(app.Current())
	for _, p := range pages {
		marker := "  "
		if p.Path == current {
			marker = "* "
		}
		fmt.Printf("%s%-40s %s\n", marker, p.Path, p.Title())
	}
	return nil
}

func show(app *notes.App, page string) error {
	p, err := app.Read(page)
	if err != nil {
		return err
	}
	fmt.Print(p.Doc.Body)
	return nil
}

// appendLine accepts both `append "text"` and `append page "text"`. A single
// argument is text, because that is what an agent writes most often.
func appendLine(app *notes.App, args []string) error {
	switch len(args) {
	case 0:
		return errors.New("usage: herdr-notes append [page] <text>")
	case 1:
		return app.Append("", args[0])
	default:
		return app.Append(args[0], strings.Join(args[1:], " "))
	}
}

func search(app *notes.App, query string) error {
	hits, err := app.Search(query)
	if err != nil {
		return err
	}
	for _, h := range hits {
		fmt.Printf("%s:%d: %s\n", h.Path, h.Line, h.Text)
	}
	if len(hits) == 0 {
		return errors.New("no matches")
	}
	return nil
}

func printList(items []string, err error) error {
	if err != nil {
		return err
	}
	for _, i := range items {
		fmt.Println(i)
	}
	return nil
}

func printPath(app *notes.App, page string) error {
	target := app.Current()
	if page != "" {
		target = page
	}
	fmt.Println(absPath(app, target))
	return nil
}

func absPath(app *notes.App, page string) string {
	return app.Store.Root() + string(os.PathSeparator) + app.Store.Resolve(page)
}

func writeIndex(app *notes.App) error {
	dir, ok := app.Store.(*bundle.Dir)
	if !ok {
		return errors.New("this store does not support index generation")
	}
	return dir.WriteIndex("herdr-notes", "0.2")
}

func doctor(app *notes.App) error {
	h, err := app.Doctor()
	if err != nil {
		return err
	}
	fmt.Print(h.String())
	return nil
}

// runTUI opens the pane. The TUI still uses the original single-file store so
// that this change is additive: the pane keeps working exactly as before while
// the bundle-backed CLI lands beside it.
func runTUI(app *notes.App) error {
	model := tui.New(app, app.EditorArgv, os.Getenv("HERDR_PANE_ID"))
	if _, runErr := tea.NewProgram(model, tea.WithAltScreen()).Run(); runErr != nil {
		return runErr
	}
	return nil
}

func togglePane(cfg config.Config) error {
	app, err := buildApp(cfg)
	if err != nil {
		return err
	}
	return launcher.Toggle(app)
}
