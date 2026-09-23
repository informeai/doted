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
)

// The job strip shows each background job as a live card above the input:
// a short name, how long it has run and its last lines of output. There's
// no layout to manage: a card shows up when a job goes to the background
// and leaves a moment after it ends.
//
// Cards watch the output as it comes. A line that looks like an error turns
// the card red with a flash, until a line that looks like success (a watcher
// passing again) clears it; the first local URL a job prints (a dev server)
// becomes a link on its card. Clicking a card, or Ctrl+1..9, opens the job.

const (
	stripMinCols    = 30 // a card's narrowest width, in cells
	stripGapCols    = 1  // between cards
	stripEnter      = 220 * time.Millisecond
	stripLingerOK   = 6 * time.Second  // a finished card stays this long
	stripLingerFail = 20 * time.Second // longer when it failed
	stripFadeOut    = 600 * time.Millisecond
	stripFlash      = 700 * time.Millisecond
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
	if j.Status != "" || j.Killed || g.watches[j] != nil && g.watches[j].failing {
		return stripLingerFail
	}
	return stripLingerOK
}

// stripHeight is how tall the strip is now, 0 without cards.
func (g *Game) stripHeight(now time.Time) float64 {
	if len(g.stripJobs(now)) == 0 || g.faces == nil {
		return 0
	}
	f := g.faces
	return float64(1+g.cfg.Jobs.StripLines)*f.lineH + f.lineH/2
}

// stripHit is a clickable spot of the strip in the last frame.
type stripHit struct {
	x0, y0, x1, y1 float64
	job            *jobs.Job // opens it; nil for the "+N" card
	url            string    // opens it instead, when set
}

