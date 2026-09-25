// Package pipeline reads and writes pipeline.toml, the named sequences of
// commands doted runs with `pipeline <name>`.
//
// Each pipeline is a table with its steps:
//
//	[check]
//	description = "vet, test and build"
//	steps = ["go vet ./...", "go test ./...", "go build ./..."]
//
// The steps run one after the other and the first that fails stops the
// rest; with parallel = true they all run at once. A step can be another
// pipeline ("pipeline check"), $1, $2... or $@ are the words typed after
// the pipeline's name, and $OUT is the output of the step before (see
// script.go).
package pipeline

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// FileName is the file's name, next to config.toml.
const FileName = "pipeline.toml"

// Path is the pipeline file that goes with the config file at configPath.
func Path(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), FileName)
}

// Pipeline is one table of the file.
type Pipeline struct {
	Name        string
	Description string
	Parallel    bool
	Steps       []string
}

// reserved are the words `pipeline` takes itself, so no pipeline can be
// called by them.
var reserved = map[string]bool{"record": true, "stop": true, "remove": true}

var validName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// CheckName says what's wrong with name as a pipeline's name, if anything.
func CheckName(name string) error {
	switch {
	case !validName.MatchString(name):
		return fmt.Errorf("%q can't be a pipeline's name: use letters, digits, - and _", name)
	case reserved[name]:
		return fmt.Errorf("%q can't be a pipeline's name: pipeline %s is a command", name, name)
	}
	return nil
}

// Load reads the pipelines at path, in the file's order. A missing file
// has none.
func Load(path string) ([]Pipeline, error) {
	var raw map[string]struct {
		Description string   `toml:"description"`
		Parallel    bool     `toml:"parallel"`
		Steps       []string `toml:"steps"`
	}
	md, err := toml.DecodeFile(path, &raw)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return nil, fmt.Errorf("unknown key %s", undecoded[0])
	}
	var out []Pipeline
	for _, key := range md.Keys() {
		if len(key) != 1 {
			continue // the tables' own keys
		}
		name := key[0]
		if err := CheckName(name); err != nil {
			return nil, err
		}
		p := raw[name]
		out = append(out, Pipeline{Name: name, Description: p.Description, Parallel: p.Parallel, Steps: p.Steps})
	}
	return out, nil
}

// Find returns the pipeline called name.
func Find(ps []Pipeline, name string) (Pipeline, bool) {
	for _, p := range ps {
		if p.Name == name {
			return p, true
		}
	}
	return Pipeline{}, false
}

// Stage is what runs at once: a single step of a pipeline, or all of a
// parallel one's.
type Stage []string

// Plan turns pipeline name, called with args, into the stages to run in
// order: the pipelines it calls are unfolded in place, and the arguments
// filled in.
func Plan(ps []Pipeline, name string, args []string) ([]Stage, error) {
	return plan(ps, name, args, map[string]bool{})
}

func plan(ps []Pipeline, name string, args []string, calling map[string]bool) ([]Stage, error) {
	p, ok := Find(ps, name)
	if !ok {
		return nil, fmt.Errorf("no pipeline called %s", name)
	}
	if calling[name] {
		return nil, fmt.Errorf("pipeline %s calls itself", name)
	}
	if len(p.Steps) == 0 {
		return nil, fmt.Errorf("pipeline %s has no steps", name)
	}
	calling[name] = true
	defer delete(calling, name)

	var stages []Stage
	var together Stage // a parallel pipeline's steps
	for _, step := range p.Steps {
		cmd, err := fill(step, args)
		if err != nil {
			return nil, fmt.Errorf("pipeline %s: %w", name, err)
		}
		if fields := strings.Fields(cmd); len(fields) >= 2 && fields[0] == "pipeline" {
			if p.Parallel {
				return nil, fmt.Errorf("pipeline %s runs in parallel, so it can't run pipeline %s", name, fields[1])
			}
			inner, err := plan(ps, fields[1], fields[2:], calling)
			if err != nil {
				return nil, err
			}
			stages = append(stages, inner...)
			continue
		}
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		if p.Parallel {
			together = append(together, cmd)
		} else {
			stages = append(stages, Stage{cmd})
		}
	}
	if len(together) > 0 {
		stages = append(stages, together)
	}
	if len(stages) == 0 {
		return nil, fmt.Errorf("pipeline %s has no steps", name)
	}
	return stages, nil
}

var argRef = regexp.MustCompile(`\$(@|[1-9])`)

