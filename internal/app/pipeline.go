package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/informeai/doted/internal/jobs"
	"github.com/informeai/doted/internal/pipeline"
	"github.com/informeai/doted/internal/terminal"
)

// Pipelines are named sequences of commands kept in pipeline.toml, next to
// config.toml:
//
//   - `pipeline` lists them;
//   - `pipeline <name> [args]` runs one in the background as a single job,
//     with one card named after it: the card shows the step running
//     ("step 2/3") and its output as it comes, and the first step that
//     fails stops the rest. The pipeline's command line keeps the result;
//   - `pipeline <name> --from n` starts at step n; `--from` alone, at the
//     step that failed last time;
//   - `pipeline record <name>` records the commands run from then on, less
//     those that fail, until `pipeline stop` saves them to the file.
//
// The job runs a script made of the steps; see pipeline.Script.

// pipelineJob is what's known of a job running a pipeline.
type pipelineJob struct {
	name string
	line string // what followed `pipeline`, to run it again
	step int    // the step running, from its marker; 0 before the first
	what string // and what it runs
	of   int
}

// pipelineRecording is a pipeline being recorded.
type pipelineRecording struct {
	name  string
	steps []recordedStep
}

type recordedStep struct {
	cmd string
	job *jobs.Job // nil for a pipeline run, which is kept whatever happens
}

func (g *Game) pipelinePath() string { return pipeline.Path(g.configPath) }

// runPipeline handles a `pipeline ...` line typed at the prompt, the newest
// line of the main view.
func (g *Game) runPipeline(arg string, now time.Time) {
	fields := strings.Fields(arg)
	if len(fields) == 0 {
		g.listPipelines()
		return
	}
	switch fields[0] {
	case "record":
		g.startRecording(fields[1:])
		return
	case "stop":
		g.stopRecording()
		return
	case "remove":
		g.removePipeline(fields[1:])
		return
	}
	line := strings.Join(fields, " ")
	j, err := g.startPipeline(line, now)
	if err != nil {
		g.pipelineError(err.Error())
		return
	}
	g.startBlock("pipeline "+line, j, true, now)
	if g.recording != nil {
		g.recording.steps = append(g.recording.steps, recordedStep{cmd: "pipeline " + stripFrom(fields)})
	}
}

// startPipeline starts the job that runs `pipeline <line>`.
func (g *Game) startPipeline(line string, now time.Time) (*jobs.Job, error) {
	name, from, args, err := parsePipelineArgs(strings.Fields(line))
	if err != nil {
		return nil, err
	}
	ps, err := pipeline.Load(g.pipelinePath())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", pipeline.FileName, err)
	}
	stages, err := pipeline.Plan(ps, name, args)
	if err != nil {
		return nil, err
	}
	switch {
	case from == -1: // --from alone: where it failed last time
		last, ok := g.pipeFailed[name]
		if !ok {
			return nil, fmt.Errorf("pipeline %s hasn't failed in this session: say which step, --from 2", name)
		}
		from = last
	case from == 0:
		from = 1
	case from > len(stages):
		return nil, fmt.Errorf("pipeline %s has %d %s", name, len(stages), plural(len(stages), "step", "steps"))
	}
	script, err := pipeline.Script(name, stages, from-1)
	if err != nil {
		return nil, err
	}
	j, err := g.jobs.Start(g.session, script, g.cols, g.outputRows, now)
	if err != nil {
		return nil, err
	}
	j.Command = "pipeline " + line // what the job view and the jobs list show
	j.Listed = true
	if g.pipes == nil {
		g.pipes = map[*jobs.Job]*pipelineJob{}
	}
	g.pipes[j] = &pipelineJob{name: name, line: line, of: len(stages)}
	g.watchOf(j).name = "pipeline " + name
	return j, nil
}

