//go:build unix

package app

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/informeai/doted/internal/jobs"
)

func TestSelectCards(t *testing.T) {
	g := newTestGame(t)
	for range 3 {
		run(g, "sleep 30 &")
	}
	cards := g.stripJobs(time.Now())

	g.selectCard(-1) // from nothing, left starts at the newest
	if g.stripSel != cards[2] {
		t.Fatalf("selected %v", g.stripSel)
	}
	g.selectCard(-1)
	g.selectCard(-1)
	g.selectCard(-1) // stops at the oldest
	if g.stripSel != cards[0] || g.stripFocus() != cards[0] {
		t.Fatalf("selected #%d", g.stripSel.ID)
	}
	g.stripSel = nil
	g.selectCard(1) // right starts at the oldest
	if g.stripSel != cards[0] {
		t.Fatal("right should start at the oldest")
	}
	if hint := g.selectionHint(); hint[:3] != "#1 " {
		t.Fatalf("hint = %q", hint)
	}
}

func TestShortcutsUseJobNumbers(t *testing.T) {
	g := newTestGame(t)
	run(g, "true &")
	first := g.jobs.Listed()[0]
	tickUntil(t, g, func() bool { return !first.Running() })
	g.jobs.Remove(first) // #1 is gone; the rest keep their numbers
	run(g, "read x &")
	run(g, "read y &")
	if g.stripJob(1) != nil || g.stripJob(3) == nil || g.stripJob(3).Command != "read y" {
		t.Fatal("cards should be found by their job's number")
	}
	if !g.sendToCard(3) || g.target.Command != "read y" {
		t.Fatalf("alt+3 should send to #3, got %v", g.target)
	}
	g.exitTarget()
	if !g.openStripCard(2) || g.viewing.Command != "read x" {
		t.Fatal("ctrl+2 should open #2")
	}
}

func TestStripGroupOpensAndCloses(t *testing.T) {
	g := newTestGame(t)
	for range 7 {
		run(g, "sleep 30 &")
	}
	now := time.Now()
	ids := func(js []*jobs.Job) (out []int) {
		for _, j := range js {
			out = append(out, j.ID)
		}
		return out
	}

	// Closed: the first four show, the rest are grouped.
	shown, grouped, open := g.stripGroups(now)
	if open || !slices.Equal(ids(shown), []int{1, 2, 3, 4}) || !slices.Equal(ids(grouped), []int{5, 6, 7}) {
		t.Fatalf("closed: shown %v grouped %v open %v", ids(shown), ids(grouped), open)
	}

	// Moving right past the fourth card selects the group, which opens.
	for range 5 {
		g.selectCard(1)
	}
	if !g.groupSel || g.stripSel != nil {
		t.Fatalf("after #4: group %v, card %v", g.groupSel, g.stripSel)
	}
	if shown, _, open = g.stripGroups(now); !open || !slices.Equal(ids(shown), []int{5, 6, 7}) {
		t.Fatalf("open: shown %v", ids(shown))
	}
	if hint := g.selectionHint(); !strings.HasPrefix(hint, "3 grouped jobs") {
		t.Fatalf("hint = %q", hint)
	}

	// On into its jobs; the group stays open while one of them is selected.
	g.selectCard(1)
	if g.stripSel == nil || g.stripSel.ID != 5 || g.groupSel {
		t.Fatalf("in the group: %v", g.stripSel)
	}
	if _, _, open = g.stripGroups(now); !open {
		t.Fatal("the group should stay open on one of its jobs")
	}

	// Back out past the group card: closed again, on #4.
	g.selectCard(-1)
	g.selectCard(-1)
	if g.stripSel == nil || g.stripSel.ID != 4 {
		t.Fatalf("back out: %v", g.stripSel)
	}
	if _, _, open = g.stripGroups(now); open {
		t.Fatal("leaving the group should close it")
	}

	// Esc closes it too.
	g.stripSel, g.groupSel = nil, true
	g.clearSelection()
	if _, _, open = g.stripGroups(now); open {
		t.Fatal("esc should close the group")
	}

	// Sending to a grouped job by its number opens the group on it.
	g.sendToCard(6)
	if shown, _, open = g.stripGroups(now); !open || !slices.Contains(ids(shown), 6) {
		t.Fatal("alt+6 should open the group")
	}
	g.exitTarget()
}

func TestSelectedCardShowsMoreLines(t *testing.T) {
	g := newTestGame(t)
	g.faces = newFaceSet(g.family, g.cfg.Font, 1)
	run(g, "sleep 30 &")
	run(g, "sleep 30 &")
	now := time.Now()
	one, two := g.stripJob(1), g.stripJob(2)
	if g.cardLines(one) != 3 || g.cardLines(two) != 3 {
		t.Fatalf("cards show %d and %d lines, want 3", g.cardLines(one), g.cardLines(two))
	}
	before := g.stripHeight(now)

	g.selectCard(1)
	// Only the selected card shows more, and the strip keeps its height:
	// the card grows down over the output instead.
	if g.cardLines(one) != selectedLines || g.cardLines(two) != 3 {
		t.Fatalf("selected shows %d, the other %d", g.cardLines(one), g.cardLines(two))
	}
	if g.stripHeight(now) != before {
		t.Fatalf("strip height %.0f → %.0f", before, g.stripHeight(now))
	}
	if g.fullCardHeight(two) >= g.linesHeight(selectedLines) {
		t.Fatal("the other card grew too")
	}
}

