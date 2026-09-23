package app

import (
	"image/color"

	"github.com/informeai/doted/internal/terminal"
)

type Theme struct {
	Background color.RGBA
	Foreground color.RGBA
	Muted      color.RGBA
	Accent     color.RGBA
	Error      color.RGBA
	Border     color.RGBA
	Cursor     color.RGBA

	// ANSI holds the 16 base colors programs pick with SGR 30-37 / 90-97.
	ANSI [16]color.RGBA

	FontSize float64 // in logical pixels, before the device scale
	Padding  float64
}

var DefaultTheme = Theme{
	Background: color.RGBA{0x14, 0x15, 0x19, 0xff},
	Foreground: color.RGBA{0xd8, 0xdb, 0xe2, 0xff},
	Muted:      color.RGBA{0x6c, 0x71, 0x7e, 0xff},
	Accent:     color.RGBA{0xd9, 0x77, 0x57, 0xff},
	Error:      color.RGBA{0xe5, 0x6b, 0x6f, 0xff},
	Border:     color.RGBA{0x3a, 0x3d, 0x46, 0xff},
	Cursor:     color.RGBA{0xd8, 0xdb, 0xe2, 0xff},

	ANSI: [16]color.RGBA{
		{0x1e, 0x20, 0x26, 0xff}, // black
		{0xe5, 0x6b, 0x6f, 0xff}, // red
		{0x98, 0xc3, 0x79, 0xff}, // green
		{0xe5, 0xc0, 0x7b, 0xff}, // yellow
		{0x61, 0xaf, 0xef, 0xff}, // blue
		{0xc6, 0x78, 0xdd, 0xff}, // magenta
		{0x56, 0xb6, 0xc2, 0xff}, // cyan
		{0xd8, 0xdb, 0xe2, 0xff}, // white
		{0x5c, 0x63, 0x70, 0xff}, // bright black
		{0xff, 0x7b, 0x7f, 0xff}, // bright red
		{0xb5, 0xe0, 0x8d, 0xff}, // bright green
		{0xf5, 0xd4, 0x8f, 0xff}, // bright yellow
		{0x7d, 0xc4, 0xff, 0xff}, // bright blue
		{0xda, 0x8e, 0xf0, 0xff}, // bright magenta
		{0x6e, 0xd0, 0xdc, 0xff}, // bright cyan
		{0xff, 0xff, 0xff, 0xff}, // bright white
	},

	FontSize: 15,
	Padding:  14,
}

func (t Theme) colorFor(k terminal.Kind) color.RGBA {
	switch k {
	case terminal.Error:
		return t.Error
	case terminal.Command:
		return t.Accent
	case terminal.System:
		return t.Muted
	default:
		return t.Foreground
	}
}

// resolve maps a cell color to RGBA; ok is false for the default color.
func (t Theme) resolve(c terminal.Color) (rgba color.RGBA, ok bool) {
	switch c.Kind {
	case terminal.IndexedColor:
		return t.indexed(c.Index), true
	case terminal.RGBColor:
		return color.RGBA{c.R, c.G, c.B, 0xff}, true
	}
	return color.RGBA{}, false
}

// indexed implements the xterm 256-color palette: the 16 theme colors, a
// 6×6×6 color cube, then a 24-step gray ramp.
func (t Theme) indexed(i uint8) color.RGBA {
	switch {
	case i < 16:
		return t.ANSI[i]
	case i < 232:
		i -= 16
		level := func(v uint8) uint8 {
			if v == 0 {
				return 0
			}
			return 55 + v*40
		}
		return color.RGBA{level(i / 36), level(i / 6 % 6), level(i % 6), 0xff}
	default:
		g := 8 + (i-232)*10
		return color.RGBA{g, g, g, 0xff}
	}
}
