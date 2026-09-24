package app

import (
	"fmt"
	"image/color"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/informeai/doted/internal/jobs"
	"github.com/informeai/doted/internal/terminal"
)

// The job strip shows each background job as a live card at the top:
// its number, a short name, how long it has run and its last lines of
// output. There's
// no layout to manage: a card shows up when a job goes to the background
// and leaves a moment after it ends.
//
// Cards watch the output as it comes. A line that looks like an error turns
// the card red with a flash, until a line that looks like success (a watcher
// passing again) clears it; the first local URL a job prints (a dev server)
// becomes a link on its card. Clicking a card, or Ctrl+1..9, opens the job;
// its actions restart, stop or send to it without opening it (see
// jobcontrol.go).

const (
	stripMinCols       = 30 // a card's narrowest width, in cells
	stripGapCols       = 1  // between cards
	stripEnter         = 220 * time.Millisecond
	stripLingerOK      = 6 * time.Second         // a finished card stays this long
	stripLingerFail    = 20 * time.Second        // longer when it failed
	stripLingerGrouped = 2500 * time.Millisecond // shorter when it ended in the group
	stripFlash         = 700 * time.Millisecond
)

var (
	errorLine   = regexp.MustCompile(`(?i)\b(error|errors|failed|fail|panic|fatal|exception|traceback)\b|✗|✘`)
	noErrorLine = regexp.MustCompile(`(?i)\b(0|no) (errors?|failures?)\b`)
	successLine = regexp.MustCompile(`(?i)\b(ok|pass|passed|success|successfully|compiled|ready|built|done)\b|✓|✔`)
	localURL    = regexp.MustCompile(`https?://(localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1?\])(:\d+)?[^\s"'<>]*`)
)

// jobWatch is what the strip knows about a job from its output.
type jobWatch struct {
	name    string
	scanned int // the seq of the next output line to look at
	failing bool
	flashAt time.Time
	url     string
	shownAt time.Time

	activityAt time.Time // when output last came in; see cardborder.go
	lastSig    [2]int    // the newest line's seq and length, to notice output

	// Where its card was drawn, as it moves to its slot, and how far it has
	// shrunk to one line (towards miniTarget); see cardlife.go.
	x, w       float64
	placed     bool
	mini       float64
	miniTarget float64
	grouped    bool      // it waits in the group card, out of view
	shed       bool      // it shed its sparks as it left
	fullH      float64   // its whole card's height, growing while selected
	promotedAt time.Time // when it moved up out of the group

	// Driving it from its card; see jobcontrol.go.
	restarting bool      // it was killed to run again
	stoppedAt  time.Time // when stop last interrupted it
}

// isError reports whether an output line looks like an error.
func isError(line string) bool {
	return errorLine.MatchString(line) && !noErrorLine.MatchString(line)
}

// watchJobs reads the new output of every background job.
func (g *Game) watchJobs(now time.Time) {
	if g.watches == nil {
		g.watches = map[*jobs.Job]*jobWatch{}
	}
	listed := g.jobs.Listed()
	alive := map[*jobs.Job]bool{}
	for _, j := range listed {
		alive[j] = true
		w := g.watches[j]
		if w == nil {
			w = &jobWatch{name: jobName(j.Command), shownAt: now}
			g.watches[j] = w
		}
		out := j.Output
		if out.Len() == 0 {
			continue
		}
		if sig := [2]int{out.Seq(out.Len() - 1), len(out.At(out.Len() - 1).Cells)}; sig != w.lastSig {
			w.lastSig, w.activityAt = sig, now
		}
		// The newest line may still be written to, until the job ends.
		end := out.Seq(out.Len() - 1)
		if !j.Running() {
			end++
		}
		for seq := max(w.scanned, out.Seq(0)); seq < end; seq++ {
			i, _ := out.Index(seq)
			line := out.At(i).Text()
			if w.url == "" {
				w.url = strings.TrimRight(localURL.FindString(line), ".,;:)]")
			}
			switch {
			case isError(line):
				if !w.failing {
					w.failing, w.flashAt = true, now
				}
			case successLine.MatchString(line):
				w.failing = false
			}
		}
		w.scanned = max(w.scanned, end)
	}
	for j := range g.watches {
		if !alive[j] {
			delete(g.watches, j)
		}
	}
}

