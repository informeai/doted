package terminal

import (
	"slices"
	"strings"
	"unicode"
)

// Editor is the single-line input at the bottom of the screen, with a cursor,
// a selection and shell-like command history.
type Editor struct {
	buf    []rune
	cursor int

	// The selection runs between anchor and the cursor while selecting.
	selecting bool
	anchor    int

	history      []string
	historyLimit int    // most lines kept; 0 is no limit
	histPos      int    // == len(history) while editing a fresh line
	draft        []rune // the fresh line, kept while browsing history
}

// SetHistory replaces the history (oldest first) and caps it at limit lines
// from now on (0 for no limit).
func (e *Editor) SetHistory(lines []string, limit int) {
	e.history = slices.Clone(lines)
	e.historyLimit = limit
	e.trimHistory()
	e.histPos = len(e.history)
}

func (e *Editor) trimHistory() {
	if over := len(e.history) - e.historyLimit; e.historyLimit > 0 && over > 0 {
		e.history = slices.Delete(e.history, 0, over)
	}
}

func (e *Editor) Text() string { return string(e.buf) }

func (e *Editor) Cursor() int { return e.cursor }

func (e *Editor) Empty() bool { return len(e.buf) == 0 }

// AtEnd reports whether the cursor is at the end of the line, with nothing
// selected: where a suggestion can continue the line.
func (e *Editor) AtEnd() bool {
	_, _, selecting := e.Selection()
	return e.cursor == len(e.buf) && !selecting
}

// History returns the submitted lines, oldest first. The slice is shared:
// don't modify it.
func (e *Editor) History() []string { return e.history }

// Selection returns the selected range [start, end) of the line, if any.
func (e *Editor) Selection() (start, end int, ok bool) {
	if !e.selecting || e.anchor == e.cursor {
		return 0, 0, false
	}
	return min(e.anchor, e.cursor), max(e.anchor, e.cursor), true
}

// SelectedText is the text in the selection, or "" without one.
func (e *Editor) SelectedText() string {
	start, end, ok := e.Selection()
	if !ok {
		return ""
	}
	return string(e.buf[start:end])
}

// deleteSelection removes the selected text, reporting whether there was any.
func (e *Editor) deleteSelection() bool {
	start, end, ok := e.Selection()
	e.selecting = false
	if !ok {
		return false
	}
	e.buf = slices.Delete(e.buf, start, end)
	e.cursor = start
	return true
}

// Cut removes the selection and returns its text ("" without one).
func (e *Editor) Cut() string {
	text := e.SelectedText()
	e.deleteSelection()
	return text
}

// Insert types rs at the cursor, replacing the selection.
func (e *Editor) Insert(rs ...rune) {
	e.deleteSelection()
	e.buf = slices.Insert(e.buf, e.cursor, rs...)
	e.cursor += len(rs)
}

// Backspace deletes the selection, or the rune before the cursor.
func (e *Editor) Backspace() {
	if e.deleteSelection() || e.cursor == 0 {
		return
	}
	e.buf = slices.Delete(e.buf, e.cursor-1, e.cursor)
	e.cursor--
}

// Delete deletes the selection, or the rune under the cursor.
func (e *Editor) Delete() {
	if e.deleteSelection() || e.cursor == len(e.buf) {
		return
	}
	e.buf = slices.Delete(e.buf, e.cursor, e.cursor+1)
}

// DeleteWordBackward removes the selection, or the word before the cursor
// (Ctrl+W), and returns the removed text.
func (e *Editor) DeleteWordBackward() string {
	if cut := e.Cut(); cut != "" {
		return cut
	}
	i := e.cursor
	for i > 0 && unicode.IsSpace(e.buf[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(e.buf[i-1]) {
		i--
	}
	removed := string(e.buf[i:e.cursor])
	e.buf = slices.Delete(e.buf, i, e.cursor)
	e.cursor = i
	return removed
}

// KillToStart removes the selection, or everything before the cursor
// (Ctrl+U), and returns the removed text.
func (e *Editor) KillToStart() string {
	if cut := e.Cut(); cut != "" {
		return cut
	}
	removed := string(e.buf[:e.cursor])
	e.buf = slices.Delete(e.buf, 0, e.cursor)
	e.cursor = 0
	return removed
}

// moveTo puts the cursor at i. With extend (Shift held) the selection grows
// or shrinks with it; otherwise any selection is dropped.
func (e *Editor) moveTo(i int, extend bool) {
	if extend && !e.selecting {
		e.selecting, e.anchor = true, e.cursor
	}
	if !extend {
		e.selecting = false
	}
	e.cursor = max(0, min(i, len(e.buf)))
}

// Left moves one rune left. Without extend, a selection collapses to its
// start instead.
func (e *Editor) Left(extend bool) {
	if start, _, ok := e.Selection(); ok && !extend {
		e.moveTo(start, false)
		return
	}
	e.moveTo(e.cursor-1, extend)
}

// Right moves one rune right. Without extend, a selection collapses to its
// end instead.
func (e *Editor) Right(extend bool) {
	if _, end, ok := e.Selection(); ok && !extend {
		e.moveTo(end, false)
		return
	}
	e.moveTo(e.cursor+1, extend)
}

func (e *Editor) Home(extend bool) { e.moveTo(0, extend) }

func (e *Editor) End(extend bool) { e.moveTo(len(e.buf), extend) }

// Reset clears the line without recording it in history.
func (e *Editor) Reset() {
	e.buf = e.buf[:0]
	e.cursor = 0
	e.selecting = false
	e.histPos = len(e.history)
	e.draft = nil
}

// Submit returns the current line, records it in history and clears the
// input. As in bash's HISTCONTROL=ignorespace, a line starting with a space
// stays out of the history.
func (e *Editor) Submit() string {
	line := string(e.buf)
	if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, " ") &&
		(len(e.history) == 0 || e.history[len(e.history)-1] != line) {
		e.history = append(e.history, line)
		e.trimHistory()
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
	e.selecting = false
}
