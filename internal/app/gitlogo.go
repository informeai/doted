package app

import (
	"image/color"
	"math"
	"regexp"
	"strconv"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// The Git logo, from git-scm.com's Git-Icon-1788C.svg (by Jason Long,
// CC BY 3.0): a rounded square turned 45° with a branch cut out of it,
// drawn from its SVG path.

const gitLogoPath = `M5,58c-2.76142,0 -5,-2.23858 -5,-5v-48c0,-2.76142 2.23858,-5 5,-5h33v12.54404c-2.06553,0.94801 -3.5,3.03446 -3.5,5.45596c0,0.73514 0.13221,1.43941 0.37415,2.09031l-15.28384,15.28384c-0.6509,-0.24194 -1.35517,-0.37415 -2.09031,-0.37415c-3.31371,0 -6,2.68629 -6,6c0,3.31371 2.68629,6 6,6c3.31371,0 6,-2.68629 6,-6c0,-0.73514 -0.13221,-1.43941 -0.37415,-2.09031l14.87415,-14.87415l0,11.50851c-2.06553,0.94801 -3.5,3.03446 -3.5,5.45596c0,3.31371 2.68629,6 6,6c3.31371,0 6,-2.68629 6,-6c0,-2.42149 -1.43447,-4.50795 -3.5,-5.45596l0,-12.08808c2.06553,-0.94801 3.5,-3.03446 3.5,-5.45596c0,-2.42149 -1.43447,-4.50795 -3.5,-5.45596l0,-12.54404h10c2.76142,0 5,2.23858 5,5v48c0,2.76142 -2.23858,5 -5,5z`

// logoOp is one drawing step of the logo, in absolute coordinates: a move,
// a line, a cubic curve (three points) or a close.
type logoOp struct {
	kind byte // 'M', 'L', 'C', 'Z'
	pts  [3][2]float64
}

// gitLogo is the logo's steps with the SVG's transform applied, and their
// bounds.
var gitLogo = sync.OnceValue(func() logoShape {
	ops := parseSVGPath(gitLogoPath)
	// transform="translate(10 10) rotate(-45 29 29)"
	sin, cos := math.Sincos(-math.Pi / 4)
	tf := func(p [2]float64) [2]float64 {
		x, y := p[0]-29, p[1]-29
		return [2]float64{29 + x*cos - y*sin + 10, 29 + x*sin + y*cos + 10}
	}
	shape := logoShape{minX: math.Inf(1), minY: math.Inf(1), maxX: math.Inf(-1), maxY: math.Inf(-1)}
	for _, op := range ops {
		n := map[byte]int{'M': 1, 'L': 1, 'C': 3}[op.kind]
		for i := range n {
			op.pts[i] = tf(op.pts[i])
			shape.minX, shape.maxX = math.Min(shape.minX, op.pts[i][0]), math.Max(shape.maxX, op.pts[i][0])
			shape.minY, shape.maxY = math.Min(shape.minY, op.pts[i][1]), math.Max(shape.maxY, op.pts[i][1])
		}
		shape.ops = append(shape.ops, op)
	}
	return shape
})

type logoShape struct {
	ops                    []logoOp
	minX, minY, maxX, maxY float64
}

var svgToken = regexp.MustCompile(`[MmLlHhVvCcZz]|-?(?:\d+\.?\d*|\.\d+)(?:e-?\d+)?`)

// parseSVGPath reads the path commands the logo uses (M L H V C Z, absolute
// or relative) into absolute steps.
func parseSVGPath(d string) []logoOp {
	tokens := svgToken.FindAllString(d, -1)
	var ops []logoOp
	var cmd byte
	var cur, start [2]float64
	i := 0
	num := func() float64 {
		v, _ := strconv.ParseFloat(tokens[i], 64)
		i++
		return v
	}
	isCmd := func(t string) bool { return t[0] >= 'A' }
	for i < len(tokens) {
		if isCmd(tokens[i]) {
			cmd = tokens[i][0]
			i++
			if cmd == 'Z' || cmd == 'z' {
				ops = append(ops, logoOp{kind: 'Z'})
				cur = start
				continue
			}
		}
		rel := cmd >= 'a'
		base := [2]float64{}
		if rel {
			base = cur
		}
		switch cmd | 0x20 { // lowercase
		case 'm':
			cur = [2]float64{base[0] + num(), base[1] + num()}
			start = cur
			ops = append(ops, logoOp{kind: 'M', pts: [3][2]float64{cur}})
			cmd = map[bool]byte{true: 'l', false: 'L'}[rel] // more pairs are lines
		case 'l':
			cur = [2]float64{base[0] + num(), base[1] + num()}
			ops = append(ops, logoOp{kind: 'L', pts: [3][2]float64{cur}})
		case 'h':
			cur[0] = base[0] + num() // base is 0 when absolute
			ops = append(ops, logoOp{kind: 'L', pts: [3][2]float64{cur}})
		case 'v':
			cur[1] = base[1] + num()
			ops = append(ops, logoOp{kind: 'L', pts: [3][2]float64{cur}})
		case 'c':
			var p [3][2]float64
			for k := range 3 {
				p[k] = [2]float64{base[0] + num(), base[1] + num()}
			}
			cur = p[2]
			ops = append(ops, logoOp{kind: 'C', pts: p})
		default:
			i++ // unsupported: skip
		}
	}
	return ops
}

// drawGitLogo draws the logo centered in the square of side size at (x, y),
// turned by angle radians around its center.
func drawGitLogo(dst *ebiten.Image, x, y, size, angle float64, clr color.RGBA) {
	shape := gitLogo()
	w, h := shape.maxX-shape.minX, shape.maxY-shape.minY
	s := size / math.Max(w, h)
	cx, cy := (shape.minX+shape.maxX)/2, (shape.minY+shape.maxY)/2
	sin, cos := math.Sincos(angle)
	pt := func(p [2]float64) (float32, float32) {
		dx, dy := p[0]-cx, p[1]-cy
		dx, dy = dx*cos-dy*sin, dx*sin+dy*cos
		return float32(x + size/2 + dx*s), float32(y + size/2 + dy*s)
	}

	var p vector.Path
	for _, op := range shape.ops {
		switch op.kind {
		case 'M':
			p.MoveTo(pt(op.pts[0]))
		case 'L':
			p.LineTo(pt(op.pts[0]))
		case 'C':
			x1, y1 := pt(op.pts[0])
			x2, y2 := pt(op.pts[1])
			x3, y3 := pt(op.pts[2])
			p.CubicTo(x1, y1, x2, y2, x3, y3)
		case 'Z':
			p.Close()
		}
	}
	op := &vector.DrawPathOptions{AntiAlias: true}
	op.ColorScale.ScaleWithColor(clr)
	vector.FillPath(dst, &p, &vector.FillOptions{}, op)
}