// stripJobs are the jobs with a card now, oldest first.
func (g *Game) stripJobs(now time.Time) []*jobs.Job {
	if !g.cfg.Jobs.Strip || g.viewing != nil || g.screenJob() != nil {
		return nil
	}
	var out []*jobs.Job
	for _, j := range g.jobs.Listed() {
		if j.Running() || now.Sub(j.Ended) < g.linger(j) {
			out = append(out, j)
		}
	}
	return out
}

// linger is how long a finished job's card stays.
func (g *Game) linger(j *jobs.Job) time.Duration {
	w := g.watches[j]
	switch {
	case j.Status != "" || j.Killed || w != nil && w.failing:
		return stripLingerFail
	case w != nil && w.grouped:
		return stripLingerGrouped // out of view, it needn't wait to be seen
	}
	return stripLingerOK
}

// cardLines is how many lines of output job j's card shows: strip_lines,
// or more while it's selected or being sent to, as far as the output area
// leaves room for it to grow into.
func (g *Game) cardLines(j *jobs.Job) int {
	base := g.cfg.Jobs.StripLines
	n := base
	switch j {
	case g.target:
		n = max(n, targetLines)
	case g.stripSel:
		n = max(n, selectedLines)
	}
	if g.faces != nil && g.stripRoom > 0 {
		fit := int((g.stripRoom-g.faces.lineH/2)/g.faces.lineH) - 1
		n = min(n, max(base, fit))
	}
	return n
}

// stripHit is a clickable spot of the strip in the last frame.
type stripHit struct {
	x0, y0, x1, y1 float64
	job            *jobs.Job // opens it
	url            string    // opens it instead, when set
	action         string    // or does this to it: restart, stop, send
	scroll         int       // or scrolls the strip that way (a scroll arrow)
	group          bool      // or opens the group card, or closes it when open
}

func (g *Game) stripHitAt(x, y float64) (stripHit, bool) {
	for _, h := range g.stripHits {
		if x >= h.x0 && x < h.x1 && y >= h.y0 && y < h.y1 {
			// A link or an action inside a card wins over the card.
			for _, u := range g.stripHits {
				if (u.url != "" || u.action != "") && x >= u.x0 && x < u.x1 && y >= u.y0 && y < u.y1 {
					return u, true
				}
			}
			return h, true
		}
	}
	return stripHit{}, false
}

// handleStripMouse opens what's clicked in the strip, reporting whether the
// mouse is over it.
func (g *Game) handleStripMouse(x, y float64) bool {
	h, ok := g.stripHitAt(x, y)
	if !ok || g.panel.open {
		return false
	}
	// The wheel over the strip scrolls it sideways.
	if dx, dy := ebiten.Wheel(); (dx != 0 || dy != 0) && g.faces != nil {
		d := dx
		if d == 0 {
			d = dy
		}
		g.stripScrollTo -= d * 4 * g.faces.cellW
		g.stripScrolledAway = g.stripScrollTo < g.stripMaxScroll-1
		g.wheelTaken = true
	}
	g.setCursorShapeTo(ebiten.CursorShapePointer)
	if !inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		return true
	}
	switch {
	case h.group && (g.groupSel || g.stripSel != nil && !g.inFirstCards(g.stripSel)):
		g.clearSelection() // a click on the open group closes it
	case h.group:
		g.stripSel, g.groupSel = nil, true
	case h.scroll != 0:
		g.scrollStrip(h.scroll)
	case h.url != "":
		g.openLink(link{url: h.url})
	case h.action == "restart":
		g.restartJob(h.job)
	case h.action == "stop":
		g.stopJob(h.job, time.Now())
	case h.action == "send":
		g.enterTarget(h.job)
	case h.job != nil:
		g.openJob(h.job)
	}
	return true
}

// jobNotice tells the main view about a background job: that it started,
// moved there or ended. With the strip on, its card already says so, and
// the command's line keeps the result, so nothing is added.
func (g *Game) jobNotice(format string, args ...any) {
	if g.cfg.Jobs.Strip {
		return
	}
	g.notify(terminal.System, fmt.Sprintf(format, args...))
}

// openStripCard opens job #n, as Ctrl+n does.
func (g *Game) openStripCard(n int) bool {
	j := g.stripJob(n)
	if j == nil {
		return false
	}
	g.openJob(j)
	return true
}

