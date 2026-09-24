//go:build unix

package app

import (
	"testing"
	"time"
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
