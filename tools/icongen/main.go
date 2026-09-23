// Command icongen draws doted's app icon and writes it in every format the
// packages need. The geometry below is the single source of the icon; the SVG
// is generated from it too.
//
// Run it through `go generate ./assets/icon`.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"

	"github.com/tc-hib/winres"
	"golang.org/x/image/vector"
)

// Geometry on a 1024×1024 grid, following the macOS icon template: an
// 824×824 rounded square with a 100px margin.
const (
	grid       = 1024
	bodyMin    = 100
	bodySize   = 824
	bodyRadius = 186
	borderW    = 3

	chevronW = 72 // stroke width of the ">"
	dotX     = 652
	dotY     = 512
	dotR     = 84
	glowR    = 190
)

var chevron = [3]point{{318, 372}, {458, 512}, {318, 652}}

var (
	navyTop     = rgb(0x18284a)
	navyBottom  = rgb(0x0b1220)
	borderColor = color.NRGBA{0xff, 0xff, 0xff, 0x14} // white at 8%
	chevronFill = rgb(0xd4def0)
	blue        = rgb(0x4d9fff)

	glowStops = []stop{{0, withAlpha(blue, 0.5)}, {0.55, withAlpha(blue, 0.12)}, {1, withAlpha(blue, 0)}}
	dotStops  = []stop{{0, rgb(0x8cc2ff)}, {0.6, blue}, {1, rgb(0x2f7de0)}}
	// The dot's highlight sits up and to the left of its center.
	dotLightX, dotLightY, dotLightR = 632.0, 485.0, 118.0
)

// Views: macOS draws the template's margin itself, so .icns keeps the full
// grid. Windows and Linux show icons edge to edge, so their images are
// cropped to the body with a small margin.
var (
	fullView  = view{0, 0, grid}
	tightView = view{84, 84, 856}
)

func main() {
	out := flag.String("out", ".", "directory for the icon files")
	syso := flag.String("syso", "", "path of the Windows resource (.syso) to write")
	flag.Parse()
	if err := run(*out, *syso); err != nil {
		log.Fatal(err)
	}
}

func run(out, syso string) error {
	if err := os.MkdirAll(filepath.Join(out, "png"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "doted.svg"), []byte(svg(tightView)), 0o644); err != nil {
		return err
	}

	// Linux (hicolor theme sizes) and doted's own window icon.
	for _, size := range []int{16, 24, 32, 48, 64, 128, 256, 512} {
		if err := writePNG(filepath.Join(out, "png", fmt.Sprintf("doted-%d.png", size)), render(size, tightView)); err != nil {
			return err
		}
	}

	var winImages []image.Image
	for _, size := range []int{16, 24, 32, 48, 64, 128, 256} {
		winImages = append(winImages, render(size, tightView))
	}
	icon, err := winres.NewIconFromImages(winImages)
	if err != nil {
		return err
	}
	var ico bytes.Buffer
	if err := icon.SaveICO(&ico); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "doted.ico"), ico.Bytes(), 0o644); err != nil {
		return err
	}

	icns, err := buildICNS()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "doted.icns"), icns, 0o644); err != nil {
		return err
	}

	if syso != "" {
		// Embeds the icon in doted.exe (Explorer, taskbar, shortcuts).
		var rs winres.ResourceSet
		if err := rs.SetIcon(winres.Name("APPICON"), icon); err != nil {
			return err
		}
		f, err := os.Create(syso)
		if err != nil {
			return err
		}
		if err := rs.WriteObject(f, winres.ArchAMD64); err != nil {
			f.Close()
			return err
		}
		return f.Close()
	}
	return nil
}

// buildICNS packs PNGs into an Apple icon file. Each entry is a 4-byte type,
// a 4-byte big-endian length (header included) and the PNG data.
func buildICNS() ([]byte, error) {
	entries := []struct {
		kind string
		size int
	}{
		{"icp4", 16}, {"icp5", 32}, {"ic11", 32}, // 16, 32 and 16@2x
		{"ic12", 64}, {"ic07", 128}, {"ic13", 256}, // 32@2x, 128, 128@2x
		{"ic08", 256}, {"ic14", 512}, {"ic09", 512}, {"ic10", 1024}, // 256, 256@2x, 512, 512@2x
	}
	var body bytes.Buffer
	for _, e := range entries {
		var img bytes.Buffer
		if err := encodePNG(&img, render(e.size, fullView)); err != nil {
			return nil, err
		}
		body.WriteString(e.kind)
		binary.Write(&body, binary.BigEndian, uint32(8+img.Len()))
		body.Write(img.Bytes())
	}
	var out bytes.Buffer
	out.WriteString("icns")
	binary.Write(&out, binary.BigEndian, uint32(8+body.Len()))
	out.Write(body.Bytes())
	return out.Bytes(), nil
}

