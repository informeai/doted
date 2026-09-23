package app

import (
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/informeai/doted/internal/config"
	"github.com/informeai/doted/internal/terminal"
	"github.com/informeai/doted/internal/winstate"
)

// doted reopens its window the way it was left: size, position (on the same
// monitor, when it's still connected) and maximized state. It saves a second
// after the window stops moving, and when doted quits.

const (
	windowSaveDelay = time.Second
	minWindowWidth  = 200
	minWindowHeight = 120
	// A saved position is only used if this much of the window's corner
	// would still be on the monitor, so it can't open out of reach.
	minVisible = 80
)

type monitorInfo struct {
	name          string
	width, height int
}

// windowPlan is how to open the window.
type windowPlan struct {
	width, height int
	monitor       int // index into the monitors given, or -1 for the default
	x, y          int
	setPosition   bool
	maximize      bool
}

// planWindow decides how to open the window from the saved one (if any), the
// configured size and the monitors connected now.
func planWindow(saved winstate.Window, ok bool, cfg config.Window, monitors []monitorInfo) windowPlan {
	plan := windowPlan{width: cfg.Width, height: cfg.Height, monitor: -1}
	if !cfg.Remember || !ok {
		return plan
	}
	if saved.Width >= minWindowWidth && saved.Height >= minWindowHeight {
		plan.width, plan.height = saved.Width, saved.Height
	}
	plan.maximize = saved.Maximized
	if !saved.HasPosition {
		return plan
	}
	for i, m := range monitors {
		if m.name != saved.Monitor {
			continue
		}
		fits := saved.X >= 0 && saved.Y >= 0 && saved.X <= m.width-minVisible && saved.Y <= m.height-minVisible
		if fits {
			plan.monitor, plan.x, plan.y, plan.setPosition = i, saved.X, saved.Y, true
		}
		break
	}
	return plan
}

// RestoreWindow sizes and places the window before it opens: as it was left,
// or at the configured size the first time.
func RestoreWindow(cfg config.Config) {
	saved, ok := winstate.Load(winstate.Path())
	monitors := ebiten.AppendMonitors(nil)
	infos := make([]monitorInfo, len(monitors))
	for i, m := range monitors {
		w, h := m.Size()
		infos[i] = monitorInfo{name: m.Name(), width: w, height: h}
	}
	plan := planWindow(saved, ok, cfg.Window, infos)
	ebiten.SetWindowSize(plan.width, plan.height)
	if plan.monitor >= 0 {
		ebiten.SetMonitor(monitors[plan.monitor])
	}
	if plan.setPosition {
		ebiten.SetWindowPosition(plan.x, plan.y)
	}
	if plan.maximize {
		ebiten.MaximizeWindow()
	}
}

// windowTracker notices changes to the window and saves them.
type windowTracker struct {
	path    string
	last    winstate.Window
	dirty   bool
	changed time.Time
}

// trackWindow records the window's current state and saves it once it has
// held still for windowSaveDelay. While maximized, the last normal size and
// position are kept, so unmaximizing next time returns to them.
func (g *Game) trackWindow(now time.Time) {
	if !g.cfg.Window.Remember || ebiten.IsWindowMinimized() {
		return
	}
	cur := g.window.last
	cur.Maximized = ebiten.IsWindowMaximized()
	if !cur.Maximized {
		cur.Width, cur.Height = ebiten.WindowSize()
		cur.X, cur.Y = ebiten.WindowPosition()
		cur.HasPosition = true
		cur.Monitor = ebiten.Monitor().Name()
	}
	g.window.note(cur, now)
	if g.window.dirty && now.Sub(g.window.changed) >= windowSaveDelay {
		g.saveWindow()
	}
}

// note records the window's state, marking it for saving if it changed.
func (w *windowTracker) note(cur winstate.Window, now time.Time) {
	if cur.Width <= 0 || cur.Height <= 0 || cur == w.last {
		return
	}
	w.last, w.dirty, w.changed = cur, true, now
}

// saveWindow writes the window's state if it changed since the last save.
func (g *Game) saveWindow() {
	if !g.window.dirty || !g.cfg.Window.Remember {
		return
	}
	g.window.dirty = false
	if err := winstate.Save(g.window.path, g.window.last); err != nil {
		g.notify(terminal.Error, "window size not saved: "+err.Error())
	}
}
