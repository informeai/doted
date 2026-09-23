package app

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
)

// The status line shows, after the directory, what you're working on: the
// git branch (with * when there are changes), the project's Go or Node
// version, and how long the last command took. Looking these up runs git
// and node, so it happens in the background: when the directory changes,
// after each command, and every so often in case something else changed
// the repository.

const (
	contextEvery   = 15 * time.Second
	contextTimeout = 2 * time.Second
)

// projectContext is what was found about a directory.
type projectContext struct {
	dir    string
	branch string
	dirty  bool
	lang   string // "go 1.26", "node 22.3.0"
}

// contextProbe looks up a directory's context; probeContext by default.
type contextProbe func(dir, pathEnv string) projectContext

type contextState struct {
	info    projectContext
	results chan projectContext
	busy    bool
	stale   bool // look again: a command ran since
	lastRun time.Time
	probe   contextProbe
}

// watchContext starts a lookup when the context may have changed and takes
// in the results.
func (g *Game) watchContext(now time.Time) {
	c := &g.context
	if c.results == nil {
		c.results = make(chan projectContext, 1)
	}
	select {
	case info := <-c.results:
		c.info, c.busy = info, false
	default:
	}
	if !g.cfg.Status.Context || c.busy {
		return
	}
	dir := g.session.Dir()
	if dir == c.info.dir && !c.stale && now.Sub(c.lastRun) < contextEvery {
		return
	}
	if dir != c.info.dir {
		c.info = projectContext{dir: dir} // don't show the old project's context meanwhile
	}
	c.busy, c.stale, c.lastRun = true, false, now
	probe, path := c.probe, g.session.Getenv("PATH")
	if probe == nil {
		probe = probeContext
	}
	go func() { c.results <- probe(dir, path) }()
}

// contextText is the context for the status line.
func (g *Game) contextText() string {
	var parts []string
	if !g.cfg.Status.Context {
		return ""
	}
	info := g.context.info
	if info.dir == g.session.Dir() {
		if info.branch != "" {
			b := "git " + info.branch
			if info.dirty {
				b += "*"
			}
			parts = append(parts, b)
		}
		if info.lang != "" {
			parts = append(parts, info.lang)
		}
	}
	if g.lastDuration > 0 {
		parts = append(parts, "last "+formatDuration(g.lastDuration))
	}
	return strings.Join(parts, " · ")
}

// probeContext looks up dir's git branch and project language, running git
// and node from pathEnv.
func probeContext(dir, pathEnv string) projectContext {
	info := projectContext{dir: dir}
	run := func(name string, args ...string) (string, bool) {
		bin := lookIn(pathEnv, name)
		if bin == "" {
			return "", false
		}
		ctx, cancel := context.WithTimeout(context.Background(), contextTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "PATH="+pathEnv, "GIT_OPTIONAL_LOCKS=0")
		out, err := cmd.Output()
		return string(out), err == nil
	}
	if out, ok := run("git", "status", "--porcelain=v1", "--branch"); ok {
		info.branch, info.dirty = parseGitStatus(out)
	}
	switch root, kind := findProject(dir); kind {
	case "go":
		if v := goModVersion(filepath.Join(root, "go.mod")); v != "" {
			info.lang = "go " + v
		}
	case "node":
		if out, ok := run("node", "--version"); ok {
			info.lang = "node " + strings.TrimPrefix(strings.TrimSpace(out), "v")
		}
	}
	return info
}

// parseGitStatus reads the branch and whether anything changed from the
// output of git status --porcelain=v1 --branch.
func parseGitStatus(out string) (branch string, dirty bool) {
	first, rest, _ := strings.Cut(out, "\n")
	head, ok := strings.CutPrefix(first, "## ")
	if !ok {
		return "", false
	}
	switch {
	case strings.HasPrefix(head, "No commits yet on "):
		branch = strings.TrimPrefix(head, "No commits yet on ")
	case strings.HasPrefix(head, "HEAD (no branch)"):
		branch = "detached"
	default:
		branch, _, _ = strings.Cut(head, "...")
		branch, _, _ = strings.Cut(branch, " ")
	}
	return branch, strings.TrimSpace(rest) != ""
}

// findProject walks up from dir to the nearest go.mod or package.json, not
// past the home directory.
func findProject(dir string) (root, kind string) {
	home, _ := os.UserHomeDir()
	for d := dir; ; d = filepath.Dir(d) {
		if exists(filepath.Join(d, "go.mod")) {
			return d, "go"
		}
		if exists(filepath.Join(d, "package.json")) {
			return d, "node"
		}
		if d == home || filepath.Dir(d) == d {
			return "", ""
		}
	}
}

// goModVersion is the go directive of a go.mod: "1.26" for "go 1.26.5".
func goModVersion(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := bufio.NewScanner(bytes.NewReader(data))
	for s.Scan() {
		if v, ok := strings.CutPrefix(strings.TrimSpace(s.Text()), "go "); ok {
			parts := strings.SplitN(strings.TrimSpace(v), ".", 3)
			return strings.Join(parts[:min(2, len(parts))], ".")
		}
	}
	return ""
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// lookIn finds the program name in the directories of pathEnv.
func lookIn(pathEnv, name string) string {
	for _, dir := range filepath.SplitList(pathEnv) {
		if !filepath.IsAbs(dir) {
			continue
		}
		for _, candidate := range []string{name, name + ".exe"} {
			p := filepath.Join(dir, candidate)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
	}
	return ""
}

// focusChanged reports whether the window just came back to the front, a
// good moment to look at the repository again.
func (g *Game) focusChanged() bool {
	focused := ebiten.IsFocused()
	back := focused && !g.focused
	g.focused = focused
	return back
}
