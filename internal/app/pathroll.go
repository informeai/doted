package app

import (
	"image"
	"image/color"
	"math"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
)

// When the working directory changes, the path on the status line rolls to
// the new one like an odometer: in each column that differs, the old
// character slides up and out while the new one rises into place, columns
// starting one after another from the first one that changed.
const (
	rollChar     = 260 * time.Millisecond // one column's roll
	rollStagger  = 22 * time.Millisecond  // delay between neighboring columns
	rollMaxDelay = 450 * time.Millisecond // cap on the last column's delay
	pathAlpha    = 0.6                    // the path is the accent color at 60%
)

type pathRoll struct {
	from, to []rune
	start    time.Time
	first    int // first column that differs; the columns before it stay put
}

// trackDir starts a roll whenever the directory shown changes. The first
// directory just appears.
func (g *Game) trackDir(now time.Time) {
	cur := []rune(shortPath(g.session.Dir()))
	if string(cur) == string(g.path.to) {
		return
	}
	if g.path.to == nil || !g.cfg.Animation.Enabled {
		g.path = pathRoll{from: cur, to: cur}
		return
	}
	g.path = newPathRoll(g.path.to, cur, now)
}

func newPathRoll(from, to []rune, now time.Time) pathRoll {
	first := 0
	for first < len(from) && first < len(to) && from[first] == to[first] {
		first++
	}
	return pathRoll{from: from, to: to, start: now, first: first}
}

// stagger is the delay between neighboring columns for a path of n columns,
// shrunk for long paths so the whole roll stays short.
func rollStaggerFor(n int) time.Duration {
	if n <= 1 {
		return 0
	}
	return min(rollStagger, rollMaxDelay/time.Duration(n-1))
}

// progress is how far column i of n is into its roll, from 0 (the old
// character in place) to 1 (the new one), eased with a slight overshoot.
// The wave starts at the first column that changed, not at the left edge.
func (r pathRoll) progress(i, n int, now time.Time) float64 {
	moving := n - r.first
	t := now.Sub(r.start) - time.Duration(max(0, i-r.first))*rollStaggerFor(moving)
	p := math.Max(0, math.Min(1, float64(t)/float64(rollChar)))
	return easeOutBack(p)
}

// rolling reports whether any column is still moving.
func (r pathRoll) rolling(now time.Time) bool {
	return !r.start.IsZero() && now.Sub(r.start) < r.duration()
}

// duration is how long the whole roll takes.
func (r pathRoll) duration() time.Duration {
	moving := max(len(r.from), len(r.to)) - r.first
	if moving <= 0 {
		return 0
	}
	return time.Duration(moving-1)*rollStaggerFor(moving) + rollChar
}

func easeOutBack(p float64) float64 {
	const c1 = 1.2 // overshoot
	const c3 = c1 + 1
	if p <= 0 || p >= 1 {
		return math.Max(0, math.Min(1, p)) // exact ends, free of rounding
	}
	return 1 + c3*math.Pow(p-1, 3) + c1*math.Pow(p-1, 2)
}

// drawPath draws the working directory at (x, y) in at most cols columns,
// rolling from the previous one if it just changed.
func (g *Game) drawPath(dst *ebiten.Image, x, y float64, cols int, now time.Time) {
	g.drawRoll(dst, g.path, x, y, cols, g.theme.Accent, pathAlpha, now)
}

// drawRoll draws r's text at (x, y) in at most cols columns, in the middle
// of its roll if it's still rolling.
func (g *Game) drawRoll(dst *ebiten.Image, r pathRoll, x, y float64, cols int, clr color.RGBA, alpha float64, now time.Time) {
	g.drawRollColors(dst, r, x, y, cols, clr, clr, alpha, now)
}

// drawRollColors is drawRoll with the old text in fromClr and the new one in
// toClr; the letters that stay turn from one to the other as it rolls.
func (g *Game) drawRollColors(dst *ebiten.Image, r pathRoll, x, y float64, cols int, fromClr, clr color.RGBA, alpha float64, now time.Time) {
	f := g.faces
	to := []rune(truncate(string(r.to), cols))
	if !r.rolling(now) {
		g.drawText(dst, string(to), x, y, clr, alpha)
		return
	}
	stay := mixRGBA(fromClr, clr, math.Min(1, float64(now.Sub(r.start))/float64(r.duration())))
	from := []rune(truncate(string(r.from), cols))
	n := max(len(from), len(to))

	// Clip to the row so rolling characters don't spill over.
	clip := image.Rect(int(x), int(y), int(math.Ceil(x+float64(n)*f.cellW)), int(math.Ceil(y+f.lineH)))
	row := dst.SubImage(clip).(*ebiten.Image)
	for i := range n {
		old, cur := runeOr(from, i), runeOr(to, i)
		cx := x + float64(i)*f.cellW
		if old == cur {
			g.drawRune(row, cur, cx, y, stay, alpha)
			continue
		}
		p := r.progress(i, n, now)
		g.drawRune(row, old, cx, y-p*f.lineH, fromClr, alpha*math.Max(0, 1-p))
		g.drawRune(row, cur, cx, y+(1-p)*f.lineH, clr, alpha*math.Min(1, p))
	}
}

func (g *Game) drawRune(dst *ebiten.Image, r rune, x, y float64, clr color.RGBA, alpha float64) {
	if r != ' ' {
		g.drawText(dst, string(r), x, y, clr, alpha)
	}
}

func runeOr(rs []rune, i int) rune {
	if i < len(rs) {
		return rs[i]
	}
	return ' '
}
