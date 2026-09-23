package app

import (
	"fmt"
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

	"github.com/informeai/doted/internal/config"
	"github.com/informeai/doted/internal/fonts"
	"github.com/informeai/doted/internal/terminal"
)

const (
	blinkPeriod = time.Second
	blinkHold   = 600 * time.Millisecond // cursor stays solid after a keystroke
	dimAlpha    = 0.6
)

func fontVariant(a terminal.Attr) fonts.Variant {
	v := fonts.Regular
	if a&terminal.Bold != 0 {
		v |= fonts.Bold
	}
	if a&terminal.Italic != 0 {
		v |= fonts.Italic
	}
	return v
}

// faceSet caches the sized faces and the monospace cell metrics for one
// font configuration and device scale.
type faceSet struct {
	scale      float64
	faces      [4]*text.GoTextFace
	cellW      float64
	lineH      float64
	textDY     float64 // centers glyphs vertically inside a row
	underlineY float64 // offset from the row top
}

func newFaceSet(fam fonts.Family, cfg config.Font, scale float64) *faceSet {
	f := &faceSet{scale: scale}
	for i, face := range fam.Faces {
		f.faces[i] = face.NewFace(cfg.Size * scale)
	}
	m := f.faces[0].Metrics()
	glyphH := m.HAscent + m.HDescent
	f.lineH = math.Ceil(glyphH * cfg.LineHeight)
	f.textDY = (f.lineH - glyphH) / 2
	f.underlineY = f.textDY + m.HAscent + scale
	f.cellW = text.AdvanceAt("M", 1, f.faces[0])
	return f
}

func (g *Game) Draw(screen *ebiten.Image) {
	if g.faces == nil || g.faces.scale != g.scale {
		g.faces = newFaceSet(g.family, g.cfg.Font, g.scale)
	}
	f := g.faces
	prompt := g.cfg.Prompt.Symbol
	promptLen := utf8.RuneCountInString(prompt)
	now := time.Now()
	screen.Fill(g.theme.Background)

	pad := g.cfg.Window.Padding * g.scale
	gap := math.Round(f.lineH * 0.4)
	w, h := float64(g.width), float64(g.height)
	g.cols = max(promptLen+1, int((w-2*pad)/f.cellW))

	// Laid out bottom-up: status line, input box between two rules, output.
	statusY := h - pad - f.lineH
	lowerRule := statusY - gap
	inputRows, cursorRow, cursorCol := g.inputLayout()
	inputTop := lowerRule - gap - float64(len(inputRows))*f.lineH
	upperRule := inputTop - gap
	outputBottom := upperRule - gap
	g.outputRows = max(1, int((outputBottom-pad)/f.lineH))

	sb, jobCursor := g.scrollback, -1
	switch {
	case g.viewing != nil:
		sb = g.viewing.Output
		if g.viewing.Running() {
			jobCursor = g.viewing.Col()
		}
	case g.attached != nil:
		jobCursor = g.parser.Col()
	}
	g.drawScrollback(screen, sb, jobCursor, pad, outputBottom, now)
	switch {
	case g.panel.open && g.panel.kind == panelHelp:
		g.drawHelpPanel(screen, pad, w-pad, upperRule-gap)
	case g.panel.open:
		g.drawJobsPanel(screen, pad, w-pad, upperRule-gap, now)
	}

	rule := func(y float64) {
		vector.StrokeLine(screen, float32(pad), float32(y), float32(w-pad), float32(y), float32(g.scale), g.theme.Border, false)
	}
	rule(upperRule)
	rule(lowerRule)

	promptW := f.cellW * float64(promptLen)
	// While a job has the keyboard, its cursor is drawn in the output instead.
	var inputHint string
	switch {
	case g.viewing != nil && g.viewing.Running():
		inputHint = fmt.Sprintf("input is sent to job %d · ctrl+b to go back", g.viewing.ID)
	case g.viewing != nil:
		inputHint = fmt.Sprintf("job %d has finished · esc to go back", g.viewing.ID)
	case g.attached != nil:
		inputHint = "input is sent to the running command · ctrl+b to background"
	}
	if inputHint != "" {
		g.drawText(screen, prompt, pad, inputTop, g.theme.Muted, 1)
		g.drawText(screen, truncate(inputHint, g.cols-promptLen), pad+promptW, inputTop, g.theme.Muted, dimAlpha)
	} else {
		for i, row := range inputRows {
			y := inputTop + float64(i)*f.lineH
			x := pad
			if i == 0 {
				g.drawText(screen, prompt, x, y, g.theme.Accent, 1)
				row, x = row[promptLen:], x+promptW
			}
			g.drawText(screen, string(row), x, y, g.theme.Foreground, 1)
		}
		g.drawCursor(screen, pad+float64(cursorCol)*f.cellW, inputTop+float64(cursorRow)*f.lineH, runeAt(inputRows[cursorRow], cursorCol), now)
	}

	g.drawStatus(screen, pad, statusY, w-pad, now)
}

