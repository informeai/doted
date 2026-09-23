package app

import (
	"math"
	"testing"
	"time"
)

func TestDotBounce(t *testing.T) {
	if lift, squash := dotBounce(0); lift != 0 || squash != 0 {
		t.Fatalf("at rest: lift %v squash %v, want 0 0", lift, squash)
	}

	// Mid-hop is the top of the jump, with no squash.
	if lift, squash := dotBounce(bouncePeriod / 2); math.Abs(lift-1) > 1e-9 || squash != 0 {
		t.Fatalf("mid-hop: lift %v squash %v, want 1 0", lift, squash)
	}

	// Landing: on the ground and fully squashed exactly at the period boundary.
	if lift, squash := dotBounce(bouncePeriod); lift != 0 || math.Abs(squash-1) > 1e-9 {
		t.Fatalf("impact: lift %v squash %v, want 0 1", lift, squash)
	}

	// Periodic, and smooth enough that no frame jumps more than a sliver.
	step := time.Second / 60
	prevLift, prevSquash := dotBounce(step)
	for tt := 2 * step; tt < 3*bouncePeriod; tt += step {
		lift, squash := dotBounce(tt)
		if lift < 0 || lift > 1 || squash < 0 || squash > 1 {
			t.Fatalf("t=%v: lift %v squash %v out of range", tt, lift, squash)
		}
		if lift > 0 && squash > 0 {
			t.Fatalf("t=%v: squashed while airborne", tt)
		}
		if math.Abs(lift-prevLift) > 0.3 || math.Abs(squash-prevSquash) > 0.8 {
			t.Fatalf("t=%v: jump from (%v, %v) to (%v, %v)", tt, prevLift, prevSquash, lift, squash)
		}
		prevLift, prevSquash = lift, squash
		if a, _ := dotBounce(tt + bouncePeriod); math.Abs(a-lift) > 1e-9 {
			t.Fatalf("t=%v: not periodic", tt)
		}
	}
}

func TestBounceOnlyWhileTyping(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(d time.Duration) time.Time { return base.Add(d) }

	if _, bouncing := bounceElapsed(at(time.Hour), time.Time{}, time.Time{}); bouncing {
		t.Fatal("bouncing before any key")
	}

	// One key: one full hop, then rest.
	start := at(0)
	if el, bouncing := bounceElapsed(at(bouncePeriod/2), start, start); !bouncing || el != bouncePeriod/2 {
		t.Fatalf("mid-hop after one key: %v %v", el, bouncing)
	}
	if _, bouncing := bounceElapsed(at(bouncePeriod), start, start); bouncing {
		t.Fatal("still bouncing after the hop of a single key")
	}

	// Typing on: keeps hopping, then lands at the end of the hop the last
	// key fell in, never mid-air.
	last := at(bouncePeriod*2 + bouncePeriod/3)
	if _, bouncing := bounceElapsed(at(bouncePeriod*2+bouncePeriod/2), start, last); !bouncing {
		t.Fatal("stopped while the last hop is in the air")
	}
	if _, bouncing := bounceElapsed(at(bouncePeriod*3), start, last); bouncing {
		t.Fatal("kept bouncing after the last hop landed")
	}
}

func TestTouchStartsAndContinuesTheBounce(t *testing.T) {
	var g Game
	g.touch()
	first := g.bounceStart
	if first.IsZero() || g.lastInput.Before(first) {
		t.Fatal("first key should start the bounce")
	}
	g.touch() // mid-bounce: the hop sequence continues instead of restarting
	if g.bounceStart != first {
		t.Fatal("a key mid-bounce restarted the hop")
	}
	g.bounceStart = g.bounceStart.Add(-time.Hour) // long since landed
	g.lastInput = g.bounceStart
	g.touch()
	if !g.bounceStart.After(first) {
		t.Fatal("a key after landing should start a new bounce")
	}
}

func TestTypingGlow(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(d time.Duration) time.Time { return base.Add(d) }

	if g := typingGlow(at(time.Minute), time.Time{}, time.Time{}); g != 0 {
		t.Fatalf("before any key: %v", g)
	}
	start, last := at(0), at(bouncePeriod+bouncePeriod/4) // the last key falls in the 2nd hop
	for _, tt := range []struct {
		at   time.Duration
		want float64
	}{
		{0, 1},                                 // first key: accent at once
		{2*bouncePeriod - time.Millisecond, 1}, // still typing until the last hop lands
		{2*bouncePeriod + typingFade/2, 0.5},   // fading back
		{2*bouncePeriod + typingFade, 0},       // back to the text color
		{2*bouncePeriod + 10*typingFade, 0},
	} {
		if got := typingGlow(at(tt.at), start, last); math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("at %v: glow %v, want %v", tt.at, got, tt.want)
		}
	}
}

func TestPromptColorFollowsTyping(t *testing.T) {
	g := newTestGame(t)
	now := time.Now()
	if got := g.promptColor(now); got != g.theme.Foreground {
		t.Fatalf("at rest: %v, want the text color %v", got, g.theme.Foreground)
	}
	g.touch()
	if got := g.promptColor(time.Now()); got != g.theme.Accent {
		t.Fatalf("while typing: %v, want the accent color %v", got, g.theme.Accent)
	}
	// Halfway through the fade the color sits between the two.
	end, _ := bounceEnd(g.bounceStart, g.lastInput)
	mid := g.promptColor(end.Add(typingFade / 2))
	if mid == g.theme.Foreground || mid == g.theme.Accent {
		t.Fatalf("mid-fade color %v should be between the text and accent colors", mid)
	}
}

func TestSelectionCells(t *testing.T) {
	g := newTestGame(t)
	g.editor.Insert([]rune("git status")...)
	if from, to := g.selectionCells(2); from != to {
		t.Fatalf("no selection should give an empty range, got %d..%d", from, to)
	}
	for range len("status") {
		g.editor.Left(true)
	}
	// "git " is 4 runes after a 2-cell prompt: "status" is cells 6..12.
	if from, to := g.selectionCells(2); from != 6 || to != 12 {
		t.Fatalf("selection cells %d..%d, want 6..12", from, to)
	}
}
