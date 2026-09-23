package app

import (
	"image/color"
	"math"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/informeai/doted/internal/config"
)

// Accepting a suggestion with Tab zaps the dot cursor: it shrinks away, a
// flickering electric underline runs to the end of the completed line, and
// the ball comes back there with a few sparks.

const (
	zapMorphIn    = 80 * time.Millisecond  // ball to bolt
	zapMorphOut   = 110 * time.Millisecond // bolt back to ball
	zapPerCell    = 14 * time.Millisecond  // travel speed
	zapMinTravel  = 120 * time.Millisecond
	zapMaxTravel  = 380 * time.Millisecond
	zapFlicker    = 30 * time.Millisecond // how often the trail's zigzag changes
	zapLandSparks = 10

	// zapBolt draws a lightning bolt leading the underline, in place of the
	// ball while it travels. Off for now, to try the underline alone.
	zapBolt = false
)

// zap is one run of the bolt between two cells of the input, counted from
// the start of the prompt.
type zap struct {
	from, to int
	start    time.Time
	active   bool
	landed   bool // the landing sparks were fired

	// The line and cursor right after the completion: once either changes
	// (the user typed or moved on), the zap stops.
	line   string
	cursor int
}

type zapStage int

const (
	zapIn zapStage = iota
	zapTravel
	zapOut
	zapDone
)

func (z zap) travel() time.Duration {
	return min(zapMaxTravel, max(zapMinTravel, time.Duration(z.to-z.from)*zapPerCell))
}

// at says which stage the zap is in at now and how far into it, 0 to 1.
func (z zap) at(now time.Time) (zapStage, float64) {
	if !z.active {
		return zapDone, 1
	}
	t := now.Sub(z.start)
	for _, st := range []struct {
		stage zapStage
		d     time.Duration
	}{{zapIn, zapMorphIn}, {zapTravel, z.travel()}, {zapOut, zapMorphOut}} {
		if t < st.d {
			return st.stage, float64(t) / float64(st.d)
		}
		t -= st.d
	}
	return zapDone, 1
}

// startZap sets off the bolt from cell from to cell to, when the cursor is
// the dot and animations are on.
func (g *Game) startZap(from, to int, now time.Time) {
	if to <= from || g.cfg.Cursor.Style != config.CursorDot || !g.cfg.Animation.Enabled {
		return
	}
	g.zap = zap{from: from, to: to, start: now, active: true, line: g.editor.Text(), cursor: g.editor.Cursor()}
}

// zapStage is the running zap's stage, ending it first if the input changed
// since it started.
func (g *Game) zapStage(now time.Time) (zapStage, float64) {
	if g.zap.active && (g.editor.Text() != g.zap.line || g.editor.Cursor() != g.zap.cursor) {
		g.zap.active = false
	}
	return g.zap.at(now)
}

// updateZap fires the landing sparks once the bolt arrives, and ends the zap.
// It runs in Update, using the layout of the last frame.
func (g *Game) updateZap(now time.Time) {
	stage, _ := g.zapStage(now)
	if !g.zap.active || stage < zapOut {
		return
	}
	if !g.zap.landed && g.faces != nil && g.cfg.Animation.Particles {
		// Sparks are placed relative to the prompt bar's center.
		f := g.faces
		col, row := g.zap.to%g.cols, g.zap.to/g.cols
		barX := math.Max(1, math.Round(promptBarWidth*g.scale)) / 2
		x := ((float64(col)+0.5)*f.cellW - barX) / g.scale
		y := (float64(row)*f.lineH + f.baselineY - f.textDY - f.glyphH/2 - f.cellW*0.3) / g.scale
		g.sparks.burstAt(zapLandSparks, x, y)
	}
	g.zap.landed = true
	if stage == zapDone {
		g.zap.active = false
	}
}

// drawZap draws the zap in the input that starts at (x, y), reporting
// whether it did: while it runs, it replaces the cursor.
func (g *Game) drawZap(dst *ebiten.Image, x, y float64, now time.Time) bool {
	stage, p := g.zapStage(now)
	if stage == zapDone {
		return false
	}
	f := g.faces
	cellAt := func(cell int) (float64, float64) {
		return x + float64(cell%g.cols)*f.cellW, y + float64(cell/g.cols)*f.lineH
	}
	fromX, fromY := cellAt(g.zap.from)
	toX, toY := cellAt(g.zap.to)

	switch stage {
	case zapIn: // the ball shrinks away (into the bolt, when it's on)
		g.drawDotCursorScaled(dst, fromX, fromY, 1-p)
		if zapBolt {
			g.drawBolt(dst, fromX, fromY, easeOutBack(p), 1)
		}
	case zapTravel:
		head := g.zap.from + int(math.Round(easeInOut(p)*float64(g.zap.to-g.zap.from)))
		hx, hy := cellAt(head)
		// Within the head's row, glide between cells instead of stepping.
		exact := float64(g.zap.from) + easeInOut(p)*float64(g.zap.to-g.zap.from)
		if head/g.cols == int(exact)/g.cols {
			hx = x + (math.Mod(exact, float64(g.cols)))*f.cellW
		}
		g.drawTrail(dst, x, y, g.zap.from, exact, 1, now)
		if zapBolt {
			g.drawBolt(dst, hx, hy, 1, 1)
		}
	case zapOut: // the trail fades and the ball comes back
		g.drawTrail(dst, x, y, g.zap.from, float64(g.zap.to), 1-p, now)
		if zapBolt {
			g.drawBolt(dst, toX, toY, 1-p, 1-p)
		}
		g.drawDotCursorScaled(dst, toX, toY, easeOutBack(p))
	}
	return true
}

