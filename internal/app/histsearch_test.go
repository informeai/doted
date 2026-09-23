package app

import (
	"testing"
)

func historyGame(t *testing.T, cmds ...string) *Game {
	g := newTestGame(t)
	g.editor.SetHistory(cmds, 100)
	return g
}

func TestHistorySearch(t *testing.T) {
	g := historyGame(t, "go test ./...", "git checkout main", "ls -la", "git checkout main", "git commit -m fix")
	g.openHistorySearch()
	if !g.panel.open || g.panel.kind != panelHistory {
		t.Fatal("Ctrl+R should open the history search")
	}
	// Empty query: everything, newest first, each command once.
	if len(g.panel.matches) != 4 || g.panel.matches[0].Text != "git commit -m fix" {
		t.Fatalf("matches = %v", g.panel.matches)
	}

	g.panel.query = []rune("gchk")
	g.refreshHistoryMatches()
	if len(g.panel.matches) == 0 || g.panel.matches[0].Text != "git checkout main" {
		t.Fatalf("gchk matched %v", g.panel.matches)
	}

	g.editor.Insert([]rune("half typed")...)
	g.pickHistory(0)
	if g.panel.open || g.editor.Text() != "git checkout main" || !g.editor.AtEnd() {
		t.Fatalf("after Enter: open %v, prompt %q", g.panel.open, g.editor.Text())
	}
}

func TestHistorySearchStartsFromTheLine(t *testing.T) {
	g := historyGame(t, "docker compose up", "git status")
	g.editor.Insert([]rune("dock")...)
	g.openHistorySearch()
	if string(g.panel.query) != "dock" || len(g.panel.matches) != 1 || g.panel.matches[0].Text != "docker compose up" {
		t.Fatalf("query %q, matches %v", string(g.panel.query), g.panel.matches)
	}
	g.pickHistory(5) // out of range: just closes
	if g.panel.open || g.editor.Text() != "dock" {
		t.Fatalf("an invalid pick changed the prompt to %q", g.editor.Text())
	}
}
