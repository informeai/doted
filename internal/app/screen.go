package app

import (
	"image/color"
	"time"
	"unicode"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/hajimehoshi/ebiten/v2"

	"github.com/informeai/doted/internal/jobs"
	"github.com/informeai/doted/internal/terminal"
)

// Full-screen programs (vim, less, htop, man...) switch to the alternate
// screen. While one does, the output area shows the job's terminal grid
// instead of the line history, and every key goes to the program, encoded
// by the job's emulator as the program asked (application cursor keys and
// so on).

// keyState is the keyboard as seen this tick; ebitenKeys reads the real one.
type keyState interface {
	fires(k ebiten.Key) bool // pressed now, or repeating while held
	held(k ebiten.Key) bool
}

type ebitenKeys struct{}

func (ebitenKeys) fires(k ebiten.Key) bool { return repeating(k) }
func (ebitenKeys) held(k ebiten.Key) bool  { return ebiten.IsKeyPressed(k) }

type namedKey struct {
	key  ebiten.Key
	code rune
}

// namedKeys are the keys sent as key events rather than text.
var namedKeys = []namedKey{
	{ebiten.KeyEnter, uv.KeyEnter}, {ebiten.KeyNumpadEnter, uv.KeyEnter},
	{ebiten.KeyTab, uv.KeyTab}, {ebiten.KeyBackspace, uv.KeyBackspace},
	{ebiten.KeyEscape, uv.KeyEscape},
	{ebiten.KeyArrowUp, uv.KeyUp}, {ebiten.KeyArrowDown, uv.KeyDown},
	{ebiten.KeyArrowRight, uv.KeyRight}, {ebiten.KeyArrowLeft, uv.KeyLeft},
	{ebiten.KeyHome, uv.KeyHome}, {ebiten.KeyEnd, uv.KeyEnd},
	{ebiten.KeyInsert, uv.KeyInsert}, {ebiten.KeyDelete, uv.KeyDelete},
	{ebiten.KeyF1, uv.KeyF1}, {ebiten.KeyF2, uv.KeyF1 + 1}, {ebiten.KeyF3, uv.KeyF1 + 2},
	{ebiten.KeyF4, uv.KeyF1 + 3}, {ebiten.KeyF5, uv.KeyF1 + 4}, {ebiten.KeyF6, uv.KeyF1 + 5},
	{ebiten.KeyF7, uv.KeyF1 + 6}, {ebiten.KeyF8, uv.KeyF1 + 7}, {ebiten.KeyF9, uv.KeyF1 + 8},
	{ebiten.KeyF10, uv.KeyF1 + 9}, {ebiten.KeyF11, uv.KeyF1 + 10}, {ebiten.KeyF12, uv.KeyF1 + 11},
}

// pageKeys go to full-screen programs; otherwise they scroll doted's history.
var pageKeys = []namedKey{{ebiten.KeyPageUp, uv.KeyPgUp}, {ebiten.KeyPageDown, uv.KeyPgDown}}

// jobInput turns this tick's keyboard into what a running job receives:
// typed text, and key events for everything else. PgUp and PgDn scroll
// doted's own history unless the job is full screen. Cmd shortcuts belong to
// doted and are left out.
func jobInput(chars []rune, ks keyState, fullScreen bool) (text string, keys []uv.KeyPressEvent) {
	if ks.held(ebiten.KeyMeta) {
		return "", nil
	}
	var mod uv.KeyMod
	if ks.held(ebiten.KeyShift) {
		mod |= uv.ModShift
	}
	if ks.held(ebiten.KeyAlt) {
		mod |= uv.ModAlt
	}
	ctrl := ks.held(ebiten.KeyControl)
	if ctrl {
		mod |= uv.ModCtrl
		// Ctrl+A..Ctrl+Z; the letter's own character isn't typed.
		for k := ebiten.KeyA; k <= ebiten.KeyZ; k++ {
			if ks.fires(k) {
				keys = append(keys, uv.KeyPressEvent{Code: 'a' + rune(k-ebiten.KeyA), Mod: mod &^ uv.ModShift})
			}
		}
	} else {
		for _, r := range chars {
			if !unicode.IsControl(r) {
				text += string(r)
			}
		}
	}

	named := namedKeys
	if fullScreen {
		named = append(named[:len(named):len(named)], pageKeys...)
	}
	for _, nk := range named {
		if ks.fires(nk.key) {
			keys = append(keys, uv.KeyPressEvent{Code: nk.code, Mod: mod})
		}
	}
	return text, keys
}

// sendKeyboard sends this tick's typing to a running job.
func (g *Game) sendKeyboard(j *jobs.Job) bool {
	g.chars = ebiten.AppendInputChars(g.chars[:0])
	text, keys := jobInput(g.chars, ebitenKeys{}, j.FullScreen())
	if text == "" && len(keys) == 0 {
		return false
	}
	if text != "" {
		j.SendText(text)
	}
	for _, k := range keys {
		j.SendKey(k)
	}
	return true
}

