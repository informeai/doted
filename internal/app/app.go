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
	"github.com/informeai/doted/internal/history"
	"github.com/informeai/doted/internal/jobs"
	"github.com/informeai/doted/internal/shell"
	"github.com/informeai/doted/internal/terminal"
	"github.com/informeai/doted/internal/winstate"
)

const (
	wheelRows = 3

	// Key repeat, in ticks (Ebiten runs Update at 60 TPS).
	repeatDelay    = 24
	repeatInterval = 3

	loginEnvTimeout = 5 * time.Second
	// Loading the rc of a shell with a big setup (oh-my-zsh) takes a second
	// or two; past this, commands just start without its aliases.
	definitionsTimeout = 15 * time.Second
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
	panel    panel     // the jobs list or the help

	scroll          int                 // visual rows scrolled up from the bottom
	sparks          *sparks             // fired from the prompt bar while typing
	historyPath     string              // where typed commands are saved
	window          windowTracker       // size and place of the window, remembered across runs
	system          systemClipboard     // the system clipboard; see clipboard.go
	clipboardEvents chan clipboardEvent // results of background clipboard calls
	definitions     chan definitions    // the shell's startup aliases and functions, once loaded
	path            pathRoll            // the directory on the status line, rolling after a cd
	lastInput       time.Time           // keeps the cursor solid while typing
	bounceStart     time.Time           // when the dot cursor started hopping; see bounceElapsed
	chars           []rune
	ptyCols         int // terminal size last given to the jobs
	ptyRows         int
	lookups         map[string]lookup // cached command lookups; see commandcheck.go
	suggest         suggestCache      // the autosuggestion; see suggest.go
	zap             zap               // the bolt run after accepting a suggestion; see zap.go
	outSel          outputSelection   // text selected in the output with the mouse; see outputselect.go
	cursorShape     ebiten.CursorShapeType
	clipboard       string    // doted's own clipboard; see clipboard.go
	flashText       string    // short status-line message, e.g. "copied"
	flashUntil      time.Time // when flashText goes away
	quitArmed       bool      // exit was asked once while jobs were still running
	quit            bool

	// Set by Layout / Draw, read by Update.
	scale      float64
	width      int
	height     int
	cols       int
	outputRows int
	faces      *faceSet // rebuilt when the font settings or the scale change
	// Where the output was drawn, for the mouse to find text in it.
	rows                       []visibleRow
	blocks                     map[int]*block              // commands run, by their line's seq; see blocks.go
	actions                    []blockAction               // clickable spots of the blocks in the last frame
	context                    contextState                // git branch and project for the status line; see statuscontext.go
	branch                     branchAnim                  // the branch's animations; see gitanim.go
	lastDuration               time.Duration               // how long the last foreground command took
	focused                    bool                        // whether the window had the focus last tick
	linkCache                  linkCache                   // the last link looked up under the mouse
	watches                    map[*jobs.Job]*jobWatch     // what the job strip read from each job; see strip.go
	stripHits                  []stripHit                  // the strip's clickable spots in the last frame
	stripDrawn                 time.Time                   // when the strip was last drawn, to move cards smoothly
	stripSel                   *jobs.Job                   // the card selected from the keyboard; see stripnav.go
	groupSel                   bool                        // the group card is selected, and so open
	stripScroll                float64                     // how far the strip is scrolled, moving towards stripScrollTo
	stripScrollTo              float64                     //
	stripMaxScroll             float64                     // how far it can scroll, as of the last frame
	stripScrolledAway          bool                        // scrolled off the newest cards by hand
	wheelTaken                 bool                        // the strip used this tick's mouse wheel
	stripGroup                 groupCard                   // the card grouping the jobs past the first ones
	stripRoom                  float64                     // how tall a card may grow: down to the output's bottom
	cardSparks                 *sparks                     // shed by cards as they leave, on screen in logical px
	target                     *jobs.Job                   // the job the input line sends to; see jobcontrol.go
	targetRaw                  bool                        // it reads a key at a time
	stash                      string                      // the shell line put aside meanwhile
	opener                     func(*Game, []string) error // starts the program that opens a link
	notifier                   func(title, body string)    // shows a desktop notification
	outTop, outBottom, outLeft float64
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
		configPath:      configPath,
		reloads:         make(chan reload, 1),
		scrollback:      sb,
		parser:          terminal.NewParser(sb),
		session:         shell.NewSession(dir),
		jobs:            jobs.NewManager(s.Config.Scrollback.Lines),
		scale:           1,
		opener:          startDetached,
		notifier:        sendNotification,
		cols:            80,
		outputRows:      24,
		sparks:          newSparks(uint64(time.Now().UnixNano())),
		historyPath:     history.Path(),
		window:          windowTracker{path: winstate.Path()},
		system:          realClipboard{},
		clipboardEvents: make(chan clipboardEvent, 8),
		definitions:     make(chan definitions, 1),
	}
	g.apply(s)
	g.loadHistory()
	g.trackDir(time.Now())
	g.watchContext(time.Now()) // start looking up the git branch right away
	if desktop {
		if err := g.session.ImportLoginEnvironment(loginEnvTimeout); err != nil {
			g.notify(terminal.Error, err.Error())
		}
	}
	g.captureDefinitions()
	g.watchConfig()
	return g, nil
}

