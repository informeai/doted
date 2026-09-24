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
	run(g, "sleep 30 &")
	if g.stripLines(time.Now()) != 3 {
		t.Fatalf("cards show %d lines, want 3", g.stripLines(time.Now()))
	}
	g.selectCard(1)
	if g.stripLines(time.Now()) != selectedLines {
		t.Fatalf("the selected card shows %d lines, want %d", g.stripLines(time.Now()), selectedLines)
	}
}
