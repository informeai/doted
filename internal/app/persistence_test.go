//go:build unix

package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/informeai/doted/internal/history"
)

// persistentGame is a test game whose shell keeps state between commands.
func persistentGame(t *testing.T) *Game {
	g := newTestGame(t)
	for _, sh := range []string{"/bin/zsh", "/bin/bash", "/usr/bin/zsh", "/usr/bin/bash"} {
		if _, err := os.Stat(sh); err == nil {
			g.session.Configure(sh, nil)
			return g
		}
	}
	t.Skip("neither zsh nor bash is installed")
	return nil
}

// runToEnd runs cmd attached and waits for it to finish.
func runToEnd(t *testing.T, g *Game, cmd string) {
	t.Helper()
	run(g, cmd)
	tickUntil(t, g, func() bool { return g.attached == nil })
}

func TestShellStateCarriesOver(t *testing.T) {
	g := persistentGame(t)
	dir := t.TempDir()
	g.session.Chdir(dir)

	runToEnd(t, g, "export DOTED_APP=carried")
	runToEnd(t, g, `echo "[$DOTED_APP]"`)
	if !strings.Contains(mainText(g), "[carried]") {
		t.Fatalf("export didn't carry over: %q", mainText(g))
	}

	runToEnd(t, g, "mkdir -p inner && cd inner")
	g.trackDir(time.Now())
	if filepath.Base(g.session.Dir()) != "inner" || !strings.HasSuffix(string(g.path.to), "inner") {
		t.Fatalf("cd inside a command: session in %q, status shows %q", g.session.Dir(), string(g.path.to))
	}
}

func TestBackgroundStateIsDropped(t *testing.T) {
	g := persistentGame(t)
	run(g, "export DOTED_BG=leaked &")
	j := g.jobs.Listed()[0]
	tickUntil(t, g, func() bool { return !j.Running() })
	runToEnd(t, g, `echo "[$DOTED_BG]"`)
	if !strings.Contains(mainText(g), "[]") || strings.Contains(mainText(g), "[leaked]") {
		t.Fatalf("a background job's state leaked: %q", mainText(g))
	}
}

func TestAliasesAreKnownCommands(t *testing.T) {
	g := persistentGame(t)
	runToEnd(t, g, "alias gohome='cd ~'")
	now := time.Now()
	if got := g.classifyCommand("gohome", now); got != commandFound {
		t.Fatalf("an alias should be a known command, got %v", got)
	}
	typeLine(g, "gohom")
	if got := g.suggestion(now); got != "e" {
		t.Fatalf("suggestion for an alias = %q", got)
	}
}

func TestHistoryIsSavedAndLoaded(t *testing.T) {
	g := newTestGame(t)
	for _, cmd := range []string{"help", " help secret", "jobs"} {
		typeLine(g, cmd)
		g.submit()
		g.panel.open = false
	}
	saved, err := history.Load(g.historyPath, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(saved, []string{"help", "jobs"}) {
		t.Fatalf("history file = %q, want help and clear (the space-prefixed line stays out)", saved)
	}

	// A new doted starts with it.
	g2, err := New(DefaultSettings(), filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g2.session.Close)
	if !slices.Equal(g2.editor.History(), []string{"help", "jobs"}) {
		t.Fatalf("a new doted loaded %q", g2.editor.History())
	}
	g2.editor.HistoryPrev()
	if g2.editor.Text() != "jobs" {
		t.Fatalf("↑ in a new doted gives %q, want the last command", g2.editor.Text())
	}
}

func TestHistorySaveCanBeTurnedOff(t *testing.T) {
	g := newTestGame(t)
	g.cfg.History.Save = false
	typeLine(g, "jobs")
	g.submit()
	if _, err := os.Stat(g.historyPath); err == nil {
		t.Fatal("history was written with history.save = false")
	}
}

// cd and clear are the shell's, not doted's: they must still move doted and
// clear its screen.
func TestShellCdAndClear(t *testing.T) {
	g := persistentGame(t)
	first, second := t.TempDir(), t.TempDir()

	runToEnd(t, g, "cd "+first)
	runToEnd(t, g, "cd "+second)
	if got, want := mustEvalPath(t, g.session.Dir()), mustEvalPath(t, second); got != want {
		t.Fatalf("after cd: %q, want %q", got, want)
	}
	runToEnd(t, g, "cd -") // OLDPWD carried over too
	if got, want := mustEvalPath(t, g.session.Dir()), mustEvalPath(t, first); got != want {
		t.Fatalf("after cd -: %q, want %q", got, want)
	}

	runToEnd(t, g, "echo before-clear")
	runToEnd(t, g, "clear")
	if strings.Contains(mainText(g), "before-clear") {
		t.Fatalf("clear left the old output: %q", mainText(g))
	}
}

func mustEvalPath(t *testing.T, p string) string {
	t.Helper()
	got, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
