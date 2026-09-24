//go:build unix

package app

import (
	"testing"
	"time"
)

func TestFloatingWindowForFullScreenJobs(t *testing.T) {
	g := newTestGame(t)
	g.faces = newFaceSet(g.family, g.cfg.Font, 1)
	g.width, g.scale = 1200, 1
	g.cols, g.outputRows = 120, 30
	g.outLeft, g.outTop, g.outBottom = 14, 200, 800
	g.syncPTYSize()

	// A program that switches to the alternate screen, like vim.
	run(g, "printf '\\033[?1049hfull screen'; sleep 30 &")
	j := g.jobs.Listed()[0]
	tickUntil(t, g, func() bool { return j.FullScreen() })

	g.sendToCard(1)
	now := time.Now()
	g.updateFloat(now)
	cols, rows := g.floatSize()
	if !g.float.open || g.float.job != j {
		t.Fatal("sending to a full-screen job should open the floating window")
	}
	if w, h := j.Screen().Width(), j.Screen().Height(); w != cols || h != rows {
		t.Fatalf("the job's screen is %dx%d, want the window's %dx%d", w, h, cols, rows)
	}
	if g.stripHeight(now) == 0 || g.cardLines(j) != g.cfg.Jobs.StripLines {
		t.Fatal("the strip stays in view, and the card keeps its size: the window shows the job")
	}

	// Ctrl+B's way out: the window closes and the job gets its size back.
	g.exitTarget()
	g.updateFloat(time.Now())
	if g.float.open {
		t.Fatal("leaving the job should close the window")
	}
	if w, h := j.Screen().Width(), j.Screen().Height(); w != g.cols || h != g.outputRows {
		t.Fatalf("the job's screen is %dx%d, want the output's %dx%d", w, h, g.cols, g.outputRows)
	}

	// A job that isn't full screen doesn't get one.
	run(g, "read x &")
	g.sendToCard(2)
	g.updateFloat(time.Now())
	if g.float.open {
		t.Fatal("a line-mode job needs no floating window")
	}
}
