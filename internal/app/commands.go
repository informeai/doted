package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/informeai/doted/internal/terminal"
)

func (g *Game) submit() {
	line := g.editor.Submit()
	g.scroll = 0
	g.scrollback.Append(terminal.Command, g.cfg.Prompt.Symbol+line, time.Now())

	cmd, background := cutBackground(strings.TrimSpace(line))
	if cmd == "" || !background && g.runBuiltin(cmd) {
		return // a trailing & always means a shell command, even for `exit 3 &`
	}
	j, err := g.jobs.Start(g.session, cmd, g.cols, g.outputRows, time.Now())
	if err != nil {
		g.scrollback.Append(terminal.Error, err.Error(), time.Now())
		return
	}
	if background {
		j.Listed = true
		g.scrollback.Append(terminal.System, fmt.Sprintf("[%d] running in background: %s · ctrl+t to see jobs", j.ID, j.Command), time.Now())
		return
	}
	g.attached = j
	g.parser.Begin()
}

// cutBackground strips a trailing `&` (but not `&&`), which asks for the
// command to start in the background.
func cutBackground(cmd string) (string, bool) {
	if !strings.HasSuffix(cmd, "&") || strings.HasSuffix(cmd, "&&") {
		return cmd, false
	}
	return strings.TrimSpace(strings.TrimSuffix(cmd, "&")), true
}

// runBuiltin handles the commands that must act on doted itself rather than
// on a child process.
func (g *Game) runBuiltin(cmd string) bool {
	name, arg, _ := strings.Cut(cmd, " ")
	arg = strings.TrimSpace(arg)
	if name != "exit" && name != "quit" {
		g.quitArmed = false // the exit confirmation only covers consecutive exits
	}
	switch name {
	case "exit", "quit":
		g.requestQuit()
	case "clear":
		g.scrollback.Clear()
	case "cd":
		if err := g.session.Chdir(arg); err != nil {
			g.scrollback.Append(terminal.Error, "cd: "+err.Error(), time.Now())
		}
	case "jobs":
		g.openPanel()
	case "fg":
		g.fg(arg)
	default:
		return false
	}
	return true
}

// fg opens a job in the job view: `fg` picks the newest, `fg 2` or `fg %2`
// a specific one.
func (g *Game) fg(arg string) {
	listed := g.jobs.Listed()
	if arg == "" {
		if len(listed) == 0 {
			g.scrollback.Append(terminal.Error, "fg: no background jobs", time.Now())
			return
		}
		g.openJob(listed[len(listed)-1])
		return
	}
	id, err := strconv.Atoi(strings.TrimPrefix(arg, "%"))
	j := g.jobs.Get(id)
	if err != nil || j == nil {
		g.scrollback.Append(terminal.Error, "fg: no such job: "+arg, time.Now())
		return
	}
	g.openJob(j)
}
