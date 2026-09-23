//go:build unix

package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// typeLine replaces the input with line, cursor at the end.
func typeLine(g *Game, line string) {
	g.editor.Reset()
	g.editor.Insert([]rune(line)...)
}

func TestSuggestFromHistory(t *testing.T) {
	g := newTestGame(t)
	for _, cmd := range []string{"git commit -m first", "git checkout main", "git commit -m second"} {
		typeLine(g, cmd)
		g.editor.Submit()
	}
	now := time.Now()
	typeLine(g, "git c")
	if got := g.suggestion(now); got != "ommit -m second" {
		t.Fatalf("suggestion = %q, want the newest matching entry", got)
	}
	typeLine(g, "git ch")
	if got := g.suggestion(now); got != "eckout main" {
		t.Fatalf("suggestion = %q", got)
	}
	typeLine(g, "git commit -m second") // already complete: nothing to add
	if got := g.suggestion(now); got != "" {
		t.Fatalf("suggestion for a complete entry = %q", got)
	}
}

func TestSuggestCommandNames(t *testing.T) {
	g := newTestGame(t)
	bin := t.TempDir()
	for _, n := range []string{"mytoolkit", "mytool", "helm", "l2"} {
		os.WriteFile(filepath.Join(bin, n), []byte("#!/bin/sh\n"), 0o755)
	}
	g.session.Configure("/bin/sh", map[string]string{"PATH": bin})
	now := time.Now()

	for line, want := range map[string]string{
		"myt":         "ool", // the shortest match
		"mytoolk":     "it",
		"he":          "lp", // help beats helm: same length, doted's commands first
		"l":           "2",  // the shortest still wins across groups (l2 over let)
		"expo":        "rt", // shell builtin
		"FOO=1 myt":   "ool",
		"zzz-nothing": "",
	} {
		typeLine(g, line)
		if got := g.suggestion(now); got != want {
			t.Errorf("%q: suggestion %q, want %q", line, got, want)
		}
	}
}

func TestSuggestPaths(t *testing.T) {
	g := newTestGame(t)
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "Projects"), 0o755)
	os.WriteFile(filepath.Join(dir, "Pictures.txt"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, ".hidden"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "Projects", "doted.go"), nil, 0o644)
	g.session.Chdir(dir)
	now := time.Now()

	for line, want := range map[string]string{
		"cd Pro":         "jects/", // directories get a slash
		"cat Pic":        "tures.txt",
		"cd P":           "rojects/", // the shortest name wins
		"cat .hi":        "dden",     // hidden files once a dot is typed
		"cat h":          "",         // ...and not before
		"cat Projects/d": "oted.go",
		"cd ":            "", // nothing typed for the argument yet
		"cd Projects/":   "", // a whole directory: nothing to prefer
		"cat nope":       "",
	} {
		typeLine(g, line)
		if got := g.suggestion(now); got != want {
			t.Errorf("%q: suggestion %q, want %q", line, got, want)
		}
	}
}

// isolatedGame is a game whose commands see an empty PATH, so tests don't
// depend on what is installed.
func isolatedGame(t *testing.T) *Game {
	g := newTestGame(t)
	g.session.Configure("/bin/sh", map[string]string{"PATH": t.TempDir()})
	return g
}

func TestSuggestionOnlyAtTheEnd(t *testing.T) {
	g := isolatedGame(t)
	typeLine(g, "hel")
	now := time.Now()
	if g.suggestion(now) != "p" {
		t.Fatalf("suggestion at the end = %q", g.suggestion(now))
	}
	g.editor.Left(false)
	if got := g.suggestion(now); got != "" {
		t.Fatalf("cursor in the middle: suggestion %q", got)
	}
	g.editor.End(false)
	g.editor.Left(true) // a selection
	if got := g.suggestion(now); got != "" {
		t.Fatalf("with a selection: suggestion %q", got)
	}
}

func TestAcceptSuggestion(t *testing.T) {
	g := isolatedGame(t)
	typeLine(g, "")
	if g.acceptSuggestion() {
		t.Fatal("accepted a suggestion on an empty line")
	}
	typeLine(g, "hel")
	if !g.acceptSuggestion() || g.editor.Text() != "help" || !g.editor.AtEnd() {
		t.Fatalf("after Tab: %q", g.editor.Text())
	}
	if hint, _ := g.statusHint(time.Now()); hint == "tab completes" {
		t.Fatal("the hint should go once there's nothing left to complete")
	}
	typeLine(g, "hel")
	if hint, _ := g.statusHint(time.Now()); hint != "tab completes" {
		t.Fatalf("status hint with a suggestion = %q", hint)
	}
}