type definitions struct {
	path string
	err  error
}

// captureDefinitions loads the aliases and functions of the user's shell
// startup files in the background; Update hands them to the session.
func (g *Game) captureDefinitions() {
	capture := g.session.CaptureDefinitions(definitionsTimeout)
	if capture == nil {
		return
	}
	go func() {
		path, err := capture()
		g.definitions <- definitions{path, err}
	}()
}

func (g *Game) handleDefinitions() {
	select {
	case d := <-g.definitions:
		if d.err != nil {
			g.notify(terminal.Error, d.err.Error())
			return
		}
		g.session.UseDefinitions(d.path)
	default:
	}
}

// loadHistory fills the input's history from the history file.
func (g *Game) loadHistory() {
	if !g.cfg.History.Save {
		return
	}
	lines, err := history.Load(g.historyPath, g.cfg.History.Lines)
	if err != nil {
		g.notify(terminal.Error, "history: "+err.Error())
	}
	g.editor.SetHistory(lines, g.cfg.History.Lines)
}

// saveHistory appends a submitted line to the history file.
func (g *Game) saveHistory(line string) {
	if !g.cfg.History.Save || !history.Keep(line) {
		return
	}
	if err := history.Append(g.historyPath, line); err != nil {
		g.flash("history not saved: " + err.Error())
	}
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
	shellChanged := g.definitions != nil && g.cfg.Shell.Program != s.Config.Shell.Program
	g.cfg = s.Config
	g.theme = newTheme(s.Config.Colors)
	g.family = s.Fonts
	g.faces = nil
	g.scrollback.SetLimit(s.Config.Scrollback.Lines)
	g.jobs.SetScrollback(s.Config.Scrollback.Lines)
	g.session.Configure(s.Config.Shell.Program, s.Config.Shell.Env)
	if shellChanged {
		g.captureDefinitions() // the new shell has its own startup files
	}
	g.suggest = suggestCache{} // the PATH may have changed
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
	g.handleDefinitions()
	g.handleClipboardEvents()
	g.handleMouse(time.Now())
	g.jobs.Poll(time.Now(), g.handleJobEvent)
	g.flushNotices()
	g.watchJobs(time.Now())

	switch {
	case (!g.panel.open || g.panel.kind == panelHistory) && clipboardChord(ebiten.KeyF):
		g.openFind()
	case g.handleStripKeys(time.Now()):
	case g.panel.open && g.panel.kind == panelFind:
		g.handleFindKeys()
	case g.panel.open && g.panel.kind == panelHelp:
		g.handleHelpKeys()
	case g.panel.open && g.panel.kind == panelHistory:
		g.handleHistoryKeys()
	case g.panel.open:
		g.handlePanelKeys()
	case altDigit() > 0 && g.sendToCard(altDigit()):
	case g.target != nil:
		g.handleTargetKeys()
	case g.viewing != nil:
		g.handleJobViewKeys()
	case g.attached != nil:
		g.handleAttachedKeys()
	default:
		g.handleKeyboard()
	}
	g.syncPTYSize()
	g.handleScrolling()
	g.updateZap(time.Now())
	g.sparks.step(tickSeconds)
	g.stepBranch()
	if g.cardSparks != nil {
		g.cardSparks.step(tickSeconds)
	}
	g.trackDir(time.Now())
	if g.focusChanged() {
		g.context.stale = true
	}
	g.watchContext(time.Now())

	g.trackWindow(time.Now())
	if g.quit || ebiten.IsWindowBeingClosed() {
		g.saveWindow()
		g.jobs.KillAll()
		g.session.Close()
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
	if ev.Done {
		g.endBlock(j, time.Now())
	}
	if j == g.attached {
		if !ev.Done {
			g.parser.Write(ev.Data, time.Now())
			return
		}
		g.parser.End()
		g.attached = nil
		// The block's badge shows how it ended; say it only without one.
		if ev.Status != "" && g.blockOf(j) == nil {
			g.notify(terminal.System, ev.Status)
		}
		// It ended in the foreground: the next command carries on from its
		// environment, directory, aliases and functions.
		g.session.Adopt(j.State())
		if !j.Listed {
			g.jobs.Remove(j) // an ordinary command: nothing to keep around
		}
		return
	}
	if ev.Done && j.Listed {
		if g.jobRestarted(j) {
			return // it runs again in its place
		}
		// It ended in the background, after newer commands: its state would
		// undo theirs.
		g.session.Discard(j.State())
		g.jobNotice("[%d] %s: %s", j.ID, jobResult(j), j.Command)
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
	shift := ebiten.IsKeyPressed(ebiten.KeyShift) // extends the selection

	if g.handleClipboardKeys() {
		return
	}

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
			g.typed()
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
		case inpututil.IsKeyJustPressed(ebiten.KeyR):
			g.openHistorySearch()
		case stripDigit() > 0:
			g.openStripCard(stripDigit())
		case inpututil.IsKeyJustPressed(ebiten.KeyD):
			if g.editor.Empty() {
				g.requestQuit()
			}
		case repeating(ebiten.KeyU):
			g.kill(g.editor.KillToStart())
		case repeating(ebiten.KeyW):
			g.kill(g.editor.DeleteWordBackward())
		case inpututil.IsKeyJustPressed(ebiten.KeyY):
			g.yank() // readline's yank: doted's own clipboard
		case inpututil.IsKeyJustPressed(ebiten.KeyA):
			g.editor.Home(shift)
		case inpututil.IsKeyJustPressed(ebiten.KeyE):
			g.editor.End(shift)
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
			g.kill(g.editor.KillToStart())
		case alt:
			g.kill(g.editor.DeleteWordBackward())
		default:
			g.editor.Backspace()
			g.typed()
		}
	case repeating(ebiten.KeyDelete):
		g.editor.Delete()
		g.typed()
	case repeating(ebiten.KeyArrowLeft):
		if meta {
			g.editor.Home(shift)
		} else {
			g.editor.Left(shift)
		}
	case inpututil.IsKeyJustPressed(ebiten.KeyTab):
		g.acceptSuggestion()
	case inpututil.IsKeyJustPressed(ebiten.KeyEscape):
		g.outSel.clear()
	case repeating(ebiten.KeyArrowRight):
		switch {
		case meta:
			g.editor.End(shift)
		case !shift && g.acceptSuggestion(): // at the end of the line, as in fish
		default:
			g.editor.Right(shift)
		}
	case repeating(ebiten.KeyArrowUp):
		g.editor.HistoryPrev()
	case repeating(ebiten.KeyArrowDown):
		g.editor.HistoryNext()
	case inpututil.IsKeyJustPressed(ebiten.KeyHome):
		g.editor.Home(shift)
	case inpututil.IsKeyJustPressed(ebiten.KeyEnd):
		g.editor.End(shift)
	default:
		return
	}
	g.touch()
}

