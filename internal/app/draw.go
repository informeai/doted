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
	promptBarWidth   = 2    // logical px
	selectionDotSize = 0.14 // radius of the selection dots, in cell widths

	blinkPeriod = time.Second
	blinkHold   = 600 * time.Millisecond // the cursor holds still after a keystroke

	// The dot cursor hops like a ball while you type: one hop per
	// bouncePeriod, rising bounceHeight rows at the top, and squashing by up
	// to bounceSquash as it lands.
	bouncePeriod  = 450 * time.Millisecond
	bounceHeight  = 0.3
	bounceSquash  = 0.22
	bounceContact = 0.1 // fraction of the period spent touching the ground

	// After typing stops, the bar and the dot fade from the accent color back
	// to the text color over typingFade.
	typingFade = 300 * time.Millisecond
	dimAlpha   = 0.6
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
	glyphH     float64 // ascent plus descent
	baselineY  float64 // offset of the text baseline from the row top
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
	f.glyphH = glyphH
	f.baselineY = f.textDY + m.HAscent
	f.underlineY = f.baselineY + scale
	f.cellW = text.AdvanceAt("M", 1, f.faces[0])
	return f
}

func (g *Game) Draw(screen *ebiten.Image) {
	if g.faces == nil || g.faces.scale != g.scale {
		g.faces = newFaceSet(g.family, g.cfg.Font, g.scale)
	}
	f := g.faces
	promptLen := utf8.RuneCountInString(g.promptText())
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
	g.outTop, g.outBottom, g.outLeft = pad, outputBottom, pad
	if sj := g.screenJob(); sj != nil {
		g.rows = g.rows[:0]
		g.drawScreen(screen, sj, pad, outputBottom-float64(sj.Screen().Height())*f.lineH, now)
	} else {
		g.drawScrollback(screen, sb, jobCursor, pad, outputBottom, now)
		g.drawLinkHover(screen)
	}
	switch {
	case g.panel.open && g.panel.kind == panelHelp:
		g.drawHelpPanel(screen, pad, w-pad, upperRule-gap)
	case g.panel.open && g.panel.kind == panelHistory:
		g.drawHistoryPanel(screen, pad, w-pad, upperRule-gap)
	case g.panel.open && g.panel.kind == panelFind:
		g.drawFindPanel(screen, pad, w-pad, upperRule-gap)
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
	case g.attached != nil && g.attached.FullScreen():
		inputHint = "the program on screen gets every key, ctrl+b included"
	case g.attached != nil:
		inputHint = "input is sent to the running command · ctrl+b to background"
	}
	if inputHint != "" {
		g.drawPrompt(screen, pad, inputTop, g.theme.Muted, 1)
		g.drawText(screen, truncate(inputHint, g.cols-promptLen), pad+promptW, inputTop, g.theme.Muted, dimAlpha)
	} else {
		colorAt := g.inputColors(promptLen, now)
		for i, row := range inputRows {
			y := inputTop + float64(i)*f.lineH
			from := 0
			if i == 0 {
				g.drawPrompt(screen, pad, y, g.promptColor(now), 1)
				from = promptLen
			}
			g.drawInputRow(screen, row, from, i*g.cols, pad, y, colorAt)
		}
		g.drawSelection(screen, pad, inputTop, promptLen)
		// The suggestion continues the line, faded, up to the end of the row.
		if suffix := g.suggestion(now); suffix != "" {
			g.drawText(screen, truncate(suffix, g.cols-cursorCol), pad+float64(cursorCol)*f.cellW, inputTop+float64(cursorRow)*f.lineH, g.theme.Muted, dimAlpha)
		}
		// Right after a Tab completion, the zap stands in for the cursor.
		if !g.drawZap(screen, pad, inputTop, now) {
			g.drawCursor(screen, pad+float64(cursorCol)*f.cellW, inputTop+float64(cursorRow)*f.lineH, runeAt(inputRows[cursorRow], cursorCol), now)
		}
	}

	// Sparks fly from the bar over everything else in the input box.
	if g.cfg.Prompt.Style == config.PromptBar {
		barX, barY := g.barCenter(pad, inputTop)
		g.sparks.draw(screen, barX, barY, g.scale, g.theme.Accent, g.theme.Foreground)
	}

	g.drawStatus(screen, pad, statusY, w-pad, now)
}

