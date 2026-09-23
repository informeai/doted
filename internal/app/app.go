// Package app is the Ebiten game that draws doted: scrollback on top, the
// input line at the bottom.
package app

import (
	"fmt"
	"os"
	"runtime"
	"time"
	"unicode"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/informeai/doted/internal/config"
	"github.com/informeai/doted/internal/fonts"
	"github.com/informeai/doted/internal/jobs"
	"github.com/informeai/doted/internal/shell"
	"github.com/informeai/doted/internal/terminal"
)

const (
	wheelRows = 3

	// Key repeat, in ticks (Ebiten runs Update at 60 TPS).
	repeatDelay    = 24
	repeatInterval = 3

	loginEnvTimeout = 5 * time.Second
)

type Game struct {
	cfg        config.Config
	theme      Theme
	family     fonts.Family
	configPath string
	reloads    chan reload

	// The main view: typed commands, doted's messages and the output of the
	// attached job, if any.
	scrollback *terminal.Scrollback
	parser     *terminal.Parser
	editor     terminal.Editor
	pending    []notice // messages held while a job writes to the scrollback

	session  *shell.Session
	jobs     *jobs.Manager
	attached *jobs.Job // runs in the main view and receives the keyboard
	viewing  *jobs.Job // shown full screen in the job view
	panel    panel     // the jobs list

	scroll    int       // visual rows scrolled up from the bottom
	lastInput time.Time // keeps the cursor solid while typing
	chars     []rune
	keyBuf    []byte
	ptyCols   int // terminal size last given to the jobs
	ptyRows   int
	quitArmed bool // exit was asked once while jobs were still running
	quit      bool

	// Set by Layout / Draw, read by Update.
	scale      float64
	width      int
	height     int
	cols       int
	outputRows int
	faces      *faceSet // rebuilt when the font settings or the scale change
}

type notice struct {
	kind terminal.Kind
	text string
}

// New creates the game with the given settings and keeps them in sync with
// the config file at configPath.
func New(s Settings, configPath string) (*Game, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	desktop := launchedFromDesktop()
	if home, err := os.UserHomeDir(); err == nil && desktop {
		dir = home // Finder and launchers start apps in /
	}
	sb := terminal.NewScrollback(s.Config.Scrollback.Lines)
	g := &Game{
		configPath: configPath,
		reloads:    make(chan reload, 1),
		scrollback: sb,
		parser:     terminal.NewParser(sb),
		session:    shell.NewSession(dir),
		jobs:       jobs.NewManager(s.Config.Scrollback.Lines),
		scale:      1,
		cols:       80,
		outputRows: 24,
	}
	g.scrollback.Append(terminal.System, "doted — type a command and press Enter. Ctrl+B sends it to the background, Ctrl+T lists jobs.", time.Now())
	g.apply(s)
	if desktop {
		if err := g.session.ImportLoginEnvironment(loginEnvTimeout); err != nil {
			g.notify(terminal.Error, err.Error())
		}
	}
	g.watchConfig()
	return g, nil
}

// launchedFromDesktop reports whether doted was opened from Finder or a
// desktop launcher rather than from another terminal, which always sets TERM.
// Such apps get a minimal environment and start in /.
func launchedFromDesktop() bool {
	return runtime.GOOS != "windows" && os.Getenv("TERM") == ""
}

// apply switches to new settings; everything but the window size takes
// effect immediately.
func (g *Game) apply(s Settings) {
	g.cfg = s.Config
	g.theme = newTheme(s.Config.Colors)
	g.family = s.Fonts
	g.faces = nil
	g.scrollback.SetLimit(s.Config.Scrollback.Lines)
	g.jobs.SetScrollback(s.Config.Scrollback.Lines)
	g.session.Configure(s.Config.Shell.Program, s.Config.Shell.Env)
	for _, n := range s.Notices {
		g.notify(terminal.System, "config: "+n)
	}
}

func (g *Game) handleReloads() {
	select {
	case r := <-g.reloads:
		if r.err != nil {
			g.notify(terminal.Error, "config not reloaded: "+r.err.Error())
			return
		}
		g.apply(r.settings)
		g.notify(terminal.System, "config reloaded")
	default:
	}
}

// notify adds a message to the main view. While a job is attached its parser
// owns the newest line, so messages wait until it detaches.
func (g *Game) notify(kind terminal.Kind, text string) {
	if g.attached != nil {
		g.pending = append(g.pending, notice{kind, text})
		return
	}
	g.scrollback.Append(kind, text, time.Now())
}

func (g *Game) flushNotices() {
	if g.attached != nil {
		return
	}
	for _, n := range g.pending {
		g.scrollback.Append(n.kind, n.text, time.Now())
	}
	g.pending = g.pending[:0]
}

