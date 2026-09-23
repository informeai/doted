package app

import (
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/informeai/doted/internal/terminal"
)

// Selecting output with the mouse: drag to select, double-click for a word,
// triple-click for a line, then Cmd+C (Ctrl+Shift+C) copies it to doted's
// clipboard. The selection holds on to lines by their Seq, so it stays on the
// same text while the view scrolls or new output arrives.

const (
	multiClickTime  = 400 * time.Millisecond // clicks closer than this add up
	dragScrollEvery = 3                      // ticks between rows when dragging past an edge
	selectionAlpha  = 0.3                    // of the accent color behind selected text
)

// textPos is a place in a scrollback: a line (by Seq) and a rune column.
type textPos struct{ seq, col int }

func (a textPos) less(b textPos) bool {
	return a.seq < b.seq || a.seq == b.seq && a.col < b.col
}

type outputSelection struct {
	sb           *terminal.Scrollback
	anchor, head textPos
	dragging     bool

	lastClick time.Time
	lastPos   textPos
	clicks    int
	dragTicks int
}

// visibleRow is where one wrapped row of output was drawn in the last frame:
// runes [start, start+n) of the line numbered seq, at the row top y.
type visibleRow struct {
	seq, start, n int
	y             float64
}

// span returns the selection in order, and whether there is one.
func (s *outputSelection) span() (from, to textPos, ok bool) {
	if s.sb == nil || s.anchor == s.head {
		return textPos{}, textPos{}, false
	}
	from, to = s.anchor, s.head
	if to.less(from) {
		from, to = to, from
	}
	return from, to, true
}

// clear drops the selection.
func (s *outputSelection) clear() { s.sb, s.dragging = nil, false }

// cellsIn returns the selected rune range of the line numbered seq, of
// length n.
func (s *outputSelection) cellsIn(seq, n int) (from, to int, ok bool) {
	a, b, ok := s.span()
	if !ok || seq < a.seq || seq > b.seq {
		return 0, 0, false
	}
	from, to = 0, n
	if seq == a.seq {
		from = a.col
	}
	if seq == b.seq {
		to = b.col
	}
	return from, min(to, n), from < to || seq != b.seq
}

// handleMouse selects output with the mouse and keeps the pointer shaped for
// text over it.
func (g *Game) handleMouse(now time.Time) {
	if g.screenJob() != nil {
		// A full-screen program owns the output area.
		g.outSel.clear()
		g.setCursorShape(false)
		return
	}
	sb := g.visibleScrollback()
	if g.outSel.sb != nil && g.outSel.sb != sb {
		g.outSel.clear() // the view changed under the selection
	}
	mx, my := ebiten.CursorPosition()
	x, y := float64(mx), float64(my)
	if !g.outSel.dragging && g.handleStripMouse(x, y) {
		return
	}
	over := !g.panel.open && y >= g.outTop && y < g.outBottom
	if _, _, ok := g.hoveredLink(x, y); ok && over {
		g.setCursorShapeTo(ebiten.CursorShapePointer)
		if g.handleLinkClick(x, y) {
			g.outSel.clear()
		}
		return
	}
	g.setCursorShape(over)

	switch {
	case over && inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft):
		if a, ok := g.actionAt(x, y); ok {
			g.outSel.clear()
			g.runBlockAction(a)
			return
		}
		pos, ok := g.hitTest(x, y)
		if !ok {
			return
		}
		sel := &g.outSel
		if now.Sub(sel.lastClick) < multiClickTime && pos.seq == sel.lastPos.seq {
			sel.clicks = sel.clicks%3 + 1
		} else {
			sel.clicks = 1
		}
		sel.lastClick, sel.lastPos = now, pos
		sel.sb = sb
		switch sel.clicks {
		case 1:
			sel.anchor, sel.head, sel.dragging = pos, pos, true
		case 2:
			sel.anchor, sel.head = wordAt(sb, pos)
		case 3:
			sel.anchor, sel.head = lineAt(sb, pos)
		}
	case g.outSel.dragging && ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft):
		// Past the top or bottom edge, scroll to reach more output.
		if y < g.outTop || y >= g.outBottom {
			if g.outSel.dragTicks++; g.outSel.dragTicks%dragScrollEvery == 0 {
				if y < g.outTop {
					g.scrollBy(1)
				} else {
					g.scrollBy(-1)
				}
			}
		}
		if pos, ok := g.hitTest(x, y); ok {
			g.outSel.head = pos
		}
	case g.outSel.dragging:
		g.outSel.dragging = false
	}
}

