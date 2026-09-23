package app

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/informeai/doted/internal/fonts"
)

// Deleting a branch or a tag plays out on the status line, after the
// current branch: the deleted name shows up in the error color behind an
// icon for its kind, an electric trail like the Tab completion's runs
// through the middle of it, and its letters fall away one after another,
// turning into red sparks, while the Git logo shakes.
//
// Deletions are found by comparing the repository's refs before and after
// each lookup, so aliases, scripts and other tools count too. Several at
// once play one after another; past maxDeletionsShown they play as one
// summary like "5 branches".

const (
	deleteDuration    = 1100 * time.Millisecond
	deleteAppear      = 0.12 // fractions of deleteDuration
	deleteStrike      = 0.35
	deleteFall        = 0.45 // one letter's fall
	deleteShake       = 400 * time.Millisecond
	deleteSparks      = 5 // per letter
	maxDeletionsShown = 3
)

type refKind int

const (
	refBranch refKind = iota
	refTag
	refRemote
	refMixed // a summary of several kinds
)

// deletedRef is one deleted branch or tag, or a summary of several.
type deletedRef struct {
	kind  refKind
	name  string
	count int // for a summary; 0 for a single ref
}

// label is the text shown for d.
func (d deletedRef) label() string {
	if d.count == 0 {
		return d.name
	}
	noun := map[refKind]string{refBranch: "branches", refTag: "tags", refRemote: "remote branches", refMixed: "refs"}[d.kind]
	return fmt.Sprintf("%d %s", d.count, noun)
}

// deletedRefs lists the refs in prev that are missing from cur, both sorted.
func deletedRefs(prev, cur []string) []deletedRef {
	var out []deletedRef
	j := 0
	for _, r := range prev {
		for j < len(cur) && cur[j] < r {
			j++
		}
		if j < len(cur) && cur[j] == r {
			continue
		}
		switch {
		case strings.HasPrefix(r, "refs/heads/"):
			out = append(out, deletedRef{kind: refBranch, name: strings.TrimPrefix(r, "refs/heads/")})
		case strings.HasPrefix(r, "refs/tags/"):
			out = append(out, deletedRef{kind: refTag, name: strings.TrimPrefix(r, "refs/tags/")})
		case strings.HasPrefix(r, "refs/remotes/"):
			out = append(out, deletedRef{kind: refRemote, name: strings.TrimPrefix(r, "refs/remotes/")})
		}
	}
	if len(out) <= maxDeletionsShown {
		return out
	}
	sum := deletedRef{kind: out[0].kind, count: len(out)}
	for _, d := range out {
		if d.kind != sum.kind {
			sum.kind = refMixed
		}
	}
	return []deletedRef{sum}
}

// deletion is a deleted ref's turn on the status line.
type deletion struct {
	ref     deletedRef
	start   time.Time
	sparked []bool // which letters already burst
}

// queueDeletions finds what was deleted between two lookups of the same
// repository and queues it to play.
func (g *Game) queueDeletions(prev, info projectContext, now time.Time) {
	if prev.gitDir == "" || prev.gitDir != info.gitDir || prev.refs == nil || info.refs == nil || !g.cfg.Animation.Enabled {
		return
	}
	a := &g.branch
	start := now
	if n := len(a.deletions); n > 0 {
		start = latest(start, a.deletions[n-1].start.Add(deleteDuration))
	}
	for _, d := range deletedRefs(prev.refs, info.refs) {
		a.deletions = append(a.deletions, deletion{ref: d, start: start, sparked: make([]bool, len([]rune(d.label())))})
		start = start.Add(deleteDuration)
	}
}

