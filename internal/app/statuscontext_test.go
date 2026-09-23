package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseGitStatus(t *testing.T) {
	cases := []struct {
		out           string
		branch        string
		ahead, behind int
		changes       gitChanges
	}{
		{"## main...origin/main [ahead 1]\n", "main", 1, 0, gitChanges{}},
		{"## main...origin/main [ahead 2, behind 3]\n", "main", 2, 3, gitChanges{}},
		{"## main...origin/main [behind 4]\n", "main", 0, 4, gitChanges{}},
		{"## feature/x\n M app.go\nMM b.go\nR  old -> new\nA  new.go\n D gone.go\nD  gone2.go\n?? tmp\nUU c.go\nAA d.go\n", "feature/x", 0, 0,
			gitChanges{modified: 3, added: 1, deleted: 2, untracked: 1, conflicts: 2}},
		{"## No commits yet on main\n?? a\n", "main", 0, 0, gitChanges{untracked: 1}},
		{"## HEAD (no branch)\n", "detached", 0, 0, gitChanges{}},
		{"", "", 0, 0, gitChanges{}},
	}
	for _, c := range cases {
		b, a, bh, ch := parseGitStatus(c.out)
		if b != c.branch || a != c.ahead || bh != c.behind || ch != c.changes {
			t.Errorf("parseGitStatus(%q) = %q ↑%d ↓%d %+v; want %q ↑%d ↓%d %+v", c.out, b, a, bh, ch, c.branch, c.ahead, c.behind, c.changes)
		}
	}
}

func TestGitMarks(t *testing.T) {
	g := newTestGame(t)
	info := projectContext{ahead: 1, changes: gitChanges{modified: 3, untracked: 2}}
	var got []string
	for _, m := range g.gitMarks(info) {
		got = append(got, m.text)
	}
	if strings.Join(got, " ") != "~3 ?2 ↑1" {
		t.Fatalf("marks = %q", got)
	}
}

func TestProbeContext(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git")
	}
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "trunk"}} {
		if out, err := exec.Command(git, append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n\ngo 1.26.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "internal", "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	info := probeContext(sub, filepath.Dir(git))
	if info.branch != "trunk" || info.changes != (gitChanges{untracked: 1}) {
		t.Fatalf("context = %+v", info)
	}
}

func TestContextText(t *testing.T) {
	g := newTestGame(t)
	dir := g.session.Dir()
	g.context.probe = func(d, _ string) projectContext {
		return projectContext{dir: d, branch: "main"}
	}
	g.watchContext(time.Now())
	deadline := time.Now().Add(2 * time.Second)
	for g.context.info.branch == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		g.watchContext(time.Now())
	}
	g.lastDuration = 1200 * time.Millisecond
	if got := g.contextText(); got != "last 1.2s" || g.context.info.branch != "main" {
		t.Fatalf("context = %q (dir %s)", got, dir)
	}
	g.cfg.Status.Context = false
	if got := g.contextText(); got != "" {
		t.Fatalf("with context off: %q", got)
	}
}

func TestGitLogoShape(t *testing.T) {
	s := gitLogo()
	// The rounded square turned 45° spans about 58·√2 ≈ 82 units each way,
	// centered on the 78-unit view box.
	w, h := s.maxX-s.minX, s.maxY-s.minY
	if len(s.ops) < 20 || w < 78 || w > 84 || h < 78 || h > 84 {
		t.Fatalf("%d ops, %.1f × %.1f", len(s.ops), w, h)
	}
	if cx := (s.minX + s.maxX) / 2; cx < 38 || cx > 40 {
		t.Fatalf("center x = %.1f, want about 39", cx)
	}
}
