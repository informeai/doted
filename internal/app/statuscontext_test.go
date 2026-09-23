package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestParseGitStatus(t *testing.T) {
	cases := []struct {
		out    string
		branch string
		dirty  bool
	}{
		{"## main...origin/main [ahead 1]\n", "main", false},
		{"## feature/x\n M app.go\n?? new.go\n", "feature/x", true},
		{"## No commits yet on main\n", "main", false},
		{"## HEAD (no branch)\n", "detached", false},
		{"", "", false},
	}
	for _, c := range cases {
		b, d := parseGitStatus(c.out)
		if b != c.branch || d != c.dirty {
			t.Errorf("parseGitStatus(%q) = %q, %v; want %q, %v", c.out, b, d, c.branch, c.dirty)
		}
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
	if info.branch != "trunk" || !info.dirty || info.lang != "go 1.26" {
		t.Fatalf("context = %+v", info)
	}
}

func TestContextText(t *testing.T) {
	g := newTestGame(t)
	dir := g.session.Dir()
	g.context.probe = func(d, _ string) projectContext {
		return projectContext{dir: d, branch: "main", dirty: true, lang: "go 1.26"}
	}
	g.watchContext(time.Now())
	deadline := time.Now().Add(2 * time.Second)
	for g.context.info.branch == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		g.watchContext(time.Now())
	}
	g.lastDuration = 1200 * time.Millisecond
	if got := g.contextText(); got != "git main* · go 1.26 · last 1.2s" {
		t.Fatalf("context = %q (dir %s)", got, dir)
	}
	g.cfg.Status.Context = false
	if got := g.contextText(); got != "" {
		t.Fatalf("with context off: %q", got)
	}
}
