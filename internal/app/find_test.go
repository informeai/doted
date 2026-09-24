//go:build unix

package app

import (
	"testing"
	"time"

	"github.com/informeai/doted/internal/terminal"
)

func TestFindAll(t *testing.T) {
	sb := terminal.NewScrollback(100)
	sb.Append(terminal.Output, "Error: disk full", time.Now())
	sb.Append(terminal.Output, "no error here, error again", time.Now())
	if got := findAll(sb, "error"); len(got) != 3 || got[1] != (findMatch{1, 3, 5}) || got[2] != (findMatch{1, 15, 5}) {
		t.Fatalf("error: %v", got)
	}
	// An uppercase letter makes the search match case.
	if got := findAll(sb, "Error"); len(got) != 1 || got[0] != (findMatch{0, 0, 5}) {
		t.Fatalf("Error: %v", got)
	}
	if got := findAll(sb, "aaa"); len(got) != 0 {
		t.Fatalf("aaa: %v", got)
	}
}

func TestFindScrollsAndUnfolds(t *testing.T) {
	g := newTestGame(t)
	g.cols, g.outputRows = 80, 10
	run(g, "echo needle; seq 1 100")
	tickUntil(t, g, func() bool { return g.attached == nil })
	seq, b := blockFor(t, g, "echo needle; seq 1 100")
	g.runBlockAction(blockAction{kind: actToggle, seq: seq})

	g.openFind()
	g.panel.query = []rune("needle")
	g.refreshFind(true)
	// The command line and its first output line both say needle; the
	// newest hit is the output, folded away until found.
	if len(g.panel.finds) != 2 || g.panel.selected != 1 {
		t.Fatalf("finds %v selected %d", g.panel.finds, g.panel.selected)
	}
	if b.collapsed {
		t.Fatal("finding a hit inside a folded block should unfold it")
	}
	// 100 lines of seq sit below the hit: the view scrolls up to it.
	if g.scroll < 100-g.outputRows || g.scroll > 100 {
		t.Fatalf("scroll = %d", g.scroll)
	}
	g.stepFind(-1)
	if m, _ := g.currentFind(); m.seq != seq {
		t.Fatalf("older hit = %v, want the command line", m)
	}
	g.stepFind(-1) // wraps to the newest
	if g.panel.selected != 1 {
		t.Fatalf("selected = %d after wrapping", g.panel.selected)
	}
}

func TestFindAcrossWideCharacters(t *testing.T) {
	sb := terminal.NewScrollback(100)
	p := terminal.NewParser(sb)
	p.Begin()
	p.Write([]byte("日本語 error 日本"), time.Now())
	p.End()
	got := findAll(sb, "error")
	// 日本語 takes six columns and a space one: error starts at column 7.
	if len(got) != 1 || got[0].col != 7 || got[0].n != 5 {
		t.Fatalf("error: %v", got)
	}
	got = findAll(sb, "日本")
	if len(got) != 2 || got[0] != (findMatch{0, 0, 4}) || got[1].col != 13 {
		t.Fatalf("日本: %v", got)
	}
}
