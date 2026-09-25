//go:build unix

package app

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/informeai/doted/internal/jobs"
	"github.com/informeai/doted/internal/pipeline"
)

func pipelineGame(t *testing.T, file string) *Game {
	t.Helper()
	g := newTestGame(t)
	if err := os.WriteFile(g.pipelinePath(), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	return g
}

const testPipelines = `
[check]
steps = ["echo one", "echo two $1", "echo three"]

[broken]
steps = ["echo first", "false", "echo never"]

[both]
parallel = true
steps = ["echo left", "echo right"]

[all]
steps = ["pipeline both", "pipeline check x"]

[out]
steps = ["echo abc123", "echo \"tag:$OUT\""]

[slow]
steps = ["echo a", "sleep 5", "echo b"]
`

// runPipelineJob runs `pipeline line` and waits for its job to end.
func runPipelineJob(t *testing.T, g *Game, line string) *jobs.Job {
	t.Helper()
	run(g, "pipeline "+line)
	listed := g.jobs.Listed()
	if len(listed) == 0 {
		t.Fatalf("no job started; last line %q", lastLine(g))
	}
	j := listed[len(listed)-1]
	tickUntil(t, g, func() bool { return !j.Running() })
	tick(g)
	return j
}

func TestPipelineRunsAsOneJob(t *testing.T) {
	g := pipelineGame(t, testPipelines)
	j := runPipelineJob(t, g, "check hi")
	if n := len(g.jobs.Listed()); n != 1 {
		t.Fatalf("%d jobs, want one", n)
	}
	out := jobText(j)
	for _, want := range []string{"▸ check 1/3 · echo one", "two hi", "▸ check 3/3 · echo three", "three"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if j.Command != "pipeline check hi" || g.watchOf(j).name != "pipeline check" || j.Status != "" {
		t.Fatalf("command %q, card %q, status %q", j.Command, g.watchOf(j).name, j.Status)
	}
	if _, b := blockFor(t, g, "pipeline check hi"); b.job != j || !b.background {
		t.Fatalf("block = %+v", b)
	}
}

func TestPipelineStopsAtAFailureAndResumes(t *testing.T) {
	g := pipelineGame(t, testPipelines)
	j := runPipelineJob(t, g, "broken")
	if strings.Contains(jobText(j), "never") || !strings.Contains(mainText(g), "✗ pipeline broken failed at step 2/3: false · exit 1 · pipeline broken --from 2 resumes there") {
		t.Fatalf("after failing:\nmain:\n%s\njob:\n%s", mainText(g), jobText(j))
	}

	// --from alone picks up at the failed step.
	j = runPipelineJob(t, g, "broken --from")
	if out := jobText(j); strings.Contains(out, "first") || !strings.Contains(out, "broken 2/3") {
		t.Fatalf("resumed:\n%s", out)
	}

	j = runPipelineJob(t, g, "broken --from 3")
	if out := jobText(j); !strings.Contains(out, "\nnever") || j.Status != "" {
		t.Fatalf("from 3 (%q):\n%s", j.Status, out)
	}
}

func TestPipelineParallelNestedAndOut(t *testing.T) {
	g := pipelineGame(t, testPipelines)
	j := runPipelineJob(t, g, "all")
	out := jobText(j)
	if !strings.Contains(out, "all 1/4 · 2 in parallel: echo left · echo right") || !strings.Contains(out, "left") || !strings.Contains(out, "right") || !strings.Contains(out, "two x") {
		t.Fatalf("nested:\n%s", out)
	}
	j = runPipelineJob(t, g, "out")
	if out := jobText(j); !strings.Contains(out, "tag:abc123") {
		t.Fatalf("$OUT:\n%s", out)
	}
}

func TestPipelineCardFollowsTheSteps(t *testing.T) {
	g := pipelineGame(t, testPipelines)
	run(g, "pipeline slow")
	j := g.jobs.Listed()[0]
	tickUntil(t, g, func() bool {
		g.watchJobs(time.Now())
		return g.pipelineLabel(j) == "step 2/3"
	})
	j.Kill()
	tickUntil(t, g, func() bool { return !j.Running() })
	if strings.Contains(mainText(g), "✗") {
		t.Fatalf("a stopped pipeline isn't a failure:\n%s", mainText(g))
	}
}

func TestPipelineErrorsAndList(t *testing.T) {
	g := pipelineGame(t, testPipelines)
	run(g, "pipeline check")
	if !strings.Contains(lastLine(g), "$1 is missing") || len(g.jobs.Listed()) != 0 {
		t.Fatalf("missing argument: %q", lastLine(g))
	}
	run(g, "pipeline nope")
	if !strings.Contains(lastLine(g), "no pipeline called nope") {
		t.Fatalf("unknown: %q", lastLine(g))
	}
	run(g, "pipeline out --from 2")
	if !strings.Contains(lastLine(g), "start at step 1") {
		t.Fatalf("$OUT from 2: %q", lastLine(g))
	}
	run(g, "pipeline")
	if text := mainText(g); !strings.Contains(text, "pipelines in ") || !strings.Contains(text, "both    2 steps in parallel · echo left · echo right") {
		t.Fatalf("list:\n%s", text)
	}
}

func TestPipelineRestart(t *testing.T) {
	g := pipelineGame(t, testPipelines)
	j := runPipelineJob(t, g, "check again")
	g.restartJob(j)
	listed := g.jobs.Listed()
	next := listed[len(listed)-1]
	if next == j || next.Command != "pipeline check again" {
		t.Fatalf("restarted as %q", next.Command)
	}
	tickUntil(t, g, func() bool { return !next.Running() })
	if !strings.Contains(jobText(next), "two again") {
		t.Fatalf("restarted output:\n%s", jobText(next))
	}
}

func TestPipelineRecord(t *testing.T) {
	g := pipelineGame(t, testPipelines)
	run(g, "pipeline record deploy")
	run(g, "echo built")
	tickUntil(t, g, func() bool { return g.attached == nil })
	run(g, "false") // failed: left out
	tickUntil(t, g, func() bool { return g.attached == nil })
	runPipelineJob(t, g, "both")
	if hint := g.recordingHint(); hint != "● recording deploy · 2 steps · pipeline stop saves" {
		t.Fatalf("hint = %q", hint)
	}
	run(g, "pipeline stop")
	if !strings.Contains(lastLine(g), "saved pipeline deploy · 2 steps") || g.recording != nil {
		t.Fatalf("stop: %q", lastLine(g))
	}
	ps, err := pipeline.Load(g.pipelinePath())
	if err != nil {
		t.Fatal(err)
	}
	p, ok := pipeline.Find(ps, "deploy")
	if !ok || strings.Join(p.Steps, "|") != "echo built|pipeline both" {
		t.Fatalf("saved %+v", p)
	}
	if _, ok := pipeline.Find(ps, "check"); !ok {
		t.Fatal("the other pipelines were lost")
	}
	run(g, "pipeline stop")
	if !strings.Contains(lastLine(g), "not recording") {
		t.Fatalf("second stop: %q", lastLine(g))
	}
}

func TestPipelineRecordCreatesTheFile(t *testing.T) {
	g := newTestGame(t)
	run(g, "pipeline record hello")
	run(g, "echo hi")
	tickUntil(t, g, func() bool { return g.attached == nil })
	run(g, "pipeline stop")
	data, err := os.ReadFile(g.pipelinePath())
	if err != nil || !strings.Contains(string(data), "# doted pipelines") || !strings.Contains(string(data), "[hello]\nsteps = [\n  \"echo hi\",\n]\n") {
		t.Fatalf("file = %q, %v", data, err)
	}
}

func TestPipelineRemove(t *testing.T) {
	g := pipelineGame(t, testPipelines)
	run(g, "pipeline remove both")
	if got := lastLine(g); !strings.Contains(got, "removed pipeline both from") || !strings.HasSuffix(got, "all still calls it") {
		t.Fatalf("remove: %q", got)
	}
	ps, _ := pipeline.Load(g.pipelinePath())
	if _, ok := pipeline.Find(ps, "both"); ok || len(ps) != 5 {
		t.Fatalf("pipelines left: %+v", ps)
	}
	run(g, "pipeline remove both")
	if !strings.Contains(lastLine(g), "no pipeline called both") {
		t.Fatalf("again: %q", lastLine(g))
	}
	run(g, "pipeline remove")
	if !strings.Contains(lastLine(g), "usage: pipeline remove <name>") {
		t.Fatalf("no name: %q", lastLine(g))
	}
}
