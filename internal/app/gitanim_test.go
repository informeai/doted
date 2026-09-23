package app

import (
	"testing"
	"time"
)

func TestBranchAnimations(t *testing.T) {
	g := newTestGame(t)
	g.cfg.Animation.Enabled, g.cfg.Animation.Particles = true, true
	t0 := time.Now()
	repo := func(branch string) projectContext { return projectContext{dir: "/r", branch: branch} }

	// The first lookup just appears.
	g.trackBranch(projectContext{}, repo("main"), t0)
	if _, a := g.branch.fade(repo("main"), t0); a != 1 || g.branch.name.rolling(t0) || g.branch.spin(t0) != 0 {
		t.Fatal("the first branch should appear without animating")
	}

	// Switching to a branch never seen: roll, spin and sparks.
	t1 := t0.Add(time.Second)
	g.trackBranch(repo("main"), repo("feature/login"), t1)
	mid := t1.Add(branchSpin / 2)
	if !g.branch.name.rolling(mid) || g.branch.spin(mid) <= 0 || !g.branch.burst {
		t.Fatalf("switch: rolling %v spin %.2f burst %v", g.branch.name.rolling(mid), g.branch.spin(mid), g.branch.burst)
	}
	if string(g.branch.name.from) != "main" || string(g.branch.name.to) != "feature/login" {
		t.Fatalf("roll %q → %q", string(g.branch.name.from), string(g.branch.name.to))
	}
	g.stepBranch()
	if g.branch.burst || len(g.branch.sparks.items) != branchNewSparks {
		t.Fatalf("burst fired %d sparks", len(g.branch.sparks.items))
	}
	if end := t1.Add(time.Second); g.branch.name.rolling(end) || g.branch.spin(end) != 0 {
		t.Fatal("the switch should settle")
	}

	// Back to a branch already seen: no sparks.
	t2 := t1.Add(2 * time.Second)
	g.trackBranch(repo("feature/login"), repo("main"), t2)
	if g.branch.burst {
		t.Fatal("a known branch should not throw sparks")
	}

	// Leaving the repository fades the old branch out.
	t3 := t2.Add(2 * time.Second)
	g.trackBranch(repo("main"), projectContext{dir: "/tmp"}, t3)
	info, a := g.branch.fade(projectContext{dir: "/tmp"}, t3.Add(branchFade/2))
	if info.branch != "main" || a <= 0 || a >= 1 {
		t.Fatalf("fading out: %q at %.2f", info.branch, a)
	}
	if info, _ := g.branch.fade(projectContext{dir: "/tmp"}, t3.Add(branchFade)); info.branch != "" {
		t.Fatal("the old branch should be gone after fading out")
	}

	// Entering one fades it in.
	t4 := t3.Add(time.Second)
	g.trackBranch(projectContext{dir: "/tmp"}, repo("main"), t4)
	if _, a := g.branch.fade(repo("main"), t4.Add(branchFade/2)); a <= 0 || a >= 1 {
		t.Fatalf("fading in at %.2f", a)
	}
}

func TestBranchAnimationsOff(t *testing.T) {
	g := newTestGame(t)
	g.cfg.Animation.Enabled = false
	now := time.Now()
	g.trackBranch(projectContext{}, projectContext{branch: "main"}, now)
	g.trackBranch(projectContext{branch: "main"}, projectContext{branch: "dev"}, now)
	if g.branch.name.rolling(now) || g.branch.spin(now) != 0 || g.branch.burst || string(g.branch.name.to) != "dev" {
		t.Fatal("with animations off the branch should just change")
	}
}
