package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cyperx84/herdr-notes/internal/config"
	"github.com/cyperx84/herdr-notes/internal/herdripc"
	"github.com/cyperx84/herdr-notes/internal/launcher"
	"github.com/cyperx84/herdr-notes/internal/store"
	"github.com/cyperx84/herdr-notes/internal/tui"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "herdr-notes:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("herdr-notes", flag.ContinueOnError)
	workspace := fs.String("workspace", "", "stable Herdr workspace ID (defaults to HERDR_WORKSPACE_ID)")
	notesDir := fs.String("notes-dir", "", "directory for canonical Markdown notes")
	showPath := fs.Bool("path", false, "print this workspace's canonical note path")
	external := fs.Bool("external", false, "open the note in configured editor (Neovim by default)")
	toggle := fs.Bool("toggle", false, "toggle the Herdr right-side Notes split")
	stamp := fs.String("stamp", "", "stamp heartbeat metadata on a pane")
	showVersion := fs.Bool("version", false, "print version")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Println(version)
		return nil
	}
	if *stamp != "" {
		return herdripc.Stamp(*stamp, time.Now())
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if *workspace != "" {
		cfg.Workspace = *workspace
	}
	if *notesDir != "" {
		cfg.NotesDir = *notesDir
	}
	s, err := store.Open(cfg.NotesDir, cfg.Workspace)
	if err != nil {
		return err
	}
	if *showPath {
		fmt.Println(s.Path)
		return nil
	}
	if *toggle {
		return launcher.Toggle(s)
	}
	if *external {
		if _, err := s.Load(); err != nil { // perform migration before editor starts
			return err
		}
		argv := cfg.EditorArgv()
		cmd := exec.Command(argv[0], append(argv[1:], s.Path)...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	model, err := tui.New(s, cfg.EditorArgv(), os.Getenv("HERDR_PANE_ID"))
	if err != nil {
		return err
	}
	_, runErr := tea.NewProgram(model, tea.WithAltScreen()).Run()
	saveErr := model.Finalize()
	if runErr != nil {
		return runErr
	}
	return saveErr
}