// fill replaces $1...$9 in step with the arguments and $@ with all of them.
func fill(step string, args []string) (string, error) {
	var missing string
	out := argRef.ReplaceAllStringFunc(step, func(ref string) string {
		if ref == "$@" {
			return strings.Join(args, " ")
		}
		n, _ := strconv.Atoi(ref[1:])
		if n > len(args) {
			if missing == "" {
				missing = ref
			}
			return ref
		}
		return args[n-1]
	})
	if missing != "" {
		return "", fmt.Errorf("%s is missing: type it after the pipeline's name", missing)
	}
	return out, nil
}

// header starts a new pipeline file.
const header = `# doted pipelines
#
# Run one with "pipeline <name>"; "pipeline" alone lists them. Each is a
# table with its commands, run one after the other in the current directory,
# in the background, with a card showing how it goes; the first that fails
# stops the rest:
#
#   [check]
#   description = "vet, test and build"
#   steps = ["go vet ./...", "go test ./...", "go build ./..."]
#
# $OUT is the output of the step before: "git rev-parse --short HEAD", then
# "docker build -t app:$OUT .". parallel = true runs all the steps at once.
# A step like "pipeline check" runs that pipeline there. $1, $2... are the
# words typed after the name ($@ is all of them): "pipeline release 1.2.0".
# A step ending in & runs alongside the ones after it (a server for the
# tests) and is stopped when the pipeline ends. "pipeline <name> --from 3"
# starts at the third step.
#
# "pipeline record <name>" records the commands you run until
# "pipeline stop", and saves them here; "pipeline remove <name>" takes one
# out.
`

// Save writes pipeline name with steps to the file at path, creating it
// when missing and replacing a pipeline of that name, whose table it
// rewrites in place; the rest of the file is kept as it was. It reports
// whether there was one.
func Save(path, name string, steps []string) (replaced bool, err error) {
	if err := CheckName(name); err != nil {
		return false, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		data, err = []byte(header), nil
	}
	if err != nil {
		return false, err
	}
	table := formatTable(name, steps)
	lines := strings.SplitAfter(string(data), "\n")
	from, to := findTable(lines, name)
	var out string
	if from < 0 {
		out = strings.TrimRight(string(data), "\n") + "\n\n" + table
	} else {
		replaced = true
		out = strings.Join(lines[:from], "") + table
		if rest := strings.Join(lines[to:], ""); rest != "" {
			out += "\n" + rest
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return replaced, os.WriteFile(path, []byte(out), 0o644)
}

// Remove takes pipeline name out of the file at path, with the comments
// right above it, keeping the rest as it was. It reports whether there was
// one.
func Remove(path, name string) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	lines := strings.SplitAfter(string(data), "\n")
	from, to := findTable(lines, name)
	if from < 0 {
		return false, nil
	}
	for from > 0 && strings.HasPrefix(strings.TrimSpace(lines[from-1]), "#") {
		from--
	}
	before := strings.TrimRight(strings.Join(lines[:from], ""), "\n")
	after := strings.TrimLeft(strings.Join(lines[to:], ""), "\n")
	out := before
	switch {
	case before != "" && after != "":
		out += "\n\n" + after
	case after != "":
		out = after
	case before != "":
		out += "\n"
	}
	return true, os.WriteFile(path, []byte(out), 0o644)
}

// findTable returns the lines [from, to) of table name: its header up to
// the next table, less the blank lines and comments just before that one,
// which go with it. from is -1 when there's no such table.
func findTable(lines []string, name string) (from, to int) {
	from = -1
	for i, l := range lines {
		h, ok := tableHeader(l)
		switch {
		case !ok:
		case from < 0 && h == name:
			from = i
		case from >= 0:
			to = i
			for to > from+1 {
				prev := strings.TrimSpace(lines[to-1])
				if prev != "" && !strings.HasPrefix(prev, "#") {
					break
				}
				to--
			}
			return from, to
		}
	}
	return from, len(lines)
}

// tableHeader reads a [name] line.
func tableHeader(line string) (string, bool) {
	l := strings.TrimSpace(line)
	if i := strings.Index(l, "#"); i >= 0 {
		l = strings.TrimSpace(l[:i])
	}
	if !strings.HasPrefix(l, "[") || !strings.HasSuffix(l, "]") || strings.HasPrefix(l, "[[") {
		return "", false
	}
	name := strings.TrimSpace(l[1 : len(l)-1])
	if unq, err := strconv.Unquote(name); err == nil {
		name = unq
	}
	return name, true
}

func formatTable(name string, steps []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s]\nsteps = [\n", name)
	for _, s := range steps {
		fmt.Fprintf(&b, "  %s,\n", quote(s))
	}
	b.WriteString("]\n")
	return b.String()
}

// quote writes s as a TOML basic string.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
