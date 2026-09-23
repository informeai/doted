// Package app is the Ebiten game that draws doted: scrollback on top, the
// input line at the bottom.
package app

import (
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/informeai/doted/internal/shell"
	"github.com/informeai/doted/internal/terminal"
)

const (
	scrollbackLimit = 10_000
	wheelRows       = 3

	// Key repeat, in ticks (Ebiten runs Update at 60 TPS).
	repeatDelay    = 24
	repeatInterval = 3
)

type Game struct {
	theme      Theme
	scrollback *terminal.Scrollback
	parser     *terminal.Parser
	editor     terminal.Editor
	runner     *shell.Runner

	scroll    int       // visual rows scrolled up from the bottom
	lastInput time.Time // keeps the cursor solid while typing
	chars     []rune
	keyBuf    []byte
	ptyCols   int // size last sent to the running command's terminal
	ptyRows   int
	quit      bool

	// Set by Layout / Draw, read by Update.
	scale      float64
	width      int
	height     int
	cols       int
	outputRows int
	fonts      *fonts
}

func New() (*Game, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	sb := terminal.NewScrollback(scrollbackLimit)
	g := &Game{
		theme:      DefaultTheme,
		scrollback: sb,
		parser:     terminal.NewParser(sb),
		runner:     shell.NewRunner(dir),
		scale:      1,
		cols:       80,
		outputRows: 24,
	}
	g.scrollback.Append(terminal.System, "doted — type a command and press Enter. Ctrl+C interrupts, Ctrl+L clears.", time.Now())
	return g, nil
}

func (g *Game) Update() error {
	g.runner.Drain(g.handleEvent)
	if g.runner.Running() {
		g.forwardKeyboard()
		g.syncPTYSize()
	} else {
		g.handleKeyboard()
	}
	g.handleScrolling()
	if g.quit {
		g.runner.Kill()
		return ebiten.Termination
	}
	return nil
}

// Layout renders at the monitor's device scale so text stays crisp on HiDPI.
func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	g.scale = ebiten.Monitor().DeviceScaleFactor()
	g.width = int(float64(outsideWidth) * g.scale)
	g.height = int(float64(outsideHeight) * g.scale)
	return g.width, g.height
}

func (g *Game) handleEvent(ev shell.Event) {
	now := time.Now()
	if !ev.Done {
		g.parser.Write(ev.Data, now)
		return
	}
	g.parser.End()
	if ev.Status != "" {
		g.scrollback.Append(terminal.System, ev.Status, now)
	}
}

// handleKeyboard edits the input line while no command is running.
func (g *Game) handleKeyboard() {
	ctrl := ebiten.IsKeyPressed(ebiten.KeyControl)
	meta := ebiten.IsKeyPressed(ebiten.KeyMeta) // Cmd on macOS
	alt := ebiten.IsKeyPressed(ebiten.KeyAlt)

	g.chars = ebiten.AppendInputChars(g.chars[:0])
	if !ctrl && !meta {
		typed := false
		for _, r := range g.chars {
			if !unicode.IsControl(r) {
				g.editor.Insert(r)
				typed = true
			}
		}
		if typed {
			g.touch()
		}
	}

	if ctrl {
		switch {
		case inpututil.IsKeyJustPressed(ebiten.KeyC):
			g.abandonLine()
		case inpututil.IsKeyJustPressed(ebiten.KeyL):
			g.scrollback.Clear()
			g.scroll = 0
		case inpututil.IsKeyJustPressed(ebiten.KeyD):
			if g.editor.Empty() {
				g.quit = true
			}
		case repeating(ebiten.KeyU):
			g.editor.KillToStart()
		case repeating(ebiten.KeyW):
			g.editor.DeleteWordBackward()
		case inpututil.IsKeyJustPressed(ebiten.KeyA):
			g.editor.Home()
		case inpututil.IsKeyJustPressed(ebiten.KeyE):
			g.editor.End()
		default:
			return
		}
		g.touch()
		return
	}

	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeyEnter), inpututil.IsKeyJustPressed(ebiten.KeyNumpadEnter):
		g.submit()
	case repeating(ebiten.KeyBackspace):
		switch {
		case meta:
			g.editor.KillToStart()
		case alt:
			g.editor.DeleteWordBackward()
		default:
			g.editor.Backspace()
		}
	case repeating(ebiten.KeyDelete):
		g.editor.Delete()
	case repeating(ebiten.KeyArrowLeft):
		if meta {
			g.editor.Home()
		} else {
			g.editor.Left()
		}
	case repeating(ebiten.KeyArrowRight):
		if meta {
			g.editor.End()
		} else {
			g.editor.Right()
		}
	case repeating(ebiten.KeyArrowUp):
		g.editor.HistoryPrev()
	case repeating(ebiten.KeyArrowDown):
		g.editor.HistoryNext()
	case inpututil.IsKeyJustPressed(ebiten.KeyHome):
		g.editor.Home()
	case inpututil.IsKeyJustPressed(ebiten.KeyEnd):
		g.editor.End()
	default:
		return
	}
	g.touch()
}

