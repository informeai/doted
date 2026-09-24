package app

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/informeai/doted/internal/jobs"
	"github.com/informeai/doted/internal/notify"
	"github.com/informeai/doted/internal/terminal"
)

// Each command run from the main view is a block: its echo line and the
// output below it, up to the next command. The echo line shows how the
// command went (a colored dot, how long it took, why it failed) and, under
// the mouse, actions to copy the block's output or run the command again.
// Clicking the prompt bar at its start folds the output away.

type block struct {
	cmd        string
	job        *jobs.Job
	start, end time.Time
	status     string // why it failed, "" on success
	killed     bool
	background bool // started with & or sent there with Ctrl+B
	collapsed  bool
}

func (b *block) running() bool { return b.end.IsZero() }

// actionKind is what clicking a spot in a block does.
type actionKind int

const (
	actToggle actionKind = iota // fold or unfold the output
	actCopy
	actRerun
)

// blockAction is a clickable spot drawn in the last frame.
type blockAction struct {
	kind           actionKind
	seq            int // the block's command line
	x0, x1, y0, y1 float64
}

// startBlock opens a block for a command just echoed as the newest line.
func (g *Game) startBlock(cmd string, j *jobs.Job, background bool, now time.Time) {
	if g.blocks == nil {
		g.blocks = map[int]*block{}
	}
	// Forget blocks whose lines were cleared or trimmed.
	for seq := range g.blocks {
		if _, ok := g.scrollback.Index(seq); !ok {
			delete(g.blocks, seq)
		}
	}
	seq := g.scrollback.Seq(g.scrollback.Len() - 1)
	g.blocks[seq] = &block{cmd: cmd, job: j, start: now, background: background}
}

// endBlock records how the job's command finished.
func (g *Game) endBlock(j *jobs.Job, now time.Time) {
	g.context.stale = true // the command may have switched branches or changed files
	for _, b := range g.blocks {
		if b.job == j && b.running() {
			b.end, b.status, b.killed = now, j.Status, j.Killed
			if !b.background {
				g.lastDuration = b.end.Sub(b.start)
			}
			g.notifyFinished(b)
		}
	}
}

func (g *Game) blockOf(j *jobs.Job) *block {
	for _, b := range g.blocks {
		if b.job == j {
			return b
		}
	}
	return nil
}

// seqRange is a run of line numbers [from, to).
type seqRange struct{ from, to int }

// foldedRanges lists the output lines hidden by folded blocks in sb.
func (g *Game) foldedRanges(sb *terminal.Scrollback) []seqRange {
	if sb != g.scrollback {
		return nil
	}
	var out []seqRange
	for seq, b := range g.blocks {
		if !b.collapsed {
			continue
		}
		i, ok := sb.Index(seq)
		if !ok {
			continue
		}
		end := i + 1
		for end < sb.Len() && sb.At(end).Kind != terminal.Command {
			end++
		}
		out = append(out, seqRange{seq + 1, sb.Seq(end)})
	}
	return out
}

func hidden(seq int, ranges []seqRange) bool {
	for _, r := range ranges {
		if seq >= r.from && seq < r.to {
			return true
		}
	}
	return false
}

// lineRows is how many rows line i of sb takes on screen: none when folded
// away, and one more under a folded command for its "… N lines" row.
func (g *Game) lineRows(sb *terminal.Scrollback, i int, ranges []seqRange) int {
	seq := sb.Seq(i)
	if hidden(seq, ranges) {
		return 0
	}
	n := terminal.RowCount(len(sb.At(i).Cells), g.cols)
	if b := g.blockAtSeq(sb, seq); b != nil && b.collapsed {
		n++
	}
	return n
}

// totalRows is how many rows sb takes on screen.
func (g *Game) totalRows(sb *terminal.Scrollback) int {
	ranges := g.foldedRanges(sb)
	n := 0
	for i := range sb.Len() {
		n += g.lineRows(sb, i, ranges)
	}
	return n
}

func (g *Game) blockAtSeq(sb *terminal.Scrollback, seq int) *block {
	if sb != g.scrollback {
		return nil
	}
	return g.blocks[seq]
}

// foldedCount is how many lines a folded block hides.
func foldedCount(ranges []seqRange, seq int) int {
	for _, r := range ranges {
		if r.from == seq+1 {
			return r.to - r.from
		}
	}
	return 0
}

