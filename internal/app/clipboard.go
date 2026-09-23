package app

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/informeai/doted/internal/jobs"
)

// doted keeps its own clipboard: text copied or cut from the input line, and
// what Ctrl+W and Ctrl+U delete (as in readline, where Ctrl+Y brings it
// back). It doesn't reach other apps yet: Ebiten has no clipboard API.

const flashDuration = 1500 * time.Millisecond

// clipboardChord reports the copy/cut/paste shortcut for key pressed this
// tick: Cmd+key on macOS, Ctrl+Shift+key elsewhere, since Ctrl+C and Ctrl+V
// alone belong to the terminal. Both work on every system.
func clipboardChord(key ebiten.Key) bool {
	meta := ebiten.IsKeyPressed(ebiten.KeyMeta)
	ctrlShift := ebiten.IsKeyPressed(ebiten.KeyControl) && ebiten.IsKeyPressed(ebiten.KeyShift)
	return (meta || ctrlShift) && inpututil.IsKeyJustPressed(key)
}

// handleClipboardKeys runs the copy, cut and paste shortcuts on the input
// line, reporting whether one was pressed.
func (g *Game) handleClipboardKeys() bool {
	switch {
	case clipboardChord(ebiten.KeyC):
		g.copySelection()
	case clipboardChord(ebiten.KeyX):
		g.cutSelection()
	case clipboardChord(ebiten.KeyV):
		g.paste()
	default:
		return false
	}
	g.touch()
	return true
}

// copySelection copies the text selected in the output, or else in the
// input line.
func (g *Game) copySelection() {
	text := g.selectedOutput()
	if text == "" {
		text = g.editor.SelectedText()
	}
	if text == "" {
		g.flash("nothing selected · shift+←→ selects")
		return
	}
	g.clipboard = text
	g.flash("copied " + describeText(text))
}

func (g *Game) cutSelection() {
	text := g.editor.Cut()
	if text == "" {
		g.flash("nothing selected · shift+←→ selects")
		return
	}
	g.clipboard = text
	g.flash("cut " + describeText(text))
	g.typed()
}

// paste types the clipboard at the cursor, replacing the selection.
func (g *Game) paste() {
	if g.clipboard == "" {
		g.flash("clipboard is empty")
		return
	}
	g.editor.Insert([]rune(singleLine(g.clipboard))...)
	g.typed()
}

// pasteTo types the clipboard into a running job, as if typed on its
// keyboard.
func (g *Game) pasteTo(j *jobs.Job) {
	if g.clipboard == "" {
		g.flash("clipboard is empty")
		return
	}
	j.Paste(g.clipboard)
	g.scroll = 0
	g.touch()
	g.typed()
}

// kill keeps text deleted by Ctrl+W or Ctrl+U for Ctrl+Y.
func (g *Game) kill(text string) {
	if text != "" {
		g.clipboard = text
	}
	g.typed()
}

// flash shows a short message on the status line.
func (g *Game) flash(msg string) {
	g.flashText, g.flashUntil = msg, time.Now().Add(flashDuration)
}

func describeText(s string) string {
	n := utf8.RuneCountInString(s)
	if n == 1 {
		return "1 character"
	}
	return fmt.Sprintf("%d characters", n)
}

// singleLine makes text safe for the one-line input: line breaks become
// spaces and other control characters are dropped.
func singleLine(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, s)
}
