package terminal

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

type ColorKind uint8

const (
	DefaultColor ColorKind = iota // the theme's foreground/background
	IndexedColor                  // xterm 256-color palette (0-15 are the ANSI colors)
	RGBColor                      // 24-bit truecolor
)

type Color struct {
	Kind    ColorKind
	Index   uint8
	R, G, B uint8
}

type Attr uint8

const (
	Bold Attr = 1 << iota
	Dim
	Italic
	Underline
	Inverse
)

// Style is comparable, so runs of equally styled cells can be drawn as one span.
type Style struct {
	FG, BG Color
	Attrs  Attr
}

type Cell struct {
	Rune  rune
	Style Style
}

// WideTail is the Rune of the cell after a wide character (CJK, most
// emoji), which takes two columns: it holds no character of its own. Keeping
// it as a cell keeps a line's cells lined up with its columns.
const WideTail rune = -1

// widthCond measures characters as terminals usually do: ambiguous ones
// (like ─ or ●) take one column, whatever the locale.
var widthCond = &runewidth.Condition{EastAsianWidth: false}

// RuneWidth is how many columns r takes: 2 for wide characters, 0 for
// combining marks and other zero-width ones, 1 otherwise.
func RuneWidth(r rune) int { return widthCond.RuneWidth(r) }

// appendRune adds r to cells with its width: a wide character is followed
// by a WideTail cell; a zero-width one isn't added.
func appendRune(cells []Cell, r rune, st Style) []Cell {
	switch RuneWidth(r) {
	case 0:
		return cells
	case 2:
		return append(cells, Cell{Rune: r, Style: st}, Cell{Rune: WideTail, Style: st})
	}
	return append(cells, Cell{Rune: r, Style: st})
}

func cellsFromString(s string) []Cell {
	cells := make([]Cell, 0, len(s))
	for _, r := range s {
		cells = appendRune(cells, r, Style{})
	}
	return cells
}

// StringOf turns cell runes back into text, leaving out WideTail cells.
func StringOf(rs []rune) string {
	var b strings.Builder
	for _, r := range rs {
		if r != WideTail {
			b.WriteRune(r)
		}
	}
	return b.String()
}
