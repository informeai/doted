package app

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/hajimehoshi/ebiten/v2"
)

// The git part of the status line: the Git logo in the text color and the
// branch name in white, followed by what changed, each
// kind in its color: ~modified +added -deleted ?untracked !conflicts, and
// ↑ahead ↓behind of the upstream.

var badgeText = color.RGBA{0xff, 0xff, 0xff, 0xff}

// gitMark is one "~3" after the badge.
type gitMark struct {
	text string
	clr  color.RGBA
}

func (g *Game) gitMarks(info projectContext) []gitMark {
	var marks []gitMark
	add := func(n int, sym string, clr color.RGBA) {
		if n > 0 {
			marks = append(marks, gitMark{fmt.Sprintf("%s%d", sym, n), clr})
		}
	}
	c := info.changes
	add(c.conflicts, "!", g.theme.Error)
	add(c.modified, "~", g.theme.ANSI[3])
	add(c.added, "+", g.theme.ANSI[2])
	add(c.deleted, "-", g.theme.ANSI[1])
	add(c.untracked, "?", g.theme.Muted)
	add(info.ahead, "↑", g.theme.Muted)
	add(info.behind, "↓", g.theme.Muted)
	return marks
}

// badgeCols is how many cells the badge takes besides the branch name:
// the icon and the gap after it (see drawGitBadge), rounded up.
const badgeCols = 2

// contextCols is how many cells drawContext wants.
func (g *Game) contextCols() int {
	n := 0
	if info, ok := g.currentContext(); ok && info.branch != "" {
		n += badgeCols + utf8.RuneCountInString(info.branch) + 1
		for _, m := range g.gitMarks(info) {
			n += 1 + utf8.RuneCountInString(m.text)
		}
	}
	if rest := g.contextText(); rest != "" {
		n += 1 + utf8.RuneCountInString(rest)
	}
	return n
}

// drawContext draws the project's context from x, stopping before right:
// the git badge and marks, then the last duration.
func (g *Game) drawContext(dst *ebiten.Image, x, y, right float64) {
	f := g.faces
	info, ok := g.currentContext()
	if ok && info.branch != "" {
		x = g.drawGitBadge(dst, info, x, y, right)
		for _, m := range g.gitMarks(info) {
			w := float64(utf8.RuneCountInString(m.text)) * f.cellW
			if x+f.cellW+w > right {
				return
			}
			x += f.cellW
			g.drawText(dst, m.text, x, y, m.clr, 1)
			x += w
		}
		x += f.cellW
	}
	rest := g.contextText()
	if rest == "" {
		return
	}
	if cols := int((right - x) / f.cellW); cols >= 6 {
		g.drawText(dst, truncate(rest, cols), x+f.cellW, y, g.theme.Muted, 1)
	}
}

// drawGitBadge draws the branch icon and name at x and returns where they
// end; x when there's no room for them.
func (g *Game) drawGitBadge(dst *ebiten.Image, info projectContext, x, y, right float64) float64 {
	f := g.faces
	iconW, gap := math.Min(f.lineH, f.glyphH*1.6)*0.78, f.cellW*0.6
	maxName := int((right - x - iconW - gap) / f.cellW)
	if maxName < 4 {
		return x
	}
	name := truncate(info.branch, maxName)
	h := math.Min(f.lineH, f.glyphH*1.6)
	top := y + (f.lineH-h)/2
	size := h * 0.78
	drawGitLogo(dst, x, top+(h-size)/2, size, g.theme.Foreground)
	g.drawText(dst, strings.TrimSpace(name), x+iconW+gap, y, badgeText, 1)
	return x + iconW + gap + float64(utf8.RuneCountInString(name))*f.cellW
}