// promptText is what the prompt takes up on the line: the symbol, or blank
// cells where the bar is drawn.
func (g *Game) promptText() string {
	if g.cfg.Prompt.Style == config.PromptBar {
		return "  "
	}
	return g.cfg.Prompt.Symbol
}

// drawPrompt draws the prompt at the start of the row at (x, y).
func (g *Game) drawPrompt(dst *ebiten.Image, x, y float64, clr color.RGBA, alpha float64) {
	if g.cfg.Prompt.Style != config.PromptBar {
		g.drawText(dst, g.cfg.Prompt.Symbol, x, y, clr, alpha)
		return
	}
	f := g.faces
	width := math.Max(1, math.Round(promptBarWidth*g.scale))
	vector.FillRect(dst, float32(x), float32(y+f.textDY), float32(width), float32(f.glyphH), scaleAlpha(clr, alpha), true)
}

// barCenter is the middle of the prompt bar in the row at (x, y), where the
// sparks start.
func (g *Game) barCenter(x, y float64) (float64, float64) {
	f := g.faces
	return x + math.Max(1, math.Round(promptBarWidth*g.scale))/2, y + f.textDY + f.glyphH/2
}

// selectionCells is the selected part of the input as a range of cells,
// counted from the start of the prompt; empty without a selection.
func (g *Game) selectionCells(promptLen int) (from, to int) {
	start, end, ok := g.editor.Selection()
	if !ok {
		return 0, 0
	}
	return promptLen + start, promptLen + end
}

// inputColors returns the color of each cell of the input, counted from the
// start of the prompt: the selection in the accent color, then the command
// word colored by what it is, then the text color.
func (g *Game) inputColors(promptLen int, now time.Time) func(cell int) color.RGBA {
	selFrom, selTo := g.selectionCells(promptLen)
	start, end := commandWord([]rune(g.editor.Text()))
	cmdFrom, cmdTo := promptLen+start, promptLen+end
	cmdColor := g.commandColor(g.classifyCommand(g.editor.Text(), now))
	return func(cell int) color.RGBA {
		switch {
		case cell >= selFrom && cell < selTo:
			return g.theme.Accent
		case cell >= cmdFrom && cell < cmdTo:
			return cmdColor
		}
		return g.theme.Foreground
	}
}

// drawInputRow draws row's runes from column from on, in runs of the same
// color. rowCell is the cell the row starts at.
func (g *Game) drawInputRow(dst *ebiten.Image, row []rune, from, rowCell int, x, y float64, colorAt func(cell int) color.RGBA) {
	for c := from; c < len(row); {
		clr := colorAt(rowCell + c)
		end := c + 1
		for end < len(row) && colorAt(rowCell+end) == clr {
			end++
		}
		g.drawText(dst, string(row[c:end]), x+float64(c)*g.faces.cellW, y, clr, 1)
		c = end
	}
}

// drawSelection marks each selected character of the input with a small dot
// above it. The input starts at (x, y) after promptLen cells of prompt and
// wraps every g.cols cells.
func (g *Game) drawSelection(dst *ebiten.Image, x, y float64, promptLen int) {
	from, to := g.selectionCells(promptLen)
	f := g.faces
	r := float32(math.Max(1, f.cellW*selectionDotSize))
	for cell := from; cell < to; cell++ {
		cx := x + (float64(cell%g.cols)+0.5)*f.cellW
		cy := y + float64(cell/g.cols)*f.lineH + f.textDY/2
		vector.FillCircle(dst, float32(cx), float32(cy), r, g.theme.Accent, true)
	}
}