// render draws the icon at size×size pixels showing the given part of the
// grid.
func render(size int, v view) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	s := float64(size) / v.size
	tx := func(p point) (float32, float32) {
		return float32((p.x - v.x) * s), float32((p.y - v.y) * s)
	}
	layer := func(shape func(*path), src image.Image) {
		z := vector.NewRasterizer(size, size)
		shape(&path{z: z, tx: tx})
		z.DrawOp = draw.Over
		z.Draw(dst, dst.Bounds(), src, image.Point{})
	}
	grad := func(fn func(x, y float64) color.NRGBA) image.Image {
		return gradient{fn: func(px, py int) color.NRGBA {
			// Sample at the pixel center, in grid coordinates.
			return fn(v.x+(float64(px)+0.5)/s, v.y+(float64(py)+0.5)/s)
		}, bounds: dst.Bounds()}
	}

	layer(func(p *path) { p.roundRect(bodyMin, bodyMin, bodySize, bodySize, bodyRadius, false) },
		grad(func(_, y float64) color.NRGBA {
			return lerp(navyTop, navyBottom, (y-bodyMin)/bodySize)
		}))
	layer(func(p *path) {
		half := borderW / 2.0
		p.roundRect(bodyMin, bodyMin, bodySize, bodySize, bodyRadius, false)
		p.roundRect(bodyMin+borderW, bodyMin+borderW, bodySize-2*borderW, bodySize-2*borderW, bodyRadius-half*2, true)
	}, image.NewUniform(borderColor))
	layer(func(p *path) {
		// Round caps on both segments also make the round join.
		p.capsule(chevron[0], chevron[1], chevronW/2)
		p.capsule(chevron[1], chevron[2], chevronW/2)
	}, image.NewUniform(chevronFill))
	layer(func(p *path) { p.circle(point{dotX, dotY}, glowR) },
		grad(func(x, y float64) color.NRGBA {
			return sample(glowStops, math.Hypot(x-dotX, y-dotY)/glowR)
		}))
	layer(func(p *path) { p.circle(point{dotX, dotY}, dotR) },
		grad(func(x, y float64) color.NRGBA {
			return sample(dotStops, math.Hypot(x-dotLightX, y-dotLightY)/dotLightR)
		}))
	return dst
}

// svg writes the same drawing as SVG, used as the scalable icon on Linux.
func svg(v view) string {
	const tmpl = `<svg xmlns="http://www.w3.org/2000/svg" width="%[1]g" height="%[1]g" viewBox="%[2]g %[3]g %[1]g %[1]g">
  <!-- Generated by tools/icongen; edit the geometry there and run go generate ./assets/icon. -->
  <defs>
    <linearGradient id="navy" x1="0" y1="%[4]d" x2="0" y2="%[5]d" gradientUnits="userSpaceOnUse">
      <stop offset="0" stop-color="%[6]s"/>
      <stop offset="1" stop-color="%[7]s"/>
    </linearGradient>
    <radialGradient id="glow" cx="%[8]d" cy="%[9]d" r="%[10]d" gradientUnits="userSpaceOnUse">%[11]s
    </radialGradient>
    <radialGradient id="dot" cx="%[12]g" cy="%[13]g" r="%[14]g" gradientUnits="userSpaceOnUse">%[15]s
    </radialGradient>
  </defs>
  <rect x="%[4]d" y="%[4]d" width="%[16]d" height="%[16]d" rx="%[17]d" fill="url(#navy)"/>
  <rect x="%[18]g" y="%[18]g" width="%[19]g" height="%[19]g" rx="%[20]g" fill="none" stroke="#ffffff" stroke-opacity="0.08" stroke-width="%[21]d"/>
  <polyline points="%[22]g,%[23]g %[24]g,%[25]g %[26]g,%[27]g" fill="none" stroke="%[28]s" stroke-width="%[29]d" stroke-linecap="round" stroke-linejoin="round"/>
  <circle cx="%[8]d" cy="%[9]d" r="%[10]d" fill="url(#glow)"/>
  <circle cx="%[8]d" cy="%[9]d" r="%[30]d" fill="url(#dot)"/>
</svg>
`
	half := borderW / 2.0
	return fmt.Sprintf(tmpl,
		v.size, v.x, v.y,
		bodyMin, bodyMin+bodySize, hex(navyTop), hex(navyBottom),
		dotX, dotY, glowR, svgStops(glowStops),
		dotLightX, dotLightY, dotLightR, svgStops(dotStops),
		bodySize, bodyRadius,
		bodyMin+half, bodySize-2*half, bodyRadius-half, borderW,
		chevron[0].x, chevron[0].y, chevron[1].x, chevron[1].y, chevron[2].x, chevron[2].y,
		hex(chevronFill), chevronW, dotR)
}

func svgStops(stops []stop) string {
	var b bytes.Buffer
	for _, s := range stops {
		fmt.Fprintf(&b, "\n      <stop offset=\"%g\" stop-color=\"%s\"", s.at, hex(s.c))
		if s.c.A != 0xff {
			fmt.Fprintf(&b, " stop-opacity=\"%.2f\"", float64(s.c.A)/0xff)
		}
		b.WriteString("/>")
	}
	return b.String()
}

type point struct{ x, y float64 }

type view struct{ x, y, size float64 }

