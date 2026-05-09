package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Integration tests for the 15 most-used git verbs.
// Each test exercises the porcelain command function directly and asserts
// against on-disk state and (where useful) captured stdout.

// --- helpers ---

func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

func setupRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	if err := cmdInit(nil); err != nil {
		t.Fatalf("init: %v", err)
	}
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if d := filepath.Dir(path); d != "." {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustCommit(t *testing.T, msg string) {
	t.Helper()
	if err := cmdCommit([]string{"-m", msg}); err != nil {
		t.Fatalf("commit %q: %v", msg, err)
	}
}

func capture(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = wr
	runErr := fn()
	wr.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, rd)
	return buf.String(), runErr
}

func mustHead(t *testing.T) string {
	t.Helper()
	r, err := FindRepo(".")
	if err != nil {
		t.Fatal(err)
	}
	h, err := r.ResolveHead()
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func mustReadCommit(t *testing.T, hash string) *Commit {
	t.Helper()
	r, err := FindRepo(".")
	if err != nil {
		t.Fatal(err)
	}
	obj, err := r.ReadObject(hash)
	if err != nil {
		t.Fatal(err)
	}
	c, err := DecodeCommit(obj.Body)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// 1 -- init
func TestCmd_Init(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := cmdInit(nil); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	for _, sub := range []string{".helix", ".helix/objects", ".helix/refs/branches", ".helix/HEAD", ".helix/config"} {
		if _, err := os.Stat(filepath.Join(dir, sub)); err != nil {
			t.Errorf("missing %s: %v", sub, err)
		}
	}
	if err := cmdInit(nil); err == nil {
		t.Error("expected error on second init in same dir")
	}
}

// 2 -- status
func TestCmd_Status(t *testing.T) {
	setupRepo(t)
	writeFile(t, "a.txt", "hello\n")
	out, err := capture(t, func() error { return cmdStatus(nil) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "+ a.txt") {
		t.Errorf("expected '+ a.txt' for new file, got: %s", out)
	}
	mustCommit(t, "first")
	out, err = capture(t, func() error { return cmdStatus(nil) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Working tree clean") {
		t.Errorf("expected 'Working tree clean' after commit, got: %s", out)
	}
	writeFile(t, "a.txt", "modified\n")
	out, err = capture(t, func() error { return cmdStatus(nil) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "~ a.txt") {
		t.Errorf("expected '~ a.txt' modified, got: %s", out)
	}
}

// 3 -- add (helix has no staging; should accept files but not error)
func TestCmd_Add(t *testing.T) {
	setupRepo(t)
	writeFile(t, "a.txt", "x")
	if err := cmdAdd([]string{"a.txt"}); err != nil {
		t.Fatalf("add path: %v", err)
	}
	if err := cmdAdd([]string{"-A"}); err != nil {
		t.Fatalf("add -A: %v", err)
	}
	if err := cmdAdd([]string{"does-not-exist.txt"}); err == nil {
		t.Error("expected error for nonexistent file")
	}
}

// 4 -- commit (and verify --amend preserves change-id)
func TestCmd_Commit(t *testing.T) {
	setupRepo(t)
	writeFile(t, "a.txt", "v1\n")
	if err := cmdCommit([]string{"-m", "first"}); err != nil {
		t.Fatal(err)
	}
	h1 := mustHead(t)
	c1 := mustReadCommit(t, h1)
	if c1.ChangeID == "" {
		t.Fatal("missing change-id on first commit")
	}
	if c1.Message != "first\n" {
		t.Errorf("unexpected message: %q", c1.Message)
	}
	if err := cmdCommit(nil); err == nil {
		t.Error("expected error without -m")
	}
	// amend
	writeFile(t, "a.txt", "v2\n")
	if err := cmdCommit([]string{"-m", "first amended", "--amend"}); err != nil {
		t.Fatal(err)
	}
	h2 := mustHead(t)
	c2 := mustReadCommit(t, h2)
	if h1 == h2 {
		t.Error("amend should produce a different commit hash")
	}
	if c1.ChangeID != c2.ChangeID {
		t.Errorf("change-id should survive --amend: %s -> %s", c1.ChangeID, c2.ChangeID)
	}
}

// 5 -- log
func TestCmd_Log(t *testing.T) {
	setupRepo(t)
	for i, msg := range []string{"one", "two", "three"} {
		writeFile(t, "a.txt", string(rune('1'+i)))
		mustCommit(t, msg)
	}
	out, err := capture(t, func() error { return cmdLog(nil) })
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range []string{"one", "two", "three"} {
		if !strings.Contains(out, msg) {
			t.Errorf("log missing %q in:\n%s", msg, out)
		}
	}
	out, err = capture(t, func() error { return cmdLog([]string{"-n", "1"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "three") {
		t.Errorf("log -n 1 should contain 'three', got: %s", out)
	}
	if strings.Contains(out, "    one\n") || strings.Contains(out, "    two\n") {
		t.Errorf("log -n 1 should not contain earlier commits, got: %s", out)
	}
}

// 6 -- diff
func TestCmd_Diff(t *testing.T) {
	setupRepo(t)
	writeFile(t, "a.txt", "alpha\nbeta\ngamma\n")
	mustCommit(t, "init")
	writeFile(t, "a.txt", "alpha\nBETA\ngamma\n")
	out, err := capture(t, func() error { return cmdDiff(nil) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "-beta") {
		t.Errorf("expected '-beta' in diff, got: %s", out)
	}
	if !strings.Contains(out, "+BETA") {
		t.Errorf("expected '+BETA' in diff, got: %s", out)
	}
	if !strings.Contains(out, "--- a/a.txt") {
		t.Errorf("expected file header, got: %s", out)
	}
}

// 7 -- branch
func TestCmd_Branch(t *testing.T) {
	setupRepo(t)
	writeFile(t, "a.txt", "1")
	mustCommit(t, "init")
	if err := cmdBranch([]string{"feature"}); err != nil {
		t.Fatal(err)
	}
	r, _ := FindRepo(".")
	bs, _ := r.ListBranches()
	if !contains(bs, "feature") {
		t.Errorf("feature not in branch list: %v", bs)
	}
	if !contains(bs, "main") {
		t.Errorf("main not in branch list: %v", bs)
	}
	if err := cmdBranch([]string{"-d", "feature"}); err != nil {
		t.Fatal(err)
	}
	bs, _ = r.ListBranches()
	if contains(bs, "feature") {
		t.Errorf("feature should be deleted, still in: %v", bs)
	}
}

// 8 -- switch
func TestCmd_Switch(t *testing.T) {
	setupRepo(t)
	writeFile(t, "a.txt", "main\n")
	mustCommit(t, "main work")
	if err := cmdBranch([]string{"feature"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdSwitch([]string{"feature"}); err != nil {
		t.Fatal(err)
	}
	r, _ := FindRepo(".")
	cur, _ := r.CurrentBranch()
	if cur != "feature" {
		t.Errorf("expected to be on feature, got %s", cur)
	}
	writeFile(t, "a.txt", "feature\n")
	mustCommit(t, "feature work")
	if err := cmdSwitch([]string{"main"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile("a.txt")
	if string(got) != "main\n" {
		t.Errorf("expected main content after switching back, got %q", got)
	}
	// Refuse to switch with dirty working tree (unless -f).
	writeFile(t, "a.txt", "WIP\n")
	if err := cmdSwitch([]string{"feature"}); err == nil {
		t.Error("expected switch to refuse dirty tree without -f")
	}
}

// 9 -- merge: 3-way line-level non-overlapping integration
func TestCmd_Merge(t *testing.T) {
	setupRepo(t)
	writeFile(t, "f.txt", "L1\nL2\nL3\n")
	mustCommit(t, "base")
	if err := cmdBranch([]string{"other"}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, "f.txt", "OURS-L1\nL2\nL3\n")
	mustCommit(t, "ours")
	if err := cmdSwitch([]string{"other"}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, "f.txt", "L1\nL2\nTHEIRS-L3\n")
	mustCommit(t, "theirs")
	if err := cmdSwitch([]string{"main"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdMergeReal([]string{"other"}); err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	got, _ := os.ReadFile("f.txt")
	want := "OURS-L1\nL2\nTHEIRS-L3\n"
	if string(got) != want {
		t.Errorf("merge result mismatch:\nwant %q\n got %q", want, got)
	}
	c := mustReadCommit(t, mustHead(t))
	if len(c.Parents) != 2 {
		t.Errorf("merge commit should have 2 parents, got %d", len(c.Parents))
	}
}

// 10 -- clone
func TestCmd_Clone(t *testing.T) {
	src := t.TempDir()
	chdir(t, src)
	if err := cmdInit(nil); err != nil {
		t.Fatal(err)
	}
	writeFile(t, "a.txt", "hello\n")
	mustCommit(t, "init")
	writeFile(t, "b.txt", "world\n")
	mustCommit(t, "add b")
	srcHead := mustHead(t)

	dstParent := t.TempDir()
	chdir(t, dstParent)
	if err := cmdClone([]string{src, "myclone"}); err != nil {
		t.Fatal(err)
	}
	chdir(t, filepath.Join(dstParent, "myclone"))
	cloneHead := mustHead(t)
	if cloneHead != srcHead {
		t.Errorf("clone HEAD %s != source HEAD %s", cloneHead, srcHead)
	}
	for _, f := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("expected %s in clone: %v", f, err)
		}
	}
	r, _ := FindRepo(".")
	rh, _ := r.ReadRef("remotes/origin/main")
	if rh != srcHead {
		t.Errorf("remotes/origin/main = %s, want %s", rh, srcHead)
	}
}

// 11 -- fetch
func TestCmd_Fetch(t *testing.T) {
	src := t.TempDir()
	chdir(t, src)
	cmdInit(nil)
	writeFile(t, "a.txt", "1")
	mustCommit(t, "first")

	dstParent := t.TempDir()
	chdir(t, dstParent)
	if err := cmdClone([]string{src}); err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(dstParent, filepath.Base(src))

	// Source advances.
	chdir(t, src)
	writeFile(t, "a.txt", "2")
	mustCommit(t, "second")
	srcHead := mustHead(t)

	// Fetch in clone.
	chdir(t, clone)
	if err := cmdFetch(nil); err != nil {
		t.Fatal(err)
	}
	r, _ := FindRepo(".")
	got, _ := r.ReadRef("remotes/origin/main")
	if got != srcHead {
		t.Errorf("remotes/origin/main not updated: got %s, want %s", got, srcHead)
	}
}

// 12 -- pull
func TestCmd_Pull(t *testing.T) {
	src := t.TempDir()
	chdir(t, src)
	cmdInit(nil)
	writeFile(t, "a.txt", "1")
	mustCommit(t, "first")

	dstParent := t.TempDir()
	chdir(t, dstParent)
	cmdClone([]string{src})
	clone := filepath.Join(dstParent, filepath.Base(src))

	chdir(t, src)
	writeFile(t, "a.txt", "2")
	mustCommit(t, "second")
	srcHead := mustHead(t)

	chdir(t, clone)
	if err := cmdPull(nil); err != nil {
		t.Fatalf("pull: %v", err)
	}
	if mustHead(t) != srcHead {
		t.Errorf("pull didn't fast-forward HEAD")
	}
	got, _ := os.ReadFile("a.txt")
	if string(got) != "2" {
		t.Errorf("working tree not updated by pull, got %q", got)
	}
}

// 13 -- push
func TestCmd_Push(t *testing.T) {
	src := t.TempDir()
	chdir(t, src)
	cmdInit(nil)
	writeFile(t, "a.txt", "1")
	mustCommit(t, "first")

	dstParent := t.TempDir()
	chdir(t, dstParent)
	cmdClone([]string{src})
	clone := filepath.Join(dstParent, filepath.Base(src))

	chdir(t, clone)
	writeFile(t, "a.txt", "from-clone")
	mustCommit(t, "clone work")
	cloneHead := mustHead(t)
	if err := cmdPush(nil); err != nil {
		t.Fatalf("push: %v", err)
	}

	// Verify source has the new commit on its main branch.
	srcRepo, _ := openRepoAt(src)
	got, _ := srcRepo.ReadRef("branches/main")
	if got != cloneHead {
		t.Errorf("source main not updated: got %s, want %s", got, cloneHead)
	}

	// A non-fast-forward push should be refused.
	chdir(t, src)
	writeFile(t, "a.txt", "src-divergent")
	mustCommit(t, "src divergent commit")

	chdir(t, clone)
	writeFile(t, "a.txt", "clone-divergent")
	mustCommit(t, "clone divergent commit")
	if err := cmdPush(nil); err == nil {
		t.Error("expected non-fast-forward push to be refused")
	}
}

// 14 -- rebase: preserves change-id across replay
func TestCmd_Rebase(t *testing.T) {
	setupRepo(t)
	writeFile(t, "a.txt", "1")
	mustCommit(t, "base")
	if err := cmdBranch([]string{"feature"}); err != nil {
		t.Fatal(err)
	}
	// main advances independently
	writeFile(t, "a.txt", "2")
	mustCommit(t, "main 1")
	writeFile(t, "a.txt", "3")
	mustCommit(t, "main 2")
	mainTip := mustHead(t)

	// feature gets a separate commit
	if err := cmdSwitch([]string{"feature"}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, "b.txt", "feat")
	mustCommit(t, "feature work")
	featCommit := mustReadCommit(t, mustHead(t))
	originalChangeID := featCommit.ChangeID

	// Rebase feature onto main.
	if err := cmdRebase([]string{"main"}); err != nil {
		t.Fatalf("rebase: %v", err)
	}
	newHead := mustHead(t)
	rebased := mustReadCommit(t, newHead)
	if rebased.ChangeID != originalChangeID {
		t.Errorf("rebase should preserve change-id: %s -> %s", originalChangeID, rebased.ChangeID)
	}
	if len(rebased.Parents) != 1 || rebased.Parents[0] != mainTip {
		t.Errorf("rebased commit's parent should be main tip %s, got %v", mainTip, rebased.Parents)
	}
	// Both files should exist after rebase.
	for _, f := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("missing %s after rebase: %v", f, err)
		}
	}
}

// 15 -- stash
func TestCmd_Stash(t *testing.T) {
	setupRepo(t)
	writeFile(t, "a.txt", "v1\n")
	mustCommit(t, "first")
	// Modify and stash.
	writeFile(t, "a.txt", "WIP\n")
	if err := cmdStash(nil); err != nil {
		t.Fatalf("stash: %v", err)
	}
	got, _ := os.ReadFile("a.txt")
	if string(got) != "v1\n" {
		t.Errorf("after stash, working tree should match HEAD, got %q", got)
	}
	r, _ := FindRepo(".")
	if h, _ := r.ReadRef("stash"); h == "" {
		t.Error("stash ref should be set after stash push")
	}
	// Pop.
	if err := cmdStash([]string{"pop"}); err != nil {
		t.Fatalf("stash pop: %v", err)
	}
	got, _ = os.ReadFile("a.txt")
	if string(got) != "WIP\n" {
		t.Errorf("after stash pop, working tree should have WIP, got %q", got)
	}
	// Stash with no changes is a no-op (no error).
	mustCommit(t, "save WIP")
	if err := cmdStash(nil); err != nil {
		t.Fatalf("stash with clean tree should not error: %v", err)
	}
}

// --- small util ---

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
