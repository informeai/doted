//go:build unix

package app

import (
	"strings"
	"testing"

	"github.com/informeai/doted/internal/jobs"
	"github.com/informeai/doted/internal/terminal"
)

func TestSelectOnAFullScreenProgram(t *testing.T) {
	g := newTestGame(t)
	g.cols, g.outputRows = 40, 10
	g.syncPTYSize()
	// Like less: the alternate screen, with a few lines of text on it.
	run(g, `printf '\033[?1049h\033[Halpha beta\r\ngamma delta\r\n日本 語'; sleep 30`)
	j := g.attached
	tickUntil(t, g, func() bool {
		return j.FullScreen() && gridRowText(j, 2) == "日本 語"
	})

	// A drag from "beta" to "gam".
	g.gridSel = gridSelection{job: j, anchor: gridPos{6, 0}, head: gridPos{3, 1}}
	if got := g.gridSelectedText(); got != "beta\ngam" {
		t.Fatalf("selected %q", got)
	}
	// A double-click on "delta", a triple-click on the last line.
	if a, b := gridWordAt(j, gridPos{8, 1}); a != (gridPos{6, 1}) || b != (gridPos{11, 1}) {
		t.Fatalf("word = %v..%v", a, b)
	}
	g.gridSel = gridSelection{job: j, anchor: gridPos{0, 2}, head: gridPos{j.Screen().Width(), 2}}
	if got := g.gridSelectedText(); got != "日本 語" {
		t.Fatalf("line = %q", got)
	}
	// Cmd+C copies it.
	g.copySelection()
	if g.clipboard != "日本 語" {
		t.Fatalf("clipboard = %q", g.clipboard)
	}
	j.Kill()
}

// gridRowText is row of j's screen as text.
func gridRowText(j *jobs.Job, row int) string {
	var runes []rune
	for _, c := range screenRow(j, row, nil) {
		runes = append(runes, c.Rune)
	}
	return strings.TrimRight(terminal.StringOf(runes), " ")
}
