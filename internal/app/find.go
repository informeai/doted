package app

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/informeai/doted/internal/terminal"
)

// Cmd+F (Ctrl+Shift+F elsewhere) searches the output on screen: the main
// view or the job being viewed. Every hit is highlighted, the current one
// more strongly, and the view scrolls to it. Enter goes to the older hit,
// Shift+Enter to the newer one; the search starts from the newest. Matching
// ignores case unless the query has an uppercase letter.

// findMatch is a hit: cells [col, col+n) of the line numbered seq.
type findMatch struct{ seq, col, n int }

const findAlpha, findCurrentAlpha = 0.22, 0.55

func (g *Game) openFind() {
	if g.screenJob() != nil {
		g.flash("search works on the output, not on full-screen programs")
		return
	}
	query := g.panel.query
	if g.panel.kind != panelFind {
		query = nil
	}
	g.panel = panel{open: true, kind: panelFind, query: query}
	g.refreshFind(true)
}

// refreshFind searches again; jump moves to the newest hit.
func (g *Game) refreshFind(jump bool) {
	sb := g.visibleScrollback()
	g.panel.finds = findAll(sb, string(g.panel.query))
	g.panel.findSB, g.panel.findLen = sb, sb.Len()
	if jump {
		g.panel.selected = len(g.panel.finds) - 1
		g.showFind()
	}
}

func findAll(sb *terminal.Scrollback, query string) []findMatch {
	if query == "" {
		return nil
	}
	q := []rune(query)
	caseSensitive := strings.IndexFunc(query, unicode.IsUpper) >= 0
	fold := func(r rune) rune {
		if caseSensitive {
			return r
		}
		return unicode.ToLower(r)
	}
	var out []findMatch
	var runes []rune
	var cols []int
	for i := range sb.Len() {
		// The line's characters and the column each starts at: a wide one
		// takes two.
		runes, cols = runes[:0], cols[:0]
		cells := sb.At(i).Cells
		for c, cell := range cells {
			if cell.Rune != terminal.WideTail {
				runes, cols = append(runes, cell.Rune), append(cols, c)
			}
		}
		for c := 0; c+len(q) <= len(runes); c++ {
			hit := true
			for k, r := range q {
				if fold(runes[c+k]) != fold(r) {
					hit = false
					break
				}
			}
			if hit {
				last := c + len(q) - 1
				end := cols[last] + max(1, terminal.RuneWidth(runes[last]))
				out = append(out, findMatch{sb.Seq(i), cols[c], end - cols[c]})
				c = last
			}
		}
	}
	return out
}

func (g *Game) handleFindKeys() {
	// New output, or another view: search again, staying on the same hit.
	if sb := g.visibleScrollback(); sb != g.panel.findSB || sb.Len() != g.panel.findLen || g.attached != nil {
		var current findMatch
		if m, ok := g.currentFind(); ok {
			current = m
		}
		g.refreshFind(false)
		g.panel.selected = len(g.panel.finds) - 1
		for i, m := range g.panel.finds {
			if m == current {
				g.panel.selected = i
			}
		}
	}

	ctrl := ebiten.IsKeyPressed(ebiten.KeyControl)
	meta := ebiten.IsKeyPressed(ebiten.KeyMeta)
	shift := ebiten.IsKeyPressed(ebiten.KeyShift)
	g.chars = ebiten.AppendInputChars(g.chars[:0])
	if !ctrl && !meta {
		typed := false
		for _, r := range g.chars {
			if !unicode.IsControl(r) {
				g.panel.query = append(g.panel.query, r)
				typed = true
			}
		}
		if typed {
			g.refreshFind(true)
		}
	}
	enter := inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeyNumpadEnter)
	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeyEscape), ctrl && inpututil.IsKeyJustPressed(ebiten.KeyC), clipboardChord(ebiten.KeyF):
		g.panel.open = false
	case repeating(ebiten.KeyBackspace):
		if n := len(g.panel.query); n > 0 {
			g.panel.query = g.panel.query[:n-1]
			g.refreshFind(true)
		}
	case enter && shift, repeating(ebiten.KeyArrowDown), clipboardChord(ebiten.KeyG) && shift:
		g.stepFind(1)
	case enter, repeating(ebiten.KeyArrowUp), clipboardChord(ebiten.KeyG):
		g.stepFind(-1)
	}
}