// handleAttachedKeys sends keystrokes to the attached job, except Ctrl+B,
// which moves it to the background.
func (g *Game) handleAttachedKeys() {
	// A full-screen program gets Ctrl+B too (vim pages up with it).
	if !g.attached.FullScreen() && ctrlPressed(ebiten.KeyB) {
		g.background()
		return
	}
	g.forwardKeyboard(g.attached)
}

// forwardKeyboard sends keystrokes to a job's terminal, which takes care of
// echo, line editing and signals (Ctrl+C, Ctrl+Z...). The job's emulator
// encodes them as the program's terminal modes ask.
func (g *Game) forwardKeyboard(j *jobs.Job) {
	if clipboardChord(ebiten.KeyV) {
		g.pasteTo(j)
		return
	}
	// Copying selected output: Ctrl+Shift+C mustn't reach the program as
	// Ctrl+C and interrupt it.
	if clipboardChord(ebiten.KeyC) {
		g.copySelection()
		return
	}
	if g.sendKeyboard(j) {
		g.scroll = 0
		g.touch()
		g.typed()
	}
}

// syncPTYSize keeps every job's terminal as large as the output area.
func (g *Game) syncPTYSize() {
	if g.cols == g.ptyCols && g.outputRows == g.ptyRows {
		return
	}
	g.jobs.Resize(g.cols, g.outputRows)
	g.parser.Rows = g.outputRows
	g.ptyCols, g.ptyRows = g.cols, g.outputRows
}