// inputLayout wraps the prompt plus the typed text and locates the cursor.
func (g *Game) inputLayout() (rows [][]rune, cursorRow, cursorCol int) {
	rows = terminal.Wrap([]rune(g.cfg.Prompt.Symbol+g.editor.Text()), g.cols)
	idx := utf8.RuneCountInString(g.cfg.Prompt.Symbol) + g.editor.Cursor()
	cursorRow, cursorCol = idx/g.cols, idx%g.cols
	if cursorRow == len(rows) {
		rows = append(rows, nil) // cursor sits just past a full row
	}
	return rows, cursorRow, cursorCol
}

// drawScrollback paints sb from the newest line upwards, so the most recent
// output always hugs the input box. A cursorCol >= 0 draws a running job's
// cursor at that column of the newest line.
func (g *Game) drawScrollback(dst *ebiten.Image, sb *terminal.Scrollback, cursorCol int, top, bottom float64, now time.Time) {
	f := g.faces
	pad := g.cfg.Window.Padding * g.scale
	y := bottom
	skip := g.scroll
	last := sb.Len() - 1
	for i := last; i >= 0 && y-f.lineH >= top; i-- {
		line := sb.At(i)
		rows := terminal.Wrap(line.Cells, g.cols)
		curRow, curCol := -1, 0
		if i == last && cursorCol >= 0 {
			curRow, curCol = cursorCol/g.cols, cursorCol%g.cols
			for len(rows) <= curRow {
				rows = append(rows, nil)
			}
		}
		alpha, dy := g.entrance(now.Sub(line.At))
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
	f := g.faces
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
func (g *Game) entrance(age time.Duration) (alpha, dy float64) {
	fadeIn := time.Duration(g.cfg.Animation.FadeInMs) * time.Millisecond
	if !g.cfg.Animation.Enabled || fadeIn <= 0 {
		return 1, 0
	}
	t := min(1, float64(age)/float64(fadeIn))
	ease := 1 - math.Pow(1-t, 3) // ease-out cubic
	return ease, (1 - ease) * g.faces.lineH * 0.35
}

// drawCursor draws the cursor in the configured style. A block cursor
// redraws the rune under it (if any) in the background color.
func (g *Game) drawCursor(dst *ebiten.Image, x, y float64, under rune, now time.Time) {
	solid := !g.cfg.Cursor.Blink || now.Sub(g.lastInput) < blinkHold
	if !solid && now.UnixMilli()%blinkPeriod.Milliseconds() >= blinkPeriod.Milliseconds()/2 {
		return
	}
	f := g.faces
	thick := math.Max(2, math.Round(2*g.scale))
	switch g.cfg.Cursor.Style {
	case config.CursorBar:
		vector.FillRect(dst, float32(x), float32(y), float32(thick), float32(f.lineH), g.theme.Cursor, false)
	case config.CursorUnderline:
		vector.FillRect(dst, float32(x), float32(y+f.lineH-thick), float32(f.cellW), float32(thick), g.theme.Cursor, false)
	default:
		vector.FillRect(dst, float32(x), float32(y), float32(f.cellW), float32(f.lineH), g.theme.Cursor, false)
		if under != 0 {
			g.drawText(dst, string(under), x, y, g.theme.Background, 1)
		}
	}
}

func (g *Game) drawStatus(dst *ebiten.Image, left, y, right float64, now time.Time) {
	f := g.faces
	title := shortPath(g.session.Dir())
	if g.viewing != nil {
		title = fmt.Sprintf("job %d · %s", g.viewing.ID, g.viewing.Command)
	}

	hint, spinner := g.statusHint(now)

	// Keep the title readable on narrow windows: the hint gives way first.
	const minTitle, spinnerCols = 12, 4
	maxHint := g.cols - minTitle - 2
	if spinner {
		maxHint -= spinnerCols
	}
	hint = truncate(hint, maxHint)
	hintLen := utf8.RuneCountInString(hint)
	reserved := hintLen
	if spinner {
		reserved += spinnerCols
	}
	g.drawText(dst, truncate(title, g.cols-reserved-2), left, y, g.theme.Muted, 1)
	if hint == "" {
		return
	}
	x := right - f.cellW*float64(hintLen)
	g.drawText(dst, hint, x, y, g.theme.Muted, 1)
	if spinner {
		g.drawSpinner(dst, x-f.cellW*spinnerCols, y+f.lineH/2, now)
	}
}

// statusHint is the right side of the status line: what is going on, and
// the keys that matter right now.
func (g *Game) statusHint(now time.Time) (hint string, spinner bool) {
	switch {
	case g.scroll > 0:
		hint = "scrolled up · pgdn to return"
	case g.attached != nil && g.parser.AltScreen, g.viewing != nil && g.viewing.AltScreen():
		hint = "full-screen programs aren't supported yet · ctrl+c"
	case g.viewing != nil && g.viewing.Running():
		hint, spinner = "running "+formatElapsed(g.viewing.Elapsed(now)), true
	case g.viewing != nil:
		hint = jobResult(g.viewing) + " after " + formatElapsed(g.viewing.Elapsed(now))
	case g.attached != nil:
		hint, spinner = "running · ctrl+b background · ctrl+c interrupt", true
	default:
		if n := g.jobs.RunningInBackground(); n > 0 {
			hint, spinner = fmt.Sprintf("%d background %s · ctrl+t", n, plural(n, "job", "jobs")), true
		} else if len(g.jobs.Listed()) > 0 {
			hint = "ctrl+t for jobs"
		} else {
			hint = helpHint
		}
	}
	return hint, spinner
}

// drawJobsPanel draws the jobs list as an overlay whose bottom edge is at
// bottom.
func (g *Game) drawJobsPanel(dst *ebiten.Image, left, right, bottom float64, now time.Time) {
	const maxRows = 8
	f := g.faces
	listed := g.jobs.Listed()
	rows := min(max(1, len(listed)), maxRows)
	x, y, cols := g.drawPanelFrame(dst, left, right, bottom, rows, "jobs", "↑↓ select · enter open · x kill/remove · esc close")

	if len(listed) == 0 {
		g.drawText(dst, truncate("no background jobs · end a command with & or press ctrl+b while it runs", cols), x, y, g.theme.Muted, 1)
		return
	}

	// Keep the selection visible when there are more jobs than rows.
	first := max(0, g.panel.selected-rows+1)
	for i := first; i < min(len(listed), first+rows); i++ {
		j := listed[i]
		if i == g.panel.selected {
			vector.FillRect(dst, float32(left+g.scale), float32(y), float32(right-left-2*g.scale), float32(f.lineH), scaleAlpha(g.theme.Accent, 0.18), false)
		}

		marker, markerColor := "●", g.theme.Accent
		state := "running " + formatElapsed(j.Elapsed(now))
		if !j.Running() {
			marker, markerColor = "○", g.theme.Muted
			if j.Status != "" || j.Killed {
				markerColor = g.theme.Error
			}
			state = jobResult(j)
		}

		// marker, id, command, state, then the last line of output if it fits.
		const idW, stateW = 4, 16
		cmdW := min(32, max(8, cols-2-idW-stateW-4))
		line := fmt.Sprintf("%*d  %-*s  %-*s", idW-1, j.ID, cmdW, truncate(j.Command, cmdW), stateW, truncate(state, stateW))
		g.drawText(dst, marker, x, y, markerColor, 1)
		g.drawText(dst, truncate(line, cols-2), x+2*f.cellW, y, g.theme.Foreground, 1)
		if rest := cols - 2 - utf8.RuneCountInString(line) - 2; rest > 4 {
			lx := x + float64(2+utf8.RuneCountInString(line)+2)*f.cellW
			g.drawText(dst, truncate(j.LastLine(), rest), lx, y, g.theme.Muted, dimAlpha)
		}
		y += f.lineH
	}
}

// drawSpinner draws three dots pulsing in sequence.
func (g *Game) drawSpinner(dst *ebiten.Image, x, cy float64, now time.Time) {
	f := g.faces
	r := f.cellW * 0.22
	t := float64(now.UnixMilli()) / 1000
	if !g.cfg.Animation.Enabled {
		t = 0
	}
	for i := range 3 {
		phase := math.Sin(t*2*math.Pi*1.2 - float64(i)*0.9)
		a := 0.3 + 0.7*(phase+1)/2
		c := scaleAlpha(g.theme.Accent, a)
		vector.FillCircle(dst, float32(x+float64(i)*r*3.2), float32(cy), float32(r*(0.8+0.2*a)), c, true)
	}
}

func (g *Game) drawText(dst *ebiten.Image, s string, x, y float64, clr color.RGBA, alpha float64) {
	g.drawTextFace(dst, g.faces.faces[fonts.Regular], s, x, y, clr, alpha)
}

func (g *Game) drawTextFace(dst *ebiten.Image, face *text.GoTextFace, s string, x, y float64, clr color.RGBA, alpha float64) {
	if s == "" || alpha <= 0 || strings.TrimLeft(s, " ") == "" {
		return
	}
	op := &text.DrawOptions{}
	op.GeoM.Translate(x, y+g.faces.textDY)
	op.ColorScale.ScaleWithColor(clr)
	op.ColorScale.ScaleAlpha(float32(alpha))
	text.Draw(dst, s, face, op)
}

// truncate shortens s to at most n runes, marking the cut with an ellipsis.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

func formatElapsed(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
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
