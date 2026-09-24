package app

import (
	"math"
	"testing"
)

func TestCardPerimeter(t *testing.T) {
	p := cardPerimeter(10, 20, 100, 40, 8)
	// Two straight sides of each pair, less the corners, plus four quarter
	// circles: 2·(100-16) + 2·(40-16) + 2π·8.
	want := 2*84 + 2*24 + 2*math.Pi*8
	if got := p.length(); math.Abs(got-want) > 0.5 {
		t.Fatalf("length = %.2f, want about %.2f", got, want)
	}
	if x, y := p.at(0); x != 18 || y != 20 {
		t.Fatalf("start = (%.1f, %.1f), want the top edge after the corner", x, y)
	}
	// Wrapping around lands in the same place.
	x0, y0 := p.at(0.3)
	x1, y1 := p.at(1.3)
	if math.Abs(x0-x1) > 1e-9 || math.Abs(y0-y1) > 1e-9 {
		t.Fatal("at should wrap around")
	}
	// Halfway is on the bottom edge.
	if _, y := p.at(0.5); math.Abs(y-60) > 0.5 {
		t.Fatalf("halfway y = %.1f, want the bottom edge", y)
	}
}
