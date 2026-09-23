//go:build unix

package app

import (
	"strings"
	"testing"
	"time"
)

func jobText(j interface{ Tail(int) []string }) string {
	return strings.Join(j.Tail(50), "\n")
}

func TestRestartJob(t *testing.T) {
	g := newTestGame(t)
	run(g, "sleep 30 &")
	run(g, "echo run-$RANDOM; sleep 30 &")
	old := g.jobs.Listed()[1]
	tickUntil(t, g, func() bool { return strings.Contains(jobText(old), "run-") })
	g.watchJobs(time.Now())
	shown := g.watches[old].shownAt

	g.restartJob(old)
	tickUntil(t, g, func() bool { l := g.jobs.Listed(); return len(l) == 2 && l[1] != old })
	next := g.jobs.Listed()[1]
	if next.Command != old.Command || !next.Running() || old.Running() {
		t.Fatalf("restart: %q running %v, old running %v", next.Command, next.Running(), old.Running())
	}
	// Same place, same card: no entrance, no "killed" message.
	if w := g.watches[next]; w == nil || w.name != "echo" || !w.shownAt.Equal(shown) {
		t.Fatalf("watch = %+v", g.watches[next])
	}
	if strings.Contains(mainText(g), "killed") {
		t.Fatalf("restarting reported the kill: %q", mainText(g))
	}
	tickUntil(t, g, func() bool { return strings.Contains(jobText(next), "run-") })
}

func TestStopJob(t *testing.T) {
	g := newTestGame(t)
	run(g, "trap 'echo got-int; exit 0' INT; while true; do sleep 0.05; done &")
	j := g.jobs.Listed()[0]
	time.Sleep(100 * time.Millisecond)
	g.stopJob(j, time.Now())
	tickUntil(t, g, func() bool { return !j.Running() })
	if !strings.Contains(jobText(j), "got-int") || j.Killed {
		t.Fatalf("stop should interrupt: killed %v, output %q", j.Killed, jobText(j))
	}

	// One that ignores Ctrl+C is killed by a second stop.
	run(g, "trap '' INT; sleep 30 &")
	k := g.jobs.Listed()[1]
	time.Sleep(100 * time.Millisecond)
	now := time.Now()
	g.stopJob(k, now)
	if !g.stopping(k, now.Add(time.Second)) {
		t.Fatal("after a stop, the next one should kill")
	}
	g.stopJob(k, now.Add(time.Second))
	tickUntil(t, g, func() bool { return !k.Running() })
	if !k.Killed {
		t.Fatal("the second stop should kill")
	}
}

func TestSendToJob(t *testing.T) {
	g := newTestGame(t)
	run(g, "read x; echo got:$x; read y &")
	j := g.jobs.Listed()[0]
	g.editor.Insert([]rune("git sta")...)

	if !g.sendToCard(1) || g.target != j || g.editor.Text() != "" {
		t.Fatalf("alt+1: target %v, line %q", g.target, g.editor.Text())
	}
	if g.promptText() != "  → read x › " || g.suggestion(time.Now()) != "" {
		t.Fatalf("prompt %q", g.promptText())
	}
	// A line at a time: Enter sends what was typed.
	g.editor.Insert([]rune("hello")...)
	g.sendTargetLine()
	tickUntil(t, g, func() bool { return strings.Contains(jobText(j), "got:hello") })
	if c, ok := j.LineMode(); !ok || !c {
		t.Fatalf("read reads a line at a time: canonical %v ok %v", c, ok)
	}

	// When the job ends, the line points back at the shell, as it was.
	j.Write([]byte("\r"))
	tickUntil(t, g, func() bool { return !j.Running() })
	g.handleTargetKeys()
	if g.target != nil || g.editor.Text() != "git sta" {
		t.Fatalf("after the job ended: target %v, line %q", g.target, g.editor.Text())
	}
}

func TestSendToKeyReader(t *testing.T) {
	g := newTestGame(t)
	run(g, "stty -icanon; k=$(dd bs=1 count=1 2>/dev/null); echo key:$k; sleep 30 &")
	j := g.jobs.Listed()[0]
	g.enterTarget(j)
	tickUntil(t, g, func() bool { g.handleTargetKeys(); return g.targetRaw })
	j.SendText("r") // what forwardKeyboard does for a key
	tickUntil(t, g, func() bool { return strings.Contains(jobText(j), "key:r") })
}
