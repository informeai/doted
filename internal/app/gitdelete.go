package app

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"

	"github.com/informeai/doted/internal/fonts"
)

// Deleting a branch or a tag plays out in the current branch's place on the
// status line:
//
//  1. the current branch's name rolls into the deleted one, turning from
//     white to the error color, while the Git logo turns red;
//  2. the Tab completion's electric trail runs through the middle of it,
//     and the logo shakes;
//  3. its letters fall away one after another, turning into red sparks;
//  4. the current branch's name rises back into place, and the logo gets
//     its color back.
//
// Deletions are found by comparing the repository's refs before and after
// each lookup, so aliases, scripts and other tools count too. Several at
// once play one after another; past maxDeletionsShown they play as one
// summary like "5 branches".

const (
	deleteTrail       = 280 * time.Millisecond // the trail's run through the name
	deleteLetterFall  = 420 * time.Millisecond // one letter's fall
	deleteStagger     = 35 * time.Millisecond  // between letters' falls
	deleteMaxStagger  = 300 * time.Millisecond // cap on the last letter's delay
	deleteIconBlend   = 150 * time.Millisecond // the logo turning red and back
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

// deletion is a deleted ref's turn in the branch's place, with its
// timeline.
type deletion struct {
	ref     deletedRef
	current string   // the branch it replaces for a moment
	in      pathRoll // the current branch's name rolling into the deleted one
	back    pathRoll // and the current one rising back
	trailAt time.Time
	fallAt  time.Time
	end     time.Time
	sparked []bool // which letters already burst
}

// newDeletion lays out the timeline of ref's deletion starting at start,
// on the branch current.
func newDeletion(ref deletedRef, current string, start time.Time) deletion {
	label := []rune(ref.label())
	d := deletion{ref: ref, current: current, sparked: make([]bool, len(label))}
	d.in = newPathRoll([]rune(current), label, start)
	d.trailAt = start.Add(d.in.duration())
	d.fallAt = d.trailAt.Add(deleteTrail)
	falling := d.stagger()*time.Duration(max(0, len(label)-1)) + deleteLetterFall
	// The branch starts rising back while the last letters still fall.
	d.back = newPathRoll(nil, []rune(current), d.fallAt.Add(falling*7/10))
	d.end = latest(d.back.start.Add(d.back.duration()), d.fallAt.Add(falling))
	return d
}

// stagger is the delay between two letters' falls.
func (d *deletion) stagger() time.Duration {
	return min(deleteStagger, deleteMaxStagger/time.Duration(max(1, len(d.sparked)-1)))
}

// letterFall is how far letter i is into its fall at now, from 0 to 1.
func (d *deletion) letterFall(i int, now time.Time) float64 {
	t := now.Sub(d.fallAt) - time.Duration(i)*d.stagger()
	return math.Max(0, math.Min(1, float64(t)/float64(deleteLetterFall)))
}

// iconBlend is how red the logo is, 0 to 1.
func (d *deletion) iconBlend(now time.Time) float64 {
	in := float64(now.Sub(d.in.start)) / float64(deleteIconBlend)
	out := 1 - float64(now.Sub(d.back.start))/float64(deleteIconBlend)
	return math.Max(0, math.Min(1, math.Min(in, out)))
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
		start = latest(start, a.deletions[n-1].end)
	}
	for _, ref := range deletedRefs(prev.refs, info.refs) {
		d := newDeletion(ref, info.branch, start)
		a.deletions = append(a.deletions, d)
		start = d.end
	}
}

