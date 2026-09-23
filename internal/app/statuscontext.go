package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
)

// The status line shows, after the directory, what you're working on: the
// git branch with how many files changed, and how long the last command
// took. Looking the branch up runs git, so it happens in the background: when the directory changes,
// after each command, and every so often in case something else changed
// the repository.

const (
	contextEvery   = 15 * time.Second
	contextTimeout = 2 * time.Second
)

// projectContext is what was found about a directory.
type projectContext struct {
	dir           string
	branch        string
	ahead, behind int // commits not yet pushed, and not yet pulled
	changes       gitChanges
	gitDir        string            // which repository this is
	refs          []string          // its branches, tags and remote branches, sorted
	oids          map[string]string // the commit (or tag object) each ref points to
}

// gitChanges counts the files git status lists, by kind.
type gitChanges struct {
	modified, added, deleted, untracked, conflicts int
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
		prev := c.info
		c.info, c.busy = info, false
		g.trackBranch(prev, info, now)
	default:
	}
	if !g.cfg.Status.Context || c.busy {
		return
	}
	dir := g.session.Dir()
	if dir == c.info.dir && !c.stale && now.Sub(c.lastRun) < contextEvery {
		return
	}
	c.busy, c.stale, c.lastRun = true, false, now
	probe, path := c.probe, g.session.Getenv("PATH")
	if probe == nil {
		probe = probeContext
	}
	go func() { c.results <- probe(dir, path) }()
}

// currentContext is the context last looked up. Right after a cd it may be
// the previous directory's for a moment, until the lookup comes back; that
// keeps the branch from blinking away between two folders of one repository.
func (g *Game) currentContext() (projectContext, bool) {
	return g.context.info, g.cfg.Status.Context
}

// contextText is the context after the git part: the last command's
// duration.
func (g *Game) contextText() string {
	if !g.cfg.Status.Context || g.lastDuration <= 0 {
		return ""
	}
	return "last " + formatDuration(g.lastDuration)
}

// probeContext looks up dir's git branch, running git from pathEnv.
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
		info.branch, info.ahead, info.behind, info.changes = parseGitStatus(out)
		if out, ok := run("git", "rev-parse", "--absolute-git-dir"); ok {
			info.gitDir = strings.TrimSpace(out)
		}
		if out, ok := run("git", "for-each-ref", "--format=%(objectname) %(refname)", "refs/heads", "refs/tags", "refs/remotes"); ok {
			info.refs, info.oids = parseRefs(out)
		}
	}
	return info
}

// parseGitStatus reads the branch, how far it is from its upstream and the
// changed files from the output of git status --porcelain=v1 --branch.
func parseGitStatus(out string) (branch string, ahead, behind int, changes gitChanges) {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	head, ok := strings.CutPrefix(lines[0], "## ")
	if !ok {
		return "", 0, 0, gitChanges{}
	}
	switch {
	case strings.HasPrefix(head, "No commits yet on "):
		branch = strings.TrimPrefix(head, "No commits yet on ")
	case strings.HasPrefix(head, "HEAD (no branch)"):
		branch = "detached"
	default:
		name, rest, _ := strings.Cut(head, " ")
		branch, _, _ = strings.Cut(name, "...")
		// "[ahead 2, behind 1]"
		rest = strings.Trim(rest, "[]")
		for _, part := range strings.Split(rest, ", ") {
			if n, ok := strings.CutPrefix(part, "ahead "); ok {
				ahead, _ = strconv.Atoi(n)
			}
			if n, ok := strings.CutPrefix(part, "behind "); ok {
				behind, _ = strconv.Atoi(n)
			}
		}
	}
	for _, l := range lines[1:] {
		if len(l) < 2 {
			continue
		}
		x, y := l[0], l[1]
		switch {
		case x == '?' && y == '?':
			changes.untracked++
		case x == 'U' || y == 'U' || x == 'A' && y == 'A' || x == 'D' && y == 'D':
			changes.conflicts++
		case x == 'A':
			changes.added++
		case x == 'D' || y == 'D':
			changes.deleted++
		default: // M, R, C, T on either side
			changes.modified++
		}
	}
	return branch, ahead, behind, changes
}

// parseRefs reads git for-each-ref's "<object> <ref>" lines into the refs'
// names, sorted, and what each points to, leaving out the remotes' HEAD
// pointers.
func parseRefs(out string) (refs []string, oids map[string]string) {
	refs, oids = []string{}, map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		oid, r, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || strings.HasPrefix(r, "refs/remotes/") && strings.HasSuffix(r, "/HEAD") {
			continue
		}
		refs = append(refs, r)
		oids[r] = oid
	}
	slices.Sort(refs)
	return refs, oids
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