// inputLayout wraps the prompt plus the typed text and locates the cursor.
func (g *Game) inputLayout() (rows [][]rune, cursorRow, cursorCol int) {
	prompt := g.promptText()
	rows = terminal.Wrap([]rune(prompt+g.editor.Text()), g.cols)
	idx := utf8.RuneCountInString(prompt) + g.editor.Cursor()
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
	g.rows = g.rows[:0]
	g.actions = g.actions[:0]
	folded := g.foldedRanges(sb)
	for i := last; i >= 0 && y-f.lineH >= top; i-- {
		line := sb.At(i)
		seq := sb.Seq(i)
		if hidden(seq, folded) {
			continue
		}
		b := g.blockAtSeq(sb, seq)
		if b != nil && b.collapsed {
			// The "… N lines" row sits under the folded command.
			if skip > 0 {
				skip--
			} else {
				y -= f.lineH
				g.drawFoldedRow(dst, seq, foldedCount(folded, seq), pad, y)
			}
		}
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
			row := visibleRow{seq: sb.Seq(i), start: j * g.cols, n: len(rows[j]), y: y}
			g.rows = append(g.rows, row)
			g.drawRowSelection(dst, sb, row, len(line.Cells), pad, y+dy)
			g.drawFindHits(dst, sb, row, pad, y+dy)
			g.drawCells(dst, rows[j], pad, y+dy, line.Kind, alpha)
			// Commands that were run keep the prompt bar they were typed at.
			if j == 0 && line.Kind == terminal.Command && g.cfg.Prompt.Style == config.PromptBar {
				g.drawPrompt(dst, pad, y+dy, g.theme.Accent, alpha)
			}
			if j == 0 && b != nil {
				g.drawBlockHeader(dst, b, seq, len(rows[j]), pad, y+dy, now)
			}
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

// drawCursor draws the cursor in the configured style. The dot hops while
// the user types and rests otherwise; the other styles blink while idle. A
// block cursor redraws the rune under it (if any) in the background color.
func (g *Game) drawCursor(dst *ebiten.Image, x, y float64, under rune, now time.Time) {
	f := g.faces
	if g.cfg.Cursor.Style == config.CursorDot {
		// Motion follows the global animation switch too.
		var t time.Duration
		if g.cfg.Cursor.Animate && g.cfg.Animation.Enabled {
			t, _ = bounceElapsed(now, g.bounceStart, g.lastInput)
		}
		g.drawDotCursor(dst, x, y, t, g.promptColor(now))
		return
	}
	resting := now.Sub(g.lastInput) < blinkHold || !g.cfg.Cursor.Animate
	if !resting && now.UnixMilli()%blinkPeriod.Milliseconds() >= blinkPeriod.Milliseconds()/2 {
		return
	}
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

// drawDotCursor draws a ball resting on the text baseline of the cell at
// (x, y), t into its bounce (0 is at rest).
func (g *Game) drawDotCursor(dst *ebiten.Image, x, y float64, t time.Duration, clr color.RGBA) {
	f := g.faces
	lift, squash := dotBounce(t)
	r := f.cellW * 0.32
	rx := r * (1 + bounceSquash*squash)
	ry := r * (1 - bounceSquash*squash)
	cx := x + f.cellW/2
	cy := y + f.baselineY - ry - lift*bounceHeight*f.lineH
	fillEllipse(dst, cx, cy, rx, ry, clr)
}

// bounceElapsed says how far the dot is into its bouncing, which started at
// start with the first keystroke. It keeps bouncing while keys keep coming
// and, once they stop, finishes the hop the last key fell in, so it always
// lands instead of freezing mid-air. bouncing is false at rest.
func bounceElapsed(now, start, lastKey time.Time) (t time.Duration, bouncing bool) {
	end, ok := bounceEnd(start, lastKey)
	if !ok || !now.Before(end) {
		return 0, false
	}
	return now.Sub(start), true
}

// bounceEnd is when the dot lands the hop the last key fell in.
func bounceEnd(start, lastKey time.Time) (time.Time, bool) {
	if start.IsZero() || lastKey.Before(start) {
		return time.Time{}, false
	}
	hops := lastKey.Sub(start)/bouncePeriod + 1
	return start.Add(hops * bouncePeriod), true
}

// typingGlow is 1 while the user is typing (the same span the dot hops for)
// and fades to 0 over typingFade once it lands.
func typingGlow(now, start, lastKey time.Time) float64 {
	end, ok := bounceEnd(start, lastKey)
	if !ok {
		return 0
	}
	if now.Before(end) {
		return 1
	}
	return math.Max(0, 1-float64(now.Sub(end))/float64(typingFade))
}

// promptColor is the prompt bar's and the dot cursor's color: the text color
// at rest, the accent color while typing.
func (g *Game) promptColor(now time.Time) color.RGBA {
	return mixRGBA(g.theme.Foreground, g.theme.Accent, typingGlow(now, g.bounceStart, g.lastInput))
}

// dotBounce returns how high the dot is at t (0 on the ground, 1 at the top
// of the hop) and how squashed it is (1 at the moment of impact). The hop is
// a parabola, like a ball under gravity; the squash happens only while the
// ball touches the ground.
func dotBounce(t time.Duration) (lift, squash float64) {
	if t <= 0 {
		return 0, 0
	}
	p := math.Mod(float64(t)/float64(bouncePeriod), 1)
	// The contact window straddles the period boundary: half at the end of a
	// hop, half at the start of the next.
	half := bounceContact / 2
	if d := math.Min(p, 1-p); d < half {
		return 0, 1 - d/half
	}
	q := (p - half) / (1 - bounceContact) // 0..1 across the airborne part
	return 4 * q * (1 - q), 0
}

// fillEllipse draws an anti-aliased filled ellipse.
func fillEllipse(dst *ebiten.Image, cx, cy, rx, ry float64, clr color.RGBA) {
	const k = 0.5522847498 // control distance for a quarter circle
	var p vector.Path
	x, y := float32(cx), float32(cy)
	a, b := float32(rx), float32(ry)
	ka, kb := float32(k*rx), float32(k*ry)
	p.MoveTo(x+a, y)
	p.CubicTo(x+a, y+kb, x+ka, y+b, x, y+b)
	p.CubicTo(x-ka, y+b, x-a, y+kb, x-a, y)
	p.CubicTo(x-a, y-kb, x-ka, y-b, x, y-b)
	p.CubicTo(x+ka, y-b, x+a, y-kb, x+a, y)
	p.Close()
	op := &vector.DrawPathOptions{AntiAlias: true}
	op.ColorScale.ScaleWithColor(clr)
	vector.FillPath(dst, &p, &vector.FillOptions{}, op)
}

func (g *Game) drawStatus(dst *ebiten.Image, left, y, right float64, now time.Time) {
	f := g.faces
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
	// The job view names the job; otherwise the working directory shows.
	titleCols := g.cols - reserved - 2
	if g.viewing != nil {
		title := fmt.Sprintf("job %d · %s", g.viewing.ID, g.viewing.Command)
		g.drawText(dst, truncate(title, titleCols), left, y, g.theme.Muted, 1)
	} else {
		// The project's context follows the path; a long path gives way to
		// it, down to a readable minimum.
		const minPath = 16
		pathMax := min(titleCols, max(minPath, titleCols-g.contextCols()-3))
		g.drawPath(dst, left, y, pathMax, now)
		pathCols := utf8.RuneCountInString(truncate(string(g.path.to), pathMax))
		g.drawContext(dst, left+float64(pathCols+3)*f.cellW, y, left+float64(titleCols)*f.cellW)
	}
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
	case g.flashText != "" && now.Before(g.flashUntil):
		hint = g.flashText
	case g.scroll > 0:
		hint = "scrolled up · pgdn to return"
	case g.screenJob() != nil:
		hint, spinner = "full screen · every key goes to the program", true
	case g.viewing != nil && g.viewing.Running():
		hint, spinner = "running "+formatElapsed(g.viewing.Elapsed(now)), true
	case g.viewing != nil:
		hint = jobResult(g.viewing) + " after " + formatElapsed(g.viewing.Elapsed(now))
	case g.attached != nil:
		hint, spinner = "running · ctrl+b background · ctrl+c interrupt", true
	default:
		if n := g.jobs.RunningInBackground(); n > 0 {
			hint, spinner = fmt.Sprintf("%d background %s · ctrl+t", n, plural(n, "job", "jobs")), true
		} else if g.attached == nil && g.viewing == nil && g.suggestion(now) != "" {
			hint = "tab completes"
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

// mixRGBA blends from a to b by t (0 is a, 1 is b).
func mixRGBA(a, b color.RGBA, t float64) color.RGBA {
	t = math.Max(0, math.Min(1, t))
	mix := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t)) }
	return color.RGBA{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B), mix(a.A, b.A)}
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
