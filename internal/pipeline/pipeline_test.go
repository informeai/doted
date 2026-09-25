package pipeline

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const sample = `
[check]
description = "vet and test"
steps = ["go vet ./...", "go test ./..."]

[dev]
parallel = true
steps = ["npm run dev", "go run ./cmd/api"]

[release]
steps = ["pipeline check", "pipeline dev", "git tag v$1", "git push origin v$1"]
`

func TestLoadKeepsTheFileOrder(t *testing.T) {
	ps, err := Load(writeFile(t, sample))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range ps {
		names = append(names, p.Name)
	}
	if !reflect.DeepEqual(names, []string{"check", "dev", "release"}) || !ps[1].Parallel || ps[0].Description != "vet and test" {
		t.Fatalf("pipelines = %+v", ps)
	}
}

func TestLoadErrors(t *testing.T) {
	if ps, err := Load(filepath.Join(t.TempDir(), "missing.toml")); ps != nil || err != nil {
		t.Fatalf("missing file = %v, %v", ps, err)
	}
	if _, err := Load(writeFile(t, "[a]\nstep = [\"x\"]\n")); err == nil || !strings.Contains(err.Error(), "a.step") {
		t.Fatalf("typo in a key: %v", err)
	}
	if _, err := Load(writeFile(t, "[stop]\nsteps = [\"x\"]\n")); err == nil {
		t.Fatal("a pipeline called stop should be refused")
	}
}

func TestPlan(t *testing.T) {
	ps, _ := Load(writeFile(t, sample))
	got, err := Plan(ps, "release", []string{"1.2.0"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Stage{
		{"go vet ./..."},
		{"go test ./..."},
		{"npm run dev", "go run ./cmd/api"},
		{"git tag v1.2.0"},
		{"git push origin v1.2.0"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("plan = %q", got)
	}
}

func TestPlanErrors(t *testing.T) {
	ps, _ := Load(writeFile(t, sample+`
[loop]
steps = ["pipeline loop"]

[fan]
parallel = true
steps = ["pipeline check"]
`))
	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"nope", nil, "no pipeline called nope"},
		{"release", nil, "$1 is missing"},
		{"loop", nil, "calls itself"},
		{"fan", nil, "runs in parallel"},
	} {
		if _, err := Plan(ps, c.name, c.args); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("Plan(%s) = %v, want %q", c.name, err, c.want)
		}
	}
}

func TestFillAllArguments(t *testing.T) {
	got, err := fill("echo $@ and $2 but $HOME", []string{"a", "b"})
	if err != nil || got != "echo a b and b but $HOME" {
		t.Fatalf("fill = %q, %v", got, err)
	}
}

func TestSaveCreatesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doted", FileName)
	replaced, err := Save(path, "deploy", []string{`echo "hi"`, `cd web && npm run build`})
	if err != nil || replaced {
		t.Fatalf("Save = %v, %v", replaced, err)
	}
	ps, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 || !reflect.DeepEqual(ps[0].Steps, []string{`echo "hi"`, `cd web && npm run build`}) {
		t.Fatalf("saved = %+v", ps)
	}
}

func TestSaveReplacesInPlace(t *testing.T) {
	path := writeFile(t, sample)
	replaced, err := Save(path, "dev", []string{"make dev"})
	if err != nil || !replaced {
		t.Fatalf("Save = %v, %v", replaced, err)
	}
	ps, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 3 || ps[1].Name != "dev" || ps[1].Parallel || !reflect.DeepEqual(ps[1].Steps, []string{"make dev"}) {
		t.Fatalf("after replacing = %+v", ps)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `description = "vet and test"`) {
		t.Fatalf("other pipelines changed:\n%s", data)
	}

	// The last table too, and a new one after it.
	if _, err := Save(path, "release", []string{"echo done"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(path, "new", []string{"true"}); err != nil {
		t.Fatal(err)
	}
	ps, _ = Load(path)
	if len(ps) != 4 || ps[2].Steps[0] != "echo done" || ps[3].Name != "new" {
		t.Fatalf("after more saves = %+v", ps)
	}
}

func TestQuote(t *testing.T) {
	path := writeFile(t, "[a]\nsteps = ["+quote("printf 'a\\tb' \"c\"\x01")+"]\n")
	ps, err := Load(path)
	if err != nil || ps[0].Steps[0] != "printf 'a\\tb' \"c\"\x01" {
		t.Fatalf("round trip = %+v, %v", ps, err)
	}
}

func TestRemove(t *testing.T) {
	path := writeFile(t, header+"\n# the checks\n[check]\nsteps = [\"a\"]\n\n# servers\n[dev]\nsteps = [\"b\"]\n\n[last]\nsteps = [\"c\"]\n")
	for _, name := range []string{"dev", "check", "last"} {
		ok, err := Remove(path, name)
		if err != nil || !ok {
			t.Fatalf("Remove(%s) = %v, %v", name, ok, err)
		}
		ps, err := Load(path)
		if err != nil {
			t.Fatalf("after removing %s: %v", name, err)
		}
		if _, still := Find(ps, name); still {
			t.Fatalf("%s is still there", name)
		}
	}
	data, _ := os.ReadFile(path)
	if got := string(data); got != header {
		t.Fatalf("left behind:\n%q\nwant the header alone", got)
	}
	if ok, err := Remove(path, "nope"); ok || err != nil {
		t.Fatalf("missing pipeline: %v, %v", ok, err)
	}
	if ok, err := Remove(filepath.Join(t.TempDir(), "none.toml"), "x"); ok || err != nil {
		t.Fatalf("missing file: %v, %v", ok, err)
	}
}

func TestRemoveKeepsTheOthers(t *testing.T) {
	path := writeFile(t, sample)
	if _, err := Remove(path, "dev"); err != nil {
		t.Fatal(err)
	}
	ps, _ := Load(path)
	if len(ps) != 2 || ps[0].Name != "check" || ps[0].Description != "vet and test" || ps[1].Name != "release" {
		t.Fatalf("after removing dev: %+v", ps)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "\n\n\n") {
		t.Fatalf("blank lines piled up:\n%s", data)
	}
}