// drawDotCursorScaled draws the resting dot cursor in the cell at (x, y),
// scaled by s (1 is its normal size).
func (g *Game) drawDotCursorScaled(dst *ebiten.Image, x, y, s float64) {
	if s <= 0 {
		return
	}
	f := g.faces
	r := f.cellW * 0.32 * s
	cy := y + f.baselineY - f.cellW*0.32
	fillEllipse(dst, x+f.cellW/2, cy, r, r, g.theme.Accent)
}

// drawBolt draws a lightning bolt centered on the cell at (x, y), scaled by
// s and faded by alpha.
func (g *Game) drawBolt(dst *ebiten.Image, x, y, s, alpha float64) {
	if s <= 0 || alpha <= 0 {
		return
	}
	f := g.faces
	h := f.glyphH * 1.05 * s
	w := h * 0.62
	cx, cy := x+f.cellW/2, y+f.textDY+f.glyphH/2
	// A classic bolt, on a unit box with its top-left at (0, 0).
	shape := [][2]float64{
		{0.62, 0}, {0.12, 0.56}, {0.44, 0.56}, {0.32, 1}, {0.9, 0.4}, {0.56, 0.4}, {0.78, 0},
	}
	var path vector.Path
	for i, pt := range shape {
		px := float32(cx + (pt[0]-0.5)*w)
		py := float32(cy + (pt[1]-0.5)*h)
		if i == 0 {
			path.MoveTo(px, py)
		} else {
			path.LineTo(px, py)
		}
	}
	path.Close()
	op := &vector.DrawPathOptions{AntiAlias: true}
	op.ColorScale.ScaleWithColor(mixRGBA(g.theme.Accent, g.theme.Foreground, 0.25))
	op.ColorScale.ScaleAlpha(float32(alpha))
	vector.FillPath(dst, &path, &vector.FillOptions{}, op)
}

// drawTrail draws the bolt's electric trail from cell from to the (possibly
// fractional) cell head, as a zigzag just under the text, like an electric
// underline. Older parts of the trail are fainter.
func (g *Game) drawTrail(dst *ebiten.Image, x, y float64, from int, head, alpha float64, now time.Time) {
	f := g.faces
	amp := f.glyphH * 0.1
	point := func(c float64) (float64, float64) {
		col := math.Mod(c, float64(g.cols))
		row := math.Floor(c / float64(g.cols))
		return x + (col+0.5)*f.cellW, y + row*f.lineH + f.baselineY + amp + 2*g.scale
	}
	sameRow := func(a, b float64) bool { return math.Floor(a/float64(g.cols)) == math.Floor(b/float64(g.cols)) }
	g.drawZigzag(dst, float64(from), head, amp, alpha, g.theme.Accent, point, sameRow, now)
}

// drawZigzag draws an electric trail in clr from cell from to the
// (possibly fractional) cell head: a zigzag of amplitude amp around the
// points point gives for each cell, that changes every zapFlicker so the
// text under it stays readable, brighter near the head. sameRow says
// whether two cells are on one row; the trail jumps between rows.
func (g *Game) drawZigzag(dst *ebiten.Image, from, head, amp, alpha float64, clr color.RGBA, point func(c float64) (x, y float64), sameRow func(a, b float64) bool, now time.Time) {
	flicker := float64(now.UnixMilli() / zapFlicker.Milliseconds())
	at := func(c float64) (float32, float32) {
		x, y := point(c)
		return float32(x), float32(y + noise(c*7.13+flicker)*amp)
	}
	glow := scaleAlpha(clr, 0.3*alpha)
	span := head - from
	const step = 0.5 // cells between zigzag points
	for c := from; c < head; c += step {
		next := math.Min(head, c+step)
		if !sameRow(c, next) {
			continue // no segment across rows
		}
		x0, y0 := at(c)
		x1, y1 := at(next)
		fade := 0.35 + 0.65*(c-from)/math.Max(span, 1) // brighter near the head
		core := scaleAlpha(mixRGBA(clr, g.theme.Foreground, 0.4), alpha*fade)
		vector.StrokeLine(dst, x0, y0, x1, y1, float32(3.5*g.scale), glow, true)
		vector.StrokeLine(dst, x0, y0, x1, y1, float32(1.2*g.scale), core, true)
	}
}

// noise is a cheap deterministic pseudo-random value in [-1, 1].
func noise(v float64) float64 {
	s := math.Sin(v*12.9898) * 43758.5453
	return 2*(s-math.Floor(s)) - 1
}

func easeInOut(p float64) float64 {
	if p < 0.5 {
		return 4 * p * p * p
	}
	return 1 - math.Pow(-2*p+2, 3)/2
}