// stripDigit is the digit key pressed this tick, 1 to 9, or 0.
func stripDigit() int {
	for i, k := range []ebiten.Key{ebiten.Key1, ebiten.Key2, ebiten.Key3, ebiten.Key4, ebiten.Key5, ebiten.Key6, ebiten.Key7, ebiten.Key8, ebiten.Key9} {
		if inpututil.IsKeyJustPressed(k) {
			return i + 1
		}
	}
	return 0
}

// jobName is a short name for a command: the script of a package manager
// or make ("npm run dev" is dev), or its first words.
func jobName(cmd string) string {
	// Only the first command of a list or pipeline.
	if i := strings.IndexAny(cmd, ";|&"); i > 0 {
		cmd = cmd[:i]
	}
	var words []string
	for _, w := range strings.Fields(cmd) {
		if strings.Contains(w, "=") && len(words) == 0 {
			continue // VAR=value before the command
		}
		words = append(words, w)
	}
	if len(words) == 0 {
		return cmd
	}
	args := words[1:]
	switch words[0] {
	case "npm", "pnpm", "yarn", "bun":
		if len(args) > 0 && args[0] == "run" {
			args = args[1:]
		}
		if len(args) > 0 {
			return args[0]
		}
	case "make", "just", "task":
		if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			return args[0]
		}
	}
	name := words[0]
	for _, a := range args {
		if strings.HasPrefix(a, "-") || strings.ContainsAny(a, "/.'\"$") || utf8.RuneCountInString(name+" "+a) > 18 {
			break
		}
		name += " " + a
	}
	return name
}

// jobColor is the color of a job's state: running, running but printing
// errors, done, failed or killed.
func (g *Game) jobColor(j *jobs.Job) color.RGBA {
	w := g.watches[j]
	switch {
	case j.Killed:
		return g.theme.Muted
	case j.Running() && w != nil && w.failing:
		return g.theme.Error
	case j.Running():
		return g.theme.Accent
	case j.Status != "" || w != nil && w.failing:
		return g.theme.Error
	}
	return g.theme.ANSI[2]
}

// drawStrip and drawCard live in cardlife.go, with how cards come and go.