// blockOutput is the output of the block at seq as text, without trailing
// blank lines.
func (g *Game) blockOutput(seq int) string {
	i, ok := g.scrollback.Index(seq)
	if !ok {
		return ""
	}
	var lines []string
	for j := i + 1; j < g.scrollback.Len() && g.scrollback.At(j).Kind != terminal.Command; j++ {
		lines = append(lines, strings.TrimRight(g.scrollback.At(j).Text(), " "))
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// runBlockAction does what a click on a block's spot asks.
func (g *Game) runBlockAction(a blockAction) {
	b := g.blocks[a.seq]
	if b == nil {
		return
	}
	switch a.kind {
	case actToggle:
		b.collapsed = !b.collapsed
		g.scrollBy(0) // keep the scroll within the new height
	case actCopy:
		text := g.blockOutput(a.seq)
		if text == "" {
			g.flash("that command printed nothing")
			return
		}
		g.clipboard = text
		g.writeSystemClipboard(text)
		g.flash("copied the output of " + truncate(b.cmd, 30))
	case actRerun:
		if g.attached != nil || g.viewing != nil {
			g.flash("a command is running · wait or ctrl+b first")
			return
		}
		g.editor.Reset()
		g.editor.Insert([]rune(b.cmd)...)
		g.submit()
	}
}

// drawBlockHeader decorates a block's command row drawn at (x, y): the
// result on the right, or the actions while the mouse is over the row.
// cmdLen is the command line's length in cells.
func (g *Game) drawBlockHeader(dst *ebiten.Image, b *block, seq, cmdLen int, x, y float64, now time.Time) {
	f := g.faces
	right := x + float64(g.cols)*f.cellW
	mx, my := ebiten.CursorPosition()
	hover := float64(my) >= y && float64(my) < y+f.lineH && float64(mx) >= x && float64(mx) < right

	// The prompt bar at the start of the row folds the block.
	g.actions = append(g.actions, blockAction{actToggle, seq, x, x + 2*f.cellW, y, y + f.lineH})
	if b.collapsed {
		chevron := x + f.cellW*0.9
		g.drawText(dst, "›", chevron, y, g.theme.Accent, 1)
	}

	if hover {
		// Actions, right-aligned: "copy  rerun".
		labels := []struct {
			text string
			kind actionKind
		}{{"copy", actCopy}, {"rerun", actRerun}}
		width := 0
		for i, l := range labels {
			width += utf8.RuneCountInString(l.text)
			if i > 0 {
				width += 2
			}
		}
		if cmdLen+2+width > g.cols {
			return
		}
		ax := right - float64(width)*f.cellW
		for _, l := range labels {
			w := float64(utf8.RuneCountInString(l.text)) * f.cellW
			over := float64(mx) >= ax && float64(mx) < ax+w
			clr := g.theme.Muted
			if over {
				clr = g.theme.Accent
			}
			g.drawText(dst, l.text, ax, y, clr, 1)
			if over {
				uy := float32(y + f.underlineY)
				vector.StrokeLine(dst, float32(ax), uy, float32(ax+w), uy, float32(g.scale), clr, false)
			}
			g.actions = append(g.actions, blockAction{l.kind, seq, ax, ax + w, y, y + f.lineH})
			ax += w + 2*f.cellW
		}
		return
	}

	text, dot := g.blockBadge(b, now)
	n := utf8.RuneCountInString(text)
	if cmdLen+4+n > g.cols {
		return // no room beside the command
	}
	tx := right - float64(n)*f.cellW
	g.drawText(dst, text, tx, y, g.theme.Muted, 1)
	r := f.cellW * 0.22
	if b.running() && !b.background && g.cfg.Animation.Enabled {
		r *= 0.8 + 0.3*(math.Sin(float64(now.UnixMilli())/1000*2*math.Pi)+1)/2
	}
	vector.FillCircle(dst, float32(tx-f.cellW), float32(y+f.lineH/2), float32(r), dot, true)
}

// blockBadge is the result shown on a block's command row, and its dot color.
func (g *Game) blockBadge(b *block, now time.Time) (string, color.RGBA) {
	switch {
	case b.running() && b.background:
		return fmt.Sprintf("#%d in the background", b.job.ID), g.theme.Muted
	case b.running():
		return "running " + formatDuration(now.Sub(b.start)), g.theme.Accent
	case b.killed:
		return "killed · " + formatDuration(b.end.Sub(b.start)), g.theme.Error
	case b.status != "":
		return strings.Replace(b.status, "exit status ", "exit ", 1) + " · " + formatDuration(b.end.Sub(b.start)), g.theme.Error
	}
	return formatDuration(b.end.Sub(b.start)), g.theme.ANSI[2]
}

// formatDuration shows tenths of a second for short runs.
func formatDuration(d time.Duration) string {
	if d < 10*time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return formatElapsed(d)
}

// drawFoldedRow draws the "… N lines" row under a folded block's command.
func (g *Game) drawFoldedRow(dst *ebiten.Image, seq, count int, x, y float64) {
	f := g.faces
	text := fmt.Sprintf("… %d %s", count, plural(count, "line", "lines"))
	g.drawText(dst, text, x+2*f.cellW, y, g.theme.Muted, dimAlpha)
	g.actions = append(g.actions, blockAction{actToggle, seq, x, x + float64(g.cols)*f.cellW, y, y + f.lineH})
}

// actionAt finds a clickable block spot at (x, y) in the last frame.
func (g *Game) actionAt(x, y float64) (blockAction, bool) {
	for _, a := range g.actions {
		if x >= a.x0 && x < a.x1 && y >= a.y0 && y < a.y1 {
			return a, true
		}
	}
	return blockAction{}, false
}

// notifyFinished tells the desktop that a long command finished while doted
// was in the background.
func (g *Game) notifyFinished(b *block) {
	took := b.end.Sub(b.start)
	if !g.cfg.Notify.Enabled || g.focused || took < time.Duration(g.cfg.Notify.AfterSeconds)*time.Second {
		return
	}
	result := "done"
	switch {
	case b.killed:
		result = "killed"
	case b.status != "":
		result = strings.Replace(b.status, "exit status ", "exit ", 1)
	}
	g.notifier("doted · "+result+" after "+formatDuration(took), truncate(b.cmd, 120))
}

func sendNotification(title, body string) {
	go notify.Send(title, body) //nolint:errcheck // a missed notification is not worth an error
}