// scrollScreen sends steps of the mouse wheel (positive up) to a
// full-screen program: as wheel events at the pointer when it asked for the
// mouse (vim with mouse=a, htop), else as arrow keys, as terminals do for
// pagers like less.
func (g *Game) scrollScreen(j *jobs.Job, steps int) {
	up := steps > 0
	if !up {
		steps = -steps
	}
	if j.MouseTracking() && g.faces != nil {
		mx, my := ebiten.CursorPosition()
		x := max(0, int((float64(mx)-g.screenOrigin[0])/g.faces.cellW))
		y := max(0, int((float64(my)-g.screenOrigin[1])/g.faces.lineH))
		button := uv.MouseWheelDown
		if up {
			button = uv.MouseWheelUp
		}
		for range steps {
			j.SendMouse(uv.MouseWheelEvent{X: x, Y: y, Button: button})
		}
		return
	}
	code := uv.KeyDown
	if up {
		code = uv.KeyUp
	}
	for range steps {
		j.SendKey(uv.KeyPressEvent{Code: code})
	}
}

// wheelScreen is the full-screen program the mouse wheel goes to: the one
// on screen, or the one in the floating window.
func (g *Game) wheelScreen() *jobs.Job {
	if j := g.screenJob(); j != nil {
		return j
	}
	if g.float.open && g.float.job.Running() && g.float.job.FullScreen() {
		return g.float.job
	}
	return nil
}

// screenJob is the job whose full-screen program fills the output area, or
// nil.
func (g *Game) screenJob() *jobs.Job {
	switch {
	case g.viewing != nil && g.viewing.Running() && g.viewing.FullScreen():
		return g.viewing
	case g.viewing == nil && g.attached != nil && g.attached.FullScreen():
		return g.attached
	}
	return nil
}

// drawScreen draws a job's terminal grid, and the program's cursor, with the
// top-left cell at (x, top).
func (g *Game) drawScreen(dst *ebiten.Image, j *jobs.Job, x, top float64, now time.Time) {
	f := g.faces
	g.screenOrigin = [2]float64{x, top} // for the wheel to find cells under the pointer
	scr := j.Screen()
	h := scr.Height()
	var row []terminal.Cell
	for y := range h {
		row = screenRow(j, y, row)
		g.drawGridSelection(dst, j, y, len(row), x, top+float64(y)*f.lineH)
		g.drawCells(dst, row, x, top+float64(y)*f.lineH, terminal.Output, 1)
	}
	if j.CursorVisible() {
		pos := scr.CursorPosition()
		under := vtCell(scr.CellAt(pos.X, pos.Y)).Rune
		if under == ' ' {
			under = 0
		}
		g.drawCursor(dst, x+float64(pos.X)*f.cellW, top+float64(pos.Y)*f.lineH, under, now)
	}
}

// screenRow is row y of job j's screen as doted's cells, reusing buf.
func screenRow(j *jobs.Job, y int, buf []terminal.Cell) []terminal.Cell {
	scr := j.Screen()
	w := scr.Width()
	buf = buf[:0]
	wide := false
	for cx := range w {
		c := scr.CellAt(cx, y)
		if wide {
			// The second column of a wide character.
			buf = append(buf, terminal.Cell{Rune: terminal.WideTail, Style: buf[cx-1].Style})
			wide = false
			continue
		}
		buf = append(buf, vtCell(c))
		wide = c != nil && c.Width == 2
	}
	return buf
}

// vtCell converts one cell of the emulator's grid to doted's cell, so the
// theme's colors apply to full-screen programs too.
func vtCell(c *uv.Cell) terminal.Cell {
	if c == nil || c.Content == "" {
		return terminal.Cell{Rune: ' '}
	}
	cell := terminal.Cell{Rune: []rune(c.Content)[0]}
	st := c.Style
	cell.Style.FG, cell.Style.BG = vtColor(st.Fg), vtColor(st.Bg)
	for attr, mine := range map[uint8]terminal.Attr{
		uv.AttrBold: terminal.Bold, uv.AttrFaint: terminal.Dim,
		uv.AttrItalic: terminal.Italic, uv.AttrReverse: terminal.Inverse,
	} {
		if st.Attrs&attr != 0 {
			cell.Style.Attrs |= mine
		}
	}
	if st.Underline != uv.UnderlineNone {
		cell.Style.Attrs |= terminal.Underline
	}
	return cell
}

// vtColor maps the emulator's colors to doted's: the 16 and 256 palette
// colors by index (so the theme's palette applies), others as RGB.
func vtColor(c color.Color) terminal.Color {
	switch v := c.(type) {
	case nil:
		return terminal.Color{}
	case ansi.BasicColor:
		return terminal.Color{Kind: terminal.IndexedColor, Index: uint8(v)}
	case ansi.IndexedColor:
		return terminal.Color{Kind: terminal.IndexedColor, Index: uint8(v)}
	}
	r, gr, b, _ := c.RGBA()
	return terminal.Color{Kind: terminal.RGBColor, R: uint8(r >> 8), G: uint8(gr >> 8), B: uint8(b >> 8)}
}