func latest(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// activeDeletion is the deletion playing now, and how far along it is.
func (a *branchAnim) activeDeletion(now time.Time) (*deletion, float64) {
	// Drop the ones that finished.
	for len(a.deletions) > 0 && now.Sub(a.deletions[0].start) >= deleteDuration {
		a.deletions = a.deletions[1:]
	}
	if len(a.deletions) == 0 || now.Before(a.deletions[0].start) {
		return nil, 0
	}
	d := &a.deletions[0]
	return d, float64(now.Sub(d.start)) / float64(deleteDuration)
}

// letterFall is how far letter i of n is into its fall at t, from 0 to 1.
func letterFall(i, n int, t float64) float64 {
	stagger := math.Min(0.04, (1-deleteStrike-deleteFall)/float64(max(1, n-1)))
	p := (t - deleteStrike - float64(i)*stagger) / deleteFall
	return math.Max(0, math.Min(1, p))
}

// shake is the logo's sideways offset while a deletion starts, in cells.
func (a *branchAnim) shake(now time.Time) float64 {
	if len(a.deletions) == 0 {
		return 0
	}
	t := now.Sub(a.deletions[0].start)
	if t < 0 || t >= deleteShake {
		return 0
	}
	p := float64(t) / float64(deleteShake)
	return 0.25 * math.Sin(p*2*math.Pi*4) * (1 - p)
}

// deletionCols is how many cells the playing deletion takes.
func (g *Game) deletionCols(now time.Time) int {
	d, _ := g.branch.activeDeletion(now)
	if d == nil {
		return 0
	}
	return 1 + 2 + len([]rune(d.ref.label()))
}

// stepDeletion bursts the sparks of letters that just finished falling,
// at the places the last frame drew them.
func (g *Game) stepDeletion(now time.Time) {
	a := &g.branch
	d, t := a.activeDeletion(now)
	if d == nil || g.faces == nil || !g.cfg.Animation.Particles {
		return
	}
	if a.redSparks == nil {
		a.redSparks = newSparks(uint64(now.UnixNano()))
	}
	for i := range d.sparked {
		if d.sparked[i] || letterFall(i, len(d.sparked), t) < 0.85 {
			continue
		}
		d.sparked[i] = true
		x, y := a.letterAt(g.faces, i, t)
		a.redSparks.burstAt(deleteSparks, x/g.scale, y/g.scale)
	}
}

// letterAt is where letter i's center is at t, on screen.
func (a *branchAnim) letterAt(f *faceSet, i int, t float64) (x, y float64) {
	p := letterFall(i, len(a.deletions[0].sparked), t)
	x = a.delName + (float64(i)+0.5)*f.cellW
	y = a.delY + f.textDY + f.glyphH/2 + p*p*f.lineH*2.2
	return x, y
}

// drawDeletion draws the playing deletion from x on the status row at y
// and returns where it ends.
func (g *Game) drawDeletion(dst *ebiten.Image, x, y, right float64, now time.Time) float64 {
	a := &g.branch
	d, t := a.activeDeletion(now)
	if d == nil {
		return x
	}
	f := g.faces
	label := []rune(d.ref.label())
	iconW, gap := f.glyphH*0.9, f.cellW*0.5
	x += f.cellW
	if x+iconW+gap+float64(len(label))*f.cellW > right {
		return x
	}
	red := g.theme.Error
	a.delName, a.delY = x+iconW+gap, y

	// It fades in, then the icon and the trail fade as the letters fall.
	appear := easeOutCubic(math.Min(1, t/deleteAppear))
	leave := 1 - math.Max(0, math.Min(1, (t-deleteStrike)/(1-deleteStrike)))
	drawRefIcon(dst, d.ref.kind, x, y+(f.lineH-iconW)/2, iconW, g.scale, scaleAlpha(red, appear*leave))

	// An electric trail, like the one after a Tab completion, runs through
	// the middle of the name.
	if t > deleteAppear {
		p := easeInOut(math.Min(1, (t-deleteAppear)/(deleteStrike-deleteAppear)))
		n := float64(len(label))
		midY := y + f.textDY + f.glyphH*0.55
		point := func(c float64) (float64, float64) { return a.delName + c*f.cellW, midY }
		sameRow := func(_, _ float64) bool { return true }
		g.drawZigzag(dst, -0.2, -0.2+p*(n+0.4), f.glyphH*0.1, leave, red, point, sameRow, now)
	}

	// The letters fall one after another, tumbling and fading.
	for i, r := range label {
		p := letterFall(i, len(label), t)
		if p >= 1 {
			continue
		}
		cx, cy := a.letterAt(f, i, t)
		spin := p * 1.3
		if i%2 == 1 {
			spin = -spin
		}
		g.drawRuneTurned(dst, r, cx, cy, spin, red, appear*(1-p*p))
	}
	return a.delName + float64(len(label))*f.cellW
}

// drawRuneTurned draws r centered at (cx, cy), turned by angle radians.
func (g *Game) drawRuneTurned(dst *ebiten.Image, r rune, cx, cy, angle float64, clr color.RGBA, alpha float64) {
	if r == ' ' || alpha <= 0 {
		return
	}
	f := g.faces
	op := &text.DrawOptions{}
	op.GeoM.Translate(-f.cellW/2, -f.glyphH/2)
	op.GeoM.Rotate(angle)
	op.GeoM.Translate(cx, cy)
	op.ColorScale.ScaleWithColor(clr)
	op.ColorScale.ScaleAlpha(float32(alpha))
	text.Draw(dst, string(r), f.faces[fonts.Regular], op)
}

// drawRefIcon draws the icon for a kind of ref in the square of side size
// at (x, y): a branch, or a tag for tags.
func drawRefIcon(dst *ebiten.Image, kind refKind, x, y, size, scale float64, clr color.RGBA) {
	if clr.A == 0 {
		return
	}
	stroke := float32(math.Max(1, 1.2*scale))
	op := &vector.DrawPathOptions{AntiAlias: true}
	op.ColorScale.ScaleWithColor(clr)
	if kind == refTag {
		// A luggage tag pointing left, with its hole.
		var p vector.Path
		m := size * 0.12
		x0, y0, x1, y1 := x+m, y+size*0.2, x+size-m, y+size*0.8
		tip := x0 + (y1-y0)/2
		p.MoveTo(float32(x0), float32((y0+y1)/2))
		p.LineTo(float32(tip), float32(y0))
		p.LineTo(float32(x1), float32(y0))
		p.LineTo(float32(x1), float32(y1))
		p.LineTo(float32(tip), float32(y1))
		p.Close()
		vector.StrokePath(dst, &p, &vector.StrokeOptions{Width: stroke, LineJoin: vector.LineJoinRound}, op)
		vector.FillCircle(dst, float32(tip+size*0.04), float32((y0+y1)/2), float32(size*0.07), clr, true)
		return
	}
	// A branch: a trunk with a commit at each end and one curving off it.
	r := math.Max(1.2*scale, size*0.12)
	trunkX, tipX := x+size*0.3, x+size*0.72
	topY, bottomY := y+size*0.18, y+size*0.82
	var p vector.Path
	p.MoveTo(float32(trunkX), float32(topY+r))
	p.LineTo(float32(trunkX), float32(bottomY-r))
	p.MoveTo(float32(tipX), float32(topY+r))
	p.QuadTo(float32(tipX), float32(y+size*0.6), float32(trunkX), float32(y+size*0.72))
	vector.StrokePath(dst, &p, &vector.StrokeOptions{Width: stroke, LineCap: vector.LineCapRound}, op)
	for _, c := range [][2]float64{{trunkX, topY}, {trunkX, bottomY}, {tipX, topY}} {
		vector.StrokeCircle(dst, float32(c[0]), float32(c[1]), float32(r), stroke, clr, true)
	}
}
