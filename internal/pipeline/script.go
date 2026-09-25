package pipeline

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// A pipeline runs as one shell script, so it is a single job: one card,
// one output, stopped or restarted as a whole, and every step runs in the
// same shell, after the one before (a cd carries on). Before each step the
// script prints a marker line, "▸ check 2/3 · go test ./...", which is how
// doted follows its progress.
//
// A step that uses $OUT gets the output of the step before it: that step
// runs as OUT=$(...), and what it printed is shown afterwards. Steps ending
// in & run alongside the ones after them, and are stopped when the pipeline
// ends.

// Marker is a step's marker line as the job's output shows it.
type Marker struct {
	Name        string
	Step, Steps int
	What        string
}

var markerLine = regexp.MustCompile(`^▸ (\S+) (\d+)/(\d+) · (.*)$`)

// ParseMarker reads a marker line.
func ParseMarker(line string) (Marker, bool) {
	m := markerLine.FindStringSubmatch(strings.TrimRight(line, " "))
	if m == nil {
		return Marker{}, false
	}
	step, _ := strconv.Atoi(m[2])
	steps, _ := strconv.Atoi(m[3])
	return Marker{Name: m[1], Step: step, Steps: steps, What: m[4]}, true
}

var outRef = regexp.MustCompile(`\$OUT\b|\$\{OUT\}`)

// usesOut reports whether a stage reads $OUT.
func usesOut(stage Stage) bool {
	for _, cmd := range stage {
		if outRef.MatchString(cmd) {
			return true
		}
	}
	return false
}

// detached reports whether cmd ends in a lone & (not &&), and strips it.
func detached(cmd string) (string, bool) {
	c := strings.TrimSpace(cmd)
	if !strings.HasSuffix(c, "&") || strings.HasSuffix(c, "&&") {
		return c, false
	}
	return strings.TrimSpace(strings.TrimSuffix(c, "&")), true
}

// Describe is what a stage's marker says it runs.
func Describe(stage Stage) string {
	if len(stage) == 1 {
		return firstLine(stage[0])
	}
	parts := make([]string, len(stage))
	for i, cmd := range stage {
		parts[i] = firstLine(cmd)
	}
	return fmt.Sprintf("%d in parallel: %s", len(stage), strings.Join(parts, " · "))
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

// Script is the shell script that runs pipeline name's stages from the
// from-th (counting from 0), for zsh and bash alike.
func Script(name string, stages []Stage, from int) (string, error) {
	for i := from; i < len(stages); i++ {
		if !usesOut(stages[i]) {
			continue
		}
		switch {
		case i == from:
			if i == 0 {
				return "", fmt.Errorf("step 1 of pipeline %s uses $OUT, but no step comes before it", name)
			}
			return "", fmt.Errorf("step %d uses $OUT, the output of step %d: start at step %d", i+1, i, i)
		case len(stages[i-1]) > 1:
			return "", fmt.Errorf("step %d uses $OUT, but step %d runs in parallel", i+1, i)
		}
		if _, bg := detached(stages[i-1][0]); bg {
			return "", fmt.Errorf("step %d uses $OUT, but step %d runs in the background", i+1, i)
		}
	}

	var b strings.Builder
	b.WriteString(`__doted_bg=
__doted_end() {
	if [ -n "$__doted_bg" ]; then eval "kill $__doted_bg" 2>/dev/null; fi
	exit "$1"
}
trap '__doted_end 130' INT TERM HUP
__doted_step() { printf '\033[1;36m▸ %s\033[0m\033[2m · %s\033[0m\n' "$1" "$2"; }
`)
	for i := from; i < len(stages); i++ {
		stage := stages[i]
		fmt.Fprintf(&b, "__doted_step %s %s\n", quote1(fmt.Sprintf("%s %d/%d", name, i+1, len(stages))), quote1(Describe(stage)))
		capture := i+1 < len(stages) && usesOut(stages[i+1])
		if len(stage) == 1 {
			cmd, bg := detached(stage[0])
			switch {
			case bg:
				fmt.Fprintf(&b, "( %s\n) & __doted_bg=\"$__doted_bg $!\"\n", cmd)
			case capture:
				fmt.Fprintf(&b, "OUT=$(%s\n)\n__doted_s=$?\n[ -n \"$OUT\" ] && printf '%%s\\n' \"$OUT\"\n", cmd)
				b.WriteString("[ $__doted_s -eq 0 ] || __doted_end $__doted_s\n")
			default:
				fmt.Fprintf(&b, "%s\n__doted_s=$?\n[ $__doted_s -eq 0 ] || __doted_end $__doted_s\n", cmd)
			}
			continue
		}
		var waits []int
		for k, step := range stage {
			cmd, bg := detached(step)
			if bg {
				fmt.Fprintf(&b, "( %s\n) & __doted_bg=\"$__doted_bg $!\"\n", cmd)
				continue
			}
			fmt.Fprintf(&b, "( %s\n) & __doted_p%d=$!\n", cmd, k)
			waits = append(waits, k)
		}
		b.WriteString("__doted_s=0\n")
		for _, k := range waits {
			fmt.Fprintf(&b, "wait $__doted_p%d || __doted_s=$?\n", k)
		}
		b.WriteString("[ $__doted_s -eq 0 ] || __doted_end $__doted_s\n")
	}
	b.WriteString("__doted_end 0\n")
	return b.String(), nil
}

// quote1 quotes s for the shell, in single quotes.
func quote1(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
