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
// and attributes, and cursor moves and erases. The cursor can move up and
// down over the lines the command has written so far (never into what came
// before it), so multi-line progress displays (docker pull, npm, cargo,
// pip) redraw their lines in place instead of repeating them.
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
	row   int  // the seq of the line the cursor is on
	first int  // the seq of the command's first line; the cursor stays at or below it
	open  bool // the command has a line being written

	savedRow, savedCol int // the cursor saved by ESC 7 or CSI s
	saved              bool

	// Rows is the height of the command's terminal, to place absolute cursor
	// moves (CSI H) on its lines: the last Rows lines are its screen. Zero
	// honors only their column.
	Rows int

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
	p.first = p.row
}

// End finishes a command, dropping the newest line if nothing was written
// to it.
func (p *Parser) End() {
	if p.open && p.sb.Len() > 0 && len(p.sb.last().Cells) == 0 {
		p.sb.pop()
	}
	p.reset()
}

func (p *Parser) reset() {
	*p = Parser{sb: p.sb, Rows: p.Rows}
}

// Col is the cursor's column.
func (p *Parser) Col() int { return p.col }

// Row is the seq of the line the cursor is on.
func (p *Parser) Row() int { return p.row }

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
			case '7':
				p.saveCursor()
				p.state = ground
			case '8':
				p.restoreCursor()
				p.state = ground
			default:
				p.state = ground // ESC =, ESC >... have no visible effect here
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
		p.lineFeed()
		p.col = 0
	case '\r':
		p.col = 0
	case '\b':
		p.col = max(0, p.col-1)
	case '\t':
		p.col = (p.col/8 + 1) * 8
	}
}

// live returns the line the cursor is on, opening one if needed.
func (p *Parser) live(at time.Time) *Line {
	if !p.open {
		p.newLine()
	}
	l := p.sb.lineAt(p.row)
	if l == nil { // trimmed away from the scrollback's top
		p.row = p.sb.lastSeq()
		p.first = max(p.first, p.sb.Seq(0))
		l = p.sb.last()
	}
	if l.At.IsZero() {
		// Animate from when content shows up, not from when the line
		// opened, and only once: a progress bar redrawn in place stays put.
		l.At = at
	}
	return l
}

func (p *Parser) newLine() {
	p.sb.push(Line{Kind: Output})
	p.open = true
	p.row = p.sb.lastSeq()
}

// lineFeed moves the cursor down a line, opening a new one below the last.
func (p *Parser) lineFeed() {
	if p.row >= p.sb.lastSeq() {
		p.newLine()
		return
	}
	p.row++
}

// moveRow puts the cursor on the line numbered seq, within the command's.
func (p *Parser) moveRow(seq int) {
	p.row = min(max(seq, p.first, p.sb.Seq(0)), p.sb.lastSeq())
}

// screenRow is the seq of row r (1-based) of the command's screen: its last
// Rows lines, opening lines below when r is past the last one.
func (p *Parser) screenRow(r int) int {
	top := max(p.first, p.sb.lastSeq()-(p.Rows-1))
	seq := top + r - 1
	for p.sb.lastSeq() < seq {
		p.sb.push(Line{Kind: Output})
	}
	return seq
}

func (p *Parser) saveCursor() { p.savedRow, p.savedCol, p.saved = p.row, p.col, true }

func (p *Parser) restoreCursor() {
	if p.saved {
		p.moveRow(p.savedRow)
		p.col = p.savedCol
	}
}

func (p *Parser) put(r rune, at time.Time) {
	if p.AltScreen {
		return // a full-screen program's screen isn't line output
	}
	w := RuneWidth(r)
	if w == 0 {
		return // combining marks and joiners have no cell of their own
	}
	l := p.live(at)
	for len(l.Cells) < p.col+w {
		l.Cells = append(l.Cells, Cell{Rune: ' '})
	}
	// Writing over half of a wide character blanks its other half.
	if p.col > 0 && l.Cells[p.col].Rune == WideTail {
		l.Cells[p.col-1] = Cell{Rune: ' ', Style: l.Cells[p.col-1].Style}
	}
	if end := p.col + w; end < len(l.Cells) && l.Cells[end].Rune == WideTail {
		l.Cells[end] = Cell{Rune: ' ', Style: l.Cells[end].Style}
	}
	l.Cells[p.col] = Cell{Rune: r, Style: p.style}
	if w == 2 {
		l.Cells[p.col+1] = Cell{Rune: WideTail, Style: p.style}
	}
	p.col += w
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
	case 'A': // cursor up
		p.moveRow(p.row - arg(0, 1))
	case 'B': // cursor down
		p.moveRow(p.row + arg(0, 1))
	case 'E': // cursor to the start of a line below
		p.moveRow(p.row + arg(0, 1))
		p.col = 0
	case 'F': // cursor to the start of a line above
		p.moveRow(p.row - arg(0, 1))
		p.col = 0
	case 'd': // cursor to a row
		if p.Rows > 0 {
			p.moveRow(p.screenRow(arg(0, 1)))
		}
	case 's':
		p.saveCursor()
	case 'u':
		p.restoreCursor()
	case 'C': // cursor forward
		p.col += arg(0, 1)
	case 'D': // cursor back
		p.col = max(0, p.col-arg(0, 1))
	case 'G': // cursor to column
		p.col = arg(0, 1) - 1
	case 'H', 'f': // cursor position
		if p.Rows > 0 {
			p.moveRow(p.screenRow(arg(0, 1)))
		}
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
	case 'J': // erase in display
		switch arg(0, 0) {
		case 0: // from the cursor down: the rest of its line, and the lines below
			l := p.live(at)
			l.Cells = l.Cells[:min(p.col, len(l.Cells))]
			p.sb.cutAfter(p.row)
		case 1: // from the command's first line to the cursor
			for seq := p.first; seq < p.row; seq++ {
				if l := p.sb.lineAt(seq); l != nil {
					l.Cells = l.Cells[:0]
				}
			}
			l := p.live(at)
			for i := 0; i <= p.col && i < len(l.Cells); i++ {
				l.Cells[i] = Cell{Rune: ' '}
			}
		case 2, 3: // everything (the `clear` command)
			p.sb.Clear()
			p.newLine()
			p.first = p.row
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
