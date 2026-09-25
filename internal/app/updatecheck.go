package app

import (
	"context"
	"os"
	"time"

	"github.com/informeai/doted/internal/terminal"
	"github.com/informeai/doted/internal/update"
)

// Once a day doted asks GitHub whether a newer release is out, in the
// background; when there is one, a line in the output says so and how to
// upgrade (brew upgrade when Homebrew installed it), and the status line
// shows it in green for a little while. Builds that aren't a release ("dev") never
// ask, and [update] check = false turns it off.

// updateHintFor is how long the status line shows a newer release.
const updateHintFor = 30 * time.Second

// Version is doted's version, set by main from the build.
var Version = "dev"

type updateState struct {
	results   chan string // the latest version, or "" when it couldn't be found
	busy      bool
	lastRun   time.Time
	available string    // a newer version than this one, once found
	foundAt   time.Time // when it was found
	fetch     update.Fetcher
	path      string
}

// watchUpdate starts a check when one is due and takes in its result.
func (g *Game) watchUpdate(now time.Time) {
	u := &g.update
	if u.results == nil {
		u.results = make(chan string, 1)
	}
	select {
	case latest := <-u.results:
		u.busy = false
		if update.Newer(Version, latest) && latest != u.available {
			u.available, u.foundAt = latest, now
			exe, _ := os.Executable()
			g.notify(terminal.System, "doted "+latest+" is out (this is "+Version+") · "+update.HowTo(exe))
		}
	default:
	}
	if !g.cfg.Update.Check || !update.IsRelease(Version) || u.busy {
		return
	}
	if !u.lastRun.IsZero() && now.Sub(u.lastRun) < update.Every {
		return
	}
	u.busy, u.lastRun = true, now
	fetch, path := u.fetch, u.path
	if fetch == nil {
		fetch = update.FetchGitHub
	}
	if path == "" {
		path = update.Path()
	}
	go func() {
		latest, err := update.Latest(context.Background(), path, now, fetch)
		if err != nil {
			latest = "" // offline or rate limited: try again tomorrow
		}
		u.results <- latest
	}()
}

// updateHint is the status line's news of a newer release, for a little
// while after it was found.
func (g *Game) updateHint(now time.Time) string {
	if g.update.available == "" || !g.cfg.Update.Check || now.Sub(g.update.foundAt) >= updateHintFor {
		return ""
	}
	return "doted " + g.update.available + " is available"
}