// forwardKeyboard sends keystrokes to the running command's terminal, which
// takes care of echo, line editing and signals (Ctrl+C, Ctrl+Z...).
func (g *Game) forwardKeyboard() {
	g.chars = ebiten.AppendInputChars(g.chars[:0])
	g.keyBuf = appendKeyBytes(g.keyBuf[:0], g.chars)
	if len(g.keyBuf) == 0 {
		return
	}
	if err := g.runner.Write(g.keyBuf); err == nil {
		g.scroll = 0
		g.touch()
	}
}

// syncPTYSize keeps the command's terminal as large as the output area.
func (g *Game) syncPTYSize() {
	if g.cols == g.ptyCols && g.outputRows == g.ptyRows {
		return
	}
	if g.runner.Resize(g.cols, g.outputRows) == nil {
		g.ptyCols, g.ptyRows = g.cols, g.outputRows
	}
}

func (g *Game) handleScrolling() {
	if _, dy := ebiten.Wheel(); dy != 0 {
		g.scrollBy(int(dy * wheelRows))
	}
	switch {
	case repeating(ebiten.KeyPageUp):
		g.scrollBy(g.outputRows - 1)
	case repeating(ebiten.KeyPageDown):
		g.scrollBy(-(g.outputRows - 1))
	}
}

func (g *Game) scrollBy(rows int) {
	maxScroll := max(0, g.scrollback.Rows(g.cols)-g.outputRows)
	g.scroll = min(max(0, g.scroll+rows), maxScroll)
}

func (g *Game) submit() {
	line := g.editor.Submit()
	g.scroll = 0
	g.scrollback.Append(terminal.Command, promptSymbol+line, time.Now())

	cmd := strings.TrimSpace(line)
	if cmd == "" || g.runBuiltin(cmd) {
		return
	}
	if err := g.runner.Start(cmd, g.cols, g.outputRows); err != nil {
		g.scrollback.Append(terminal.Error, err.Error(), time.Now())
		return
	}
	g.ptyCols, g.ptyRows = g.cols, g.outputRows
	g.parser.Begin()
}

// runBuiltin handles the commands that must act on doted itself rather than
// on a child process.
func (g *Game) runBuiltin(cmd string) bool {
	name, arg, _ := strings.Cut(cmd, " ")
	arg = strings.TrimSpace(arg)
	switch name {
	case "exit", "quit":
		g.quit = true
	case "clear":
		g.scrollback.Clear()
	case "cd":
		if err := g.runner.Chdir(arg); err != nil {
			g.scrollback.Append(terminal.Error, "cd: "+err.Error(), time.Now())
		}
	default:
		return false
	}
	return true
}

// abandonLine is Ctrl+C at the prompt: keep what was typed in the scrollback
// and start a fresh line.
func (g *Game) abandonLine() {
	if !g.editor.Empty() {
		g.scrollback.Append(terminal.Command, promptSymbol+g.editor.Text()+"^C", time.Now())
	}
	g.editor.Reset()
}

func (g *Game) touch() { g.lastInput = time.Now() }

// repeating reports whether key fires this tick: on press, then at the
// repeat rate once it has been held past the delay.
func repeating(key ebiten.Key) bool {
	d := inpututil.KeyPressDuration(key)
	return d == 1 || (d >= repeatDelay && (d-repeatDelay)%repeatInterval == 0)
}
