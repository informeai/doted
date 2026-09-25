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
	g.cfg.Jobs.Strip = false // without the strip, messages tell about jobs
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
	g.cfg.Jobs.Strip = false
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

func TestReloadNoticeWaitsForAttachedJob(t *testing.T) {
	g := newTestGame(t)
	run(g, "read _")
	defer g.jobs.KillAll()

	g.reloads <- reload{settings: DefaultSettings()}
	g.handleReloads()
	if got := lastLine(g); strings.Contains(got, "config reloaded") {
		t.Fatal("notice written into the attached job's output")
	}
	if len(g.pending) != 1 {
		t.Fatalf("pending = %v", g.pending)
	}
}

func TestHelpHintOnStatusLine(t *testing.T) {
	g := newTestGame(t)
	if g.scrollback.Len() != 0 {
		t.Fatalf("nothing should be printed at startup, got %q", mainText(g))
	}
	if hint, _ := g.statusHint(time.Now()); hint != helpHint {
		t.Fatalf("status hint at startup = %q", hint)
	}

	run(g, "read _")
	defer g.jobs.KillAll()
	if hint, spinner := g.statusHint(time.Now()); !strings.HasPrefix(hint, "running") || !spinner {
		t.Fatalf("while running, hint = %q spinner %v", hint, spinner)
	}
	g.attached.Write([]byte("\r"))
	tickUntil(t, g, func() bool { return g.attached == nil })
	if hint, _ := g.statusHint(time.Now()); hint != helpHint {
		t.Fatalf("the help hint should be back once idle, got %q", hint)
	}
}

func TestPasteIntoRunningCommand(t *testing.T) {
	g := newTestGame(t)
	fake(g).text = "doted" // on the system clipboard
	run(g, `read name; echo "got:$name"`)
	j := g.attached
	g.pasteTo(j)
	awaitClipboard(t, g)
	j.Write([]byte("\r"))
	tickUntil(t, g, func() bool { return g.attached == nil })
	if !strings.Contains(mainText(g), "got:doted") {
		t.Fatalf("the program didn't receive the paste: %q", mainText(g))
	}
}

func TestFullScreenTakesTheOutputArea(t *testing.T) {
	g := newTestGame(t)
	run(g, `printf '\033[?1049h'; read x; printf '\033[?1049l'`)
	j := g.attached
	tickUntil(t, g, func() bool { return j.FullScreen() })
	if g.screenJob() != j {
		t.Fatal("the full-screen job should fill the output area")
	}
	if hint, _ := g.statusHint(time.Now()); !strings.HasPrefix(hint, "full screen") {
		t.Fatalf("status hint = %q", hint)
	}
	j.Write([]byte("\r"))
	tickUntil(t, g, func() bool { return g.attached == nil })
	if g.screenJob() != nil {
		t.Fatal("the grid should give way to the history once the program ends")
	}
}

// A paste read in the background lands only where the keyboard still is.
func TestLatePasteIsDropped(t *testing.T) {
	g := newTestGame(t)
	fake(g).text = "late"
	g.paste()        // for the input line...
	run(g, "read x") // ...but a command takes the keyboard first
	defer g.jobs.KillAll()
	awaitClipboard(t, g)
	if !g.editor.Empty() {
		t.Fatalf("a late paste landed in the input: %q", g.editor.Text())
	}
}

func TestWheelScrollsTheJobView(t *testing.T) {
	g := newTestGame(t)
	g.outputRows, g.cols = 10, 80
	run(g, "seq 1 100 &")
	j := g.jobs.Listed()[0]
	tickUntil(t, g, func() bool { return !j.Running() })
	g.openJob(j)

	// A trackpad's small steps, as they come: they add up to rows.
	var acc float64
	for range 10 {
		if n := addWheel(&acc, 0.3); n != 0 {
			g.scrollBy(n)
		}
	}
	if g.scroll != 9 {
		t.Fatalf("scrolled %d rows in the job view, want 9", g.scroll)
	}
	// And back down.
	for range 10 {
		if n := addWheel(&acc, -0.3); n != 0 {
			g.scrollBy(n)
		}
	}
	if g.scroll != 0 {
		t.Fatalf("scrolled back to %d, want 0", g.scroll)
	}
}

func TestScrollToStartAndEnd(t *testing.T) {
	g := newTestGame(t)
	g.outputRows, g.cols = 10, 80
	run(g, "seq 1 50")
	tickUntil(t, g, func() bool { return g.attached == nil })

	g.scrollToStart()
	max := g.totalRows(g.scrollback) - g.outputRows
	if g.scroll != max || max <= 0 {
		t.Fatalf("scrolled to %d, want the start at %d", g.scroll, max)
	}
	g.scrollBy(-1) // Shift+Down
	if g.scroll != max-1 {
		t.Fatalf("one row down: %d", g.scroll)
	}
	g.scrollBy(1000) // can't go past the start
	if g.scroll != max {
		t.Fatalf("past the start: %d", g.scroll)
	}
}
