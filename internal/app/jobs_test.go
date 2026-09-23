//go:build unix

package app

import (
	"strings"
	"testing"
	"time"
)

// run types cmd at the prompt and presses Enter.
func run(g *Game, cmd string) {
	g.editor.Insert([]rune(cmd)...)
	g.submit()
}

// tick does what Update does besides reading the keyboard.
func tick(g *Game) {
	g.jobs.Poll(time.Now(), g.handleJobEvent)
	g.flushNotices()
}

func tickUntil(t *testing.T, g *Game, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tick(g)
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not reached; last line %q", lastLine(g))
}

func mainText(g *Game) string {
	var lines []string
	for i := range g.scrollback.Len() {
		lines = append(lines, g.scrollback.At(i).Text())
	}
	return strings.Join(lines, "\n")
}

func TestForegroundCommandIsNotListed(t *testing.T) {
	g := newTestGame(t)
	run(g, "echo hi")
	if g.attached == nil {
		t.Fatal("command should be attached to the main view")
	}
	tickUntil(t, g, func() bool { return g.attached == nil })
	if !strings.Contains(mainText(g), "hi") || len(g.jobs.Listed()) != 0 {
		t.Fatalf("main %q, listed %d", mainText(g), len(g.jobs.Listed()))
	}
}

func TestBackgroundKeepsOutputAndNotifies(t *testing.T) {
	g := newTestGame(t)
	run(g, "echo before; read _; echo after")
	j := g.attached
	tickUntil(t, g, func() bool { return strings.Contains(mainText(g), "before") })

	g.background()
	if g.attached != nil || !j.Listed || !strings.Contains(lastLine(g), "[1] moved to background") {
		t.Fatalf("attached %v listed %v last %q", g.attached, j.Listed, lastLine(g))
	}

	// The prompt is free: another command runs while the job waits.
	run(g, "echo meanwhile")
	tickUntil(t, g, func() bool { return g.attached == nil })

	j.Write([]byte("\r"))
	tickUntil(t, g, func() bool { return !j.Running() })
	if strings.Contains("\n"+mainText(g)+"\n", "\nafter\n") {
		t.Fatal("background output leaked into the main view")
	}
	out := j.Output.At(j.Output.Len() - 1).Text()
	if out != "after" {
		t.Fatalf("job's last line = %q, want after", out)
	}
	if !strings.Contains(lastLine(g), "[1] done: echo before") {
		t.Fatalf("last line %q", lastLine(g))
	}
}

func TestTrailingAmpersandStartsInBackground(t *testing.T) {
	g := newTestGame(t)
	run(g, "sleep 30 &")
	defer g.jobs.KillAll()

	if g.attached != nil {
		t.Fatal("job should not be attached")
	}
	l := g.jobs.Listed()
	if len(l) != 1 || l[0].Command != "sleep 30" || !strings.Contains(lastLine(g), "running in background") {
		t.Fatalf("listed %v, last %q", l, lastLine(g))
	}

	run(g, "exit 3 &")
	if g.quit || g.quitArmed || len(g.jobs.Listed()) != 2 {
		t.Fatalf("`exit 3 &` should run in the shell, not quit (listed %d)", len(g.jobs.Listed()))
	}

	run(g, "true && true")
	if g.attached == nil {
		t.Fatal("&& must not be treated as background")
	}
}

func TestFgOpensJobView(t *testing.T) {
	g := newTestGame(t)
	run(g, "sleep 30 &")
	run(g, "sleep 30 &")
	defer g.jobs.KillAll()

	run(g, "fg")
	if g.viewing == nil || g.viewing.ID != 2 {
		t.Fatalf("fg should open the newest job, got %v", g.viewing)
	}
	g.closeJob()
	run(g, "fg %1")
	if g.viewing == nil || g.viewing.ID != 1 {
		t.Fatalf("fg %%1 opened %v", g.viewing)
	}
	g.closeJob()
	run(g, "fg 9")
	if g.viewing != nil || !strings.Contains(lastLine(g), "no such job") {
		t.Fatalf("viewing %v, last %q", g.viewing, lastLine(g))
	}
}

func TestQuitAsksOnceWithRunningJobs(t *testing.T) {
	g := newTestGame(t)
	run(g, "sleep 30 &")
	defer g.jobs.KillAll()

	run(g, "exit")
	if g.quit || !strings.Contains(lastLine(g), "exit again") {
		t.Fatalf("quit %v, last %q", g.quit, lastLine(g))
	}
	run(g, "exit")
	if !g.quit {
		t.Fatal("second exit should quit")
	}
}

func TestCutBackground(t *testing.T) {
	for in, want := range map[string]struct {
		cmd string
		bg  bool
	}{
		"npm run dev &": {"npm run dev", true},
		"a && b":        {"a && b", false},
		"sleep 1&":      {"sleep 1", true},
		"echo a & b":    {"echo a & b", false},
	} {
		cmd, bg := cutBackground(in)
		if cmd != want.cmd || bg != want.bg {
			t.Errorf("cutBackground(%q) = %q, %v", in, cmd, bg)
		}
	}
}
