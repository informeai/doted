package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/informeai/doted/internal/explain"
	"github.com/informeai/doted/internal/jobs"
	"github.com/informeai/doted/internal/pipeline"
	"github.com/informeai/doted/internal/terminal"
)

// `explain` asks an AI command line tool (Claude Code, for now) why a
// command failed and how to fix it:
//
//   - `explain` is about the last command that failed in the main view;
//   - `explain #3` (or `explain 3`) about job 3, running or not: a server
//     that broke, a watcher printing errors, a pipeline, whose failed step
//     is what's explained;
//   - a tool's name after it picks the tool, `explain #3 claude`, instead of
//     [explain] tool;
//   - a failed command's line offers it too, under the mouse.
//
// The tool runs as an ordinary command in the current directory: its answer
// is its output, in a block, and Ctrl+B sends it to the background like any
// other. Lines of the answer starting with ▸ are commands: clicking one puts
// it on the prompt, to be checked and run.

const defaultExplainLines = 150

// runExplain handles an `explain [#n]` line typed at the prompt, the newest
// line of the main view.
func (g *Game) runExplain(arg string, now time.Time) {
	seq := g.explainSeq
	g.explainSeq = 0
	j, about, err := g.startExplain(arg, seq, now)
	if err != nil {
		g.scrollback.Append(terminal.Error, "explain: "+err.Error(), now)
		return
	}
	g.startBlock(strings.TrimSpace("explain "+arg), j, false, now)
	g.markExplain(g.scrollback.Seq(g.scrollback.Len() - 1))
	g.scrollback.Append(terminal.System, about, now)
	g.attached = j
	g.parser.Begin()
}

// parseExplainArgs reads `[#n] [tool]`, in either order: the job, "" for
// none, and the tool, "" for [explain] tool.
func parseExplainArgs(arg string) (job, tool string, err error) {
	for _, f := range strings.Fields(arg) {
		_, numErr := strconv.Atoi(strings.TrimPrefix(f, "#"))
		switch {
		case numErr == nil && job == "":
			job = f
		case numErr == nil:
			return "", "", fmt.Errorf("one job at a time: explain #n [tool]")
		case tool != "":
			return "", "", fmt.Errorf("usage: explain [#n] [tool]")
		default:
			if _, ok := explain.Tools[f]; !ok {
				return "", "", fmt.Errorf("%q isn't a job or a tool doted knows (%s) · usage: explain [#n] [tool]", f, explain.ToolNames())
			}
			tool = f
		}
	}
	return job, tool, nil
}

// startExplain starts the tool on what arg names, or on the block at seq
// when set; about says what that is and who's asked.
func (g *Game) startExplain(arg string, seq int, now time.Time) (j *jobs.Job, about string, err error) {
	jobArg, name, err := parseExplainArgs(arg)
	if err != nil {
		return nil, "", err
	}
	// [explain] model goes with [explain] tool; another tool uses its own.
	model := ""
	if name == "" || name == g.cfg.Explain.Tool {
		name, model = g.cfg.Explain.Tool, g.cfg.Explain.Model
	}
	if name == "" {
		name = "claude"
	}
	tool, ok := explain.Tools[name]
	if !ok {
		return nil, "", fmt.Errorf("[explain] tool = %q isn't known; doted knows %s", name, explain.ToolNames())
	}
	program := ""
	for _, p := range tool.Programs {
		if g.session.FindCommand(p) {
			program = p
			break
		}
	}
	if program == "" {
		return nil, "", fmt.Errorf("%s isn't installed (%s): %s", name, strings.Join(tool.Programs, " or "), tool.Install)
	}
	var f explain.Failure
	switch {
	case jobArg != "":
		f, about, err = g.jobFailure(jobArg)
	case seq != 0:
		f, about, err = g.blockFailure(seq)
	default:
		f, about, err = g.lastFailure()
	}
	if err != nil {
		return nil, "", err
	}
	f.Dir, f.System = shortPath(g.session.Dir()), g.systemName()
	if info, ok := g.currentContext(); ok {
		f.Branch = info.branch
	}
	line, err := explain.CommandLine(tool, program, f, model, g.explainLanguage())
	if err != nil {
		return nil, "", err
	}
	j, err = g.jobs.Start(g.session, line, g.cols, g.outputRows, now)
	if err != nil {
		return nil, "", err
	}
	about += " · asking " + name + "…"
	j.Command = strings.TrimSpace("explain " + arg) // what its card and the jobs list show
	if g.explains == nil {
		g.explains = map[*jobs.Job]string{}
	}
	g.explains[j] = arg
	return j, about, nil
}

