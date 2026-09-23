package app

import (
	"image/color"

	"github.com/informeai/doted/internal/config"
	"github.com/informeai/doted/internal/terminal"
)

// Theme holds the resolved colors the renderer draws with.
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
}

func newTheme(c config.Colors) Theme {
	return Theme{
		Background: c.Background.RGBA,
		Foreground: c.Foreground.RGBA,
		Muted:      c.Muted.RGBA,
		Accent:     c.Accent.RGBA,
		Error:      c.Error.RGBA,
		Border:     c.Border.RGBA,
		Cursor:     c.Cursor.RGBA,
		ANSI:       c.Palette(),
	}
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
