package terminal

import (
	"slices"
	"unicode"
)

// Editor is the single-line input at the bottom of the screen, with a cursor
// and shell-like command history.
type Editor struct {
	buf    []rune
	cursor int

	history []string
	histPos int    // == len(history) while editing a fresh line
	draft   []rune // the fresh line, kept while browsing history
}

func (e *Editor) Text() string { return string(e.buf) }

func (e *Editor) Cursor() int { return e.cursor }

func (e *Editor) Empty() bool { return len(e.buf) == 0 }

func (e *Editor) Insert(rs ...rune) {
	e.buf = slices.Insert(e.buf, e.cursor, rs...)
	e.cursor += len(rs)
}

func (e *Editor) Backspace() {
	if e.cursor == 0 {
		return
	}
	e.buf = slices.Delete(e.buf, e.cursor-1, e.cursor)
	e.cursor--
}

func (e *Editor) Delete() {
	if e.cursor == len(e.buf) {
		return
	}
	e.buf = slices.Delete(e.buf, e.cursor, e.cursor+1)
}

// DeleteWordBackward removes the word before the cursor (Ctrl+W).
func (e *Editor) DeleteWordBackward() {
	i := e.cursor
	for i > 0 && unicode.IsSpace(e.buf[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(e.buf[i-1]) {
		i--
	}
	e.buf = slices.Delete(e.buf, i, e.cursor)
	e.cursor = i
}

// KillToStart removes everything before the cursor (Ctrl+U).
func (e *Editor) KillToStart() {
	e.buf = slices.Delete(e.buf, 0, e.cursor)
	e.cursor = 0
}

func (e *Editor) Left() {
	if e.cursor > 0 {
		e.cursor--
	}
}

func (e *Editor) Right() {
	if e.cursor < len(e.buf) {
		e.cursor++
	}
}

func (e *Editor) Home() { e.cursor = 0 }

func (e *Editor) End() { e.cursor = len(e.buf) }

// Reset clears the line without recording it in history.
func (e *Editor) Reset() {
	e.buf = e.buf[:0]
	e.cursor = 0
	e.histPos = len(e.history)
	e.draft = nil
}

// Submit returns the current line, records it in history and clears the input.
func (e *Editor) Submit() string {
	line := string(e.buf)
	if line != "" && (len(e.history) == 0 || e.history[len(e.history)-1] != line) {
		e.history = append(e.history, line)
	}
	e.Reset()
	return line
}

// HistoryPrev replaces the line with the previous history entry (Up).
func (e *Editor) HistoryPrev() {
	if e.histPos == 0 {
		return
	}
	if e.histPos == len(e.history) {
		e.draft = append([]rune(nil), e.buf...)
	}
	e.histPos--
	e.load([]rune(e.history[e.histPos]))
}

// HistoryNext moves forward in history, back to the draft at the end (Down).
func (e *Editor) HistoryNext() {
	if e.histPos == len(e.history) {
		return
	}
	e.histPos++
	if e.histPos == len(e.history) {
		e.load(e.draft)
		return
	}
	e.load([]rune(e.history[e.histPos]))
}

func (e *Editor) load(rs []rune) {
	e.buf = append(e.buf[:0], rs...)
	e.cursor = len(e.buf)
}