func (g *Game) Update() error {
	g.handleReloads()
	g.jobs.Poll(time.Now(), g.handleJobEvent)
	g.flushNotices()

	switch {
	case g.panel.open:
		g.handlePanelKeys()
	case g.viewing != nil:
		g.handleJobViewKeys()
	case g.attached != nil:
		g.handleAttachedKeys()
	default:
		g.handleKeyboard()
	}
	g.syncPTYSize()
	g.handleScrolling()

	if g.quit {
		g.jobs.KillAll()
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

// handleJobEvent mirrors the attached job's output into the main view and
// reports background jobs that finish.
func (g *Game) handleJobEvent(j *jobs.Job, ev shell.Event) {
	if j == g.attached {
		if !ev.Done {
			g.parser.Write(ev.Data, time.Now())
			return
		}
		g.parser.End()
		g.attached = nil
		if ev.Status != "" {
			g.notify(terminal.System, ev.Status)
		}
		if !j.Listed {
			g.jobs.Remove(j) // an ordinary command: nothing to keep around
		}
		return
	}
	if ev.Done && j.Listed {
		g.notify(terminal.System, fmt.Sprintf("[%d] %s: %s", j.ID, jobResult(j), j.Command))
	}
}

func jobResult(j *jobs.Job) string {
	switch {
	case j.Killed:
		return "killed"
	case j.Status != "":
		return j.Status
	}
	return "done"
}

// handleKeyboard edits the input line while no job is attached.
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
		case inpututil.IsKeyJustPressed(ebiten.KeyT):
			g.openPanel()
		case inpututil.IsKeyJustPressed(ebiten.KeyD):
			if g.editor.Empty() {
				g.requestQuit()
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

// handleAttachedKeys sends keystrokes to the attached job, except Ctrl+B,
// which moves it to the background.
func (g *Game) handleAttachedKeys() {
	if ctrlPressed(ebiten.KeyB) {
		g.background()
		return
	}
	g.forwardKeyboard(g.attached)
}

// forwardKeyboard sends keystrokes to a job's terminal, which takes care of
// echo, line editing and signals (Ctrl+C, Ctrl+Z...).
func (g *Game) forwardKeyboard(j *jobs.Job) {
	g.chars = ebiten.AppendInputChars(g.chars[:0])
	g.keyBuf = appendKeyBytes(g.keyBuf[:0], g.chars)
	if len(g.keyBuf) == 0 {
		return
	}
	if err := j.Write(g.keyBuf); err == nil {
		g.scroll = 0
		g.touch()
	}
}

// syncPTYSize keeps every job's terminal as large as the output area.
func (g *Game) syncPTYSize() {
	if g.cols == g.ptyCols && g.outputRows == g.ptyRows {
		return
	}
	g.jobs.Resize(g.cols, g.outputRows)
	g.ptyCols, g.ptyRows = g.cols, g.outputRows
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
	maxScroll := max(0, g.visibleScrollback().Rows(g.cols)-g.outputRows)
	g.scroll = min(max(0, g.scroll+rows), maxScroll)
}

// visibleScrollback is what the output area shows: the viewed job's output
// or the main view.
func (g *Game) visibleScrollback() *terminal.Scrollback {
	if g.viewing != nil {
		return g.viewing.Output
	}
	return g.scrollback
}

// abandonLine is Ctrl+C at the prompt: keep what was typed in the scrollback
// and start a fresh line.
func (g *Game) abandonLine() {
	if !g.editor.Empty() {
		g.scrollback.Append(terminal.Command, g.cfg.Prompt.Symbol+g.editor.Text()+"^C", time.Now())
	}
	g.editor.Reset()
}

// requestQuit exits, asking for confirmation once if background jobs would
// be killed.
func (g *Game) requestQuit() {
	if n := g.jobs.RunningInBackground(); n > 0 && !g.quitArmed {
		g.quitArmed = true
		g.notify(terminal.System, fmt.Sprintf("%d background job(s) still running; exit again to kill them", n))
		return
	}
	g.quit = true
}

func (g *Game) touch() { g.lastInput = time.Now() }

// repeating reports whether key fires this tick: on press, then at the
// repeat rate once it has been held past the delay.
func repeating(key ebiten.Key) bool {
	d := inpututil.KeyPressDuration(key)
	return d == 1 || (d >= repeatDelay && (d-repeatDelay)%repeatInterval == 0)
}

// ctrlPressed reports Ctrl+key pressed this tick.
func ctrlPressed(key ebiten.Key) bool {
	return ebiten.IsKeyPressed(ebiten.KeyControl) && inpututil.IsKeyJustPressed(key)
}
