package app

import (
	"strconv"
	"strings"
	"time"

	"github.com/informeai/doted/internal/terminal"
)

func (g *Game) submit() {
	line := g.editor.Submit()
	g.saveHistory(line)
	g.scroll = 0
	g.scrollback.Append(terminal.Command, g.promptText()+line, time.Now())

	cmd, background := cutBackground(strings.TrimSpace(line))
	if name, arg, _ := strings.Cut(cmd, " "); background && name == "pipeline" {
		g.quitArmed = false
		g.runPipeline(arg, time.Now()) // it runs in the background anyway
		return
	}
	if cmd == "" || !background && g.runBuiltin(cmd) {
		return // a trailing & always means a shell command, even for `exit 3 &`
	}
	j, err := g.jobs.Start(g.session, cmd, g.cols, g.outputRows, time.Now())
	if err != nil {
		g.scrollback.Append(terminal.Error, err.Error(), time.Now())
		return
	}
	g.startBlock(cmd, j, background, time.Now())
	if background {
		g.record(cmd+" &", j)
	} else {
		g.record(cmd, j)
	}
	if background {
		j.Listed = true
		g.jobNotice("[%d] running in background: %s · ctrl+t to see jobs", j.ID, j.Command)
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
// on a child process. Everything else, cd and clear included, goes to the
// shell: its cd moves doted too (see shell.Session.Adopt), and the screen
// clear clear prints is understood by the parser.
func (g *Game) runBuiltin(cmd string) bool {
	name, arg, _ := strings.Cut(cmd, " ")
	arg = strings.TrimSpace(arg)
	if name != "exit" && name != "quit" {
		g.quitArmed = false // the exit confirmation only covers consecutive exits
	}
	switch name {
	case "exit", "quit":
		g.requestQuit()
	case "jobs":
		g.openPanel()
	case "help":
		g.openHelp()
	case "fg":
		g.fg(arg)
	case "pipeline":
		g.runPipeline(arg, time.Now())
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
