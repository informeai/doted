package app

import (
	"testing"
	"time"

	"github.com/informeai/doted/internal/terminal"
)

// selectionGame is a test game with some output and a known layout: cells
// 10px wide, rows 20px tall, output starting at x=0.
func selectionGame(t *testing.T, text string) *Game {
	g := newTestGame(t)
	g.scrollback.Append(terminal.Output, text, time.Now())
	g.faces = &faceSet{scale: 1, cellW: 10, lineH: 20}
	g.outLeft = 0
	return g
}

func selectRange(g *Game, a, b textPos) {
	g.outSel.sb, g.outSel.anchor, g.outSel.head = g.scrollback, a, b
}

func TestHitTest(t *testing.T) {
	g := selectionGame(t, "first line\nlast")
	s0, s1 := g.scrollback.Seq(0), g.scrollback.Seq(1)
	g.rows = []visibleRow{{seq: s1, start: 0, n: 4, y: 120}, {seq: s0, start: 0, n: 10, y: 100}} // drawn bottom-up

	for _, tt := range []struct {
		x, y float64
		want textPos
	}{
		{34, 105, textPos{s0, 3}},  // snaps to the nearer boundary (3.4 cells)
		{36, 105, textPos{s0, 4}},  // (3.6 cells)
		{-50, 110, textPos{s0, 0}}, // left of the text
		{999, 110, textPos{s0, 10}},
		{15, 125, textPos{s1, 2}},
		{15, 10, textPos{s0, 0}},  // above the output: start of the top row
		{15, 999, textPos{s1, 4}}, // below: end of the bottom row
	} {
		if got, ok := g.hitTest(tt.x, tt.y); !ok || got != tt.want {
			t.Errorf("hitTest(%v, %v) = %v, want %v", tt.x, tt.y, got, tt.want)
		}
	}
	g.rows = nil
	if _, ok := g.hitTest(0, 0); ok {
		t.Error("hit test with nothing drawn")
	}
}

func TestWordAndLineSelection(t *testing.T) {
	g := selectionGame(t, "go test ./internal/...  ok")
	seq := g.scrollback.Seq(0)
	a, b := wordAt(g.scrollback, textPos{seq, 10})
	selectRange(g, a, b)
	if got := g.selectedOutput(); got != "./internal/..." {
		t.Fatalf("double-click selected %q", got)
	}
	a, b = wordAt(g.scrollback, textPos{seq, 22}) // a space: just that
	if b.col-a.col != 1 {
		t.Fatalf("double-click on a space selected %d chars", b.col-a.col)
	}
	a, b = lineAt(g.scrollback, textPos{seq, 3})
	selectRange(g, a, b)
	if got := g.selectedOutput(); got != "go test ./internal/...  ok" {
		t.Fatalf("triple-click selected %q", got)
	}
}

func TestSelectedOutputAcrossLines(t *testing.T) {
	g := selectionGame(t, "alpha one   \n\nbravo two\ncharlie")
	seq := g.scrollback.Seq(0)
	// From "one" on the first line to "bravo" on the third, backwards: the
	// order of anchor and head doesn't matter.
	selectRange(g, textPos{seq + 2, 5}, textPos{seq, 6})
	if got := g.selectedOutput(); got != "one\n\nbravo" {
		t.Fatalf("selected %q, want %q", got, "one\n\nbravo")
	}

	// New output and trimming don't move it.
	g.scrollback.Append(terminal.Output, "more output", time.Now())
	if got := g.selectedOutput(); got != "one\n\nbravo" {
		t.Fatalf("after new output: %q", got)
	}
	// Once the lines are cleared, nothing is left to copy.
	g.scrollback.Clear()
	if got := g.selectedOutput(); got != "" {
		t.Fatalf("after clear: %q", got)
	}
}

func TestCellsIn(t *testing.T) {
	var s outputSelection
	s.sb = terminal.NewScrollback(0)
	s.anchor, s.head = textPos{10, 4}, textPos{12, 3}
	for _, tt := range []struct {
		seq, n   int
		from, to int
		ok       bool
	}{
		{9, 5, 0, 0, false},
		{10, 8, 4, 8, true},
		{11, 0, 0, 0, true}, // an empty line in the middle: its line break is selected
		{12, 9, 0, 3, true},
		{13, 5, 0, 0, false},
	} {
		from, to, ok := s.cellsIn(tt.seq, tt.n)
		if from != tt.from || to != tt.to || ok != tt.ok {
			t.Errorf("cellsIn(%d, %d) = %d, %d, %v; want %d, %d, %v", tt.seq, tt.n, from, to, ok, tt.from, tt.to, tt.ok)
		}
	}
}

func TestCopyPrefersOutputSelection(t *testing.T) {
	g := selectionGame(t, "error: boom\nat main.go:12")
	g.editor.Insert([]rune("typed")...)
	g.editor.Home(true) // an input selection too
	seq := g.scrollback.Seq(0)
	selectRange(g, textPos{seq, 7}, textPos{seq + 1, 13})
	g.copySelection()
	if g.clipboard != "boom\nat main.go:12" {
		t.Fatalf("copied %q, want the output selection", g.clipboard)
	}

	// Pasted into the one-line input, the line break becomes a space.
	g.editor.Reset()
	settle(t, g, func() bool { return fake(g).get() != "" })
	g.paste()
	settle(t, g, func() bool { return !g.editor.Empty() })
	if g.editor.Text() != "boom at main.go:12" {
		t.Fatalf("pasted %q", g.editor.Text())
	}

	// Without an output selection, the input's is copied as before.
	g.outSel.clear()
	g.editor.Reset()
	g.editor.Insert([]rune("abc")...)
	g.editor.Left(true)
	g.copySelection()
	if g.clipboard != "c" {
		t.Fatalf("copied %q, want the input selection", g.clipboard)
	}
}