func (g *Game) handleScrolling() {
	if j := g.screenJob(); j != nil {
		g.scrollScreen(j) // the wheel goes to the program; PgUp/PgDn go as keys
		return
	}
	if _, dy := ebiten.Wheel(); dy != 0 && !g.wheelTaken {
		g.scrollBy(int(dy * wheelRows))
	}
	g.wheelTaken = false
	switch {
	case repeating(ebiten.KeyPageUp):
		g.scrollBy(g.outputRows - 1)
	case repeating(ebiten.KeyPageDown):
		g.scrollBy(-(g.outputRows - 1))
	}
}

func (g *Game) scrollBy(rows int) {
	maxScroll := max(0, g.totalRows(g.visibleScrollback())-g.outputRows)
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
		g.scrollback.Append(terminal.Command, g.promptText()+g.editor.Text()+"^C", time.Now())
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

// typed fires a burst of sparks from the prompt bar for a keystroke that
// edited text: typing at the shell's prompt, or to a card's job from it.
// Keys that go straight into a job don't: in its job view, or to the
// command running in the main view (full screen or not).
func (g *Game) typed() {
	if g.cfg.Prompt.Style != config.PromptBar || !g.cfg.Animation.Particles || !g.cfg.Animation.Enabled {
		return
	}
	if g.viewing != nil || g.attached != nil && g.target == nil {
		return
	}
	barHeight := g.cfg.Font.Size * 1.2 // logical px, until faces are measured
	if g.faces != nil {
		barHeight = g.faces.glyphH / g.scale
	}
	g.sparks.burst(sparksPerKey, barHeight)
}

// touch records a keystroke: the blinking cursors hold still, and the dot
// starts hopping unless it is already mid-bounce.
func (g *Game) touch() {
	now := time.Now()
	if _, bouncing := bounceElapsed(now, g.bounceStart, g.lastInput); !bouncing {
		g.bounceStart = now
	}
	g.lastInput = now
}

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
