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
	d := g.branch.activeDeletion(now)
	if d == nil || d.ref.name != "a" || d.current != "main" {
		t.Fatalf("first deletion = %+v", d)
	}
	// The current branch rolls into the deleted one, and rises back at the end.
	if string(d.in.from) != "main" || string(d.in.to) != "a" || string(d.back.to) != "main" {
		t.Fatalf("rolls: %q → %q, back to %q", string(d.in.from), string(d.in.to), string(d.back.to))
	}
	if !d.in.start.Before(d.trailAt) || !d.trailAt.Before(d.fallAt) || !d.fallAt.Before(d.back.start) || d.end.Before(d.back.start.Add(d.back.duration())) {
		t.Fatal("the steps should come in order: roll in, trail, fall, roll back")
	}
	// The logo turns red, then gets its color back.
	if d.iconBlend(now) != 0 || d.iconBlend(d.trailAt) != 1 || d.iconBlend(d.end) != 0 {
		t.Fatalf("icon blend: %.2f %.2f %.2f", d.iconBlend(now), d.iconBlend(d.trailAt), d.iconBlend(d.end))
	}
	if g.branch.shake(d.trailAt.Add(deleteShake/8)) == 0 {
		t.Fatal("the logo should shake while the trail runs")
	}
	// One after the other.
	second := g.branch.deletions[1]
	if !second.in.start.Equal(d.end) || second.ref.name != "v1" || second.ref.kind != refTag {
		t.Fatalf("second deletion = %+v", second)
	}
	if g.branch.activeDeletion(second.end) != nil {
		t.Fatal("the queue should empty")
	}
}

func TestLetterFall(t *testing.T) {
	d := newDeletion(deletedRef{kind: refBranch, name: "old-feature"}, "main", time.Now())
	if d.letterFall(0, d.fallAt) != 0 || d.letterFall(len(d.sparked)-1, d.end) != 1 {
		t.Fatal("letters should rest until the trail ends and all land by the end")
	}
	mid := d.fallAt.Add(deleteLetterFall / 2)
	if d.letterFall(0, mid) <= d.letterFall(len(d.sparked)-1, mid) {
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