// stepFind moves to the next hit in direction dir (-1 older, +1 newer),
// wrapping around.
func (g *Game) stepFind(dir int) {
	n := len(g.panel.finds)
	if n == 0 {
		return
	}
	g.panel.selected = ((g.panel.selected+dir)%n + n) % n
	g.showFind()
}

func (g *Game) currentFind() (findMatch, bool) {
	if !g.panel.open || g.panel.kind != panelFind || g.panel.selected < 0 || g.panel.selected >= len(g.panel.finds) {
		return findMatch{}, false
	}
	return g.panel.finds[g.panel.selected], true
}

// showFind scrolls so the current hit sits mid-screen, unfolding its block
// if it was folded away.
func (g *Game) showFind() {
	m, ok := g.currentFind()
	if !ok {
		return
	}
	sb := g.visibleScrollback()
	i, ok := sb.Index(m.seq)
	if !ok {
		return
	}
	ranges := g.foldedRanges(sb)
	if hidden(m.seq, ranges) {
		for j := i; j >= 0; j-- {
			if b := g.blockAtSeq(sb, sb.Seq(j)); b != nil {
				b.collapsed = false
				break
			}
		}
		ranges = g.foldedRanges(sb)
	}
	// Rows below the hit's row: the lines after it, then its own rows after
	// the one the hit is on.
	below := 0
	for j := i + 1; j < sb.Len(); j++ {
		below += g.lineRows(sb, j, ranges)
	}
	below += g.lineRows(sb, i, ranges) - 1 - m.col/max(1, g.cols)
	if b := g.blockAtSeq(sb, m.seq); b != nil && b.collapsed {
		below-- // the "… N lines" row
	}
	if below >= g.scroll && below < g.scroll+g.outputRows {
		return // already on screen
	}
	g.scroll = 0
	g.scrollBy(below - g.outputRows/2)
}

// drawFindHits highlights the hits in row r of sb, drawn at (x, y).
func (g *Game) drawFindHits(dst *ebiten.Image, sb *terminal.Scrollback, r visibleRow, x, y float64) {
	if !g.panel.open || g.panel.kind != panelFind || g.panel.findSB != sb {
		return
	}
	f := g.faces
	current, _ := g.currentFind()
	for _, m := range g.panel.finds {
		if m.seq != r.seq {
			continue
		}
		from, to := max(m.col, r.start), min(m.col+m.n, r.start+g.cols)
		if to <= from {
			continue
		}
		alpha := findAlpha
		if m == current {
			alpha = findCurrentAlpha
		}
		vector.FillRect(dst, float32(x+float64(from-r.start)*f.cellW), float32(y), float32(float64(to-from)*f.cellW), float32(f.lineH), scaleAlpha(g.theme.Accent, alpha), false)
	}
}

func (g *Game) drawFindPanel(dst *ebiten.Image, left, right, bottom float64) {
	f := g.faces
	n := len(g.panel.finds)
	var count string
	switch {
	case len(g.panel.query) == 0:
		count = "type to search the output"
	case n == 0:
		count = "no matches"
	default:
		count = fmt.Sprintf("%d of %d", g.panel.selected+1, n)
	}
	x, y, cols := g.drawPanelFrame(dst, left, right, bottom, 1, "find", count+" · enter older · shift+enter newer · esc")
	g.drawText(dst, "›", x, y, g.theme.Accent, 1)
	clr := g.theme.Foreground
	if n == 0 && len(g.panel.query) > 0 {
		clr = g.theme.Error
	}
	g.drawText(dst, truncate(string(g.panel.query), cols-4), x+2*f.cellW, y, clr, 1)
	caret := x + float64(2+len(g.panel.query))*f.cellW
	vector.FillRect(dst, float32(caret), float32(y+f.textDY), float32(max(1, g.scale)), float32(f.glyphH), g.theme.Accent, false)
}
