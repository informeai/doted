package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/informeai/doted/internal/clipboard"
	"github.com/informeai/doted/internal/jobs"
)

// Copy and paste use the system clipboard, so text goes to and comes from
// other apps. doted also keeps its own copy: it's what Ctrl+Y brings back
// (with what Ctrl+W and Ctrl+U delete, as in readline), and what Cmd+V falls
// back to when the system clipboard can't be reached. Talking to the system
// can take a moment (it runs a helper on macOS and Linux), so it happens in
// the background and never holds up a frame.

// systemClipboard is the system clipboard; tests swap in a fake.
type systemClipboard interface {
	Read(ctx context.Context) (string, error)
	Write(ctx context.Context, text string) error
}

type realClipboard struct{}

func (realClipboard) Read(ctx context.Context) (string, error) { return clipboard.Read(ctx) }
func (realClipboard) Write(ctx context.Context, text string) error {
	return clipboard.Write(ctx, text)
}

const clipboardTimeout = 2 * time.Second

// clipboardEvent is the outcome of a background clipboard call, handled by
// Update: a read to paste somewhere, or a failed write to report.
type clipboardEvent struct {
	read   bool
	text   string
	err    error
	target *jobs.Job // where a read pastes: a job, or the input line if nil
}

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
	g.writeSystemClipboard(text)
}

func (g *Game) cutSelection() {
	text := g.editor.Cut()
	if text == "" {
		g.flash("nothing selected · shift+←→ selects")
		return
	}
	g.clipboard = text
	g.flash("cut " + describeText(text))
	g.writeSystemClipboard(text)
	g.typed()
}

// paste pastes the system clipboard at the input's cursor (Cmd+V),
// replacing the selection, once it has been read.
func (g *Game) paste() { g.readSystemClipboard(nil) }

// pasteTo pastes the system clipboard into a running job, once read.
func (g *Game) pasteTo(j *jobs.Job) { g.readSystemClipboard(j) }

// yank pastes doted's own clipboard at the cursor (Ctrl+Y): the last text
// copied, cut or deleted with Ctrl+W or Ctrl+U.
func (g *Game) yank() { g.pasteText(nil, g.clipboard) }

func (g *Game) writeSystemClipboard(text string) {
	if !g.cfg.Clipboard.System {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), clipboardTimeout)
		defer cancel()
		if err := g.system.Write(ctx, text); err != nil {
			g.clipboardEvents <- clipboardEvent{err: err}
		}
	}()
}

func (g *Game) readSystemClipboard(target *jobs.Job) {
	if !g.cfg.Clipboard.System {
		g.pasteText(target, g.clipboard)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), clipboardTimeout)
		defer cancel()
		text, err := g.system.Read(ctx)
		g.clipboardEvents <- clipboardEvent{read: true, text: text, err: err, target: target}
	}()
}

// handleClipboardEvents finishes background clipboard calls. Run by Update.
func (g *Game) handleClipboardEvents() {
	for {
		select {
		case ev := <-g.clipboardEvents:
			switch {
			case !ev.read:
				g.flash("copied inside doted only: " + clipboardError(ev.err))
			case ev.err != nil:
				g.flash("system clipboard: " + clipboardError(ev.err) + " · pasted doted's own")
				g.pasteText(ev.target, g.clipboard)
			default:
				g.pasteText(ev.target, ev.text)
			}
		default:
			return
		}
	}
}

func clipboardError(err error) string {
	if errors.Is(err, clipboard.ErrUnavailable) {
		return "no clipboard tool (install wl-clipboard, xclip or xsel)"
	}
	return err.Error()
}

// pasteText pastes text into target, a running job, or the input line when
// target is nil. A paste that arrives after the keyboard moved elsewhere is
// dropped rather than landing somewhere unexpected.
func (g *Game) pasteText(target *jobs.Job, text string) {
	if text == "" {
		g.flash("clipboard is empty")
		return
	}
	if target != nil {
		if !target.Running() || (target != g.attached && target != g.viewing) {
			return
		}
		target.Paste(text)
		g.scroll = 0
	} else {
		if g.attached != nil || g.viewing != nil {
			return
		}
		g.editor.Insert([]rune(singleLine(text))...)
	}
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