// lastFailure is the newest command of the main view that failed.
func (g *Game) lastFailure() (explain.Failure, string, error) {
	best := -1
	for seq, b := range g.blocks {
		if seq > best && !b.running() && (b.status != "" || b.killed) && !g.explainSeqs[seq] {
			if _, ok := g.scrollback.Index(seq); ok {
				best = seq
			}
		}
	}
	if best < 0 {
		return explain.Failure{}, "", fmt.Errorf("no command has failed here · explain #n asks about job n")
	}
	return g.blockFailure(best)
}

// blockFailure is the command whose block starts at seq.
func (g *Game) blockFailure(seq int) (explain.Failure, string, error) {
	b := g.blocks[seq]
	if b == nil {
		return explain.Failure{}, "", fmt.Errorf("that command is gone from the screen")
	}
	if b.background && b.job != nil {
		return g.failureOf(b.job) // its output is the job's
	}
	var out []string
	if i, ok := g.scrollback.Index(seq); ok {
		for k := i + 1; k < g.scrollback.Len() && g.scrollback.At(k).Kind != terminal.Command; k++ {
			out = append(out, strings.TrimRight(g.scrollback.At(k).Text(), " "))
		}
	}
	result := "exit 0"
	switch {
	case b.killed:
		result = "killed"
	case b.status != "":
		result = strings.Replace(b.status, "exit status ", "exit ", 1)
	}
	f := explain.Failure{Command: b.cmd, Result: result, Output: g.lastLines(out)}
	return f, truncate(b.cmd, 40) + " · " + result, nil
}

// jobFailure is job arg ("#3" or "3").
func (g *Game) jobFailure(arg string) (explain.Failure, string, error) {
	id, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(arg), "#"))
	j := g.jobs.Get(id)
	if err != nil || j == nil {
		return explain.Failure{}, "", fmt.Errorf("no job %s · ctrl+t lists them", arg)
	}
	return g.failureOf(j)
}

// failureOf is job j: how it ended or is doing, and the end of its output.
// A pipeline's is the step it stopped at, from that step's marker.
func (g *Game) failureOf(j *jobs.Job) (explain.Failure, string, error) {
	cmd := j.Command
	var out []string
	for i := range j.Output.Len() {
		line := strings.TrimRight(j.Output.At(i).Text(), " ")
		if m, ok := pipeline.ParseMarker(line); ok && g.pipes[j] != nil {
			cmd = fmt.Sprintf("%s (step %d/%d of %s)", m.What, m.Step, m.Steps, j.Command)
			out = out[:0]
			continue
		}
		out = append(out, line)
	}
	var result string
	switch {
	case j.Running() && g.watches[j] != nil && g.watches[j].failing:
		result = "still running, printing errors"
	case j.Running():
		result = "still running"
	default:
		result = strings.Replace(jobResult(j), "exit status ", "exit ", 1)
		if result == "done" {
			result = "exit 0"
		}
	}
	f := explain.Failure{Command: cmd, Result: result, Output: g.lastLines(out)}
	return f, fmt.Sprintf("job #%d · %s · %s", j.ID, truncate(j.Command, 40), result), nil
}

// lastLines keeps the end of out, as much as [explain] lines asks.
func (g *Game) lastLines(out []string) []string {
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	n := g.cfg.Explain.Lines
	if n <= 0 {
		n = defaultExplainLines
	}
	return out[max(0, len(out)-n):]
}

