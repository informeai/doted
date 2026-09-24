package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/informeai/doted/internal/terminal"
)

// Holding Cmd (Ctrl on Linux and Windows) turns URLs and paths to existing
// files in the output into links: they underline under the mouse and a
// click opens them, URLs in the browser and files in the editor at the line
// and column written after them (main.go:42:7).

// link is a URL or file found in a line: runes [start, end) of it.
type link struct {
	start, end int
	url        string
	file       string // absolute
	line, col  int    // 0 when not given
}

// linkDelims end a word that could be a link.
const linkDelims = " \t\"'`()<>[]{}|"

var fileSuffix = regexp.MustCompile(`^(.+?)(?::(\d+))?(?::(\d+))?$`)

// linkAt finds the link around col in text, resolving relative paths from
// dir. Files must exist.
func linkAt(text []rune, col int, dir string) (link, bool) {
	if col < 0 || col >= len(text) || strings.ContainsRune(linkDelims, text[col]) {
		return link{}, false
	}
	start, end := col, col+1
	for start > 0 && !strings.ContainsRune(linkDelims, text[start-1]) {
		start--
	}
	for end < len(text) && !strings.ContainsRune(linkDelims, text[end]) {
		end++
	}
	// Punctuation that ends a sentence or a compiler message isn't part of it.
	for end > start && strings.ContainsRune(".,;:!?", text[end-1]) {
		end--
	}
	if col >= end {
		return link{}, false
	}
	word := terminal.StringOf(text[start:end])

	if strings.HasPrefix(word, "http://") || strings.HasPrefix(word, "https://") {
		return link{start: start, end: end, url: word}, len(word) > len("https://")
	}
	if rest, ok := strings.CutPrefix(word, "file://"); ok {
		word = rest
	}
	m := fileSuffix.FindStringSubmatch(word)
	path := m[1]
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return link{}, false
		}
		path = filepath.Join(home, rest)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	// A bare name like "done" or "." is too likely to be a word.
	if !strings.ContainsAny(m[1], "/.") || m[1] == "." || m[1] == ".." {
		return link{}, false
	}
	if _, err := os.Stat(path); err != nil {
		return link{}, false
	}
	l := link{start: start, end: end, file: path}
	l.line, _ = strconv.Atoi(m[2])
	l.col, _ = strconv.Atoi(m[3])
	return l, true
}

// linkModifier is the key that turns on links.
func linkModifier() bool {
	if runtime.GOOS == "darwin" {
		return ebiten.IsKeyPressed(ebiten.KeyMeta)
	}
	return ebiten.IsKeyPressed(ebiten.KeyControl)
}

// hoveredLink is the link under the mouse in the output, with the row it's
// on, while the link modifier is held.
func (g *Game) hoveredLink(x, y float64) (link, visibleRow, bool) {
	if !linkModifier() || g.faces == nil || x < g.outLeft {
		return link{}, visibleRow{}, false
	}
	sb := g.visibleScrollback()
	for _, r := range g.rows {
		if y < r.y || y >= r.y+g.faces.lineH {
			continue
		}
		col := r.start + int((x-g.outLeft)/g.faces.cellW)
		if col >= r.start+r.n {
			break
		}
		l, ok := g.cachedLinkAt(r.seq, col, lineRunes(sb, r.seq))
		return l, r, ok
	}
	return link{}, visibleRow{}, false
}

// cachedLinkAt is linkAt, remembered for the last spot so hovering doesn't
// check the disk every frame.
func (g *Game) cachedLinkAt(seq, col int, text []rune) (link, bool) {
	key := linkKey{seq, col, len(text), g.session.Dir()}
	if g.linkCache.key != key || !g.linkCache.valid {
		l, ok := linkAt(text, col, key.dir)
		g.linkCache = linkCache{key: key, valid: true, link: l, ok: ok}
	}
	return g.linkCache.link, g.linkCache.ok
}

type linkKey struct {
	seq, col, n int
	dir         string
}

type linkCache struct {
	key   linkKey
	valid bool
	link  link
	ok    bool
}

// handleLinkClick opens the link clicked with the modifier held, reporting
// whether there was one.
func (g *Game) handleLinkClick(x, y float64) bool {
	if !inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		return false
	}
	l, _, ok := g.hoveredLink(x, y)
	if !ok {
		return false
	}
	g.openLink(l)
	return true
}

func (g *Game) openLink(l link) {
	args := g.linkCommand(l)
	name := l.url
	if name == "" {
		name = filepath.Base(l.file)
	}
	if err := g.opener(g, args); err != nil {
		g.flash("could not open " + name + ": " + err.Error())
		return
	}
	g.flash("opening " + truncate(name, 40))
}

// linkCommand is the command line that opens l.
func (g *Game) linkCommand(l link) []string {
	if l.url != "" {
		return systemOpener(l.url)
	}
	line, col := max(l.line, 1), max(l.col, 1)
	if tmpl := strings.TrimSpace(g.cfg.Links.Editor); tmpl != "" {
		r := strings.NewReplacer("{file}", l.file, "{line}", strconv.Itoa(line), "{col}", strconv.Itoa(col))
		var args []string
		for _, f := range strings.Fields(tmpl) {
			args = append(args, r.Replace(f)) // per word, so paths with spaces survive
		}
		return args
	}
	if _, ok := g.session.LookPath("code"); ok {
		return []string{"code", "-g", l.file + ":" + strconv.Itoa(line) + ":" + strconv.Itoa(col)}
	}
	return systemOpener(l.file)
}

func systemOpener(target string) []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"open", target}
	case "windows":
		return []string{"rundll32", "url.dll,FileProtocolHandler", target}
	}
	return []string{"xdg-open", target}
}

// startDetached runs args with the session's environment without waiting
// for it. It's the default Game.opener.
func startDetached(g *Game, args []string) error {
	path := args[0]
	if !filepath.IsAbs(path) {
		if p, ok := g.session.LookPath(path); ok {
			path = p
		}
	}
	cmd := exec.Command(path, args[1:]...)
	cmd.Dir = g.session.Dir()
	cmd.Env = append(os.Environ(), "PATH="+g.session.Getenv("PATH"))
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() //nolint:errcheck // nothing to report once it's running
	return nil
}

// drawLinkHover underlines the link under the mouse in the output.
func (g *Game) drawLinkHover(dst *ebiten.Image) {
	mx, my := ebiten.CursorPosition()
	l, r, ok := g.hoveredLink(float64(mx), float64(my))
	if !ok {
		return
	}
	f := g.faces
	// The link may wrap over several rows; underline all that are drawn.
	for _, row := range g.rows {
		if row.seq != r.seq {
			continue
		}
		from, to := max(l.start, row.start), min(l.end, row.start+row.n)
		if to <= from {
			continue
		}
		x0 := g.outLeft + float64(from-row.start)*f.cellW
		x1 := g.outLeft + float64(to-row.start)*f.cellW
		uy := float32(row.y + f.underlineY)
		vector.StrokeLine(dst, float32(x0), uy, float32(x1), uy, float32(max(1, g.scale)), g.theme.Accent, false)
	}
}
