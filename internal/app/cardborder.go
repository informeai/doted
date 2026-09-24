package app

import (
	"image/color"
	"math"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// A job card's border tells what's happening in it:
//
//   - while output comes in, a beam of light runs around the border in the
//     accent color, and fades away once the job goes quiet;
//   - while the job is printing errors, the beam turns red and the border
//     pulses;
//   - when the job ends, the border lights up in one sweep, green when it
//     went well and red when it failed, then settles back.
//
// Cards also sit on a soft shadow with a faint highlight on their top edge,
// and lift a little under the mouse.

const (
	beamLap      = 1400 * time.Millisecond // one lap around the border
	beamTail     = 0.4                     // the beam's length, as a fraction of the border
	beamActive   = 1200 * time.Millisecond // output this recent keeps the beam running
	beamFade     = 500 * time.Millisecond  // it fades out over this after that
	endSweep     = 700 * time.Millisecond  // the border lighting up as the job ends
	endSettle    = 1500 * time.Millisecond // and fading back afterwards
	cornerSteps  = 8                       // segments in each rounded corner
	beamSegments = 48                      // segments drawn along the beam
)

// perimeter is a card's border as a polyline, walked by arc length.
type perimeter struct {
	pts [][2]float64
	cum []float64 // length from the start to each point
}

// cardPerimeter walks the border of the rounded box (x, y, w, h) clockwise
// from the top-left, after its corner.
func cardPerimeter(x, y, w, h, r float64) perimeter {
	r = math.Min(r, math.Min(w, h)/2)
	var p perimeter
	add := func(px, py float64) { p.pts = append(p.pts, [2]float64{px, py}) }
	corner := func(cx, cy, from float64) {
		for i := 1; i <= cornerSteps; i++ {
			a := from + float64(i)/cornerSteps*math.Pi/2
			add(cx+r*math.Cos(a), cy+r*math.Sin(a))
		}
	}
	add(x+r, y)
	add(x+w-r, y)
	corner(x+w-r, y+r, -math.Pi/2) // top right
	add(x+w, y+h-r)
	corner(x+w-r, y+h-r, 0) // bottom right
	add(x+r, y+h)
	corner(x+r, y+h-r, math.Pi/2) // bottom left
	add(x, y+r)
	corner(x+r, y+r, math.Pi) // top left, back to the start
	p.cum = make([]float64, len(p.pts))
	for i := 1; i < len(p.pts); i++ {
		p.cum[i] = p.cum[i-1] + math.Hypot(p.pts[i][0]-p.pts[i-1][0], p.pts[i][1]-p.pts[i-1][1])
	}
	return p
}

func (p perimeter) length() float64 { return p.cum[len(p.cum)-1] }

// at is the point at fraction t of the way around, wrapping.
func (p perimeter) at(t float64) (float64, float64) {
	t -= math.Floor(t)
	d := t * p.length()
	i := 1
	for i < len(p.cum)-1 && p.cum[i] < d {
		i++
	}
	seg := p.cum[i] - p.cum[i-1]
	f := 0.0
	if seg > 0 {
		f = (d - p.cum[i-1]) / seg
	}
	a, b := p.pts[i-1], p.pts[i]
	return a[0] + (b[0]-a[0])*f, a[1] + (b[1]-a[1])*f
}

// drawBeam draws a beam of light along p whose head is at fraction head of
// the way around, with a tail of fraction tail fading behind it.
func drawBeam(dst *ebiten.Image, p perimeter, head, tail, width, alpha float64, clr, core color.RGBA) {
	if alpha <= 0 {
		return
	}
	step := tail / beamSegments
	for i := range beamSegments {
		t0 := head - tail + float64(i)*step
		x0, y0 := p.at(t0)
		x1, y1 := p.at(t0 + step)
		k := float64(i+1) / beamSegments // brighter towards the head
		k *= k
		vector.StrokeLine(dst, float32(x0), float32(y0), float32(x1), float32(y1), float32(width*4), scaleAlpha(clr, alpha*k*0.35), true)
		vector.StrokeLine(dst, float32(x0), float32(y0), float32(x1), float32(y1), float32(width), scaleAlpha(core, alpha*k), true)
	}
	// A bright spark leads it.
	hx, hy := p.at(head)
	vector.FillCircle(dst, float32(hx), float32(hy), float32(width*2.2), scaleAlpha(clr, alpha*0.45), true)
	vector.FillCircle(dst, float32(hx), float32(hy), float32(width*1.1), scaleAlpha(core, alpha), true)
}

// drawTrace draws the border from the start to fraction to of the way
// around, as when it lights up.
func drawTrace(dst *ebiten.Image, p perimeter, to, width, alpha float64, clr color.RGBA) {
	steps := int(math.Ceil(to * beamSegments * 2))
	for i := range steps {
		t0 := float64(i) / float64(steps) * to
		t1 := float64(i+1) / float64(steps) * to
		x0, y0 := p.at(t0)
		x1, y1 := p.at(t1)
		vector.StrokeLine(dst, float32(x0), float32(y0), float32(x1), float32(y1), float32(width), scaleAlpha(clr, alpha), true)
	}
}

// drawCardShadow draws a soft shadow under the rounded box, lifted by lift
// (0 resting, 1 under the mouse).
func drawCardShadow(dst *ebiten.Image, x, y, w, h, r, scale, lift, alpha float64) {
	const layers = 5
	for i := range layers {
		k := float64(i+1) / layers
		spread := (1 + 2*lift) * k * 4 * scale
		drop := (2 + 3*lift) * k * scale
		a := alpha * (0.09 + 0.05*lift) * (1 - k*0.6)
		fillRoundRect(dst, x-spread/2, y+drop, w+spread, h+spread/2, r+spread/2, scaleAlpha(color.RGBA{0, 0, 0, 255}, a))
	}
}

// cardBorder draws the border of job j's card along p, as its events say.
func (g *Game) cardBorder(dst *ebiten.Image, p perimeter, w *jobWatch, running bool, ended time.Time, state color.RGBA, alpha float64, now time.Time) {
	width := math.Max(1.5, 2*g.scale)
	base := mixRGBA(g.theme.Border, g.theme.Foreground, 0.12)
	drawTrace(dst, p, 1, math.Max(1, g.scale), alpha, base)
	if !g.cfg.Animation.Enabled {
		return
	}
	red := w.failing

	// Output coming in: the beam runs, fading once the job goes quiet.
	if idle := now.Sub(w.activityAt); running && !w.activityAt.IsZero() && idle < beamActive+beamFade {
		a := 1.0
		if idle > beamActive {
			a = 1 - float64(idle-beamActive)/float64(beamFade)
		}
		clr := g.theme.Accent
		if red {
			clr = g.theme.Error
		}
		head := float64(now.Sub(w.shownAt)%beamLap) / float64(beamLap)
		drawBeam(dst, p, head, beamTail, width, alpha*a, clr, mixRGBA(clr, g.theme.Foreground, 0.35))
	}

	// Errors: the border pulses red.
	if red && running {
		pulse := 0.35 + 0.35*(math.Sin(float64(now.UnixMilli())/1000*2*math.Pi*0.8)+1)/2
		drawTrace(dst, p, 1, width, alpha*pulse, g.theme.Error)
	}

	// The end: the border lights up in one sweep, then settles.
	if !running && !ended.IsZero() {
		since := now.Sub(ended)
		switch {
		case since < endSweep:
			drawTrace(dst, p, easeOutCubic(float64(since)/float64(endSweep)), width*1.4, alpha, state)
		case since < endSweep+endSettle:
			drawTrace(dst, p, 1, width*1.4, alpha*(1-float64(since-endSweep)/float64(endSettle)), state)
		}
	}
}
