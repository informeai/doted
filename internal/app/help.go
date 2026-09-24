package app

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// helpHint is the status line's resting hint, pointing at the help panel.
const helpHint = "type help for commands"

type helpCommand struct {
	usage  string // how it's typed, as listed
	insert string // what picking it puts on the prompt; empty for nothing
	desc   string
}

// helpCommands are doted's builtins plus the one syntax it adds. cd, clear
// and the rest are the shell's own.
var helpCommands = []helpCommand{
	{"jobs", "jobs", "list background jobs"},
	{"fg [n]", "fg ", "open job n, or the newest one"},
	{"<command> &", "", "start a command in the background"},
	{"help", "help", "show this list"},
	{"exit", "exit", "quit doted"},
}

var helpKeys = []struct{ keys, desc string }{
	{"enter · ↑↓", "run the line · browse history"},
	{"ctrl+r", "search the history"},
	{"cmd+f", "search the output (ctrl+shift+f on linux/windows)"},
	{"cmd+click", "open a URL or file:line in the output (ctrl+click on linux/windows)"},
	{"click the bar", "fold a command's output · hover it to copy or rerun"},
	{"tab · →", "accept the suggestion shown after the cursor"},
	{"shift+←→", "select text (also shift+home/end); typing replaces it"},
	{"mouse drag", "select output (double-click a word, triple-click a line)"},
	{"cmd+c · x · v", "copy · cut · paste, with other apps too (ctrl+shift+c/x/v on linux/windows)"},
	{"ctrl+y", "paste back what ctrl+w or ctrl+u deleted"},
	{"ctrl+c", "interrupt the running command, or discard the line"},
	{"ctrl+b", "send the running command to the background"},
	{"ctrl+t", "list background jobs"},
	{"ctrl+number", "open the job with that number, from its card at the top (hold ctrl for more digits: 1 then 2 is #12)"},
	{"alt+number", "type to that job without opening it (more digits the same way) · esc returns"},
	{"alt+← →", "select a card at the top, grouped ones too: enter open · alt+s send · alt+r restart · alt+. stop"},
	{"ctrl+l", "clear the screen"},
	{"pgup · pgdn", "scroll the output"},
}

func (g *Game) openHelp() {
	g.panel = panel{open: true, kind: panelHelp}
}

func (g *Game) handleHelpKeys() {
	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeyEscape):
		g.panel.open = false
	case repeating(ebiten.KeyArrowUp):
		if g.panel.scroll > 0 {
			g.panel.scroll--
		} else {
			g.panel.selected = max(0, g.panel.selected-1)
		}
	case repeating(ebiten.KeyArrowDown):
		// Past the last command, keep going to reveal the shortcuts below
		// when the window is too short to show them all.
		if g.panel.selected < len(helpCommands)-1 {
			g.panel.selected++
		} else if _, _, more := g.helpWindow(); more {
			g.panel.scroll++
		}
	case inpututil.IsKeyJustPressed(ebiten.KeyEnter), inpututil.IsKeyJustPressed(ebiten.KeyNumpadEnter):
		g.pickHelp(g.panel.selected)
	}
}

// pickHelp closes the panel and puts the chosen command on the prompt, ready
// to be completed and run.
func (g *Game) pickHelp(i int) {
	g.panel.open = false
	if insert := helpCommands[i].insert; insert != "" {
		g.editor.Reset()
		g.editor.Insert([]rune(insert)...)
		g.touch()
	}
}

// helpLine is one row of the help panel; cmd is the index into helpCommands,
// or -1 for rows that can't be selected.
type helpLine struct {
	left, right string
	cmd         int
	heading     bool
}

func helpLines() []helpLine {
	var lines []helpLine
	for i, c := range helpCommands {
		lines = append(lines, helpLine{left: c.usage, right: c.desc, cmd: i})
	}
	lines = append(lines, helpLine{cmd: -1}, helpLine{left: "shortcuts", cmd: -1, heading: true})
	for _, k := range helpKeys {
		lines = append(lines, helpLine{left: k.keys, right: k.desc, cmd: -1})
	}
	return lines
}

// helpWindow picks which help lines fit: rows of them starting at first, and
// whether more follow below.
func (g *Game) helpWindow() (first, rows int, more bool) {
	lines := helpLines()
	rows = min(len(lines), max(1, g.outputRows-1))
	// Command lines come first, in helpCommands order, so the selected
	// command is line g.panel.selected.
	first = max(0, g.panel.selected-rows+1, g.panel.scroll)
	first = min(first, len(lines)-rows)
	return first, rows, first+rows < len(lines)
}

func (g *Game) drawHelpPanel(dst *ebiten.Image, left, right, bottom float64) {
	f := g.faces
	lines := helpLines()
	first, rows, more := g.helpWindow()
	keys := "↑↓ select · enter use · esc close"
	if more {
		keys += " · ↓ more"
	}
	x, y, cols := g.drawPanelFrame(dst, left, right, bottom, rows, "help", keys)

	leftW := 0
	for _, l := range lines {
		leftW = max(leftW, len([]rune(l.left)))
	}
	for _, l := range lines[first:min(len(lines), first+rows)] {
		switch {
		case l.heading:
			g.drawText(dst, l.left, x, y, g.theme.Accent, 1)
		case l.cmd >= 0:
			if l.cmd == g.panel.selected {
				vector.FillRect(dst, float32(left+g.scale), float32(y), float32(right-left-2*g.scale), float32(f.lineH), scaleAlpha(g.theme.Accent, 0.18), false)
			}
			g.drawText(dst, truncate(l.left, cols), x, y, g.theme.Foreground, 1)
		default:
			g.drawText(dst, truncate(l.left, cols), x, y, g.theme.Foreground, dimAlpha)
		}
		if rest := cols - leftW - 2; l.right != "" && rest > 4 {
			g.drawText(dst, truncate(l.right, rest), x+float64(leftW+2)*f.cellW, y, g.theme.Muted, 1)
		}
		y += f.lineH
	}
}

// drawPanelFrame draws an overlay box with rows lines of content below a
// title row, its bottom edge at bottom. It returns where content starts and
// how many text columns fit.
func (g *Game) drawPanelFrame(dst *ebiten.Image, left, right, bottom float64, rows int, title, keys string) (x, y float64, cols int) {
	f := g.faces
	height := float64(rows+1)*f.lineH + f.lineH/2
	top := bottom - height
	vector.FillRect(dst, float32(left), float32(top), float32(right-left), float32(height), g.theme.Background, false)
	vector.StrokeRect(dst, float32(left), float32(top), float32(right-left), float32(height), float32(g.scale), g.theme.Border, false)

	cols = g.cols - 2 // one cell of padding on each side
	x = left + f.cellW
	y = top + f.lineH/4
	titleW := len([]rune(title)) + 2
	g.drawText(dst, title, x, y, g.theme.Accent, 1)
	g.drawText(dst, truncate(keys, cols-titleW), x+float64(titleW)*f.cellW, y, g.theme.Muted, 1)
	return x, y + f.lineH, cols
}
