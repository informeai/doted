package app

import (
	"fmt"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/informeai/doted/internal/jobs"
	"github.com/informeai/doted/internal/terminal"
)

// panel is the overlay above the input: the jobs list or the help.
type panel struct {
	open     bool
	kind     panelKind
	selected int // index into jobs.Listed() or helpCommands
	scroll   int // help: extra lines scrolled past the selection on short windows
}

type panelKind int

const (
	panelJobs panelKind = iota
	panelHelp
)

// background detaches the attached job: it keeps running and its output keeps
// going to its own scrollback, while the prompt is free again.
func (g *Game) background() {
	j := g.attached
	g.parser.End()
	g.attached = nil
	j.Listed = true
	g.scrollback.Append(terminal.System, fmt.Sprintf("[%d] moved to background: %s · ctrl+t to see jobs", j.ID, j.Command), time.Now())
	g.scroll = 0
}

// openJob shows a job full screen; the keyboard goes to it while it runs.
func (g *Game) openJob(j *jobs.Job) {
	g.viewing = j
	g.panel.open = false
	g.scroll = 0
}

func (g *Game) closeJob() {
	g.viewing = nil
	g.scroll = 0
}

func (g *Game) openPanel() {
	// Start on the newest job, the one most likely wanted.
	g.panel = panel{open: true, kind: panelJobs, selected: max(0, len(g.jobs.Listed())-1)}
}

func (g *Game) handleJobViewKeys() {
	j := g.viewing
	switch {
	case ctrlPressed(ebiten.KeyB):
		g.closeJob()
	case ctrlPressed(ebiten.KeyT) && !j.FullScreen():
		g.openPanel()
	case !j.Running():
		// Nothing to type into any more: any of these goes back.
		if inpututil.IsKeyJustPressed(ebiten.KeyEscape) || inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeyQ) {
			g.closeJob()
		}
	default:
		g.forwardKeyboard(j)
	}
}

func (g *Game) handlePanelKeys() {
	listed := g.jobs.Listed()
	g.panel.selected = min(max(0, g.panel.selected), max(0, len(listed)-1))

	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeyEscape), ctrlPressed(ebiten.KeyT):
		g.panel.open = false
	case repeating(ebiten.KeyArrowUp):
		g.panel.selected = max(0, g.panel.selected-1)
	case repeating(ebiten.KeyArrowDown):
		g.panel.selected = min(len(listed)-1, g.panel.selected+1)
	case len(listed) == 0:
	case inpututil.IsKeyJustPressed(ebiten.KeyEnter):
		g.openJob(listed[g.panel.selected])
	case inpututil.IsKeyJustPressed(ebiten.KeyX):
		// Kill a running job; remove a finished one from the list.
		j := listed[g.panel.selected]
		if j.Running() {
			j.Kill()
			return
		}
		if g.viewing == j {
			g.closeJob()
		}
		g.jobs.Remove(j)
	}
}
