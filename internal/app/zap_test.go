//go:build unix

package app

import (
	"testing"
	"time"

	"github.com/informeai/doted/internal/config"
)

func TestZapStages(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	z := zap{from: 2, to: 12, start: start, active: true}
	travel := z.travel()
	for _, tt := range []struct {
		at   time.Duration
		want zapStage
	}{
		{0, zapIn},
		{zapMorphIn - time.Millisecond, zapIn},
		{zapMorphIn, zapTravel},
		{zapMorphIn + travel - time.Millisecond, zapTravel},
		{zapMorphIn + travel, zapOut},
		{zapMorphIn + travel + zapMorphOut, zapDone},
	} {
		if got, _ := z.at(start.Add(tt.at)); got != tt.want {
			t.Errorf("at %v: stage %v, want %v", tt.at, got, tt.want)
		}
	}
	if got, _ := (zap{}).at(start); got != zapDone {
		t.Fatal("an inactive zap should be done")
	}
}

func TestZapTravelFollowsDistance(t *testing.T) {
	short := zap{from: 0, to: 2}.travel()
	mid := zap{from: 0, to: 20}.travel()
	long := zap{from: 0, to: 500}.travel()
	if short != zapMinTravel || long != zapMaxTravel || mid <= short || mid >= long {
		t.Fatalf("travel times short %v mid %v long %v", short, mid, long)
	}
}

func TestStartZapConditions(t *testing.T) {
	now := time.Now()
	for name, setup := range map[string]func(*Game){
		"block cursor":   func(g *Game) { g.cfg.Cursor.Style = config.CursorBlock },
		"animations off": func(g *Game) { g.cfg.Animation.Enabled = false },
	} {
		g := newTestGame(t)
		setup(g)
		g.startZap(2, 10, now)
		if g.zap.active {
			t.Errorf("%s: zap started", name)
		}
	}
	g := newTestGame(t)
	g.startZap(5, 5, now)
	if g.zap.active {
		t.Error("zap started with nowhere to go")
	}
	g.startZap(2, 10, now)
	if !g.zap.active {
		t.Fatal("zap didn't start")
	}
}

// The key handler calls touch after every key, Tab included: that must not
// cancel the zap Tab just started. Changing the line must.
func TestZapSurvivesTheTabKeyButNotEdits(t *testing.T) {
	g := isolatedGame(t)
	typeLine(g, "hel")
	now := time.Now()
	g.acceptSuggestion() // what Tab does...
	g.touch()            // ...followed by the handler's touch
	if stage, _ := g.zapStage(now); stage == zapDone {
		t.Fatal("the zap was cancelled by the Tab key itself")
	}

	g.editor.Insert(' ') // the user types on
	if stage, _ := g.zapStage(now); stage != zapDone {
		t.Fatal("typing should end the zap")
	}

	typeLine(g, "hel")
	g.acceptSuggestion()
	g.editor.Left(false) // or moves the cursor
	if stage, _ := g.zapStage(now); stage != zapDone {
		t.Fatal("moving the cursor should end the zap")
	}
}

func TestZapLandsWithSparksOnce(t *testing.T) {
	g := newTestGame(t)
	g.faces = &faceSet{scale: 1, cellW: 9, lineH: 20, textDY: 3, glyphH: 14, baselineY: 14}
	g.cols = 80
	start := time.Now()
	g.startZap(2, 12, start)
	landing := start.Add(zapMorphIn + g.zap.travel())

	g.updateZap(landing.Add(-time.Millisecond))
	if len(g.sparks.items) != 0 {
		t.Fatal("sparks before the bolt landed")
	}
	g.updateZap(landing)
	if len(g.sparks.items) != zapLandSparks {
		t.Fatalf("%d sparks on landing, want %d", len(g.sparks.items), zapLandSparks)
	}
	g.updateZap(landing.Add(zapMorphOut / 2))
	if len(g.sparks.items) != zapLandSparks {
		t.Fatal("sparks fired twice")
	}
	g.updateZap(landing.Add(zapMorphOut))
	if g.zap.active {
		t.Fatal("zap should end after morphing back")
	}
}

func TestAcceptSuggestionZapsToTheEnd(t *testing.T) {
	g := isolatedGame(t)
	typeLine(g, "hel")
	g.acceptSuggestion()
	prompt := len([]rune(g.promptText()))
	if !g.zap.active || g.zap.from != prompt+3 || g.zap.to != prompt+4 {
		t.Fatalf("zap %+v, want from %d to %d", g.zap, prompt+3, prompt+4)
	}
}
