//go:build unix

package app

import (
	"strings"
	"testing"
	"time"
)

func TestJobName(t *testing.T) {
	for cmd, want := range map[string]string{
		"npm run dev":                 "dev",
		"pnpm dev --host":             "dev",
		"yarn test":                   "test",
		"make watch":                  "watch",
		"go test -run X ./...":        "go test",
		"docker compose up api":       "docker compose up",
		"NODE_ENV=dev node server.js": "node",
		"./scripts/build.sh":          "./scripts/build.sh",
	} {
		if got := jobName(cmd); got != want {
			t.Errorf("jobName(%q) = %q, want %q", cmd, got, want)
		}
	}
}

func TestIsError(t *testing.T) {
	for line, want := range map[string]bool{
		"error: cannot find module":   true,
		"--- FAIL: TestLogin (0.02s)": true,
		"panic: runtime error":        true,
		"Found 0 errors.":             false,
		"compiled with no errors":     false,
		"ok  	internal/auth	0.4s":     false,
	} {
		if got := isError(line); got != want {
			t.Errorf("isError(%q) = %v", line, got)
		}
	}
}

func TestStripWatchesJobs(t *testing.T) {
	g := newTestGame(t)
	run(g, "echo '  ➜  Local:   http://localhost:5173/'; echo 'error: boom'; read _; echo 'ready in 20ms'; sleep 0.2 &")
	j := g.jobs.Listed()[0]
	tickUntil(t, g, func() bool {
		g.watchJobs(time.Now())
		w := g.watches[j]
		return w != nil && w.failing
	})
	w := g.watches[j]
	if w.url != "http://localhost:5173/" || w.name != "echo" || w.flashAt.IsZero() {
		t.Fatalf("watch = %+v", w)
	}
	if cards := g.stripJobs(time.Now()); len(cards) != 1 || cards[0] != j {
		t.Fatalf("cards = %v", cards)
	}
	if g.jobColor(j) != g.theme.Error {
		t.Fatal("a job printing errors should show red")
	}

	// A line that looks like success clears the error.
	j.Write([]byte("\r"))
	tickUntil(t, g, func() bool { g.watchJobs(time.Now()); return !g.watches[j].failing })
	tickUntil(t, g, func() bool { return !j.Running() })
	if g.jobColor(j) != g.theme.ANSI[2] {
		t.Fatal("a job that ended well should show green")
	}
	// The card leaves a while after the job ends.
	if len(g.stripJobs(j.Ended.Add(stripLingerOK/2))) != 1 || len(g.stripJobs(j.Ended.Add(stripLingerOK))) != 0 {
		t.Fatal("a finished card should linger, then leave")
	}
}

func TestStripCardOpensJob(t *testing.T) {
	g := newTestGame(t)
	run(g, "sleep 5 &")
	run(g, "sleep 5 &")
	cards := g.stripJobs(time.Now())
	if len(cards) != 2 {
		t.Fatalf("%d cards", len(cards))
	}
	if g.openStripCard(3) || g.viewing != nil {
		t.Fatal("there's no third card")
	}
	if !g.openStripCard(2) || g.viewing != cards[1] {
		t.Fatal("ctrl+2 should open the second card's job")
	}
	// The strip hides in the job view, and with the setting off.
	if len(g.stripJobs(time.Now())) != 0 {
		t.Fatal("the strip should hide in the job view")
	}
	g.closeJob()
	g.cfg.Jobs.Strip = false
	if len(g.stripJobs(time.Now())) != 0 {
		t.Fatal("strip = false should hide it")
	}
}

func TestStripKeepsTheOutputClean(t *testing.T) {
	g := newTestGame(t)
	run(g, "echo before; read _")
	j := g.attached
	g.background()
	run(g, "true &")
	j.Write([]byte("\r"))
	tickUntil(t, g, func() bool { return !j.Running() && !g.jobs.Listed()[1].Running() })
	if text := mainText(g); strings.Contains(text, "[1]") || strings.Contains(text, "[2]") {
		t.Fatalf("job messages in the main view with the strip on: %q", text)
	}
	// The command's line keeps how it went.
	if b := g.blockOf(j); b == nil || b.running() || !b.background {
		t.Fatalf("block = %+v", b)
	}
}
