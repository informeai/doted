package app

import (
	"testing"
	"time"

	"github.com/informeai/doted/internal/config"
	"github.com/informeai/doted/internal/winstate"
)

func TestPlanWindow(t *testing.T) {
	cfg := config.Window{Width: 1280, Height: 800, Remember: true}
	monitors := []monitorInfo{{"Built-in", 1512, 982}, {"Studio Display", 2560, 1440}}
	saved := winstate.Window{Width: 1400, Height: 900, X: 300, Y: 200, HasPosition: true, Monitor: "Studio Display"}

	for name, tt := range map[string]struct {
		saved winstate.Window
		ok    bool
		cfg   config.Window
		want  windowPlan
	}{
		"first run": {ok: false, cfg: cfg,
			want: windowPlan{width: 1280, height: 800, monitor: -1}},
		"as it was left": {saved: saved, ok: true, cfg: cfg,
			want: windowPlan{width: 1400, height: 900, monitor: 1, x: 300, y: 200, setPosition: true}},
		"remember off": {saved: saved, ok: true, cfg: config.Window{Width: 1280, Height: 800},
			want: windowPlan{width: 1280, height: 800, monitor: -1}},
		"monitor gone": {saved: withMonitor(saved, "Old TV"), ok: true, cfg: cfg,
			want: windowPlan{width: 1400, height: 900, monitor: -1}},
		"off the monitor": {saved: withPos(saved, 2550, 200), ok: true, cfg: cfg,
			want: windowPlan{width: 1400, height: 900, monitor: -1}},
		"maximized": {saved: withMax(saved), ok: true, cfg: cfg,
			want: windowPlan{width: 1400, height: 900, monitor: 1, x: 300, y: 200, setPosition: true, maximize: true}},
		"too small": {saved: winstate.Window{Width: 50, Height: 40}, ok: true, cfg: cfg,
			want: windowPlan{width: 1280, height: 800, monitor: -1}},
	} {
		if got := planWindow(tt.saved, tt.ok, tt.cfg, monitors); got != tt.want {
			t.Errorf("%s: %+v, want %+v", name, got, tt.want)
		}
	}
}

func withMonitor(w winstate.Window, m string) winstate.Window { w.Monitor = m; return w }
func withPos(w winstate.Window, x, y int) winstate.Window     { w.X, w.Y = x, y; return w }
func withMax(w winstate.Window) winstate.Window               { w.Maximized = true; return w }

func TestWindowTrackerSavesChanges(t *testing.T) {
	g := newTestGame(t)
	now := time.Now()
	w := winstate.Window{Width: 1300, Height: 820, X: 10, Y: 20, HasPosition: true, Monitor: "Built-in"}

	g.window.note(w, now)
	if !g.window.dirty {
		t.Fatal("a new size should be marked for saving")
	}
	g.saveWindow()
	if got, ok := winstate.Load(g.window.path); !ok || got != w {
		t.Fatalf("saved %+v, %v", got, ok)
	}

	g.window.note(w, now.Add(time.Second)) // unchanged
	if g.window.dirty {
		t.Fatal("an unchanged window shouldn't be saved again")
	}

	g.cfg.Window.Remember = false
	g.window.note(winstate.Window{Width: 900, Height: 700}, now)
	g.saveWindow()
	if got, _ := winstate.Load(g.window.path); got != w {
		t.Fatalf("saved %+v with remember off", got)
	}
}
