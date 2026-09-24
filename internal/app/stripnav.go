package app

import (
	"fmt"
	"slices"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/informeai/doted/internal/jobs"
)

// Getting around the job strip from the keyboard.
//
// The strip shows the first strip_cards jobs; the ones after them wait in
// a group card on the right. Alt+Left and Alt+Right move a selection along
// the cards and on to the group card, and selecting the group opens it:
// its jobs take the strip's place, behind the group card now on the left,
// and the selection goes on through them. Esc, or moving back out of the
// group, closes it and the strip is as it was.
//
// With a card selected, it shows more of its output and the keyboard
// reaches its actions: Enter opens the job, Alt+S types to it, Alt+R
// restarts it, Alt+. stops it, and Esc (or any other key, which then goes
// on as usual) lets it go. Ctrl+n opens job #n and Alt+n types to it, by
// the number on its card (the one fg takes), whether it shows or is
// grouped.

const (
	quietAfter    = 10 * time.Second // a job printing nothing this long may shrink to one line
	selectedLines = 5                // lines the selected card shows
)

// stripFocus is the card the strip keeps in view: the selected one, else
// the one being typed to.
func (g *Game) stripFocus() *jobs.Job {
	if g.stripSel != nil {
		return g.stripSel
	}
	return g.target
}

// stripGroups splits the cards into those shown and those in the group
// card: normally the first strip_cards jobs show and the rest are grouped;
// while the group is open (selected, or holding the card in focus), its
// jobs show instead.
func (g *Game) stripGroups(now time.Time) (shown, grouped []*jobs.Job, open bool) {
	cards := g.stripJobs(now)
	limit := g.cfg.Jobs.StripCards
	if len(cards) <= limit {
		return cards, nil, false
	}
	first, rest := cards[:limit], cards[limit:]
	if g.groupSel || slices.Contains(rest, g.stripFocus()) {
		return rest, rest, true
	}
	return first, rest, false
}

// inFirstCards reports whether j is one of the jobs that show with the
// group closed.
func (g *Game) inFirstCards(j *jobs.Job) bool {
	cards := g.stripJobs(time.Now())
	return slices.Index(cards, j) < g.cfg.Jobs.StripCards
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

// navItem is a stop for the selection: a job's card, or the group card
// (job nil).
type navItem struct{ job *jobs.Job }

// navItems are the selection's stops in order: the first cards, the group
// card, then the grouped jobs.
func (g *Game) navItems(now time.Time) []navItem {
	cards := g.stripJobs(now)
	limit := g.cfg.Jobs.StripCards
	var items []navItem
	for i, j := range cards {
		if i == limit {
			items = append(items, navItem{}) // the group card
		}
		items = append(items, navItem{job: j})
	}
	return items
}

// selectCard moves the selection dir stops along (-1 left, 1 right). With
// nothing selected, left starts from the last stop in view and right from
// the first.
func (g *Game) selectCard(dir int) {
	now := time.Now()
	items := g.navItems(now)
	if len(items) == 0 {
		return
	}
	i := -1
	for k, it := range items {
		if it.job == nil && g.groupSel || it.job != nil && it.job == g.stripSel {
			i = k
		}
	}
	switch {
	case i < 0 && dir < 0:
		// The last one in view: the group card when there's one.
		i = min(len(items)-1, g.cfg.Jobs.StripCards)
	case i < 0:
		i = 0
	default:
		i = min(max(0, i+dir), len(items)-1)
	}
	g.stripSel, g.groupSel = items[i].job, items[i].job == nil
}

// clearSelection lets the selection go, closing the group if it was open.
func (g *Game) clearSelection() { g.stripSel, g.groupSel = nil, false }

// handleStripKeys handles the strip's keys, reporting whether it took this
// tick's keyboard.
func (g *Game) handleStripKeys(now time.Time) bool {
	cards := g.stripJobs(now)
	if len(cards) == 0 || g.panel.open {
		g.clearSelection()
		return false
	}
	if g.stripSel != nil && g.stripJob(g.stripSel.ID) != g.stripSel {
		g.stripSel = nil // its card has left
	}
	if _, grouped, _ := g.stripGroups(now); len(grouped) == 0 {
		g.groupSel = false // nothing is grouped anymore
	}
	alt := ebiten.IsKeyPressed(ebiten.KeyAlt) && !ebiten.IsKeyPressed(ebiten.KeyControl) && !ebiten.IsKeyPressed(ebiten.KeyMeta)
	left, right := repeating(ebiten.KeyArrowLeft), repeating(ebiten.KeyArrowRight)
	selected := g.stripSel != nil || g.groupSel
	if alt && (left || right) || selected && (left || right) {
		if left {
			g.selectCard(-1)
		} else {
			g.selectCard(1)
		}
		ebiten.AppendInputChars(g.chars[:0]) // what Alt+arrow typed, if anything
		return true
	}
	if !selected {
		return false
	}
	enter := inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeyNumpadEnter)
	j := g.stripSel
	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeyEscape):
		g.clearSelection()
	case g.groupSel && enter:
		g.selectCard(1) // into the group, at its first job
	case g.groupSel:
		if len(inpututil.AppendJustPressedKeys(nil)) > 0 && !onlyModifiers() {
			g.clearSelection()
		}
		return false
	case enter:
		g.clearSelection()
		g.openJob(j)
	case alt && inpututil.IsKeyJustPressed(ebiten.KeyS):
		g.clearSelection()
		g.enterTarget(j)
	case alt && inpututil.IsKeyJustPressed(ebiten.KeyR):
		g.restartJob(j)
	case alt && inpututil.IsKeyJustPressed(ebiten.KeyPeriod):
		g.stopJob(j, now)
	default:
		// Any other key lets the card go and does what it always does.
		if len(inpututil.AppendJustPressedKeys(nil)) > 0 && !onlyModifiers() {
			g.clearSelection()
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

// selectionHint is the status line while a card, or the group, is
// selected.
func (g *Game) selectionHint() string {
	if g.groupSel {
		_, grouped, _ := g.stripGroups(time.Now())
		return fmt.Sprintf("%d grouped %s · → or enter to go through them · esc closes", len(grouped), plural(len(grouped), "job", "jobs"))
	}
	return fmt.Sprintf("#%d %s · enter open · alt+s send · alt+r restart · alt+. stop · esc", g.stripSel.ID, g.watchOf(g.stripSel).name)
}
