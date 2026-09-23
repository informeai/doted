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