// stripFrom is a pipeline's line without --from, as a recording keeps it.
func stripFrom(fields []string) string {
	name, _, args, _ := parsePipelineArgs(fields)
	return strings.TrimSpace(name + " " + strings.Join(args, " "))
}

// parsePipelineArgs reads `name [--from [n]] [args...]`; from is 0 without
// --from and -1 for --from without a number.
func parsePipelineArgs(fields []string) (name string, from int, args []string, err error) {
	name = fields[0]
	for i := 1; i < len(fields); i++ {
		f := fields[i]
		if f != "--from" && !strings.HasPrefix(f, "--from=") {
			args = append(args, f)
			continue
		}
		value, hasValue := strings.CutPrefix(f, "--from=")
		if !hasValue && i+1 < len(fields) {
			if _, err := strconv.Atoi(fields[i+1]); err == nil {
				value, hasValue = fields[i+1], true
				i++
			}
		}
		if !hasValue {
			from = -1
			continue
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 {
			return "", 0, nil, fmt.Errorf("--from takes a step number, from 1")
		}
		from = n
	}
	return name, from, args, nil
}

// followPipeline reads a marker line of job j's output, reporting whether
// it was one.
func (g *Game) followPipeline(j *jobs.Job, line string) bool {
	p := g.pipes[j]
	if p == nil {
		return false
	}
	m, ok := pipeline.ParseMarker(line)
	if !ok || m.Name != p.name {
		return false
	}
	p.step, p.of, p.what = m.Step, m.Steps, m.What
	return true
}

// pipelineLabel is what a pipeline's card says on the right while it runs.
func (g *Game) pipelineLabel(j *jobs.Job) string {
	p := g.pipes[j]
	if p == nil || p.step == 0 {
		return ""
	}
	return fmt.Sprintf("step %d/%d", p.step, p.of)
}

// pipelineJobDone tells how a pipeline's job ended, and drops a recorded
// command that failed.
func (g *Game) pipelineJobDone(j *jobs.Job) {
	failed := j.Killed || j.Status != ""
	if r := g.recording; r != nil && failed {
		for i, s := range r.steps {
			if s.job == j {
				r.steps = append(r.steps[:i], r.steps[i+1:]...)
				break
			}
		}
	}
	p := g.pipes[j]
	if p == nil {
		return
	}
	// The last marker says where it stopped.
	for i := j.Output.Len() - 1; i >= 0; i-- {
		if g.followPipeline(j, j.Output.At(i).Text()) {
			break
		}
	}
	if g.pipeFailed == nil {
		g.pipeFailed = map[string]int{}
	}
	switch {
	case j.Killed || g.watchOf(j).restarting:
	case failed && p.step > 0:
		g.pipeFailed[p.name] = p.step
		g.notify(terminal.Error, fmt.Sprintf("✗ pipeline %s failed at step %d/%d: %s · %s · pipeline %s --from %d resumes there",
			p.name, p.step, p.of, p.what, strings.Replace(jobResult(j), "exit status ", "exit ", 1), p.name, p.step))
	case !failed:
		delete(g.pipeFailed, p.name)
	}
}

func (g *Game) listPipelines() {
	path := g.pipelinePath()
	ps, err := pipeline.Load(path)
	if err != nil {
		g.pipelineError(pipeline.FileName + ": " + err.Error())
		return
	}
	now := time.Now()
	if len(ps) == 0 {
		g.scrollback.Append(terminal.System, "no pipelines yet: write them in "+shortPath(path)+" or record one with pipeline record <name>", now)
		return
	}
	width := 0
	for _, p := range ps {
		width = max(width, len(p.Name))
	}
	g.scrollback.Append(terminal.System, "pipelines in "+shortPath(path)+":", now)
	for _, p := range ps {
		var what string
		switch {
		case p.Description != "":
			what = p.Description
		case p.Parallel:
			what = strings.Join(p.Steps, " · ")
		default:
			what = strings.Join(p.Steps, " → ")
		}
		kind := fmt.Sprintf("%d %s", len(p.Steps), plural(len(p.Steps), "step", "steps"))
		if p.Parallel {
			kind += " in parallel"
		}
		g.scrollback.Append(terminal.System, fmt.Sprintf("  %-*s  %s · %s", width, p.Name, kind, what), now)
	}
}

// removePipeline takes pipelines out of pipeline.toml.
func (g *Game) removePipeline(names []string) {
	if len(names) == 0 {
		g.pipelineError("usage: pipeline remove <name>")
		return
	}
	path := g.pipelinePath()
	ps, err := pipeline.Load(path)
	if err != nil {
		g.pipelineError(pipeline.FileName + ": " + err.Error())
		return
	}
	for _, name := range names {
		if _, ok := pipeline.Find(ps, name); !ok {
			g.pipelineError("no pipeline called " + name)
			continue
		}
		if _, err := pipeline.Remove(path, name); err != nil {
			g.pipelineError("pipeline " + name + " wasn't removed: " + err.Error())
			continue
		}
		msg := "removed pipeline " + name + " from " + shortPath(path)
		var callers []string
		for _, p := range ps {
			for _, step := range p.Steps {
				if f := strings.Fields(step); len(f) >= 2 && f[0] == "pipeline" && f[1] == name && p.Name != name {
					callers = append(callers, p.Name)
					break
				}
			}
		}
		if len(callers) > 0 {
			msg += " · " + strings.Join(callers, ", ") + " still " + plural(len(callers), "calls", "call") + " it"
		}
		g.scrollback.Append(terminal.System, msg, time.Now())
	}
}

func (g *Game) startRecording(args []string) {
	if g.recording != nil {
		g.pipelineError("already recording pipeline " + g.recording.name + " · pipeline stop saves it")
		return
	}
	if len(args) != 1 {
		g.pipelineError("usage: pipeline record <name>")
		return
	}
	if err := pipeline.CheckName(args[0]); err != nil {
		g.pipelineError(err.Error())
		return
	}
	g.recording = &pipelineRecording{name: args[0]}
	g.scrollback.Append(terminal.System, "recording pipeline "+args[0]+": the commands you run from now on, less those that fail · pipeline stop saves it", time.Now())
}

func (g *Game) stopRecording() {
	r := g.recording
	if r == nil {
		g.pipelineError("not recording · pipeline record <name> starts")
		return
	}
	g.recording = nil
	if len(r.steps) == 0 {
		g.scrollback.Append(terminal.System, "nothing recorded: pipeline "+r.name+" wasn't saved", time.Now())
		return
	}
	steps := make([]string, len(r.steps))
	for i, s := range r.steps {
		steps[i] = s.cmd
	}
	path := g.pipelinePath()
	replaced, err := pipeline.Save(path, r.name, steps)
	if err != nil {
		g.pipelineError("pipeline " + r.name + " wasn't saved: " + err.Error())
		return
	}
	verb := "saved"
	if replaced {
		verb = "replaced"
	}
	g.scrollback.Append(terminal.System, fmt.Sprintf("%s pipeline %s · %d %s in %s · pipeline %s runs it", verb, r.name, len(steps), plural(len(steps), "step", "steps"), shortPath(path), r.name), time.Now())
}

// record adds a command just started from the prompt to the recording.
func (g *Game) record(cmd string, j *jobs.Job) {
	if g.recording != nil {
		g.recording.steps = append(g.recording.steps, recordedStep{cmd: cmd, job: j})
	}
}

// recordingHint is the status line while recording.
func (g *Game) recordingHint() string {
	r := g.recording
	if r == nil {
		return ""
	}
	return fmt.Sprintf("● recording %s · %d %s · pipeline stop saves", r.name, len(r.steps), plural(len(r.steps), "step", "steps"))
}

func (g *Game) pipelineError(msg string) {
	g.scrollback.Append(terminal.Error, msg, time.Now())
}