func (g *Game) setCursorShape(text bool) {
	shape := ebiten.CursorShapeDefault
	if text {
		shape = ebiten.CursorShapeText
	}
	g.setCursorShapeTo(shape)
}

func (g *Game) setCursorShapeTo(shape ebiten.CursorShapeType) {
	if shape != g.cursorShape {
		ebiten.SetCursorShape(shape)
		g.cursorShape = shape
	}
}

// hitTest finds the text position at (x, y) in the output drawn last frame.
// Above the output it gives the start of the top row, below it the end of
// the bottom row; columns snap to the nearest boundary between characters.
func (g *Game) hitTest(x, y float64) (textPos, bool) {
	if len(g.rows) == 0 || g.faces == nil {
		return textPos{}, false
	}
	top, bottom := g.rows[0], g.rows[0]
	for _, r := range g.rows {
		if r.y < top.y {
			top = r
		}
		if r.y > bottom.y {
			bottom = r
		}
	}
	row := bottom
	switch {
	case y < top.y:
		return textPos{top.seq, top.start}, true
	case y >= bottom.y+g.faces.lineH:
		return textPos{bottom.seq, bottom.start + bottom.n}, true
	}
	for _, r := range g.rows {
		if y >= r.y && y < r.y+g.faces.lineH {
			row = r
		}
	}
	col := int(math.Round((x - g.outLeft) / g.faces.cellW))
	return textPos{row.seq, row.start + min(max(col, 0), row.n)}, true
}

// wordAt returns the run of non-space characters around pos, or the single
// character there if it's a space.
func wordAt(sb *terminal.Scrollback, pos textPos) (textPos, textPos) {
	runes := lineRunes(sb, pos.seq)
	if pos.col >= len(runes) {
		return lineAt(sb, pos)
	}
	isWord := func(r rune) bool { return !unicode.IsSpace(r) }
	if !isWord(runes[pos.col]) {
		return pos, textPos{pos.seq, pos.col + 1}
	}
	from, to := pos.col, pos.col
	for from > 0 && isWord(runes[from-1]) {
		from--
	}
	for to < len(runes) && isWord(runes[to]) {
		to++
	}
	return textPos{pos.seq, from}, textPos{pos.seq, to}
}

// lineAt returns the whole line at pos.
func lineAt(sb *terminal.Scrollback, pos textPos) (textPos, textPos) {
	return textPos{pos.seq, 0}, textPos{pos.seq, len(lineRunes(sb, pos.seq))}
}

func lineRunes(sb *terminal.Scrollback, seq int) []rune {
	i, ok := sb.Index(seq)
	if !ok {
		return nil
	}
	return []rune(sb.At(i).Text())
}

// selectedOutput is the selected output as text, one line per line, without
// trailing spaces.
func (g *Game) selectedOutput() string {
	a, b, ok := g.outSel.span()
	if !ok {
		return ""
	}
	var lines []string
	for seq := a.seq; seq <= b.seq; seq++ {
		if _, ok := g.outSel.sb.Index(seq); !ok {
			continue // cleared or trimmed since it was selected
		}
		runes := lineRunes(g.outSel.sb, seq)
		from, to, _ := g.outSel.cellsIn(seq, len(runes))
		lines = append(lines, strings.TrimRight(string(runes[from:max(from, to)]), " "))
	}
	return strings.Join(lines, "\n")
}

// drawRowSelection highlights the selected part of a row drawn at (x, y).
func (g *Game) drawRowSelection(dst *ebiten.Image, sb *terminal.Scrollback, r visibleRow, lineLen int, x, y float64) {
	if g.outSel.sb != sb {
		return
	}
	from, to, ok := g.outSel.cellsIn(r.seq, lineLen)
	if !ok {
		return
	}
	f := g.faces
	from, to = max(from, r.start), min(to, r.start+r.n)
	width := float64(to-from) * f.cellW
	switch {
	case to > from:
	case r.n == 0: // an empty line inside the selection: show its line break
		from, width = r.start, f.cellW/2
	default:
		return
	}
	vector.FillRect(dst, float32(x+float64(from-r.start)*f.cellW), float32(y), float32(width), float32(f.lineH), scaleAlpha(g.theme.Accent, selectionAlpha), false)
}
