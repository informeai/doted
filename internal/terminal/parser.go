package terminal

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type parserState int

const (
	ground parserState = iota
	escape
	csiEntry
	oscString
	oscEscape
	charsetSelect
)

// Parser interprets the bytes a program writes to its PTY and appends them to
// the scrollback as styled lines.
//
// It covers what line-oriented programs use: text, CR/LF/BS/TAB, SGR colors
// and attributes, and cursor moves and erases within the current line.
// Full-screen programs (on the alternate screen) are drawn from a grid
// emulator instead, so the parser skips what they write there and keeps the
// lines printed around them.
type Parser struct {
	sb *Scrollback

	state  parserState
	params []byte
	utf8   []byte // incomplete UTF-8 sequence carried to the next Write

	style Style
	col   int
	open  bool // the newest scrollback line is the one being written

	// AltScreen is set while the program asks for the alternate screen.
	AltScreen bool
}

func NewParser(sb *Scrollback) *Parser {
	return &Parser{sb: sb}
}

// Begin starts a command: an empty live line is opened for its output.
func (p *Parser) Begin() {
	p.reset()
	p.newLine()
}

// End finishes a command, dropping the live line if nothing was written to it.
func (p *Parser) End() {
	if p.open && len(p.sb.last().Cells) == 0 {
		p.sb.pop()
	}
	p.reset()
}

func (p *Parser) reset() {
	*p = Parser{sb: p.sb}
}

// Col is the cursor column on the live line.
func (p *Parser) Col() int { return p.col }

// Write consumes a chunk of program output. Escape and UTF-8 sequences may
// be split across chunks.
func (p *Parser) Write(data []byte, at time.Time) {
	if len(p.utf8) > 0 {
		data = append(p.utf8, data...)
		p.utf8 = nil
	}
	for i := 0; i < len(data); {
		b := data[i]
		switch p.state {
		case ground:
			switch {
			case b == 0x1b:
				p.state = escape
			case b < 0x20 || b == 0x7f:
				p.control(b, at)
			default:
				if !utf8.FullRune(data[i:]) {
					p.utf8 = append([]byte(nil), data[i:]...)
					return
				}
				r, size := utf8.DecodeRune(data[i:])
				p.put(r, at)
				i += size
				continue
			}
		case escape:
			switch b {
			case '[':
				p.state = csiEntry
				p.params = p.params[:0]
			case ']':
				p.state = oscString
			case '(', ')', '*', '+':
				p.state = charsetSelect
			default:
				p.state = ground // ESC 7, ESC =, ... have no visible effect here
			}
		case csiEntry:
			switch {
			case b >= 0x30 && b <= 0x3f:
				p.params = append(p.params, b)
			case b >= 0x40 && b <= 0x7e:
				p.csi(b, at)
				p.state = ground
			case b == 0x1b:
				p.state = escape // malformed sequence: start over
			}
			// Intermediate bytes (0x20-0x2f) are ignored.
		case oscString:
			switch b {
			case 0x07:
				p.state = ground
			case 0x1b:
				p.state = oscEscape
			}
		case oscEscape, charsetSelect:
			p.state = ground
		}
		i++
	}
}

func (p *Parser) control(b byte, at time.Time) {
	if p.AltScreen {
		return // a full-screen program's screen isn't line output
	}
	switch b {
	case '\n':
		p.live(at)
		p.newLine()
		p.col = 0
	case '\r':
		p.col = 0
	case '\b':
		p.col = max(0, p.col-1)
	case '\t':
		p.col = (p.col/8 + 1) * 8
	}
}

// live returns the line being written, opening one if needed.
func (p *Parser) live(at time.Time) *Line {
	if !p.open {
		p.newLine()
	}
	l := p.sb.last()
	if len(l.Cells) == 0 {
		l.At = at // animate from when content shows up, not from when the line opened
	}
	return l
}

func (p *Parser) newLine() {
	p.sb.push(Line{Kind: Output})
	p.open = true
}

func (p *Parser) put(r rune, at time.Time) {
	if p.AltScreen {
		return // a full-screen program's screen isn't line output
	}
	l := p.live(at)
	for len(l.Cells) < p.col {
		l.Cells = append(l.Cells, Cell{Rune: ' '})
	}
	c := Cell{Rune: r, Style: p.style}
	if p.col < len(l.Cells) {
		l.Cells[p.col] = c
	} else {
		l.Cells = append(l.Cells, c)
	}
	p.col++
}

