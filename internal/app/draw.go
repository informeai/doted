package app

import (
	"bytes"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"

	"github.com/informeai/doted/internal/terminal"
)

const (
	promptSymbol = "> "

	fadeInDuration = 180 * time.Millisecond
	blinkPeriod    = time.Second
	blinkHold      = 600 * time.Millisecond // cursor stays solid after a keystroke
	dimAlpha       = 0.6
)

// Font variants, indexed by fontVariant.
var monoSources [4]*text.GoTextFaceSource

func init() {
	for i, ttf := range [][]byte{gomono.TTF, gomonobold.TTF, gomonoitalic.TTF, gomonobolditalic.TTF} {
		src, err := text.NewGoTextFaceSource(bytes.NewReader(ttf))
		if err != nil {
			panic(err) // embedded fonts: can only fail if the binary is broken
		}
		monoSources[i] = src
	}
}

func fontVariant(a terminal.Attr) int {
	v := 0
	if a&terminal.Bold != 0 {
		v |= 1
	}
	if a&terminal.Italic != 0 {
		v |= 2
	}
	return v
}

// fonts caches the faces and the monospace cell metrics for one device scale.
type fonts struct {
	scale      float64
	faces      [4]*text.GoTextFace
	cellW      float64
	lineH      float64
	textDY     float64 // centers glyphs vertically inside a row
	underlineY float64 // offset from the row top
}

func newFonts(size, scale float64) *fonts {
	f := &fonts{scale: scale}
	for i, src := range monoSources {
		f.faces[i] = &text.GoTextFace{Source: src, Size: size * scale}
	}
	m := f.faces[0].Metrics()
	glyphH := m.HAscent + m.HDescent
	f.lineH = math.Ceil(glyphH * 1.3)
	f.textDY = (f.lineH - glyphH) / 2
	f.underlineY = f.textDY + m.HAscent + scale
	f.cellW = text.AdvanceAt("M", 1, f.faces[0])
	return f
}

func (g *Game) Draw(screen *ebiten.Image) {
	if g.fonts == nil || g.fonts.scale != g.scale {
		g.fonts = newFonts(g.theme.FontSize, g.scale)
	}
	f := g.fonts
	now := time.Now()
	screen.Fill(g.theme.Background)

	pad := g.theme.Padding * g.scale
	gap := math.Round(f.lineH * 0.4)
	w, h := float64(g.width), float64(g.height)
	g.cols = max(len(promptSymbol)+1, int((w-2*pad)/f.cellW))

	// Laid out bottom-up: status line, input box between two rules, output.
	statusY := h - pad - f.lineH
	lowerRule := statusY - gap
	inputRows, cursorRow, cursorCol := g.inputLayout()
	inputTop := lowerRule - gap - float64(len(inputRows))*f.lineH
	upperRule := inputTop - gap
	outputBottom := upperRule - gap
	g.outputRows = max(1, int((outputBottom-pad)/f.lineH))

	g.drawOutput(screen, pad, outputBottom, now)

	rule := func(y float64) {
		vector.StrokeLine(screen, float32(pad), float32(y), float32(w-pad), float32(y), float32(g.scale), g.theme.Border, false)
	}
	rule(upperRule)
	rule(lowerRule)

	promptW := f.cellW * float64(len(promptSymbol))
	if g.runner.Running() {
		// Keys go to the program; its cursor is drawn in the output instead.
		g.drawText(screen, promptSymbol, pad, inputTop, g.theme.Muted, 1)
		g.drawText(screen, "input is sent to the running command", pad+promptW, inputTop, g.theme.Muted, dimAlpha)
	} else {
		for i, row := range inputRows {
			y := inputTop + float64(i)*f.lineH
			x := pad
			if i == 0 {
				g.drawText(screen, promptSymbol, x, y, g.theme.Accent, 1)
				row, x = row[len(promptSymbol):], x+promptW
			}
			g.drawText(screen, string(row), x, y, g.theme.Foreground, 1)
		}
		g.drawCursor(screen, pad+float64(cursorCol)*f.cellW, inputTop+float64(cursorRow)*f.lineH, runeAt(inputRows[cursorRow], cursorCol), now)
	}

	g.drawStatus(screen, pad, statusY, w-pad, now)
}

// inputLayout wraps the prompt plus the typed text and locates the cursor.
func (g *Game) inputLayout() (rows [][]rune, cursorRow, cursorCol int) {
	rows = terminal.Wrap([]rune(promptSymbol+g.editor.Text()), g.cols)
	idx := len(promptSymbol) + g.editor.Cursor()
	cursorRow, cursorCol = idx/g.cols, idx%g.cols
	if cursorRow == len(rows) {
		rows = append(rows, nil) // cursor sits just past a full row
	}
	return rows, cursorRow, cursorCol
}

// drawOutput paints the scrollback from the newest line upwards, so the most
// recent output always hugs the input box. While a command runs, its cursor
// is drawn on the newest (live) line.
func (g *Game) drawOutput(dst *ebiten.Image, top, bottom float64, now time.Time) {
	f := g.fonts
	pad := g.theme.Padding * g.scale
	y := bottom
	skip := g.scroll
	last := g.scrollback.Len() - 1
	for i := last; i >= 0 && y-f.lineH >= top; i-- {
		line := g.scrollback.At(i)
		rows := terminal.Wrap(line.Cells, g.cols)
		curRow, curCol := -1, 0
		if i == last && g.runner.Running() {
			col := g.parser.Col()
			curRow, curCol = col/g.cols, col%g.cols
			for len(rows) <= curRow {
				rows = append(rows, nil)
			}
		}
		alpha, dy := entrance(now.Sub(line.At), f.lineH)
		for j := len(rows) - 1; j >= 0 && y-f.lineH >= top; j-- {
			if skip > 0 {
				skip--
				continue
			}
			y -= f.lineH
			g.drawCells(dst, rows[j], pad, y+dy, line.Kind, alpha)
			if j == curRow {
				var under rune
				if curCol < len(rows[j]) {
					under = rows[j][curCol].Rune
				}
				g.drawCursor(dst, pad+float64(curCol)*f.cellW, y, under, now)
			}
		}
	}
}

