package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestGame(t *testing.T) *Game {
	t.Helper()
	t.Setenv("TERM", "xterm-256color") // as if started from a terminal
	g, err := New(DefaultSettings(), filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func lastLine(g *Game) string {
	return g.scrollback.At(g.scrollback.Len() - 1).Text()
}

func TestLoadSettingsFallsBackToEmbeddedFont(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[font]\nfamily = \"/nope/missing.ttf\"\nsize = 20\n"), 0o644)

	s, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Fonts.Name != "Go Mono" || s.Config.Font.Size != 20 || len(s.Notices) != 1 {
		t.Fatalf("font %q size %v notices %q", s.Fonts.Name, s.Config.Font.Size, s.Notices)
	}
}

func TestReloadAppliesSettings(t *testing.T) {
	g := newTestGame(t)
	s := DefaultSettings()
	s.Config.Prompt.Symbol = "$ "
	s.Config.Colors.Accent.R = 0x01
	s.Notices = []string{"heads up"}

	g.reloads <- reload{settings: s}
	g.handleReloads()

	if g.cfg.Prompt.Symbol != "$ " || g.theme.Accent.R != 0x01 {
		t.Fatalf("settings not applied: prompt %q accent %v", g.cfg.Prompt.Symbol, g.theme.Accent)
	}
	if got := lastLine(g); got != "config reloaded" {
		t.Fatalf("last line %q", got)
	}
}

func TestReloadErrorKeepsCurrentSettings(t *testing.T) {
	g := newTestGame(t)
	g.reloads <- reload{err: errors.New("font.size must be between 6 and 96")}
	g.handleReloads()

	if g.cfg.Font.Size != DefaultSettings().Config.Font.Size {
		t.Fatal("settings changed after a failed reload")
	}
	if got := lastLine(g); !strings.Contains(got, "config not reloaded") {
		t.Fatalf("last line %q", got)
	}
}

func TestHelpPanel(t *testing.T) {
	g := newTestGame(t)
	g.editor.Insert([]rune("help")...)
	g.submit()
	if !g.panel.open || g.panel.kind != panelHelp {
		t.Fatalf("help should open the help panel, got %+v", g.panel)
	}

	// Picking a command puts it on the prompt, replacing what was typed.
	g.editor.Insert([]rune("half-typed")...)
	for i, c := range helpCommands {
		if c.usage == "cd [dir]" {
			g.pickHelp(i)
		}
	}
	if g.panel.open || g.editor.Text() != "cd " || g.editor.Cursor() != len("cd ") {
		t.Fatalf("panel open %v, prompt %q", g.panel.open, g.editor.Text())
	}

	// Entries with nothing to insert just close the panel.
	g.openHelp()
	for i, c := range helpCommands {
		if c.insert == "" {
			g.pickHelp(i)
		}
	}
	if g.panel.open || g.editor.Text() != "cd " {
		t.Fatalf("panel open %v, prompt %q", g.panel.open, g.editor.Text())
	}
}

// Every builtin the help lists must really be a builtin.
func TestHelpListsRealBuiltins(t *testing.T) {
	for _, c := range helpCommands {
		name, _, _ := strings.Cut(c.usage, " ")
		if strings.HasPrefix(name, "<") {
			continue // syntax, not a command
		}
		g := newTestGame(t)
		if !g.runBuiltin(name) {
			t.Errorf("help lists %q, but it isn't a builtin", name)
		}
	}
}

func TestHelpScrollsOnShortWindows(t *testing.T) {
	g := newTestGame(t)
	g.openHelp()
	total := len(helpLines())

	g.outputRows = 100 // everything fits
	if first, rows, more := g.helpWindow(); first != 0 || rows != total || more {
		t.Fatalf("tall window: first %d rows %d more %v", first, rows, more)
	}

	g.outputRows = 9 // 8 lines of content
	g.panel.selected = len(helpCommands) - 1
	first, rows, more := g.helpWindow()
	if rows != 8 || !more || first > g.panel.selected || g.panel.selected >= first+rows {
		t.Fatalf("selection not visible: first %d rows %d more %v", first, rows, more)
	}
	g.panel.scroll = total // scrolling past the end stops at the last line
	if first, rows, more := g.helpWindow(); first+rows != total || more {
		t.Fatalf("scrolled to the end: first %d rows %d more %v", first, rows, more)
	}
}