func (p *Parser) csi(final byte, at time.Time) {
	raw := string(p.params)
	private := raw != "" && strings.IndexByte("<=>?", raw[0]) >= 0
	if private {
		raw = raw[1:]
	}
	args := parseParams(raw)
	arg := func(i, def int) int {
		if i < len(args) && args[i] > 0 {
			return args[i]
		}
		return def
	}

	if private {
		if final == 'h' || final == 'l' {
			for _, a := range args {
				if a == 47 || a == 1047 || a == 1049 {
					p.AltScreen = final == 'h'
				}
			}
		}
		return
	}
	if p.AltScreen {
		return // colors and moves on the alternate screen don't touch the lines
	}

	switch final {
	case 'm':
		p.sgr(args)
	case 'C': // cursor forward
		p.col += arg(0, 1)
	case 'D': // cursor back
		p.col = max(0, p.col-arg(0, 1))
	case 'G': // cursor to column
		p.col = arg(0, 1) - 1
	case 'H', 'f': // cursor position: only the column is honored
		p.col = arg(1, 1) - 1
	case 'K': // erase in line
		l := p.live(at)
		switch arg(0, 0) {
		case 0:
			l.Cells = l.Cells[:min(p.col, len(l.Cells))]
		case 1:
			for i := 0; i <= p.col && i < len(l.Cells); i++ {
				l.Cells[i] = Cell{Rune: ' '}
			}
		case 2:
			l.Cells = l.Cells[:0]
		}
	case 'J': // erase in display: only a full clear (the `clear` command)
		if n := arg(0, 0); n == 2 || n == 3 {
			p.sb.Clear()
			p.newLine()
		}
	}
}

func (p *Parser) sgr(args []int) {
	if len(args) == 0 {
		p.style = Style{}
		return
	}
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == 0:
			p.style = Style{}
		case a == 1:
			p.style.Attrs |= Bold
		case a == 2:
			p.style.Attrs |= Dim
		case a == 3:
			p.style.Attrs |= Italic
		case a == 4:
			p.style.Attrs |= Underline
		case a == 7:
			p.style.Attrs |= Inverse
		case a == 21 || a == 22:
			p.style.Attrs &^= Bold | Dim
		case a == 23:
			p.style.Attrs &^= Italic
		case a == 24:
			p.style.Attrs &^= Underline
		case a == 27:
			p.style.Attrs &^= Inverse
		case a >= 30 && a <= 37:
			p.style.FG = indexed(a - 30)
		case a == 38:
			p.style.FG, i = extendedColor(args, i)
		case a == 39:
			p.style.FG = Color{}
		case a >= 40 && a <= 47:
			p.style.BG = indexed(a - 40)
		case a == 48:
			p.style.BG, i = extendedColor(args, i)
		case a == 49:
			p.style.BG = Color{}
		case a >= 90 && a <= 97:
			p.style.FG = indexed(a - 90 + 8)
		case a >= 100 && a <= 107:
			p.style.BG = indexed(a - 100 + 8)
		}
	}
}

// extendedColor parses `38;5;n` and `38;2;r;g;b` starting at args[i] and
// returns the index of the last argument it consumed.
func extendedColor(args []int, i int) (Color, int) {
	at := func(j int) uint8 {
		if j < len(args) {
			return uint8(args[j])
		}
		return 0
	}
	if i+1 >= len(args) {
		return Color{}, i
	}
	switch args[i+1] {
	case 5:
		return indexed(int(at(i + 2))), i + 2
	case 2:
		return Color{Kind: RGBColor, R: at(i + 2), G: at(i + 3), B: at(i + 4)}, i + 4
	}
	return Color{}, i + 1
}

func indexed(n int) Color { return Color{Kind: IndexedColor, Index: uint8(n)} }

// parseParams splits "1;2;;4" into [1 2 0 4]. Colon sub-parameters are
// treated as plain separators.
func parseParams(s string) []int {
	if s == "" {
		return nil
	}
	var out []int
	for part := range strings.SplitSeq(strings.ReplaceAll(s, ":", ";"), ";") {
		n, _ := strconv.Atoi(part)
		out = append(out, n)
	}
	return out
}
