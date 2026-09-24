package app

import (
	"image/color"
	"math"
	"testing"

	"github.com/informeai/doted/internal/config"
)

func TestSparksLifecycle(t *testing.T) {
	s := newSparks(1)
	s.burst(sparksPerKey, 20)
	if len(s.items) != sparksPerKey {
		t.Fatalf("burst made %d sparks, want %d", len(s.items), sparksPerKey)
	}
	for _, p := range s.items {
		speed := math.Hypot(p.vx, p.vy)
		if p.x != 0 || math.Abs(p.y) > 10 || speed < sparkMinSpeed-1e-9 || speed > sparkMaxSpeed+1e-9 {
			t.Fatalf("spark born off the bar or too fast/slow: %+v", p)
		}
	}

	// They move outwards and slow down.
	before := s.items[0]
	s.step(tickSeconds)
	after := s.items[0]
	if math.Hypot(after.x, after.y-before.y) == 0 || math.Hypot(after.vx, after.vy) >= math.Hypot(before.vx, before.vy) {
		t.Fatalf("spark didn't move or didn't slow down: %+v -> %+v", before, after)
	}

	// All gone once the longest life has passed.
	for range int(sparkMaxLife/tickSeconds) + 2 {
		s.step(tickSeconds)
	}
	if len(s.items) != 0 {
		t.Fatalf("%d sparks outlived their life", len(s.items))
	}
}

func TestSparksAreCapped(t *testing.T) {
	s := newSparks(2)
	for range 100 {
		s.burst(sparksPerKey, 20)
	}
	if len(s.items) != maxSparks {
		t.Fatalf("%d sparks, want the cap of %d", len(s.items), maxSparks)
	}
}

func TestSparksAreReproducible(t *testing.T) {
	a, b := newSparks(7), newSparks(7)
	a.burst(3, 20)
	b.burst(3, 20)
	for i := range a.items {
		if a.items[i] != b.items[i] {
			t.Fatal("same seed, different sparks")
		}
	}
}

func TestTypedFollowsSettings(t *testing.T) {
	g := newTestGame(t)
	g.typed()
	if len(g.sparks.items) != sparksPerKey {
		t.Fatalf("default settings: %d sparks after a key", len(g.sparks.items))
	}

	for name, change := range map[string]func(*config.Config){
		"particles off":  func(c *config.Config) { c.Animation.Particles = false },
		"animations off": func(c *config.Config) { c.Animation.Enabled = false },
		"symbol prompt":  func(c *config.Config) { c.Prompt.Style = config.PromptSymbol },
	} {
		g := newTestGame(t)
		change(&g.cfg)
		g.typed()
		if len(g.sparks.items) != 0 {
			t.Errorf("%s: %d sparks", name, len(g.sparks.items))
		}
	}
}

func TestPromptText(t *testing.T) {
	g := newTestGame(t)
	if got := g.promptText(); got != "  " {
		t.Fatalf("bar prompt takes %q, want two blank cells", got)
	}
	g.cfg.Prompt.Style, g.cfg.Prompt.Symbol = config.PromptSymbol, "$ "
	if got := g.promptText(); got != "$ " {
		t.Fatalf("symbol prompt = %q", got)
	}
}

func TestBurstLineFallsTheWayAsked(t *testing.T) {
	s := newSparks(1)
	red := color.RGBA{0xff, 0, 0, 0xff}
	s.burstLine(30, 10, 110, 50, math.Pi*0.2, math.Pi*0.8, red)
	if len(s.items) != 30 {
		t.Fatalf("%d sparks", len(s.items))
	}
	for _, p := range s.items {
		if p.x < 10 || p.x > 110 || p.y != 50 {
			t.Fatalf("spark at (%.0f, %.0f), off the segment", p.x, p.y)
		}
		if p.vy <= 0 {
			t.Fatalf("spark going up (vy %.1f); it should fall", p.vy)
		}
		if p.clr != red {
			t.Fatal("spark should keep its color")
		}
	}
}