func TestGroupNoticesChanges(t *testing.T) {
	g := newTestGame(t)
	g.cfg.Animation.Enabled = true
	for range 4 {
		run(g, "sleep 30 &")
	}
	run(g, "sleep 30 &")
	run(g, "read x &") // #6, to end in the group
	run(g, "sleep 30 &")
	now := time.Now()
	_, grouped, _ := g.stripGroups(now)
	g.stripGroup.placed = true
	g.noticeGroupChanges(grouped, false, now)
	if !slices.Equal(g.stripGroup.ids, []int{5, 6, 7}) {
		t.Fatalf("ids = %v", g.stripGroup.ids)
	}

	// #6 ends in the group: the group lights up and the status line says so.
	j := g.stripJob(6)
	j.Write([]byte("\r"))
	tickUntil(t, g, func() bool { return !j.Running() })
	g.watchOf(j).grouped = true
	_, grouped, _ = g.stripGroups(time.Now())
	g.noticeGroupChanges(grouped, false, time.Now())
	if g.stripGroup.flashAt.IsZero() || g.stripGroup.flashClr != g.theme.ANSI[2] || !strings.HasPrefix(g.flashText, "#6 read x finished in") {
		t.Fatalf("flash %v %v, status %q", g.stripGroup.flashAt, g.stripGroup.flashClr, g.flashText)
	}
	// A job that ended in the group, out of view, leaves sooner.
	if g.linger(j) != stripLingerGrouped {
		t.Fatalf("linger = %v", g.linger(j))
	}

	// Once it's gone, the count rolls from +3 to +2 and the pile hops.
	after := j.Ended.Add(stripLingerGrouped)
	_, grouped, _ = g.stripGroups(after)
	g.noticeGroupChanges(grouped, false, after)
	if len(grouped) != 2 || string(g.stripGroup.label.from) != "+3" || string(g.stripGroup.label.to) != "+2" || !g.stripGroup.bumpAt.Equal(after) {
		t.Fatalf("grouped %d, label %q → %q", len(grouped), string(g.stripGroup.label.from), string(g.stripGroup.label.to))
	}
}

func TestPromotedCardComesFromThePile(t *testing.T) {
	g := newTestGame(t)
	g.cfg.Animation.Enabled = true
	for range 5 {
		run(g, "sleep 30 &")
	}
	now := time.Now()
	g.stripGroup = groupCard{x: 900, w: 60, placed: true, ids: []int{5}}

	// #5 was in the group: it starts from the pile and glows.
	w5 := g.watchOf(g.stripJob(5))
	g.placeCard(g.stripJob(5), w5, 600, 300, now)
	if w5.x != 900 || w5.w != 60 || !w5.promotedAt.Equal(now) {
		t.Fatalf("#5 starts at %.0f, %.0f wide, promoted %v", w5.x, w5.w, w5.promotedAt)
	}
	// A card new to the strip grows in its own slot.
	w1 := g.watchOf(g.stripJob(1))
	g.placeCard(g.stripJob(1), w1, 0, 300, now)
	if w1.x != 0 || w1.w != 0 || !w1.promotedAt.IsZero() {
		t.Fatalf("#1 starts at %.0f, %.0f wide", w1.x, w1.w)
	}
}

func TestSelectedCardGrowsWithinTheOutput(t *testing.T) {
	g := newTestGame(t)
	g.faces = newFaceSet(g.family, g.cfg.Font, 1)
	run(g, "sleep 30 &")
	g.selectCard(1)
	j := g.stripJob(1)
	if g.cardLines(j) != selectedLines {
		t.Fatalf("with room: %d lines", g.cardLines(j))
	}
	// A short window: it grows only as far as the output goes.
	g.stripRoom = 7.5 * g.faces.lineH
	if n := g.cardLines(j); n != 6 {
		t.Fatalf("short window: %d lines, want 6", n)
	}
	// Never less than usual.
	g.stripRoom = 2 * g.faces.lineH
	if n := g.cardLines(j); n != g.cfg.Jobs.StripLines {
		t.Fatalf("tiny window: %d lines", n)
	}
}

func TestSendingToACardShowsTenLines(t *testing.T) {
	g := newTestGame(t)
	run(g, "read x &")
	run(g, "sleep 30 &")
	g.sendToCard(1)
	if g.cardLines(g.stripJob(1)) != 10 || g.cardLines(g.stripJob(2)) != g.cfg.Jobs.StripLines {
		t.Fatalf("sent to: %d lines, other: %d", g.cardLines(g.stripJob(1)), g.cardLines(g.stripJob(2)))
	}
	g.exitTarget()
}
