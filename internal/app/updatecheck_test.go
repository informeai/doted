package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpdateNotice(t *testing.T) {
	g := newTestGame(t)
	defer func(v string) { Version = v }(Version)
	Version = "1.0.2"
	calls := 0
	g.update.path = filepath.Join(t.TempDir(), "update.json")
	g.update.fetch = func(context.Context) (string, error) { calls++; return "v1.0.3", nil }

	now := time.Now()
	g.watchUpdate(now)
	deadline := time.Now().Add(2 * time.Second)
	for g.update.available == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		g.watchUpdate(now)
	}
	if g.update.available != "1.0.3" {
		t.Fatalf("available = %q", g.update.available)
	}
	last := g.scrollback.At(g.scrollback.Len() - 1).Text()
	if !strings.Contains(last, "doted 1.0.3 is out (this is 1.0.2)") {
		t.Fatalf("notice = %q", last)
	}
	if got := g.statusHintText(); got != "doted 1.0.3 is available" {
		t.Fatalf("status hint = %q", got)
	}
	// Not asked again within the day.
	g.watchUpdate(now.Add(time.Hour))
	if calls != 1 {
		t.Fatalf("GitHub asked %d times", calls)
	}
}

func TestUpdateNotCheckedForDevBuilds(t *testing.T) {
	g := newTestGame(t)
	g.update.fetch = func(context.Context) (string, error) { t.Error("dev build asked GitHub"); return "", nil }
	g.watchUpdate(time.Now())
	if g.update.busy {
		t.Fatal("a dev build started a check")
	}
}

func (g *Game) statusHintText() string {
	h, _ := g.statusHint(time.Now())
	return h
}
