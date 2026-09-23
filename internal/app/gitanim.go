package app

import (
	"math"
	"time"
)

// The branch on the status line animates when it changes:
//
//   - switching branches rolls the name like the path does on cd, only the
//     letters that differ, while the Git logo turns a quarter (the square
//     lands on the same shape);
//   - a branch not seen before in this session, like one just made with
//     git switch -c, also throws sparks from the logo;
//   - entering or leaving a repository fades the logo, name and changes in
//     or out.
//
// All of it follows [animation] enabled, and the sparks [animation]
// particles.

const (
	branchSpin      = 350 * time.Millisecond
	branchFade      = 280 * time.Millisecond
	branchNewSparks = 16
)

type branchAnim struct {
	started bool // a first lookup came back; it appears without animating
	name    pathRoll
	spun    time.Time // when the logo started its quarter turn

	// Fading in (entered a repository) or out (left one, still drawing
	// leaving meanwhile).
	faded   time.Time
	fadeIn  bool
	leaving projectContext

	seen   map[string]bool // branches shown in this session
	sparks *sparks         // around the logo's center, in logical px
	burst  bool            // fire the new-branch sparks on the next tick
	center [2]float64      // the logo's center on screen in the last frame

	// Deleted branches and tags; see gitdelete.go.
	deletions     []deletion
	redSparks     *sparks // on screen, in logical px
	delName, delY float64 // where the last frame drew the deleted name
}

// stepBranch moves the logo's sparks along; Update calls it every tick.
func (g *Game) stepBranch() {
	a := &g.branch
	g.stepDeletion(time.Now())
	if a.redSparks != nil {
		a.redSparks.step(tickSeconds)
	}
	if a.sparks == nil {
		return
	}
	if a.burst {
		a.burst = false
		a.sparks.burstAt(branchNewSparks, 0, 0)
	}
	a.sparks.step(tickSeconds)
}

// trackBranch starts the animations for a lookup that just came back, info,
// which replaces prev.
func (g *Game) trackBranch(prev, info projectContext, now time.Time) {
	g.queueDeletions(prev, info, now)
	a := &g.branch
	if a.seen == nil {
		a.seen = map[string]bool{}
		a.sparks = newSparks(uint64(now.UnixNano()))
	}
	old, cur := string(a.name.to), info.branch
	seenBefore := a.seen[cur]
	if cur != "" {
		a.seen[cur] = true
	}
	if !a.started || !g.cfg.Animation.Enabled {
		a.started = true
		a.name = pathRoll{from: []rune(cur), to: []rune(cur)}
		return
	}
	switch {
	case old == cur:
		return
	case old == "": // entered a repository
		a.name = pathRoll{from: []rune(cur), to: []rune(cur)}
		a.faded, a.fadeIn = now, true
	case cur == "": // left one
		a.leaving = prev
		a.name = pathRoll{}
		a.faded, a.fadeIn = now, false
	default: // switched branches
		a.name = newPathRoll([]rune(old), []rune(cur), now)
		a.spun = now
		if !seenBefore && g.cfg.Animation.Particles {
			a.burst = true
		}
	}
}

// fade is how visible the branch is, 0 to 1, and what to draw.
func (a *branchAnim) fade(info projectContext, now time.Time) (projectContext, float64) {
	if a.faded.IsZero() {
		return info, 1
	}
	p := math.Min(1, float64(now.Sub(a.faded))/float64(branchFade))
	if a.fadeIn {
		return info, easeOutCubic(p)
	}
	if p >= 1 {
		return info, 1 // done: nothing left to draw
	}
	return a.leaving, 1 - easeOutCubic(p)
}

// spin is the logo's angle in its quarter turn.
func (a *branchAnim) spin(now time.Time) float64 {
	if a.spun.IsZero() {
		return 0
	}
	p := math.Min(1, float64(now.Sub(a.spun))/float64(branchSpin))
	if p >= 1 {
		return 0 // a full quarter turn looks the same as none
	}
	return easeOutCubic(p) * math.Pi / 2
}

func easeOutCubic(p float64) float64 { return 1 - math.Pow(1-p, 3) }
