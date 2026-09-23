package terminal

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

func cellsFromString(s string) []Cell {
	cells := make([]Cell, 0, len(s))
	for _, r := range s {
		cells = append(cells, Cell{Rune: r})
	}
	return cells
}
