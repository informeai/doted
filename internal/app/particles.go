package app

import (
	"image/color"
	"math"
	"math/rand/v2"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// Typing sparks: each keystroke that edits text fires a small burst of
// particles from the prompt bar. Positions and speeds are in logical pixels,
// relative to the bar's center, so the burst follows the bar if the input
// box grows; Draw scales them to the screen.
const (
	sparksPerKey  = 8
	maxSparks     = 240
	sparkMinSpeed = 70.0  // logical px per second
	sparkMaxSpeed = 200.0 //
	sparkMinLife  = 0.35  // seconds
	sparkMaxLife  = 0.75  //
	sparkMinSize  = 0.9   // radius, logical px
	sparkMaxSize  = 1.9   //
	sparkDrag     = 3.2   // speed lost per second, as a fraction (exponential)
	tickSeconds   = 1.0 / 60
)

type spark struct {
	x, y, vx, vy float64
	age, life    float64
	size         float64
	light        bool // a lighter spark among the accent ones
}

type sparks struct {
	items []spark
	rng   *rand.Rand
}

func newSparks(seed uint64) *sparks {
	return &sparks{rng: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))}
}

// burst fires n sparks from random points along a bar of the given height
// (logical px), in every direction.
func (s *sparks) burst(n int, barHeight float64) {
	for range n {
		angle := s.rng.Float64() * 2 * math.Pi
		speed := lerpF(sparkMinSpeed, sparkMaxSpeed, s.rng.Float64())
		s.items = append(s.items, spark{
			y:     (s.rng.Float64() - 0.5) * barHeight,
			vx:    math.Cos(angle) * speed,
			vy:    math.Sin(angle) * speed,
			life:  lerpF(sparkMinLife, sparkMaxLife, s.rng.Float64()),
			size:  lerpF(sparkMinSize, sparkMaxSize, s.rng.Float64()),
			light: s.rng.Float64() < 0.3,
		})
	}
	// Keep the newest when a fast typist outruns the limit.
	if over := len(s.items) - maxSparks; over > 0 {
		s.items = s.items[over:]
	}
}

// step advances every spark by dt seconds and drops the ones that faded.
func (s *sparks) step(dt float64) {
	drag := math.Exp(-sparkDrag * dt)
	live := s.items[:0]
	for _, p := range s.items {
		p.age += dt
		if p.age >= p.life {
			continue
		}
		p.x += p.vx * dt
		p.y += p.vy * dt
		p.vx *= drag
		p.vy *= drag
		live = append(live, p)
	}
	s.items = live
}

// draw paints the sparks around (ox, oy), the bar's center on screen.
func (s *sparks) draw(dst *ebiten.Image, ox, oy, scale float64, accent, light color.RGBA) {
	for _, p := range s.items {
		fade := 1 - p.age/p.life
		clr := accent
		if p.light {
			clr = light
		}
		r := p.size * scale * (0.4 + 0.6*fade) // shrink as it fades
		vector.FillCircle(dst, float32(ox+p.x*scale), float32(oy+p.y*scale), float32(r), scaleAlpha(clr, fade*fade), true)
	}
}

func lerpF(a, b, t float64) float64 { return a + (b-a)*t }