// drawCardContent draws the inside of job j's card, laid out for the box
// at (x, y) of width w: the header with the actions under the mouse (when
// hover) or the URL, and the last lines of output.
func (g *Game) drawCardContent(dst *ebiten.Image, j *jobs.Job, wt *jobWatch, x, y, w float64, hover bool, alpha float64, now time.Time) {
	f := g.faces
	mx, my := ebiten.CursorPosition()
	state := g.jobColor(j)
	pad := f.cellW
	cols := int((w - 2*pad) / f.cellW)
	tx, ty := x+pad, y+f.lineH/4

	// Header: the state's dot, the name and how long it has run; the URL it
	// serves on the right, or the key that opens it under the mouse.
	dotR := f.cellW * 0.22
	if j.Running() && g.cfg.Animation.Enabled {
		dotR *= 0.8 + 0.3*(math.Sin(float64(now.UnixMilli())/1000*2*math.Pi)+1)/2
	}
	vector.FillCircle(dst, float32(tx+f.cellW/2), float32(ty+f.lineH/2), float32(dotR), scaleAlpha(state, alpha), true)
	// On the right: the actions under the mouse, or the URL it serves.
	type spot struct{ text, action string }
	var spots []spot
	switch {
	case hover && j.Running():
		stop := "stop"
		if g.stopping(j, now) {
			stop = "kill"
		}
		spots = []spot{{"restart", "restart"}, {stop, "stop"}}
		if j != g.target {
			spots = append(spots, spot{"send", "send"})
		}
	case hover:
		spots = []spot{{"restart", "restart"}}
	case wt.url != "":
		spots = []spot{{strings.TrimPrefix(strings.TrimPrefix(wt.url, "http://"), "https://"), ""}}
	}
	// The job's number (as fg takes it) comes before its name.
	id := fmt.Sprintf("#%d ", j.ID)
	idCols := utf8.RuneCountInString(id)
	nameCols := idCols + utf8.RuneCountInString(wt.name)
	rightCols := -2
	for _, sp := range spots {
		rightCols += 2 + utf8.RuneCountInString(sp.text)
	}
	// They get what the name leaves; a URL shortens, actions go if they
	// don't fit.
	if room := cols - 2 - nameCols - 2; rightCols > room {
		if len(spots) == 1 && spots[0].action == "" && room >= 8 {
			spots[0].text = truncate(spots[0].text, room)
			rightCols = room
		} else {
			spots, rightCols = nil, 0
		}
	}
	leftCols := cols - 2
	if len(spots) > 0 {
		leftCols -= rightCols + 1
	}
	g.drawText(dst, truncate(id, leftCols), tx+2*f.cellW, ty, g.theme.Muted, alpha)
	g.drawText(dst, truncate(wt.name, leftCols-idCols), tx+float64(2+idCols)*f.cellW, ty, g.theme.Foreground, alpha)
	if rest := leftCols - nameCols; rest > 3 {
		g.drawText(dst, truncate(" · "+formatElapsed(j.Elapsed(now)), rest), tx+float64(2+nameCols)*f.cellW, ty, g.theme.Muted, alpha)
	}
	rx := x + w - pad - float64(max(0, rightCols))*f.cellW
	for _, sp := range spots {
		sw := float64(utf8.RuneCountInString(sp.text)) * f.cellW
		over := float64(mx) >= rx && float64(mx) < rx+sw && float64(my) >= ty && float64(my) < ty+f.lineH
		clr := g.theme.Muted
		switch {
		case sp.action == "":
			clr = g.theme.Accent // the URL
		case over && sp.text == "kill":
			clr = g.theme.Error
		case over:
			clr = g.theme.Accent
		}
		g.drawText(dst, sp.text, rx, ty, clr, alpha)
		if sp.action == "" || over {
			uy := float32(ty + f.underlineY)
			vector.StrokeLine(dst, float32(rx), uy, float32(rx+sw), uy, float32(g.scale), scaleAlpha(clr, alpha*0.6), false)
		}
		hit := stripHit{x0: rx, y0: ty, x1: rx + sw, y1: ty + f.lineH, job: j, action: sp.action}
		if sp.action == "" {
			hit.url = wt.url
		}
		g.stripHits = append(g.stripHits, hit)
		rx += sw + 2*f.cellW
	}

	// The last lines, errors in red.
	for i, line := range j.Tail(g.cardLines(j)) {
		clr, a := g.theme.Foreground, alpha*0.7
		if isError(line) {
			clr, a = g.theme.Error, alpha
		}
		g.drawText(dst, truncate(strings.TrimSpace(line), cols), tx, ty+float64(i+1)*f.lineH, clr, a)
	}
}

// roundRect is the outline of a rectangle with rounded corners of radius r.
func roundRect(x, y, w, h, r float64) *vector.Path {
	r = math.Min(r, math.Min(w, h)/2)
	var p vector.Path
	x0, y0, x1, y1 := float32(x), float32(y), float32(x+w), float32(y+h)
	rr := float32(r)
	p.MoveTo(x0+rr, y0)
	p.LineTo(x1-rr, y0)
	p.ArcTo(x1, y0, x1, y0+rr, rr)
	p.LineTo(x1, y1-rr)
	p.ArcTo(x1, y1, x1-rr, y1, rr)
	p.LineTo(x0+rr, y1)
	p.ArcTo(x0, y1, x0, y1-rr, rr)
	p.LineTo(x0, y0+rr)
	p.ArcTo(x0, y0, x0+rr, y0, rr)
	p.Close()
	return &p
}

// fillRoundRect draws a filled rectangle with rounded corners of radius r.
func fillRoundRect(dst *ebiten.Image, x, y, w, h, r float64, clr color.RGBA) {
	op := &vector.DrawPathOptions{AntiAlias: true}
	op.ColorScale.ScaleWithColor(clr)
	vector.FillPath(dst, roundRect(x, y, w, h, r), &vector.FillOptions{}, op)
}

// strokeRoundRect outlines a rectangle with rounded corners of radius r.
func strokeRoundRect(dst *ebiten.Image, x, y, w, h, r, width float64, clr color.RGBA) {
	op := &vector.DrawPathOptions{AntiAlias: true}
	op.ColorScale.ScaleWithColor(clr)
	vector.StrokePath(dst, roundRect(x, y, w, h, r), &vector.StrokeOptions{Width: float32(width)}, op)
}
