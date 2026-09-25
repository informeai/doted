package app

import (
	"time"
	"unicode"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/informeai/doted/internal/jobs"
)

// Background jobs can be driven from their cards, without opening them:
//
//   - restart stops the job and, once it has ended (so a dev server frees
//     its port), runs the same command again in its place;
//   - stop sends Ctrl+C, and a second stop soon after kills it;
//   - send points the input line at the job: what's typed goes to it while
//     its card grows to show the answer. A job that reads a line at a time
//     gets each line on Enter, with doted's line editing; one that reads a
//     key at a time (a watcher waiting for r or q, a REPL) gets every key as
//     it's pressed. Esc points the line back at the shell.

const (
	stopToKill  = 3 * time.Second // a second stop within this kills the job
	targetLines = selectedLines   // lines the card of the job being sent to shows
)

// restartJob runs j's command again in its place, once j has ended.
func (g *Game) restartJob(j *jobs.Job) {
	w := g.watchOf(j)
	if j.Running() {
		w.restarting = true
		j.Kill()
		g.flash("restarting " + w.name)
		return
	}
	var next *jobs.Job
	var err error
	if p := g.pipes[j]; p != nil {
		next, err = g.startPipeline(p.line, time.Now()) // pipeline.toml may have changed
	} else if arg, ok := g.explains[j]; ok {
		next, _, err = g.startExplain(arg, 0, time.Now())
		if err == nil {
			next.Listed = true
		}
	} else {
		next, err = g.jobs.Start(g.session, j.Command, g.cols, g.outputRows, time.Now())
	}
	if err != nil {
		g.flash("could not restart " + w.name + ": " + err.Error())
		return
	}
	next.Listed = true
	g.jobs.Replace(j, next)
	// The card carries on: same name, no entrance.
	g.watches[next] = &jobWatch{name: w.name, shownAt: w.shownAt, x: w.x, w: w.w, placed: w.placed}
	delete(g.watches, j)
	if g.target == j {
		g.target = next
	}
}

// stopJob interrupts j, or kills it when it was just interrupted.
func (g *Game) stopJob(j *jobs.Job, now time.Time) {
	w := g.watchOf(j)
	if !j.Running() {
		return
	}
	if now.Sub(w.stoppedAt) < stopToKill {
		j.Kill()
		return
	}
	w.stoppedAt = now
	j.Interrupt()
}

// stopping reports whether j was interrupted a moment ago, so the next stop
// kills it.
func (g *Game) stopping(j *jobs.Job, now time.Time) bool {
	w := g.watches[j]
	return w != nil && j.Running() && now.Sub(w.stoppedAt) < stopToKill
}

func (g *Game) watchOf(j *jobs.Job) *jobWatch {
	if g.watches == nil {
		g.watches = map[*jobs.Job]*jobWatch{}
	}
	w := g.watches[j]
	if w == nil {
		w = &jobWatch{name: jobName(j.Command), shownAt: time.Now()}
		g.watches[j] = w
	}
	return w
}

// jobRestarted handles the end of a job being restarted, reporting whether
// it was one.
func (g *Game) jobRestarted(j *jobs.Job) bool {
	w := g.watches[j]
	if w == nil || !w.restarting {
		return false
	}
	g.session.Discard(j.State())
	g.restartJob(j)
	return true
}

// enterTarget points the input line at j, keeping what was typed for the
// shell until it comes back.
func (g *Game) enterTarget(j *jobs.Job) {
	if !j.Running() {
		g.flash(g.watchOf(j).name + " has ended")
		return
	}
	if g.target == nil {
		g.stash = g.editor.Text()
	}
	g.target = j
	g.editor.Reset()
	g.outSel.clear()
	g.touch()
}

// exitTarget points the input line back at the shell.
func (g *Game) exitTarget() {
	if g.target == nil {
		return
	}
	g.target, g.targetRaw = nil, false
	g.editor.Reset()
	g.editor.Insert([]rune(g.stash)...)
	g.stash = ""
	g.touch()
}

