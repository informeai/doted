package app

import (
	"fmt"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/informeai/doted/internal/jobs"
)

// Getting around the job strip from the keyboard.
//
// Ctrl+n opens job #n and Alt+n types to it, by the number on its card
// (the one fg takes), whether its card is in view or not; the strip scrolls
// to it. Alt+Left and Alt+Right select a card, scrolling the strip when it
// doesn't all fit. With a card selected, the keyboard reaches its actions:
// Enter opens it, Alt+S types to it, Alt+R restarts it, Alt+. stops it,
// and Esc (or any other key, which then goes on as usual) lets it go.

const quietAfter = 10 * time.Second // a job printing nothing this long may shrink to one line

// stripFocus is the card the strip keeps in view: the selected one, else
// the one being typed to.
func (g *Game) stripFocus() *jobs.Job {
	if g.stripSel != nil {
		return g.stripSel
	}
	return g.target
}

// stripJob is the job with a card numbered id, or nil.
func (g *Game) stripJob(id int) *jobs.Job {
	for _, j := range g.stripJobs(time.Now()) {
		if j.ID == id {
			return j
		}
	}
	return nil
}

// selectCard moves the selection dir cards along (-1 left, 1 right). With
// nothing selected, left starts from the newest card and right from the
// oldest.
func (g *Game) selectCard(dir int) {
	cards := g.stripJobs(time.Now())
	if len(cards) == 0 {
		return
	}
	i := -1
	for k, j := range cards {
		if j == g.stripSel {
			i = k
		}
	}
	switch {
	case i < 0 && dir < 0:
		i = len(cards) - 1
	case i < 0:
		i = 0
	default:
		i = min(max(0, i+dir), len(cards)-1)
	}
	g.stripSel = cards[i]
}

// handleStripKeys handles the strip's keys, reporting whether it took this
// tick's keyboard.
func (g *Game) handleStripKeys(now time.Time) bool {
	cards := g.stripJobs(now)
	if len(cards) == 0 || g.panel.open {
		g.stripSel = nil
		return false
	}
	if g.stripSel != nil && g.stripJob(g.stripSel.ID) != g.stripSel {
		g.stripSel = nil // its card has left
	}
	alt := ebiten.IsKeyPressed(ebiten.KeyAlt) && !ebiten.IsKeyPressed(ebiten.KeyControl) && !ebiten.IsKeyPressed(ebiten.KeyMeta)
	left, right := repeating(ebiten.KeyArrowLeft), repeating(ebiten.KeyArrowRight)
	if alt && (left || right) || g.stripSel != nil && (left || right) {
		if left {
			g.selectCard(-1)
		} else {
			g.selectCard(1)
		}
		ebiten.AppendInputChars(g.chars[:0]) // what Alt+arrow typed, if anything
		return true
	}
	j := g.stripSel
	if j == nil {
		return false
	}
	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeyEnter), inpututil.IsKeyJustPressed(ebiten.KeyNumpadEnter):
		g.stripSel = nil
		g.openJob(j)
	case inpututil.IsKeyJustPressed(ebiten.KeyEscape):
		g.stripSel = nil
	case alt && inpututil.IsKeyJustPressed(ebiten.KeyS):
		g.stripSel = nil
		g.enterTarget(j)
	case alt && inpututil.IsKeyJustPressed(ebiten.KeyR):
		g.restartJob(j)
	case alt && inpututil.IsKeyJustPressed(ebiten.KeyPeriod):
		g.stopJob(j, now)
	default:
		// Any other key lets the card go and does what it always does.
		if len(inpututil.AppendJustPressedKeys(nil)) > 0 && !onlyModifiers() {
			g.stripSel = nil
		}
		return false
	}
	ebiten.AppendInputChars(g.chars[:0]) // Alt+letters type symbols on macOS; drop them
	return true
}

// onlyModifiers reports whether the keys pressed this tick are all
// modifiers, which don't end a selection.
func onlyModifiers() bool {
	for _, k := range inpututil.AppendJustPressedKeys(nil) {
		switch k {
		case ebiten.KeyAlt, ebiten.KeyAltLeft, ebiten.KeyAltRight, ebiten.KeyShift, ebiten.KeyShiftLeft, ebiten.KeyShiftRight,
			ebiten.KeyControl, ebiten.KeyControlLeft, ebiten.KeyControlRight, ebiten.KeyMeta, ebiten.KeyMetaLeft, ebiten.KeyMetaRight:
		default:
			return false
		}
	}
	return true
}

// scrollStrip scrolls the strip by one card's width towards dir.
func (g *Game) scrollStrip(dir int) {
	if g.faces == nil {
		return
	}
	g.stripScrollTo += float64(dir) * float64(stripMinCols+stripGapCols) * g.faces.cellW
	g.stripScrolledAway = g.stripScrollTo < g.stripMaxScroll-1
}

// selectionHint is the status line while a card is selected.
func (g *Game) selectionHint() string {
	return fmt.Sprintf("#%d %s · enter open · alt+s send · alt+r restart · alt+. stop · esc", g.stripSel.ID, g.watchOf(g.stripSel).name)
}
