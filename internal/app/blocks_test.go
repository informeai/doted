//go:build unix

package app

import (
	"strings"
	"testing"
	"time"
)

// blockFor is the block of the newest command line reading cmd.
func blockFor(t *testing.T, g *Game, cmd string) (int, *block) {
	t.Helper()
	for i := g.scrollback.Len() - 1; i >= 0; i-- {
		seq := g.scrollback.Seq(i)
		if b := g.blocks[seq]; b != nil && b.cmd == cmd {
			return seq, b
		}
	}
	t.Fatalf("no block for %q", cmd)
	return 0, nil
}

func TestBlocksRecordTheResult(t *testing.T) {
	g := newTestGame(t)
	run(g, "printf 'a\\nb\\nc\\n'")
	seq, ok := blockFor(t, g, "printf 'a\\nb\\nc\\n'")
	if !ok.running() {
		t.Fatal("a block starts running")
	}
	tickUntil(t, g, func() bool { return g.attached == nil })
	if ok.running() || ok.status != "" {
		t.Fatalf("finished block: running %v status %q", ok.running(), ok.status)
	}
	if text, _ := g.blockBadge(ok, time.Now()); !strings.HasSuffix(text, "s") {
		t.Fatalf("success badge = %q, want a duration", text)
	}
	if out := g.blockOutput(seq); out != "a\nb\nc" {
		t.Fatalf("output = %q", out)
	}

	run(g, "(exit 3)")
	_, bad := blockFor(t, g, "(exit 3)")
	tickUntil(t, g, func() bool { return g.attached == nil })
	if text, _ := g.blockBadge(bad, time.Now()); !strings.HasPrefix(text, "exit 3 · ") {
		t.Fatalf("failure badge = %q", text)
	}
	if strings.Contains(mainText(g), "exit status 3") {
		t.Fatal("the failure is repeated below the command; the badge already shows it")
	}

	// Builtins are not blocks.
	run(g, "jobs")
	if len(g.blocks) != 2 {
		t.Fatalf("%d blocks, want 2", len(g.blocks))
	}
}

func TestFoldingABlockHidesItsOutput(t *testing.T) {
	g := newTestGame(t)
	run(g, "seq 1 20")
	tickUntil(t, g, func() bool { return g.attached == nil })
	seq, b := blockFor(t, g, "seq 1 20")
	before := g.totalRows(g.scrollback)

	g.runBlockAction(blockAction{kind: actToggle, seq: seq})
	if !b.collapsed {
		t.Fatal("toggle should fold the block")
	}
	ranges := g.foldedRanges(g.scrollback)
	if got := foldedCount(ranges, seq); got != 20 {
		t.Fatalf("folded %d lines, want 20", got)
	}
	// 20 lines of output give way to one "… 20 lines" row.
	if after := g.totalRows(g.scrollback); after != before-19 {
		t.Fatalf("rows %d → %d, want %d", before, after, before-19)
	}

	g.runBlockAction(blockAction{kind: actToggle, seq: seq})
	if b.collapsed || g.totalRows(g.scrollback) != before {
		t.Fatal("toggling again should unfold the block")
	}
}

func TestBlockCopyAndRerun(t *testing.T) {
	g := newTestGame(t)
	run(g, "echo copied")
	tickUntil(t, g, func() bool { return g.attached == nil })
	seq, _ := blockFor(t, g, "echo copied")

	g.runBlockAction(blockAction{kind: actCopy, seq: seq})
	if g.clipboard != "copied" {
		t.Fatalf("clipboard = %q", g.clipboard)
	}

	g.runBlockAction(blockAction{kind: actRerun, seq: seq})
	if g.attached == nil || len(g.blocks) != 2 {
		t.Fatalf("rerun: attached %v, %d blocks", g.attached, len(g.blocks))
	}
	tickUntil(t, g, func() bool { return g.attached == nil })
	if strings.Count(mainText(g), "copied") < 3 { // two echoes of the command, output twice
		t.Fatalf("main = %q", mainText(g))
	}
}

func TestBackgroundBlock(t *testing.T) {
	g := newTestGame(t)
	run(g, "read _")
	j := g.attached
	g.background()
	_, b := blockFor(t, g, "read _")
	if text, _ := g.blockBadge(b, time.Now()); text != "job 1 in the background" {
		t.Fatalf("badge = %q", text)
	}
	j.Write([]byte("\r"))
	tickUntil(t, g, func() bool { return !b.running() })
}

func TestFormatDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		120 * time.Millisecond:  "0.1s",
		3400 * time.Millisecond: "3.4s",
	} {
		if got := formatDuration(d); got != want {
			t.Errorf("formatDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestNotifyWhenALongCommandEndsUnseen(t *testing.T) {
	g := newTestGame(t)
	var got []string
	g.notifier = func(title, body string) { got = append(got, title+" | "+body) }
	g.cfg.Notify.AfterSeconds = 0

	g.focused = true
	run(g, "true")
	tickUntil(t, g, func() bool { return g.attached == nil })
	if len(got) != 0 {
		t.Fatalf("notified while focused: %q", got)
	}

	g.focused = false
	run(g, "(exit 2)")
	tickUntil(t, g, func() bool { return g.attached == nil })
	if len(got) != 1 || !strings.HasPrefix(got[0], "doted · exit 2 after ") || !strings.HasSuffix(got[0], "| (exit 2)") {
		t.Fatalf("notifications = %q", got)
	}

	g.cfg.Notify.AfterSeconds = 60 // quick commands aren't worth one
	run(g, "true")
	tickUntil(t, g, func() bool { return g.attached == nil })
	if len(got) != 1 {
		t.Fatalf("notified a quick command: %q", got)
	}
}