func latest(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// activeDeletion is the deletion playing now, if any.
func (a *branchAnim) activeDeletion(now time.Time) *deletion {
	for len(a.deletions) > 0 && !now.Before(a.deletions[0].end) {
		a.deletions = a.deletions[1:] // finished
	}
	if len(a.deletions) == 0 || now.Before(a.deletions[0].in.start) {
		return nil
	}
	return &a.deletions[0]
}

// shake is the logo's sideways offset while the trail runs, in cells.
func (a *branchAnim) shake(now time.Time) float64 {
	d := a.activeDeletion(now)
	if d == nil {
		return 0
	}
	t := now.Sub(d.trailAt)
	if t < 0 || t >= deleteShake {
		return 0
	}
	p := float64(t) / float64(deleteShake)
	return 0.25 * math.Sin(p*2*math.Pi*4) * (1 - p)
}

// deletionCols is how many more cells than the current branch the playing
// deletion takes.
func (g *Game) deletionCols(now time.Time) int {
	d := g.branch.activeDeletion(now)
	if d == nil {
		return 0
	}
	return max(0, len(d.sparked)-len([]rune(d.current)))
}

// stepDeletion bursts the sparks of letters that just finished falling,
// at the places the last frame drew them.
func (g *Game) stepDeletion(now time.Time) {
	a := &g.branch
	d := a.activeDeletion(now)
	if d == nil || g.faces == nil || !g.cfg.Animation.Particles {
		return
	}
	if a.redSparks == nil {
		a.redSparks = newSparks(uint64(now.UnixNano()))
	}
	for i := range d.sparked {
		if d.sparked[i] || d.letterFall(i, now) < 0.85 {
			continue
		}
		d.sparked[i] = true
		x, y := a.letterAt(g.faces, d, i, now)
		a.redSparks.burstAt(deleteSparks, x/g.scale, y/g.scale)
	}
}

// letterAt is where letter i's center is at now, on screen.
func (a *branchAnim) letterAt(f *faceSet, d *deletion, i int, now time.Time) (x, y float64) {
	p := d.letterFall(i, now)
	x = a.delName + (float64(i)+0.5)*f.cellW
	y = a.delY + f.textDY + f.glyphH/2 + p*p*f.lineH*2.2
	return x, y
}

// drawDeletionName draws the playing deletion in the branch name's place at
// (x, y), in at most cols columns, and returns how many it took.
func (g *Game) drawDeletionName(dst *ebiten.Image, d *deletion, x, y float64, cols int, alpha float64, now time.Time) int {
	a := &g.branch
	f := g.faces
	red := g.theme.Error
	label := []rune(truncate(d.ref.label(), cols))
	a.delName, a.delY = x, y
	width := min(cols, max(len(label), len([]rune(d.current))))

	if now.Before(d.trailAt) {
		// The current branch rolls into the deleted one.
		g.drawRollColors(dst, d.in, x, y, cols, badgeText, red, alpha, now)
		return width
	}

	// The trail runs through the middle of the name, then fades as the
	// letters fall.
	falling := float64(d.end.Sub(d.fallAt))
	leave := 1 - math.Max(0, math.Min(1, float64(now.Sub(d.fallAt))/falling))
	p := easeInOut(math.Min(1, float64(now.Sub(d.trailAt))/float64(deleteTrail)))
	n := float64(len(label))
	midY := y + f.textDY + f.glyphH*0.55
	point := func(c float64) (float64, float64) { return x + c*f.cellW, midY }
	sameRow := func(_, _ float64) bool { return true }
	g.drawZigzag(dst, -0.2, -0.2+p*(n+0.4), f.glyphH*0.1, alpha*leave, red, point, sameRow, now)

	// The letters fall one after another, tumbling and fading.
	for i, r := range label {
		fall := d.letterFall(i, now)
		if fall >= 1 {
			continue
		}
		cx, cy := a.letterAt(f, d, i, now)
		spin := fall * 1.3
		if i%2 == 1 {
			spin = -spin
		}
		g.drawRuneTurned(dst, r, cx, cy, spin, red, alpha*(1-fall*fall))
	}

	// And the current branch rises back into place.
	if !now.Before(d.back.start) {
		g.drawRoll(dst, d.back, x, y, cols, badgeText, alpha, now)
	}
	return width
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