// drawCells draws one row, one span per run of equally styled cells.
func (g *Game) drawCells(dst *ebiten.Image, cells []terminal.Cell, x, y float64, kind terminal.Kind, alpha float64) {
	f := g.fonts
	var runes []rune
	for start := 0; start < len(cells); {
		st := cells[start].Style
		end := start + 1
		for end < len(cells) && cells[end].Style == st {
			end++
		}
		runes = runes[:0]
		for _, c := range cells[start:end] {
			runes = append(runes, c.Rune)
		}
		sx := x + float64(start)*f.cellW
		width := float64(end-start) * f.cellW

		fg, ok := g.theme.resolve(st.FG)
		if !ok {
			fg = g.theme.colorFor(kind)
		}
		bg, hasBG := g.theme.resolve(st.BG)
		if st.Attrs&terminal.Inverse != 0 {
			if !hasBG {
				bg = g.theme.Background
			}
			fg, bg, hasBG = bg, fg, true
		}
		if hasBG {
			vector.FillRect(dst, float32(sx), float32(y), float32(width), float32(f.lineH), scaleAlpha(bg, alpha), false)
		}
		a := alpha
		if st.Attrs&terminal.Dim != 0 {
			a *= dimAlpha
		}
		g.drawTextFace(dst, f.faces[fontVariant(st.Attrs)], string(runes), sx, y, fg, a)
		if st.Attrs&terminal.Underline != 0 {
			uy := float32(y + f.underlineY)
			vector.StrokeLine(dst, float32(sx), uy, float32(sx+width), uy, float32(g.scale), scaleAlpha(fg, a), false)
		}
		start = end
	}
}

// entrance animates a freshly appended line: it fades in while sliding up.
func entrance(age time.Duration, lineH float64) (alpha, dy float64) {
	t := min(1, float64(age)/float64(fadeInDuration))
	ease := 1 - math.Pow(1-t, 3) // ease-out cubic
	return ease, (1 - ease) * lineH * 0.35
}

// drawCursor draws a blinking block cursor, redrawing the rune under it (if
// any) in the background color.
func (g *Game) drawCursor(dst *ebiten.Image, x, y float64, under rune, now time.Time) {
	solid := now.Sub(g.lastInput) < blinkHold
	if !solid && now.UnixMilli()%blinkPeriod.Milliseconds() >= blinkPeriod.Milliseconds()/2 {
		return
	}
	f := g.fonts
	vector.FillRect(dst, float32(x), float32(y), float32(f.cellW), float32(f.lineH), g.theme.Cursor, false)
	if under != 0 {
		g.drawText(dst, string(under), x, y, g.theme.Background, 1)
	}
}

func (g *Game) drawStatus(dst *ebiten.Image, left, y, right float64, now time.Time) {
	f := g.fonts
	g.drawText(dst, shortPath(g.runner.Dir()), left, y, g.theme.Muted, 1)

	var hint string
	switch {
	case g.scroll > 0:
		hint = "scrolled up · pgdn to return"
	case g.parser.AltScreen:
		hint = "full-screen programs aren't supported yet · ctrl+c"
	case g.runner.Running():
		hint = "running · ctrl+c to interrupt"
	default:
		return
	}
	x := right - f.cellW*float64(utf8.RuneCountInString(hint))
	g.drawText(dst, hint, x, y, g.theme.Muted, 1)
	if g.runner.Running() {
		g.drawSpinner(dst, x-f.cellW*4, y+f.lineH/2, now)
	}
}

// drawSpinner draws three dots pulsing in sequence.
func (g *Game) drawSpinner(dst *ebiten.Image, x, cy float64, now time.Time) {
	f := g.fonts
	r := f.cellW * 0.22
	t := float64(now.UnixMilli()) / 1000
	for i := range 3 {
		phase := math.Sin(t*2*math.Pi*1.2 - float64(i)*0.9)
		a := 0.3 + 0.7*(phase+1)/2
		c := scaleAlpha(g.theme.Accent, a)
		vector.FillCircle(dst, float32(x+float64(i)*r*3.2), float32(cy), float32(r*(0.8+0.2*a)), c, true)
	}
}

func (g *Game) drawText(dst *ebiten.Image, s string, x, y float64, clr color.RGBA, alpha float64) {
	g.drawTextFace(dst, g.fonts.faces[0], s, x, y, clr, alpha)
}

func (g *Game) drawTextFace(dst *ebiten.Image, face *text.GoTextFace, s string, x, y float64, clr color.RGBA, alpha float64) {
	if s == "" || alpha <= 0 || strings.TrimLeft(s, " ") == "" {
		return
	}
	op := &text.DrawOptions{}
	op.GeoM.Translate(x, y+g.fonts.textDY)
	op.ColorScale.ScaleWithColor(clr)
	op.ColorScale.ScaleAlpha(float32(alpha))
	text.Draw(dst, s, face, op)
}

func runeAt(row []rune, col int) rune {
	if col < len(row) {
		return row[col]
	}
	return 0
}

func scaleAlpha(c color.RGBA, a float64) color.RGBA {
	return color.RGBA{uint8(float64(c.R) * a), uint8(float64(c.G) * a), uint8(float64(c.B) * a), uint8(float64(c.A) * a)}
}

// shortPath abbreviates the home directory as ~.
func shortPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rest
	}
	return p
}
