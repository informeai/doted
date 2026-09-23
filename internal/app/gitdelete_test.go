package app

import (
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestDeletedRefs(t *testing.T) {
	prev := []string{"refs/heads/main", "refs/heads/old", "refs/remotes/origin/gone", "refs/remotes/origin/main", "refs/tags/v1.0"}
	cur := []string{"refs/heads/main", "refs/remotes/origin/main"}
	got := deletedRefs(prev, cur)
	want := []deletedRef{{kind: refBranch, name: "old"}, {kind: refRemote, name: "origin/gone"}, {kind: refTag, name: "v1.0"}}
	if !slices.Equal(got, want) {
		t.Fatalf("deleted = %+v", got)
	}
	// Past three, one summary.
	many := []string{"refs/heads/a", "refs/heads/b", "refs/heads/c", "refs/heads/d", "refs/heads/e"}
	if got := deletedRefs(many, nil); len(got) != 1 || got[0].label() != "5 branches" {
		t.Fatalf("summary = %+v", got)
	}
	if got := deletedRefs(append(many, "refs/tags/v2"), nil); got[0].label() != "6 refs" {
		t.Fatalf("mixed summary = %q", got[0].label())
	}
	if got := deletedRefs(cur, prev); len(got) != 0 {
		t.Fatalf("created refs counted as deleted: %+v", got)
	}
}

func TestDeletionsQueueAndPlay(t *testing.T) {
	g := newTestGame(t)
	g.cfg.Animation.Enabled = true
	now := time.Now()
	before := projectContext{gitDir: "/r/.git", branch: "main", refs: []string{"refs/heads/a", "refs/heads/main", "refs/tags/v1"}}
	after := projectContext{gitDir: "/r/.git", branch: "main", refs: []string{"refs/heads/main"}}

	// Another repository's refs are not deletions.
	g.queueDeletions(projectContext{gitDir: "/other/.git", refs: before.refs}, after, now)
	if len(g.branch.deletions) != 0 {
		t.Fatal("moving between repositories should not count as deleting")
	}

	g.queueDeletions(before, after, now)
	if len(g.branch.deletions) != 2 {
		t.Fatalf("%d deletions queued", len(g.branch.deletions))
	}
	// One after the other.
	if d, _ := g.branch.activeDeletion(now); d == nil || d.ref.name != "a" {
		t.Fatal("the first deletion should play first")
	}
	if g.branch.shake(now.Add(deleteShake/8)) == 0 {
		t.Fatal("the logo should shake as a deletion starts")
	}
	later := now.Add(deleteDuration + time.Millisecond)
	if d, _ := g.branch.activeDeletion(later); d == nil || d.ref.name != "v1" || d.ref.kind != refTag {
		t.Fatalf("second deletion = %+v", d)
	}
	if d, _ := g.branch.activeDeletion(later.Add(deleteDuration)); d != nil {
		t.Fatal("the queue should empty")
	}
}

func TestLetterFall(t *testing.T) {
	if letterFall(0, 5, deleteStrike) != 0 || letterFall(4, 5, 1) != 1 {
		t.Fatal("letters should rest until the strike ends and all land by the end")
	}
	if letterFall(0, 5, 0.5) <= letterFall(4, 5, 0.5) {
		t.Fatal("the first letter should fall first")
	}
}

func TestProbeFindsRefs(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	cmd := exec.Command("sh", "-c", "git init -q -b main && git -c user.email=a@b -c user.name=a commit -q --allow-empty -m x && git branch old && git tag v1")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	info := probeContext(dir, filepath.Dir(git))
	if !slices.Equal(info.refs, []string{"refs/heads/main", "refs/heads/old", "refs/tags/v1"}) || info.gitDir == "" {
		t.Fatalf("refs %q gitDir %q", info.refs, info.gitDir)
	}
}
