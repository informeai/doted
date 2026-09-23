package app

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestPathRollTiming(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := newPathRoll([]rune("~/Projects/doted"), []rune("~/Projects/other"), start)
	n, first := len(r.to), len("~/Projects/")
	if r.first != first {
		t.Fatalf("first changed column %d, want %d", r.first, first)
	}

	if p := r.progress(first, n, start); p != 0 {
		t.Fatalf("first changed column at the start: %v, want 0", p)
	}
	// The wave starts right at the first change, with no wait for the
	// unchanged prefix, and moves right from there.
	mid := start.Add(rollChar / 2)
	if left, right := r.progress(first, n, mid), r.progress(n-1, n, mid); left < 0.5 || left <= right {
		t.Fatalf("first changed column %v should be well under way and ahead of the last %v", left, right)
	}
	// It overshoots a little before settling on exactly 1.
	peak := 0.0
	for d := time.Duration(0); d <= rollChar; d += time.Millisecond {
		peak = math.Max(peak, r.progress(first, n, start.Add(d)))
	}
	if peak <= 1 || peak > 1.2 || r.progress(first, n, start.Add(rollChar)) != 1 {
		t.Fatalf("overshoot peak %v, settle %v", peak, r.progress(first, n, start.Add(rollChar)))
	}
}

func TestPathRollEndsQuicklyEvenForLongPaths(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	long := []rune(strings.Repeat("x", 200))
	r := newPathRoll([]rune("~"), long, start)
	limit := rollMaxDelay + rollChar
	if !r.rolling(start.Add(limit - time.Millisecond)) {
		t.Fatal("stopped before the last column rolled")
	}
	if r.rolling(start.Add(limit)) {
		t.Fatalf("still rolling after %v", limit)
	}
	if newPathRoll(long, long, start).rolling(start) {
		t.Fatal("a path that never changed shouldn't roll")
	}
}

func TestTrackDir(t *testing.T) {
	g := newTestGame(t)
	first := string(g.path.to)
	if first == "" || g.path.rolling(time.Now()) {
		t.Fatalf("startup: path %q rolling %v, want the directory shown still", first, g.path.rolling(time.Now()))
	}

	g.runBuiltin("cd " + t.TempDir())
	now := time.Now()
	g.trackDir(now)
	if string(g.path.from) != first || string(g.path.to) == first || !g.path.rolling(now) {
		t.Fatalf("after cd: %q -> %q rolling %v", string(g.path.from), string(g.path.to), g.path.rolling(now))
	}

	// With animations off the new directory just appears.
	g.cfg.Animation.Enabled = false
	g.runBuiltin("cd " + t.TempDir())
	g.trackDir(time.Now())
	if g.path.rolling(time.Now()) {
		t.Fatal("rolled with animations off")
	}
}
