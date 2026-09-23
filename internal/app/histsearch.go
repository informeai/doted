package app

import (
	"fmt"
	"slices"
	"unicode"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/informeai/doted/internal/terminal"
)

// Ctrl+R searches the history: typing narrows a list of past commands by
// fuzzy match, Enter puts the chosen one on the prompt (without running it),
// Ctrl+R or Down moves to the next match, and Esc goes back.

const historyPanelRows = 12

func (g *Game) openHistorySearch() {
	g.panel = panel{open: true, kind: panelHistory, query: []rune(g.editor.Text())}
	g.refreshHistoryMatches()
}

// historyCandidates is the history newest first, each command once.
func (g *Game) historyCandidates() []string {
	seen := map[string]bool{}
	var out []string
	for _, h := range slices.Backward(g.editor.History()) {
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}

func (g *Game) refreshHistoryMatches() {
	g.panel.matches = terminal.FuzzyFind(string(g.panel.query), g.historyCandidates())
	g.panel.selected = 0
}

func (g *Game) handleHistoryKeys() {
	ctrl := ebiten.IsKeyPressed(ebiten.KeyControl)
	g.chars = ebiten.AppendInputChars(g.chars[:0])
	if !ctrl && !ebiten.IsKeyPressed(ebiten.KeyMeta) {
		typed := false
		for _, r := range g.chars {
			if !unicode.IsControl(r) {
				g.panel.query = append(g.panel.query, r)
				typed = true
			}
		}
		if typed {
			g.refreshHistoryMatches()
		}
	}
	last := len(g.panel.matches) - 1
	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeyEscape), ctrl && inpututil.IsKeyJustPressed(ebiten.KeyC):
		g.panel.open = false
	case inpututil.IsKeyJustPressed(ebiten.KeyEnter), inpututil.IsKeyJustPressed(ebiten.KeyNumpadEnter):
		g.pickHistory(g.panel.selected)
	case repeating(ebiten.KeyBackspace):
		if n := len(g.panel.query); n > 0 {
			g.panel.query = g.panel.query[:n-1]
			g.refreshHistoryMatches()
		}
	case repeating(ebiten.KeyArrowUp):
		g.panel.selected = max(0, g.panel.selected-1)
	case repeating(ebiten.KeyArrowDown), ctrl && repeating(ebiten.KeyR):
		g.panel.selected = min(max(last, 0), g.panel.selected+1)
	}
}

// pickHistory puts match i on the prompt, ready to edit or run.
func (g *Game) pickHistory(i int) {
	g.panel.open = false
	if i < 0 || i >= len(g.panel.matches) {
		return
	}
	g.editor.Reset()
	g.editor.Insert([]rune(g.panel.matches[i].Text)...)
	g.touch()
}

func (g *Game) drawHistoryPanel(dst *ebiten.Image, left, right, bottom float64) {
	f := g.faces
	matches := g.panel.matches
	rows := min(max(1, len(matches)), historyPanelRows, max(1, g.outputRows-2))
	keys := fmt.Sprintf("%d of %d · ↑↓ · enter use · esc", len(matches), len(g.historyCandidates()))
	x, y, cols := g.drawPanelFrame(dst, left, right, bottom, rows+1, "history", keys)

	// The query, with a caret after it.
	g.drawText(dst, "›", x, y, g.theme.Accent, 1)
	g.drawText(dst, truncate(string(g.panel.query), cols-4), x+2*f.cellW, y, g.theme.Foreground, 1)
	caret := x + float64(2+len(g.panel.query))*f.cellW
	vector.FillRect(dst, float32(caret), float32(y+f.textDY), float32(max(1, g.scale)), float32(f.glyphH), g.theme.Accent, false)
	y += f.lineH

	if len(matches) == 0 {
		g.drawText(dst, "no command in the history matches", x, y, g.theme.Muted, 1)
		return
	}
	first := max(0, g.panel.selected-rows+1)
	for i := first; i < min(len(matches), first+rows); i++ {
		m := matches[i]
		if i == g.panel.selected {
			vector.FillRect(dst, float32(left+g.scale), float32(y), float32(right-left-2*g.scale), float32(f.lineH), scaleAlpha(g.theme.Accent, 0.18), false)
		}
		// The letters that matched are drawn in the accent color.
		hit := map[int]bool{}
		for _, p := range m.Positions {
			hit[p] = true
		}
		for c, r := range []rune(truncate(m.Text, cols)) {
			clr := g.theme.Foreground
			if hit[c] {
				clr = g.theme.Accent
			}
			g.drawText(dst, string(r), x+float64(c)*f.cellW, y, clr, 1)
		}
		y += f.lineH
	}
}
