package app

import (
	"strings"
	"time"
	"unicode"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/informeai/doted/internal/jobs"
	"github.com/informeai/doted/internal/terminal"
)

// Selecting text on a full-screen program's screen (vim, less, git log),
// whether it fills the output area or the floating window: drag to select,
// double-click for a word, triple-click for a line, and Cmd+C (Ctrl+Shift+C)
// copies it without reaching the program. The selection runs from cell to
// cell in reading order, as terminals select, and goes away once a key is
// sent to the program.

// gridPos is a cell of a screen: column and row.
type gridPos struct{ col, row int }

func (a gridPos) less(b gridPos) bool {
	return a.row < b.row || a.row == b.row && a.col < b.col
}

type gridSelection struct {
	job          *jobs.Job
	anchor, head gridPos
	dragging     bool

	lastClick time.Time
	lastPos   gridPos
	clicks    int
}

// span returns the selection in reading order, and whether there is one.
func (s *gridSelection) span() (from, to gridPos, ok bool) {
	if s.job == nil || s.anchor == s.head {
		return gridPos{}, gridPos{}, false
	}
	from, to = s.anchor, s.head
	if to.less(from) {
		from, to = to, from
	}
	return from, to, true
}

// colsIn returns the selected columns [from, to) of row, of width w.
func (s *gridSelection) colsIn(row, w int) (from, to int, ok bool) {
	a, b, ok := s.span()
	if !ok || row < a.row || row > b.row {
		return 0, 0, false
	}
	from, to = 0, w
	if row == a.row {
		from = a.col
	}
	if row == b.row {
		to = b.col
	}
	return from, min(to, w), from < to
}

func (s *gridSelection) clear() {
	*s = gridSelection{lastClick: s.lastClick, lastPos: s.lastPos, clicks: s.clicks}
}

// gridUnderMouse is the full-screen program whose screen is under (x, y),
// and the cell there; ok is false when there's none.
func (g *Game) gridUnderMouse(x, y float64) (j *jobs.Job, pos gridPos, ok bool) {
	switch {
	case g.screenJob() != nil:
		j = g.screenJob()
	case g.float.open && g.float.job.Running() && g.float.job.FullScreen():
		r := g.floatRect()
		if x < r.x || x >= r.x+r.w || y < r.y || y >= r.y+r.h {
			return nil, gridPos{}, false
		}
		j = g.float.job
	default:
		return nil, gridPos{}, false
	}
	if g.faces == nil {
		return nil, gridPos{}, false
	}
	scr := j.Screen()
	col := int((x - g.screenOrigin[0]) / g.faces.cellW)
	row := int((y - g.screenOrigin[1]) / g.faces.lineH)
	return j, gridPos{min(max(col, 0), scr.Width()), min(max(row, 0), scr.Height()-1)}, true
}

// handleGridMouse selects on a full-screen program's screen, reporting
// whether the mouse is over one.
func (g *Game) handleGridMouse(x, y float64, now time.Time) bool {
	sel := &g.gridSel
	j, pos, ok := g.gridUnderMouse(x, y)
	if sel.job != nil && (j != sel.job || !sel.job.Running() || !sel.job.FullScreen()) && !sel.dragging {
		sel.clear() // the screen it was on went away
	}
	if !ok {
		sel.dragging = sel.dragging && ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)
		return false
	}
	g.setCursorShape(true)
	switch {
	case inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft):
		if now.Sub(sel.lastClick) < multiClickTime && pos.row == sel.lastPos.row {
			sel.clicks = sel.clicks%3 + 1
		} else {
			sel.clicks = 1
		}
		sel.lastClick, sel.lastPos, sel.job = now, pos, j
		switch sel.clicks {
		case 1:
			sel.anchor, sel.head, sel.dragging = pos, pos, true
		case 2:
			sel.anchor, sel.head = gridWordAt(j, pos)
		case 3:
			sel.anchor, sel.head = gridPos{0, pos.row}, gridPos{j.Screen().Width(), pos.row}
		}
	case sel.dragging && ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft):
		sel.head = pos
	case sel.dragging:
		sel.dragging = false
	}
	return true
}

// gridWordAt returns the run of non-space characters around pos on j's
// screen.
func gridWordAt(j *jobs.Job, pos gridPos) (gridPos, gridPos) {
	cells := screenRow(j, pos.row, nil)
	isWord := func(c int) bool {
		return c < len(cells) && (cells[c].Rune == terminal.WideTail || !unicode.IsSpace(cells[c].Rune))
	}
	if !isWord(pos.col) {
		return pos, gridPos{pos.col + 1, pos.row}
	}
	from, to := pos.col, pos.col
	for from > 0 && isWord(from-1) {
		from--
	}
	for isWord(to) {
		to++
	}
	return gridPos{from, pos.row}, gridPos{to, pos.row}
}

// gridSelectedText is the selected text of the screen, a line per row,
// without trailing spaces.
func (g *Game) gridSelectedText() string {
	sel := &g.gridSel
	a, b, ok := sel.span()
	if !ok || !sel.job.Running() {
		return ""
	}
	var lines []string
	var row []terminal.Cell
	for r := a.row; r <= b.row; r++ {
		row = screenRow(sel.job, r, row)
		from, to, _ := sel.colsIn(r, len(row))
		runes := make([]rune, 0, max(0, to-from))
		for _, c := range row[from:max(from, to)] {
			runes = append(runes, c.Rune)
		}
		lines = append(lines, strings.TrimRight(terminal.StringOf(runes), " "))
	}
	return strings.Join(lines, "\n")
}

// drawGridSelection highlights the selected cells of row y of j's screen,
// drawn at (x, top).
func (g *Game) drawGridSelection(dst *ebiten.Image, j *jobs.Job, y, w int, x, top float64) {
	if g.gridSel.job != j {
		return
	}
	from, to, ok := g.gridSel.colsIn(y, w)
	if !ok {
		return
	}
	f := g.faces
	vector.FillRect(dst, float32(x+float64(from)*f.cellW), float32(top), float32(float64(to-from)*f.cellW), float32(f.lineH), scaleAlpha(g.theme.Accent, selectionAlpha), false)
}