func (g *Game) stripHitAt(x, y float64) (stripHit, bool) {
	for _, h := range g.stripHits {
		if x >= h.x0 && x < h.x1 && y >= h.y0 && y < h.y1 {
			// A URL inside a card wins over the card.
			for _, u := range g.stripHits {
				if u.url != "" && x >= u.x0 && x < u.x1 && y >= u.y0 && y < u.y1 {
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
	g.setCursorShapeTo(ebiten.CursorShapePointer)
	if !inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		return true
	}
	switch {
	case h.url != "":
		g.openLink(link{url: h.url})
	case h.job != nil:
		g.openJob(h.job)
	default:
		g.openPanel()
	}
	return true
}

// jobsHint ends the message about a job going to the background: how to see
// it, unless its card already shows it.
func (g *Game) jobsHint() string {
	if g.cfg.Jobs.Strip {
		return ""
	}
	return " · ctrl+t to see jobs"
}

// openStripCard opens the job on card n (1-based), as Ctrl+n does.
func (g *Game) openStripCard(n int) bool {
	cards := g.stripJobs(time.Now())
	if n < 1 || n > len(cards) {
		return false
	}
	g.openJob(cards[n-1])
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

// drawStrip draws the cards in the band from left to right whose top is y.
func (g *Game) drawStrip(dst *ebiten.Image, left, right, y float64, now time.Time) {
	g.stripHits = g.stripHits[:0]
	cards := g.stripJobs(now)
	if len(cards) == 0 {
		return
	}
	f := g.faces
	lines := g.cfg.Jobs.StripLines
	h := float64(1+lines)*f.lineH + f.lineH/2
	cols := int((right - left) / f.cellW)
	fit := max(2, (cols+stripGapCols)/(stripMinCols+stripGapCols))
	gap := float64(stripGapCols) * f.cellW
	more, moreW := 0, 0.0
	if len(cards) > fit {
		// The newest cards show; the rest wait behind a "+N" card.
		more, moreW = len(cards)-(fit-1), 6*f.cellW
		cards = cards[len(cards)-(fit-1):]
	}
	avail := right - left - gap*float64(len(cards)-1)
	if more > 0 {
		avail -= moreW + gap
	}
	w := avail / float64(len(cards))
	x := left
	for i, j := range cards {
		g.drawCard(dst, j, i+1, x, y, w, h, now)
		x += w + gap
	}
	if more > 0 {
		mw := right - x
		fillRoundRect(dst, x, y, mw, h, f.lineH/3, mixRGBA(g.theme.Background, g.theme.Border, 0.5))
		label := fmt.Sprintf("+%d", more)
		g.drawText(dst, label, x+(mw-float64(len(label))*f.cellW)/2, y+(h-f.lineH)/2, g.theme.Muted, 1)
		g.stripHits = append(g.stripHits, stripHit{x0: x, y0: y, x1: right, y1: y + h})
	}
}

// drawCard draws job j's card, number n, in the box (x, y, w, h).
func (g *Game) drawCard(dst *ebiten.Image, j *jobs.Job, n int, x, y, w, h float64, now time.Time) {
	f := g.faces
	wt := g.watches[j]
	if wt == nil {
		wt = &jobWatch{name: jobName(j.Command)}
	}
	// Cards rise in when they show up and fade before they leave.
	alpha, dy := 1.0, 0.0
	if g.cfg.Animation.Enabled {
		if age := now.Sub(wt.shownAt); age < stripEnter {
			p := easeOutCubic(float64(age) / float64(stripEnter))
			alpha, dy = p, (1-p)*f.lineH*0.6
		}
		if !j.Running() {
			if left := g.linger(j) - now.Sub(j.Ended); left < stripFadeOut {
				alpha *= math.Max(0, float64(left)/float64(stripFadeOut))
			}
		}
	}
	y += dy
	mx, my := ebiten.CursorPosition()
	hover := float64(mx) >= x && float64(mx) < x+w && float64(my) >= y && float64(my) < y+h
	state := g.jobColor(j)

	bg := mixRGBA(g.theme.Background, g.theme.Border, 0.45)
	if hover {
		bg = mixRGBA(g.theme.Background, g.theme.Border, 0.8)
	}
	r := f.lineH / 3
	fillRoundRect(dst, x, y, w, h, r, scaleAlpha(bg, alpha))
	// A new error flashes the card's outline.
	if p := float64(now.Sub(wt.flashAt)) / float64(stripFlash); !wt.flashAt.IsZero() && p < 1 && g.cfg.Animation.Enabled {
		glow := scaleAlpha(g.theme.Error, alpha*(1-p))
		strokeRoundRect(dst, x, y, w, h, r, 2*g.scale, glow)
	}
	// The state's color runs down the card's left edge.
	vector.FillRect(dst, float32(x+r/3), float32(y+r), float32(math.Max(2, 2*g.scale)), float32(h-2*r), scaleAlpha(state, alpha), true)

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
	var right string
	switch {
	case hover && n <= 9:
		right = fmt.Sprintf("ctrl+%d", n)
	case wt.url != "":
		right = strings.TrimPrefix(strings.TrimPrefix(wt.url, "http://"), "https://")
	}
	nameCols := utf8.RuneCountInString(wt.name)
	// The right side gets what the name and time leave, if that's enough.
	rightCols := min(utf8.RuneCountInString(right), cols-2-nameCols-9)
	if rightCols < 8 {
		right, rightCols = "", 0
	}
	right = truncate(right, rightCols)
	leftCols := cols - 2
	if rightCols > 0 {
		leftCols -= rightCols + 1
	}
	g.drawText(dst, truncate(wt.name, leftCols), tx+2*f.cellW, ty, g.theme.Foreground, alpha)
	if rest := leftCols - nameCols; rest > 3 {
		g.drawText(dst, truncate(" · "+formatElapsed(j.Elapsed(now)), rest), tx+float64(2+nameCols)*f.cellW, ty, g.theme.Muted, alpha)
	}
	if right != "" {
		rx := x + w - pad - float64(rightCols)*f.cellW
		clr := g.theme.Muted
		if wt.url != "" && !hover {
			clr = g.theme.Accent
			uy := float32(ty + f.underlineY)
			vector.StrokeLine(dst, float32(rx), uy, float32(rx+float64(rightCols)*f.cellW), uy, float32(g.scale), scaleAlpha(clr, alpha*0.6), false)
			g.stripHits = append(g.stripHits, stripHit{x0: rx, y0: ty, x1: rx + float64(rightCols)*f.cellW, y1: ty + f.lineH, url: wt.url})
		}
		g.drawText(dst, right, rx, ty, clr, alpha)
	}

	// The last lines, errors in red.
	for i, line := range j.Tail(g.cfg.Jobs.StripLines) {
		clr, a := g.theme.Foreground, alpha*0.7
		if isError(line) {
			clr, a = g.theme.Error, alpha
		}
		g.drawText(dst, truncate(strings.TrimSpace(line), cols), tx, ty+float64(i+1)*f.lineH, clr, a)
	}
	g.stripHits = append(g.stripHits, stripHit{x0: x, y0: y, x1: x + w, y1: y + h, job: j})
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
