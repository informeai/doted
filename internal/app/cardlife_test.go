//go:build unix

package app

import (
	"math"
	"testing"
	"time"
)

func TestCardLife(t *testing.T) {
	g := newTestGame(t)
	g.faces = newFaceSet(g.family, g.cfg.Font, 1)
	g.cfg.Animation.Enabled = true
	run(g, "true &")
	j := g.jobs.Listed()[0]
	tickUntil(t, g, func() bool { return !j.Running() })
	end := j.Ended

	if c, e := g.cardPhase(j, end.Add(collapseAfter/2)); c != 0 || e != 0 {
		t.Fatalf("right after the end the card stays whole: %.2f %.2f", c, e)
	}
	if c, _ := g.cardPhase(j, end.Add(collapseAfter+collapseTime/2)); c <= 0 || c >= 1 {
		t.Fatalf("folding: %.2f", c)
	}
	if c, e := g.cardPhase(j, end.Add(stripLingerOK/2)); c != 1 || e != 0 {
		t.Fatalf("a pill until its time is up: %.2f %.2f", c, e)
	}
	if _, e := g.cardPhase(j, end.Add(stripLingerOK-exitTime/2)); e <= 0 || e >= 1 {
		t.Fatalf("leaving: %.2f", e)
	}
	if got := g.pillLabel(j); got != "#1 true · "+formatDuration(j.Elapsed(end)) {
		t.Fatalf("pill = %q", got)
	}
	// Folded into a pill, the strip is as tall as the pill.
	_, ph := g.pillSize(j)
	if h := g.stripHeight(end.Add(stripLingerOK / 2)); math.Abs(h-ph) > 0.01 {
		t.Fatalf("strip height %.1f, want the pill's %.1f", h, ph)
	}
}

func TestPillLabelOfFailures(t *testing.T) {
	g := newTestGame(t)
	run(g, "sh -c 'exit 3' &")
	j := g.jobs.Listed()[0]
	tickUntil(t, g, func() bool { return !j.Running() })
	if got := g.pillLabel(j); got != "#1 sh · exit 3" {
		t.Fatalf("pill = %q", got)
	}
}

func TestStripLayout(t *testing.T) {
	g := newTestGame(t)
	g.faces = newFaceSet(g.family, g.cfg.Font, 1)
	for range 3 {
		run(g, "sleep 30 &")
	}
	now := time.Now()
	cards := g.stripJobs(now)
	width := 120 * g.faces.cellW
	slots, total, overflow := g.stripLayout(cards, width, now)
	if overflow || len(slots) != 3 || math.Abs(total-width) > 0.5 || slots[0].x != 0 {
		t.Fatalf("three cards should share the strip: overflow %v, total %.0f of %.0f", overflow, total, width)
	}

	// Short of room, cards narrow down to fit before anything scrolls.
	narrow := 70 * g.faces.cellW
	slots, total, overflow = g.stripLayout(cards, narrow, now)
	if overflow || math.Abs(total-narrow) > 0.5 || slots[0].w >= float64(stripMinCols)*g.faces.cellW {
		t.Fatalf("narrow: overflow %v, total %.0f of %.0f, first %.0f wide", overflow, total, narrow, slots[0].w)
	}

	// Too narrow even for that: it scrolls, and nothing is left out.
	slots, total, overflow = g.stripLayout(cards, 40*g.faces.cellW, now)
	if !overflow || len(slots) != 3 || total <= 40*g.faces.cellW {
		t.Fatalf("very narrow: overflow %v, %d slots, total %.0f", overflow, len(slots), total)
	}
}

func TestQuietCardsShrinkWhenRoomIsShort(t *testing.T) {
	g := newTestGame(t)
	g.faces = newFaceSet(g.family, g.cfg.Font, 1)
	for range 4 {
		run(g, "sleep 30 &")
	}
	now := time.Now().Add(2 * quietAfter) // nothing printed for a while
	cards := g.stripJobs(now)

	// With room for all, quiet cards stay whole.
	g.stripLayout(cards, 200*g.faces.cellW, now)
	if g.watchOf(cards[0]).miniTarget != 0 {
		t.Fatal("quiet cards shrank with room to spare")
	}
	// Short of room, they shrink to one line, which fits them all.
	g.stripLayout(cards, 90*g.faces.cellW, now)
	for _, j := range cards {
		g.watchOf(j).mini = g.watchOf(j).miniTarget
	}
	if g.watchOf(cards[0]).miniTarget != 1 {
		t.Fatal("quiet cards should shrink when room is short")
	}
	if _, _, overflow := g.stripLayout(cards, 90*g.faces.cellW, now); overflow {
		t.Fatal("one-line cards should fit without scrolling")
	}
	// The selected card stays whole.
	g.stripSel = cards[1]
	g.stripLayout(cards, 90*g.faces.cellW, now)
	if g.watchOf(cards[1]).miniTarget != 0 {
		t.Fatal("the selected card should stay whole")
	}
}