// handleTargetKeys sends the keyboard to the job the input line points at.
func (g *Game) handleTargetKeys() {
	j := g.target
	if !j.Running() {
		name := g.watchOf(j).name
		g.exitTarget()
		g.flash(name + " has ended")
		return
	}
	// A full-screen program gets every key, Esc included; Ctrl+B goes back,
	// as it does from a job view.
	if j.FullScreen() {
		if ctrlPressed(ebiten.KeyB) {
			g.exitTarget()
			return
		}
		g.forwardKeyboard(j)
		return
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		g.exitTarget()
		return
	}
	canonical, ok := j.LineMode()
	if raw := ok && !canonical; raw != g.targetRaw {
		g.targetRaw = raw
		g.editor.Reset() // a line typed in one mode means nothing in the other
	}
	if g.targetRaw {
		// Every key goes as it's pressed. The line shows what was typed, as
		// programs reading keys rarely echo them, until Enter.
		enter := inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeyNumpadEnter)
		back := repeating(ebiten.KeyBackspace)
		g.forwardKeyboard(j)
		for _, r := range g.chars {
			if !unicode.IsControl(r) {
				g.editor.Insert(r)
			}
		}
		switch {
		case enter:
			g.editor.Reset()
		case back:
			g.editor.Backspace()
		}
		return
	}

	// A line at a time: edit it here, send it on Enter.
	if g.handleClipboardKeys() {
		return
	}
	ctrl := ebiten.IsKeyPressed(ebiten.KeyControl)
	meta := ebiten.IsKeyPressed(ebiten.KeyMeta)
	shift := ebiten.IsKeyPressed(ebiten.KeyShift)
	g.chars = ebiten.AppendInputChars(g.chars[:0])
	if !ctrl && !meta {
		for _, r := range g.chars {
			if !unicode.IsControl(r) {
				g.editor.Insert(r)
				g.touch()
				g.typed()
			}
		}
	}
	if ctrl {
		switch {
		case inpututil.IsKeyJustPressed(ebiten.KeyC):
			j.Interrupt()
		case inpututil.IsKeyJustPressed(ebiten.KeyD):
			if g.editor.Empty() {
				j.Write([]byte{0x04}) // end of input
			}
		case repeating(ebiten.KeyU):
			g.kill(g.editor.KillToStart())
		case repeating(ebiten.KeyW):
			g.kill(g.editor.DeleteWordBackward())
		case inpututil.IsKeyJustPressed(ebiten.KeyA):
			g.editor.Home(shift)
		case inpututil.IsKeyJustPressed(ebiten.KeyE):
			g.editor.End(shift)
		default:
			return
		}
		g.touch()
		return
	}
	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeyEnter), inpututil.IsKeyJustPressed(ebiten.KeyNumpadEnter):
		g.sendTargetLine()
	case repeating(ebiten.KeyBackspace):
		g.editor.Backspace()
	case repeating(ebiten.KeyDelete):
		g.editor.Delete()
	case repeating(ebiten.KeyArrowLeft):
		g.editor.Left(shift)
	case repeating(ebiten.KeyArrowRight):
		g.editor.Right(shift)
	case inpututil.IsKeyJustPressed(ebiten.KeyHome):
		g.editor.Home(shift)
	case inpututil.IsKeyJustPressed(ebiten.KeyEnd):
		g.editor.End(shift)
	default:
		return
	}
	g.touch()
}

// sendTargetLine sends the typed line to the job the input points at.
func (g *Game) sendTargetLine() {
	g.target.Write([]byte(g.editor.Text() + "\r"))
	g.editor.Reset()
}

// targetPrompt is the label the input line shows while it points at a job.
func (g *Game) targetPrompt() string {
	if g.target == nil {
		return ""
	}
	return "→ " + g.watchOf(g.target).name + " › "
}

// sendToCard points the input line at job #n, as Alt+n does; the strip
// scrolls to its card.
func (g *Game) sendToCard(n int) bool {
	j := g.stripJob(n)
	if j == nil {
		return false
	}
	g.enterTarget(j)
	return true
}
