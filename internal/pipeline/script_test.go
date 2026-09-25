//go:build unix

package pipeline

import (
	"os/exec"
	"strings"
	"testing"
)

// runScript runs the script in each shell there is, returning its output
// and exit code per shell.
func runScript(t *testing.T, script string) map[string]struct {
	out  string
	code int
} {
	t.Helper()
	res := map[string]struct {
		out  string
		code int
	}{}
	for _, sh := range []string{"bash", "zsh"} {
		path, err := exec.LookPath(sh)
		if err != nil {
			continue
		}
		cmd := exec.Command(path, "-c", script)
		cmd.Dir = t.TempDir()
		out, _ := cmd.CombinedOutput()
		res[sh] = struct {
			out  string
			code int
		}{string(out), cmd.ProcessState.ExitCode()}
	}
	if len(res) == 0 {
		t.Skip("no bash or zsh")
	}
	return res
}

func plain(out string) string {
	var b strings.Builder
	for i := 0; i < len(out); i++ {
		if out[i] == 0x1b {
			for i < len(out) && out[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(out[i])
	}
	return b.String()
}

func TestScriptRunsStepsInOrder(t *testing.T) {
	script, err := Script("check", []Stage{{"mkdir sub && cd sub"}, {"basename \"$PWD\""}, {"echo 'it''s' three"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	for sh, r := range runScript(t, script) {
		want := "▸ check 1/3 · mkdir sub && cd sub\n▸ check 2/3 · basename \"$PWD\"\nsub\n▸ check 3/3 · echo 'it''s' three\nits three\n"
		if got := plain(r.out); got != want || r.code != 0 {
			t.Errorf("%s: code %d, output\n%s", sh, r.code, got)
		}
	}
}

func TestScriptStopsAtAFailure(t *testing.T) {
	script, _ := Script("b", []Stage{{"echo one"}, {"exit 3"}, {"echo never"}}, 0)
	for sh, r := range runScript(t, script) {
		if out := plain(r.out); r.code != 3 || strings.Contains(out, "never") || !strings.Contains(out, "▸ b 2/3 · exit 3") {
			t.Errorf("%s: code %d, output\n%s", sh, r.code, out)
		}
	}
}

func TestScriptPassesOut(t *testing.T) {
	script, err := Script("o", []Stage{{"printf 'a b\\n\\n'"}, {"echo \"got [$OUT]\""}, {"echo ${OUT}!"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	for sh, r := range runScript(t, script) {
		out := plain(r.out)
		if r.code != 0 || !strings.Contains(out, "\na b\n") || !strings.Contains(out, "got [a b]") || !strings.Contains(out, "got [a b]!") {
			t.Errorf("%s: code %d, output\n%s", sh, r.code, out)
		}
	}
}

func TestScriptParallelAndDetached(t *testing.T) {
	script, _ := Script("p", []Stage{{"sleep 30 &"}, {"echo left", "exit 4"}, {"echo never"}}, 0)
	for sh, r := range runScript(t, script) {
		out := plain(r.out)
		if r.code != 4 || !strings.Contains(out, "left") || strings.Contains(out, "never") || !strings.Contains(out, "p 2/3 · 2 in parallel: echo left · exit 4") {
			t.Errorf("%s: code %d, output\n%s", sh, r.code, out)
		}
	}
}

func TestScriptFrom(t *testing.T) {
	script, _ := Script("f", []Stage{{"echo one"}, {"echo two"}}, 1)
	for sh, r := range runScript(t, script) {
		if out := plain(r.out); strings.Contains(out, "one") || !strings.Contains(out, "▸ f 2/2 · echo two\ntwo") {
			t.Errorf("%s: output\n%s", sh, out)
		}
	}
}

func TestScriptOutErrors(t *testing.T) {
	for _, c := range []struct {
		stages []Stage
		from   int
		want   string
	}{
		{[]Stage{{"echo $OUT"}}, 0, "no step comes before it"},
		{[]Stage{{"a"}, {"echo $OUT"}}, 1, "start at step 1"},
		{[]Stage{{"a", "b"}, {"echo $OUT"}}, 0, "runs in parallel"},
		{[]Stage{{"a &"}, {"echo $OUT"}}, 0, "runs in the background"},
	} {
		if _, err := Script("x", c.stages, c.from); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q from %d: %v", c.stages, c.from, err)
		}
	}
}

func TestParseMarker(t *testing.T) {
	m, ok := ParseMarker("▸ check 2/3 · go test ./...")
	if !ok || m != (Marker{"check", 2, 3, "go test ./..."}) {
		t.Fatalf("marker = %+v, %v", m, ok)
	}
	if _, ok := ParseMarker("▸ not a marker"); ok {
		t.Fatal("parsed a plain line")
	}
}
