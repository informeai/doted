package app

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/informeai/doted/internal/jobs"
)

// How a job's card comes and goes. It drops in from the top when the job
// goes to the background. When the job ends, the card stays whole for a
// moment, its border lighting up in the result's color; then it folds into
// a small pill with the result (✓ #3 build · 1.2s, ✗ #2 test · exit 1),
// which stays until the card's time is up and slides out through the top.
// Cards move to their places instead of jumping: a new one grows into its
// slot while the others make room, and a leaving one shrinks as they close
// the gap.

const (
	collapseAfter   = 1500 * time.Millisecond // the whole card stays this long after the job ends
	collapseTime    = 350 * time.Millisecond  // folding into the pill
	exitTime        = 300 * time.Millisecond  // sliding out at the end
	slideRate       = 14.0                    // how fast cards move to their places, per second
	stripNarrowCols = 18                      // cards narrow down to this before the strip scrolls
	maxGroupDots    = 8                       // dots the group card shows
)

// groupCard is where the group card was drawn, as it slides, and what it
// held last, to draw it as it leaves.
type groupCard struct {
	x, w   float64
	placed bool
	n      int
	dots   []color.RGBA
}

// cardPhase is how far job j's card has folded into its pill and how far
// it has left, both from 0 to 1.
func (g *Game) cardPhase(j *jobs.Job, now time.Time) (collapse, exit float64) {
	if j.Running() || j.Ended.IsZero() {
		return 0, 0
	}
	since := now.Sub(j.Ended)
	if !g.cfg.Animation.Enabled {
		if since >= collapseAfter {
			collapse = 1
		}
		return collapse, 0
	}
	collapse = clamp01(float64(since-collapseAfter) / float64(collapseTime))
	exit = clamp01(1 - float64(g.linger(j)-since)/float64(exitTime))
	return easeInOut(collapse), easeInOut(exit)
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

// pillLabel is what the pill of a finished job says.
func (g *Game) pillLabel(j *jobs.Job) string {
	result := formatDuration(j.Elapsed(j.Ended))
	switch {
	case j.Killed:
		result = "killed"
	case j.Status != "":
		result = shortStatus(j.Status)
	}
	return fmt.Sprintf("#%d %s · %s", j.ID, g.watchOf(j).name, result)
}

// shortStatus turns "exit status 1" into "exit 1".
func shortStatus(s string) string {
	return strings.Replace(s, "exit status ", "exit ", 1)
}

// pillSize is the size of job j's pill.
func (g *Game) pillSize(j *jobs.Job) (w, h float64) {
	f := g.faces
	return f.cellW*(0.9+1.2+0.6+0.9) + float64(utf8.RuneCountInString(g.pillLabel(j)))*f.cellW, f.lineH * 1.5
}

// fullCardHeight is the height of a whole card.
func (g *Game) fullCardHeight(now time.Time) float64 {
	f := g.faces
	return float64(1+g.stripLines(now))*f.lineH + f.lineH/2
}

// miniLabel is what the one-line card of a quiet running job says.
func (g *Game) miniLabel(j *jobs.Job, now time.Time) string {
	return fmt.Sprintf("#%d %s · %s", j.ID, g.watchOf(j).name, formatElapsed(j.Elapsed(now)))
}

// smallSize is the size job j's card shrinks to: its result's pill once
// it ended, or a one-line card while it runs quietly.
func (g *Game) smallSize(j *jobs.Job, now time.Time) (w, h float64) {
	if !j.Running() {
		return g.pillSize(j)
	}
	f := g.faces
	return f.cellW*(0.9+1.0+0.5+0.9) + float64(utf8.RuneCountInString(g.miniLabel(j, now)))*f.cellW, f.lineH * 1.5
}

// smallness is how far job j's card has shrunk, from 0 (whole) to 1: into
// its pill as it ended, or into a one-line card while it's quiet and room
// is short.
func (g *Game) smallness(j *jobs.Job, now time.Time) float64 {
	c, _ := g.cardPhase(j, now)
	return math.Max(c, g.watchOf(j).mini)
}

// cardHeight is job j's card's height now.
func (g *Game) cardHeight(j *jobs.Job, now time.Time) float64 {
	_, sh := g.smallSize(j, now)
	return lerp(g.fullCardHeight(now), sh, g.smallness(j, now))
}

// stripHeight is how tall the strip is now, 0 without cards: the tallest
// card, so it shrinks once they're all small.
func (g *Game) stripHeight(now time.Time) float64 {
	if g.faces == nil {
		return 0
	}
	h := 0.0
	shown, _, _ := g.stripGroups(now)
	for _, j := range shown {
		_, e := g.cardPhase(j, now)
		h = math.Max(h, g.cardHeight(j, now)*(1-e)) // a leaving card gives its room back
	}
	return h
}

// quiet reports whether job j runs without printing anything lately, so its
// card may shrink to one line when room is short.
func (g *Game) quiet(j *jobs.Job, now time.Time) bool {
	w := g.watchOf(j)
	return j.Running() && !w.failing && j != g.target && j != g.stripSel && now.Sub(w.activityAt) > quietAfter && now.Sub(w.shownAt) > quietAfter
}

// slot is where a card belongs in the strip this frame, in the strip's own
// coordinates (0 is its left edge, before scrolling).
type slot struct {
	job      *jobs.Job
	x, w     float64
	contentW float64 // the width its whole card's content is laid out for
}

// stripLayout lays out the cards in a strip width wide. When they fit, whole
// cards share the room small cards leave; when they don't, quiet jobs'
// cards shrink to one line, and if that's not enough the strip scrolls
// (overflow): whole cards take a fixed width and total is how wide the row
// is. It also sets which quiet cards should be small.
func (g *Game) stripLayout(cards []*jobs.Job, width float64, now time.Time) (slots []slot, total float64, overflow bool) {
	f := g.faces
	gap := float64(stripGapCols) * f.cellW
	minFull := float64(stripMinCols) * f.cellW
	need := func(j *jobs.Job, small float64) float64 {
		_, e := g.cardPhase(j, now)
		sw, _ := g.smallSize(j, now)
		return (lerp(minFull, sw, small) + gap) * (1 - e)
	}
	sum := func(small func(*jobs.Job) float64) float64 {
		t := -gap
		for _, j := range cards {
			t += need(j, small(j))
		}
		return t
	}
	ended := func(j *jobs.Job) float64 { c, _ := g.cardPhase(j, now); return c }
	// Quiet jobs shrink only when the cards wouldn't fit otherwise.
	shrinkQuiet := sum(ended) > width
	for _, j := range cards {
		w := g.watchOf(j)
		w.miniTarget = 0
		if shrinkQuiet && g.quiet(j, now) {
			w.miniTarget = 1
		}
	}
	target := func(j *jobs.Job) float64 { return math.Max(ended(j), g.watchOf(j).miniTarget) }
	// Still too wide: whole cards narrow down to fit, as far as they stay
	// readable, before the strip scrolls.
	if sum(target) > width {
		whole, rest := 0.0, -gap
		for _, j := range cards {
			_, e := g.cardPhase(j, now)
			sw, _ := g.smallSize(j, now)
			whole += (1 - target(j)) * (1 - e)
			rest += (target(j)*sw + gap) * (1 - e)
		}
		if whole > 0 {
			minFull = math.Max(float64(stripNarrowCols)*f.cellW, (width-rest)/whole)
		}
	}
	overflow = sum(target) > width+0.5

	fullW := minFull
	if overflow {
		// Whole cards fill the view in whole numbers of cards.
		inner := width - 2*stripArrowW(f)
		k := math.Max(1, math.Floor((inner+gap)/(minFull+gap)))
		fullW = math.Max(minFull, (inner-(k-1)*gap)/k)
	} else {
		// Whole cards share the room small cards and gaps leave, weighted by
		// how much of a whole card each still is.
		weights, fixed := 0.0, 0.0
		for i, j := range cards {
			_, e := g.cardPhase(j, now)
			sw, _ := g.smallSize(j, now)
			small := g.smallness(j, now)
			weights += (1 - small) * (1 - e)
			fixed += small * (1 - e) * sw
			if i > 0 {
				fixed += gap * (1 - e)
			}
		}
		if weights > 0 {
			fullW = math.Max(minFull*0.5, (width-fixed)/weights)
		}
	}
	x := 0.0
	for i, j := range cards {
		_, e := g.cardPhase(j, now)
		sw, _ := g.smallSize(j, now)
		if i > 0 {
			x += gap * (1 - e)
		}
		w := lerp(fullW, sw, g.smallness(j, now)) * (1 - e)
		slots = append(slots, slot{job: j, x: x, w: w, contentW: fullW})
		x += w
	}
	return slots, x, overflow
}

// stripArrowW is the room each scroll arrow takes when the strip scrolls.
func stripArrowW(f *faceSet) float64 { return 5 * f.cellW }

// drawStrip draws the cards in the band from left to right whose top is y,
// moving each one smoothly towards its slot, and the scroll arrows when
// they don't all fit.
func (g *Game) drawStrip(dst *ebiten.Image, left, right, y float64, now time.Time) {
	g.stripHits = g.stripHits[:0]
	cards, grouped, open := g.stripGroups(now)
	dt := math.Min(0.1, now.Sub(g.stripDrawn).Seconds())
	g.stripDrawn = now
	if len(cards) == 0 {
		g.stripScroll, g.stripScrollTo = 0, 0
		return
	}
	f := g.faces
	k := 1 - math.Exp(-slideRate*dt)
	if !g.cfg.Animation.Enabled {
		k = 1
	}
	for _, j := range cards {
		w := g.watchOf(j)
		w.mini += (w.miniTarget - w.mini) * k
	}
	// Jobs past the first ones wait in a group card on the right; while
	// it's open, its jobs show and it leads them on the left.
	groupW, groupX := 0.0, right
	if len(grouped) > 0 {
		groupW = g.groupWidth(len(grouped))
		gap := groupW + float64(stripGapCols)*f.cellW
		if open {
			groupX, left = left, left+gap
		} else {
			right -= gap
			groupX = right + float64(stripGapCols)*f.cellW
		}
	}
	slots, total, overflow := g.stripLayout(cards, right-left, now)

	// Scrolling: the view follows the selected card, else the one being
	// sent to, else stays at the newest unless scrolled away.
	view, arrow := right-left, 0.0
	if overflow {
		arrow = stripArrowW(f)
		view -= 2 * arrow
	}
	g.stripMaxScroll = math.Max(0, total-view)
	if focus := g.stripFocus(); focus != nil {
		for _, s := range slots {
			if s.job == focus {
				g.stripScrollTo = math.Min(math.Max(g.stripScrollTo, s.x+s.w-view), s.x)
			}
		}
	} else if !g.stripScrolledAway {
		g.stripScrollTo = g.stripMaxScroll
	}
	g.stripScrollTo = math.Min(math.Max(0, g.stripScrollTo), g.stripMaxScroll)
	g.stripScroll += (g.stripScrollTo - g.stripScroll) * k
	origin := left + arrow - g.stripScroll

	// Cards are cut at the arrows while the strip scrolls.
	area := dst
	if overflow {
		area = dst.SubImage(image.Rect(int(left+arrow), 0, int(right-arrow), dst.Bounds().Max.Y)).(*ebiten.Image)
	}
	hidden := [2]int{}
	for _, s := range slots {
		wt := g.watchOf(s.job)
		target := origin + s.x
		if !wt.placed {
			// A new card grows into its slot from nothing.
			wt.x, wt.w, wt.placed = target, 0, g.cfg.Animation.Enabled
			if !g.cfg.Animation.Enabled {
				wt.w = s.w
			}
		}
		wt.x += (target - wt.x) * k
		wt.w += (s.w - wt.w) * k
		switch {
		case s.x+s.w <= g.stripScrollTo+1:
			hidden[0]++
		case s.x >= g.stripScrollTo+view-1:
			hidden[1]++
		}
		c, e := g.cardPhase(s.job, now)
		g.drawCard(area, s.job, wt.x, y, wt.w, g.cardHeight(s.job, now), s.contentW, g.smallness(s.job, now), c > 0, e, now)
	}
	if overflow {
		g.drawStripArrow(dst, left, y, arrow, -1, hidden[0])
		g.drawStripArrow(dst, right-arrow, y, arrow, 1, hidden[1])
	}
	for _, j := range g.stripJobs(now) {
		if !slices.Contains(cards, j) {
			g.watchOf(j).placed = false // it grows into place when it shows again
		}
	}
	g.drawGroup(dst, grouped, groupX, y, groupW, k, open, now)
}

// groupWidth is the width of the card grouping n jobs: room for "+n" and
// a dot for each, up to a few.
func (g *Game) groupWidth(n int) float64 {
	f := g.faces
	label := utf8.RuneCountInString(fmt.Sprintf("+%d", n))
	return f.cellW * float64(max(label, min(n, maxGroupDots))+3)
}

// drawGroup draws the card grouping the jobs past the first ones, as a
// pile, at (x, y) of width w, sliding in and out as it comes and goes: how
// many there are and a dot in each one's state color. Selecting it, or a
// click, opens it; a click on it open closes it.
func (g *Game) drawGroup(dst *ebiten.Image, grouped []*jobs.Job, x, y, w, k float64, open bool, now time.Time) {
	gs := &g.stripGroup
	if len(grouped) == 0 {
		gs.w += (0 - gs.w) * k
		if gs.w < 1 {
			gs.w, gs.placed = 0, false
			return
		}
	} else {
		if !gs.placed {
			gs.x, gs.w, gs.placed = x, 0, true
		}
		gs.n = len(grouped)
		gs.dots = gs.dots[:0]
		for _, j := range grouped {
			gs.dots = append(gs.dots, g.jobColor(j))
		}
		gs.x += (x - gs.x) * k
		gs.w += (w - gs.w) * k
	}
	f := g.faces
	h := g.stripHeight(now)
	if gs.w < 2 || h < 1 {
		return
	}
	mx, my := ebiten.CursorPosition()
	hover := len(grouped) > 0 && float64(mx) >= gs.x && float64(mx) < gs.x+gs.w && float64(my) >= y && float64(my) < y+h
	lift := 0.0
	gy := y
	if hover {
		lift, gy = 1, y-1.5*g.scale
	}
	r := math.Min(f.lineH/3, h/2)
	surface := mixRGBA(g.theme.Background, g.theme.Border, 0.55+0.25*lift)
	edge := mixRGBA(g.theme.Border, g.theme.Foreground, 0.12)
	// A pile: two cards peek out behind the front one.
	for i := 2; i >= 1; i-- {
		off := float64(i) * 3 * g.scale
		fillRoundRect(dst, gs.x+off, gy+off, gs.w-off, h-off, r, scaleAlpha(mixRGBA(g.theme.Background, g.theme.Border, 0.35), 1))
		drawTrace(dst, cardPerimeter(gs.x+off, gy+off, gs.w-off, h-off, r), 1, math.Max(1, g.scale), 0.6, edge)
	}
	fw, fh := gs.w-6*g.scale, h-6*g.scale
	drawCardShadow(dst, gs.x, gy, fw, fh, r, g.scale, lift, 1)
	fillRoundRect(dst, gs.x, gy, fw, fh, r, surface)
	drawTrace(dst, cardPerimeter(gs.x, gy, fw, fh, r), 1, math.Max(1, g.scale), 1, edge)

	clip := dst.SubImage(image.Rect(int(gs.x), int(gy), int(math.Ceil(gs.x+fw)), int(math.Ceil(gy+fh)))).(*ebiten.Image)
	label := fmt.Sprintf("+%d", gs.n)
	g.drawText(clip, label, gs.x+(fw-float64(len(label))*f.cellW)/2, gy+f.lineH/4, g.theme.Foreground, 1)
	// A dot per job, in its state's color.
	n := min(len(gs.dots), maxGroupDots)
	dotsW := float64(n) * f.cellW * 0.8
	for i, clr := range gs.dots[:n] {
		cx := gs.x + (fw-dotsW)/2 + (float64(i)+0.5)*f.cellW*0.8
		vector.FillCircle(clip, float32(cx), float32(gy+f.lineH*1.75), float32(f.cellW*0.18), clr, true)
	}
	// Selected, it's outlined in the accent color; open, fainter, and it
	// points back with an arrow.
	switch {
	case g.groupSel:
		strokeRoundRect(dst, gs.x-2*g.scale, gy-2*g.scale, fw+4*g.scale, fh+4*g.scale, r+2*g.scale, 3*g.scale, scaleAlpha(g.theme.Accent, 0.3))
		strokeRoundRect(dst, gs.x, gy, fw, fh, r, 2*g.scale, g.theme.Accent)
	case open:
		strokeRoundRect(dst, gs.x, gy, fw, fh, r, 1.5*g.scale, scaleAlpha(g.theme.Accent, 0.5))
	}
	if open {
		g.drawText(clip, "‹", gs.x+f.cellW*0.4, gy+f.lineH/4, g.theme.Accent, 1)
	}
	if len(grouped) > 0 {
		g.stripHits = append(g.stripHits, stripHit{x0: gs.x, y0: y, x1: gs.x + gs.w, y1: y + h, group: true})
	}
}

// drawStripArrow draws the scroll arrow in the box at (x, y) of width w,
// pointing left (dir -1) or right, with how many cards are hidden that way.
func (g *Game) drawStripArrow(dst *ebiten.Image, x, y, w float64, dir, hidden int) {
	f := g.faces
	h := g.stripHeight(time.Now())
	clr := g.theme.Muted
	if hidden == 0 {
		clr = scaleAlpha(clr, 0.35)
	}
	label := fmt.Sprintf("‹ %d", hidden)
	if dir > 0 {
		label = fmt.Sprintf("%d ›", hidden)
	}
	n := utf8.RuneCountInString(label)
	g.drawText(dst, label, x+(w-float64(n)*f.cellW)/2, y+(h-f.lineH)/2, clr, 1)
	if hidden > 0 {
		g.stripHits = append(g.stripHits, stripHit{x0: x, y0: y, x1: x + w, y1: y + h, scroll: dir})
	}
}

// drawCard draws job j's card in the box (x, y, w, h), whole or shrunk
// (small, from 0 to 1) into its result's pill once it ended or a one-line
// card while it's quiet, and leaving (exit). A whole card's content is laid
// out for contentW.
func (g *Game) drawCard(dst *ebiten.Image, j *jobs.Job, x, y, w, h, contentW, small float64, ended bool, exit float64, now time.Time) {
	if w < 1 || h < 1 {
		return
	}
	f := g.faces
	wt := g.watchOf(j)
	// It drops in from the top when it shows up, and slides out through the
	// top as it leaves.
	alpha, dy := 1.0, 0.0
	if g.cfg.Animation.Enabled {
		if age := now.Sub(wt.shownAt); age < stripEnter {
			p := easeOutCubic(float64(age) / float64(stripEnter))
			alpha, dy = p, -(1-p)*f.lineH*0.6
		}
		alpha *= 1 - exit
		dy -= exit * h * 0.8
	}
	mx, my := ebiten.CursorPosition()
	hover := float64(mx) >= x && float64(mx) < x+w && float64(my) >= y && float64(my) < y+h
	lift := 0.0
	if hover && exit == 0 {
		lift = 1
		dy -= g.scale * 1.5 // it rises a little under the mouse
	}
	y += dy
	state := g.jobColor(j)

	// A card: a raised surface on a soft shadow, with a faint highlight on
	// its top edge, and a border that tells what's happening.
	r := math.Min(f.lineH/3, h/2)
	drawCardShadow(dst, x, y, w, h, r, g.scale, lift, alpha)
	surface := mixRGBA(g.theme.Background, g.theme.Border, 0.55+0.25*lift)
	fillRoundRect(dst, x, y, w, h, r, scaleAlpha(surface, alpha))
	hl := float32(y + g.scale)
	vector.StrokeLine(dst, float32(x+r), hl, float32(x+w-r), hl, float32(g.scale), scaleAlpha(g.theme.Foreground, alpha*0.07), true)
	per := cardPerimeter(x, y, w, h, r)
	g.cardBorder(dst, per, wt, j.Running(), j.Ended, state, alpha, now)
	// A new error flashes around the card.
	if p := float64(now.Sub(wt.flashAt)) / float64(stripFlash); !wt.flashAt.IsZero() && p < 1 && g.cfg.Animation.Enabled {
		drawTrace(dst, per, 1, 3*g.scale*(1-p), alpha*(1-p), g.theme.Error)
	}
	// The card being sent to is outlined in the accent color, and the one
	// selected from the keyboard glows in it too.
	if j == g.target {
		strokeRoundRect(dst, x, y, w, h, r, 1.5*g.scale, scaleAlpha(g.theme.Accent, alpha))
	}
	if j == g.stripSel {
		strokeRoundRect(dst, x-2*g.scale, y-2*g.scale, w+4*g.scale, h+4*g.scale, r+2*g.scale, 3*g.scale, scaleAlpha(g.theme.Accent, alpha*0.3))
		strokeRoundRect(dst, x, y, w, h, r, 2*g.scale, scaleAlpha(g.theme.Accent, alpha))
	}

	// What's inside is cut to the card as it shrinks: the whole card's
	// content fades out while the small one's fades in.
	clip := dst.SubImage(image.Rect(int(x), int(y), int(math.Ceil(x+w)), int(math.Ceil(y+h)))).(*ebiten.Image)
	if small < 1 {
		g.drawCardContent(clip, j, wt, x, y, contentW, hover && small == 0, alpha*(1-clamp01(small/0.6)), now)
	}
	switch smallAlpha := alpha * clamp01((small-0.4)/0.6); {
	case small > 0 && ended:
		g.drawPill(clip, j, x, y, h, state, smallAlpha)
	case small > 0:
		g.drawMini(clip, j, x, y, h, state, smallAlpha, now)
	}
	g.stripHits = append(g.stripHits, stripHit{x0: x, y0: y, x1: x + w, y1: y + h, job: j})
}

// drawPill draws a finished job's result in its pill at (x, y) of height h:
// a check or a cross in the state's color, then its number, name and how
// it ended.
func (g *Game) drawPill(dst *ebiten.Image, j *jobs.Job, x, y, h float64, state color.RGBA, alpha float64) {
	if alpha <= 0 {
		return
	}
	f := g.faces
	pad, icon := f.cellW*0.9, f.cellW*1.2
	cx, cy := x+pad+icon/2, y+h/2
	s := icon * 0.42
	width := float32(math.Max(1.5, 1.8*g.scale))
	clr := scaleAlpha(state, alpha)
	if j.Status == "" && !j.Killed {
		// ✓
		var p vector.Path
		p.MoveTo(float32(cx-s), float32(cy))
		p.LineTo(float32(cx-s*0.3), float32(cy+s*0.7))
		p.LineTo(float32(cx+s), float32(cy-s*0.7))
		op := &vector.DrawPathOptions{AntiAlias: true}
		op.ColorScale.ScaleWithColor(clr)
		vector.StrokePath(dst, &p, &vector.StrokeOptions{Width: width, LineCap: vector.LineCapRound, LineJoin: vector.LineJoinRound}, op)
	} else {
		// ✗
		d := s * 0.75
		vector.StrokeLine(dst, float32(cx-d), float32(cy-d), float32(cx+d), float32(cy+d), width, clr, true)
		vector.StrokeLine(dst, float32(cx-d), float32(cy+d), float32(cx+d), float32(cy-d), width, clr, true)
	}
	tx := x + pad + icon + f.cellW*0.6
	ty := y + (h-f.lineH)/2
	label := g.pillLabel(j)
	id := fmt.Sprintf("#%d ", j.ID)
	g.drawText(dst, id, tx, ty, g.theme.Muted, alpha)
	g.drawText(dst, label[len(id):], tx+float64(utf8.RuneCountInString(id))*f.cellW, ty, g.theme.Foreground, alpha)
}

// drawMini draws a quiet running job's one-line card at (x, y) of height h:
// its state's dot, number, name and how long it has run.
func (g *Game) drawMini(dst *ebiten.Image, j *jobs.Job, x, y, h float64, state color.RGBA, alpha float64, now time.Time) {
	if alpha <= 0 {
		return
	}
	f := g.faces
	pad := f.cellW * 0.9
	r := f.cellW * 0.22
	if g.cfg.Animation.Enabled {
		r *= 0.8 + 0.3*(math.Sin(float64(now.UnixMilli())/1000*2*math.Pi)+1)/2
	}
	vector.FillCircle(dst, float32(x+pad+f.cellW/2), float32(y+h/2), float32(r), scaleAlpha(state, alpha), true)
	tx, ty := x+pad+f.cellW*1.5, y+(h-f.lineH)/2
	label := g.miniLabel(j, now)
	id := fmt.Sprintf("#%d ", j.ID)
	g.drawText(dst, id, tx, ty, g.theme.Muted, alpha)
	g.drawText(dst, label[len(id):], tx+float64(utf8.RuneCountInString(id))*f.cellW, ty, g.theme.Foreground, alpha*0.8)
}
