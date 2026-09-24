package app

import (
	"fmt"
	"image"
	"math"
	"time"
	"unicode/utf8"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/informeai/doted/internal/jobs"
	"github.com/informeai/doted/internal/terminal"
)

// drawCardScreen draws, in a card, the rows of a full-screen program's
// screen around its cursor: n rows from (x, y), cut at cols columns. Its
// line history has nothing of what it shows.
func (g *Game) drawCardScreen(dst *ebiten.Image, j *jobs.Job, x, y float64, cols, n int, alpha float64) {
	f := g.faces
	scr := j.Screen()
	h := scr.Height()
	pos := scr.CursorPosition()
	first := min(max(0, pos.Y-n/2), max(0, h-n))
	var row []terminal.Cell
	for i := range min(n, h) {
		row = screenRow(j, first+i, row)
		g.drawCells(dst, row[:min(len(row), cols)], x, y+float64(i)*f.lineH, terminal.Output, alpha*0.85)
	}
	// Where the cursor is, underlined in the accent color.
	if cy := pos.Y - first; j.CursorVisible() && cy >= 0 && cy < n && pos.X < cols {
		cx := x + float64(pos.X)*f.cellW
		uy := float32(y + float64(cy)*f.lineH + f.underlineY)
		vector.StrokeLine(dst, float32(cx), uy, float32(cx+f.cellW), uy, float32(math.Max(1.5, 2*g.scale)), scaleAlpha(g.theme.Accent, alpha), false)
	}
}

// Sending to a job whose program is full screen (vim, less, htop) opens it
// in a floating window over the output: its whole screen, at full size,
// while the strip and the status line stay in view. It grows out of the
// job's card and goes back into it; the job's terminal takes the window's
// size meanwhile, so the program redraws for it. Every key goes to the
// program, Esc included; Ctrl+B closes the window, as it leaves a job view.

const floatAnim = 260 * time.Millisecond

// rectF is a box on screen.
type rectF struct{ x, y, w, h float64 }

func lerpRect(a, b rectF, t float64) rectF {
	return rectF{lerp(a.x, b.x, t), lerp(a.y, b.y, t), lerp(a.w, b.w, t), lerp(a.h, b.h, t)}
}

// floatWindow is the floating window's state.
type floatWindow struct {
	job        *jobs.Job
	open       bool
	closing    bool      // shrinking back into the card
	at         time.Time // when it started opening or closing
	from       rectF     // the card it grows out of
	cols, rows int       // the size given to the job's terminal
}

// floatJob is the job the floating window should show: the one being sent
// to, while its program is full screen.
func (g *Game) floatJob() *jobs.Job {
	if j := g.target; j != nil && j.Running() && j.FullScreen() {
		return j
	}
	return nil
}

// floatRect is where the floating window sits: most of the output area.
func (g *Game) floatRect() rectF {
	f := g.faces
	pad := g.cfg.Window.Padding * g.scale
	left, right := g.outLeft, float64(g.width)-pad
	mx := math.Max(2*f.cellW, (right-left)*0.04)
	return rectF{left + mx, g.outTop + f.lineH/2, right - left - 2*mx, g.outBottom - g.outTop - f.lineH}
}

// floatHeader is the height of the floating window's title row.
func (g *Game) floatHeader() float64 { return g.faces.lineH * 1.4 }

// floatSize is the terminal size that fits the floating window.
func (g *Game) floatSize() (cols, rows int) {
	f := g.faces
	r := g.floatRect()
	return max(20, int((r.w-2*f.cellW)/f.cellW)), max(5, int((r.h-g.floatHeader()-f.lineH/2)/f.lineH))
}

// cardRect is where job j's card was drawn last.
func (g *Game) cardRect(j *jobs.Job, now time.Time) rectF {
	w := g.watchOf(j)
	return rectF{w.x, g.stripTop, math.Max(w.w, 1), g.cardHeight(j, now)}
}

// updateFloat opens and closes the floating window as the job being sent
// to goes in and out of full screen, and keeps its terminal the window's
// size. Update calls it every tick.
func (g *Game) updateFloat(now time.Time) {
	if g.faces == nil {
		return
	}
	fs := &g.float
	want := g.floatJob()
	switch {
	case want != nil && (!fs.open || fs.job != want):
		*fs = floatWindow{job: want, open: true, at: now, from: g.cardRect(want, now)}
	case want == nil && fs.open:
		fs.open, fs.closing, fs.at = false, g.cfg.Animation.Enabled, now
		fs.from = g.cardRect(fs.job, now)
		if fs.job.Running() {
			fs.job.Resize(g.cols, g.outputRows) // back to the output's size
		}
		return
	}
	if !fs.open {
		return
	}
	if cols, rows := g.floatSize(); cols != fs.cols || rows != fs.rows {
		fs.cols, fs.rows = cols, rows
		fs.job.Resize(cols, rows)
	}
}

// drawFloat draws the floating window, growing out of or back into its
// card.
func (g *Game) drawFloat(dst *ebiten.Image, now time.Time) {
	fs := &g.float
	if fs.job == nil || !fs.open && !fs.closing {
		return
	}
	p := 1.0
	if g.cfg.Animation.Enabled {
		p = easeOutCubic(math.Min(1, float64(now.Sub(fs.at))/float64(floatAnim)))
	}
	if fs.closing {
		if p >= 1 {
			fs.closing = false
			return
		}
		p = 1 - p
	}
	f := g.faces
	r := lerpRect(fs.from, g.floatRect(), p)
	rad := f.lineH / 3
	drawCardShadow(dst, r.x, r.y, r.w, r.h, rad, g.scale, 1, 1)
	fillRoundRect(dst, r.x, r.y, r.w, r.h, rad, mixRGBA(g.theme.Background, g.theme.Border, 0.3))
	strokeRoundRect(dst, r.x, r.y, r.w, r.h, rad, 1.5*g.scale, g.theme.Accent)

	clip := dst.SubImage(image.Rect(int(r.x), int(r.y), int(math.Ceil(r.x+r.w)), int(math.Ceil(r.y+r.h)))).(*ebiten.Image)
	name := fmt.Sprintf("#%d %s", fs.job.ID, g.watchOf(fs.job).name)
	hy := r.y + (g.floatHeader()-f.lineH)/2
	g.drawText(clip, name, r.x+f.cellW, hy, g.theme.Foreground, 1)
	hint := "every key goes to it · ctrl+b returns to the shell"
	g.drawText(clip, hint, r.x+r.w-f.cellW-float64(utf8.RuneCountInString(hint))*f.cellW, hy, g.theme.Muted, 1)
	// The program's screen, once the window has mostly grown.
	if p > 0.85 && fs.job.Running() {
		g.drawScreen(clip, fs.job, r.x+f.cellW, r.y+g.floatHeader(), now)
	}
}
