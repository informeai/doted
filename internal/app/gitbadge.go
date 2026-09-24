package app

import (
	"fmt"
	"image/color"
	"math"
	"time"
	"unicode/utf8"

	"github.com/hajimehoshi/ebiten/v2"
)

// The git part of the status line: the Git logo in the text color and the
// branch name in white, followed by what changed, each
// kind in its color: ~modified +added -deleted ?untracked !conflicts, and
// ↑ahead (accent) ↓behind (cyan) of the upstream.

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
	add(info.ahead, "↑", g.theme.Accent)   // commits to push: something to do
	add(info.behind, "↓", g.theme.ANSI[6]) // commits to pull, in cyan
	return marks
}

// badgeCols is how many cells the badge takes besides the branch name:
// the icon and the gap after it (see drawGitBadge), rounded up.
const badgeCols = 2

// contextCols is how many cells drawContext wants.
func (g *Game) contextCols() int {
	n := 0
	info, ok := g.currentContext()
	if info, _ = g.branch.fade(info, time.Now()); ok && info.branch != "" {
		n += badgeCols + utf8.RuneCountInString(info.branch) + 1
		for _, m := range g.gitMarks(info) {
			n += 1 + utf8.RuneCountInString(m.text)
		}
		n += g.deletionCols(time.Now())
	}
	if rest := g.contextText(); rest != "" {
		n += 1 + utf8.RuneCountInString(rest)
	}
	return n
}

// drawContext draws the project's context from x, stopping before right:
// the git badge and marks, then the last duration.
func (g *Game) drawContext(dst *ebiten.Image, x, y, right float64, now time.Time) {
	f := g.faces
	info, ok := g.currentContext()
	info, alpha := g.branch.fade(info, now)
	if ok && info.branch != "" && alpha > 0 {
		x = g.drawGitBadge(dst, info, x, y, right, alpha, now)
		for _, m := range g.gitMarks(info) {
			w := float64(utf8.RuneCountInString(m.text)) * f.cellW
			if x+f.cellW+w > right {
				break
			}
			x += f.cellW
			g.drawText(dst, m.text, x, y, m.clr, alpha)
			x += w
		}
		x += f.cellW
	}
	if a := g.branch; a.redSparks != nil {
		a.redSparks.draw(dst, 0, 0, g.scale, g.theme.Error, g.theme.ANSI[9])
	}
	if a := g.branch; a.sparks != nil {
		a.sparks.draw(dst, a.center[0], a.center[1], g.scale, g.theme.Accent, g.theme.Foreground)
	}
	rest := g.contextText()
	if rest == "" {
		return
	}
	if cols := int((right - x) / f.cellW); cols >= 6 {
		g.drawText(dst, truncate(rest, cols), x+f.cellW, y, g.theme.Muted, 1)
	}
}

// drawGitBadge draws the Git logo and the branch name at x and returns
// where they end; x when there's no room for them.
func (g *Game) drawGitBadge(dst *ebiten.Image, info projectContext, x, y, right, alpha float64, now time.Time) float64 {
	f := g.faces
	h := math.Min(f.lineH, f.glyphH*1.6)
	size := h * 0.78
	iconW, gap := size, f.cellW*0.6
	maxName := int((right - x - iconW - gap) / f.cellW)
	if maxName < 4 {
		return x
	}
	top := y + (f.lineH-size)/2
	// While a deletion plays, the logo turns red and the deleted name
	// takes the branch's place; see gitdelete.go.
	del := g.branch.activeDeletion(now)
	blend := 0.0
	if del != nil {
		blend = del.iconBlend(now)
	}
	shake := g.branch.shake(now) * f.cellW
	logo := mixRGBA(g.theme.Foreground, g.theme.Error, blend)
	spin := g.branch.spin(now)
	if del != nil {
		spin += del.spin(now)
	}
	drawGitLogo(dst, x+shake, top, size, spin, scaleAlpha(logo, alpha))
	g.branch.center = [2]float64{x + size/2, top + size/2}
	if del != nil {
		n := g.drawDeletionName(dst, del, x+iconW+gap, y, maxName, alpha, now)
		return x + iconW + gap + float64(n)*f.cellW
	}

	// While switching, the name rolls from the old one; it takes the room of
	// the longer of the two meanwhile.
	roll := g.branch.name
	if string(roll.to) != info.branch {
		roll = pathRoll{from: []rune(info.branch), to: []rune(info.branch)} // fading out
	}
	g.drawRoll(dst, roll, x+iconW+gap, y, maxName, badgeText, alpha, now)
	n := len([]rune(truncate(string(roll.to), maxName)))
	if roll.rolling(now) {
		n = max(n, len([]rune(truncate(string(roll.from), maxName))))
	}
	return x + iconW + gap + float64(n)*f.cellW
}