// explainLanguage is what the answer comes in: [explain] language, or the
// one $LANG names ("pt_BR.UTF-8" is pt-BR).
func (g *Game) explainLanguage() string {
	if l := g.cfg.Explain.Language; l != "" {
		return l
	}
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := g.session.Getenv(key)
		if v == "" {
			continue
		}
		v, _, _ = strings.Cut(v, ".")
		if v == "C" || v == "POSIX" {
			return ""
		}
		return strings.ReplaceAll(v, "_", "-")
	}
	return ""
}

func (g *Game) systemName() string {
	osName := map[string]string{"darwin": "macOS", "linux": "Linux", "windows": "Windows"}[runtime.GOOS]
	if osName == "" {
		osName = runtime.GOOS
	}
	sh := g.cfg.Shell.Program
	if sh == "" {
		sh = os.Getenv("SHELL")
	}
	if sh == "" {
		return osName
	}
	return osName + ", " + filepath.Base(sh)
}

// markExplain remembers that the block at seq holds an answer.
func (g *Game) markExplain(seq int) {
	if g.explainSeqs == nil {
		g.explainSeqs = map[int]bool{}
	}
	g.explainSeqs[seq] = true
}

// explainBlockAction explains the failed block at seq, from its line.
func (g *Game) explainBlockAction(seq int) {
	b := g.blocks[seq]
	if b == nil {
		return
	}
	if g.attached != nil || g.viewing != nil {
		g.flash("a command is running · wait or ctrl+b first")
		return
	}
	line := "explain"
	if b.background && b.job != nil && b.job.Listed {
		line = fmt.Sprintf("explain #%d", b.job.ID)
	} else {
		g.explainSeq = seq
	}
	g.editor.Reset()
	g.editor.Insert([]rune(line)...)
	g.submit()
}

// suggestionAt reports whether line i of sb is a command an answer
// suggests, and which.
func (g *Game) suggestionAt(sb *terminal.Scrollback, i int) (string, bool) {
	if sb != g.scrollback || len(g.explainSeqs) == 0 {
		return "", false
	}
	cmd, ok := explain.Suggestion(sb.At(i).Text())
	if !ok {
		return "", false
	}
	for k := i - 1; k >= 0; k-- {
		if sb.At(k).Kind == terminal.Command {
			return cmd, g.explainSeqs[sb.Seq(k)]
		}
	}
	return "", false
}

// drawSuggestion marks a suggested command's row at y, brighter under the
// mouse, and makes it clickable.
func (g *Game) drawSuggestion(dst *ebiten.Image, seq int, x, y float64) {
	f := g.faces
	w := float64(g.cols) * f.cellW
	mx, my := ebiten.CursorPosition()
	alpha := 0.07
	if float64(mx) >= x && float64(mx) < x+w && float64(my) >= y && float64(my) < y+f.lineH {
		alpha = 0.18
	}
	vector.FillRect(dst, float32(x-f.cellW/2), float32(y), float32(w+f.cellW), float32(f.lineH), scaleAlpha(g.theme.Accent, alpha), false)
	g.actions = append(g.actions, blockAction{kind: actSuggest, seq: seq, x0: x, x1: x + w, y0: y, y1: y + f.lineH})
}

// useSuggestion puts the command suggested on the line at seq on the
// prompt, to be checked and run.
func (g *Game) useSuggestion(seq int) {
	i, ok := g.scrollback.Index(seq)
	if !ok {
		return
	}
	cmd, ok := g.suggestionAt(g.scrollback, i)
	if !ok {
		return
	}
	if g.attached != nil {
		g.flash("a command is running · wait or ctrl+b first")
		return
	}
	g.editor.Reset()
	g.editor.Insert([]rune(cmd)...)
	g.flash("enter runs it")
	g.touch()
}

// explainJobDone forgets a job that explained.
func (g *Game) explainJobDone(j *jobs.Job) {
	if _, ok := g.explains[j]; ok && !j.Listed {
		delete(g.explains, j)
	}
}