// path builds shapes in grid coordinates on a rasterizer.
type path struct {
	z  *vector.Rasterizer
	tx func(point) (float32, float32)
}

func (p *path) moveTo(pt point) { p.z.MoveTo(p.tx(pt)) }
func (p *path) lineTo(pt point) { p.z.LineTo(p.tx(pt)) }
func (p *path) cubeTo(b, c, d point) {
	bx, by := p.tx(b)
	cx, cy := p.tx(c)
	dx, dy := p.tx(d)
	p.z.CubeTo(bx, by, cx, cy, dx, dy)
}

// arc appends a circular arc of at most 90° as a cubic Bézier.
func (p *path) arc(c point, r, from, to float64) {
	k := 4.0 / 3.0 * math.Tan((to-from)/4)
	a := point{c.x + r*math.Cos(from), c.y + r*math.Sin(from)}
	d := point{c.x + r*math.Cos(to), c.y + r*math.Sin(to)}
	b := point{a.x - k*r*math.Sin(from), a.y + k*r*math.Cos(from)}
	cc := point{d.x + k*r*math.Sin(to), d.y - k*r*math.Cos(to)}
	p.cubeTo(b, cc, d)
}

func (p *path) circle(c point, r float64) {
	p.moveTo(point{c.x + r, c.y})
	for i := range 4 {
		p.arc(c, r, float64(i)*math.Pi/2, float64(i+1)*math.Pi/2)
	}
	p.z.ClosePath()
}

// roundRect adds a rounded rectangle. reverse winds it the other way, which
// cuts it out of a shape wound normally in the same rasterizer.
func (p *path) roundRect(x, y, w, h, r float64, reverse bool) {
	corners := []struct {
		c     point
		start float64
	}{
		{point{x + w - r, y + r}, -math.Pi / 2},
		{point{x + w - r, y + h - r}, 0},
		{point{x + r, y + h - r}, math.Pi / 2},
		{point{x + r, y + r}, math.Pi},
	}
	if !reverse {
		p.moveTo(point{x + r, y})
		for _, c := range corners {
			p.lineTo(point{c.c.x + r*math.Cos(c.start), c.c.y + r*math.Sin(c.start)})
			p.arc(c.c, r, c.start, c.start+math.Pi/2)
		}
	} else {
		p.moveTo(point{x + r, y})
		for i := len(corners) - 1; i >= 0; i-- {
			c := corners[i]
			end := c.start + math.Pi/2
			p.lineTo(point{c.c.x + r*math.Cos(end), c.c.y + r*math.Sin(end)})
			p.arc(c.c, r, end, c.start)
		}
	}
	p.z.ClosePath()
}

// capsule adds a thick line from a to b with round ends.
func (p *path) capsule(a, b point, r float64) {
	angle := math.Atan2(b.y-a.y, b.x-a.x)
	n := angle + math.Pi/2
	p.moveTo(point{a.x + r*math.Cos(n), a.y + r*math.Sin(n)})
	p.arc(a, r, n, n+math.Pi/2)
	p.arc(a, r, n+math.Pi/2, n+math.Pi)
	p.lineTo(point{b.x + r*math.Cos(n+math.Pi), b.y + r*math.Sin(n+math.Pi)})
	p.arc(b, r, n+math.Pi, n+3*math.Pi/2)
	p.arc(b, r, n+3*math.Pi/2, n+2*math.Pi)
	p.z.ClosePath()
}

type stop struct {
	at float64
	c  color.NRGBA
}

func sample(stops []stop, t float64) color.NRGBA {
	if t <= stops[0].at {
		return stops[0].c
	}
	for i := 1; i < len(stops); i++ {
		if t <= stops[i].at {
			a, b := stops[i-1], stops[i]
			return lerp(a.c, b.c, (t-a.at)/(b.at-a.at))
		}
	}
	return stops[len(stops)-1].c
}

func lerp(a, b color.NRGBA, t float64) color.NRGBA {
	t = math.Max(0, math.Min(1, t))
	mix := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t)) }
	return color.NRGBA{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B), mix(a.A, b.A)}
}

// gradient is an image whose color is computed per pixel.
type gradient struct {
	fn     func(x, y int) color.NRGBA
	bounds image.Rectangle
}

func (g gradient) ColorModel() color.Model { return color.NRGBAModel }
func (g gradient) Bounds() image.Rectangle { return g.bounds }
func (g gradient) At(x, y int) color.Color { return g.fn(x, y) }

func rgb(v uint32) color.NRGBA {
	return color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 0xff}
}

func withAlpha(c color.NRGBA, a float64) color.NRGBA {
	c.A = uint8(math.Round(a * 0xff))
	return c
}

func hex(c color.NRGBA) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

func encodePNG(w *bytes.Buffer, img image.Image) error {
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	return enc.Encode(w, img)
}

func writePNG(name string, img image.Image) error {
	var b bytes.Buffer
	if err := encodePNG(&b, img); err != nil {
		return err
	}
	return os.WriteFile(name, b.Bytes(), 0o644)
}
