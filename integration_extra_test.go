package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 1 -- blame
func TestCmd_Blame(t *testing.T) {
	setupRepo(t)
	writeFile(t, "f.txt", "L1\n")
	mustCommit(t, "first")
	headFirst := mustHead(t)
	writeFile(t, "f.txt", "L1\nL2\n")
	mustCommit(t, "second")
	headSecond := mustHead(t)

	out, err := capture(t, func() error { return cmdBlame([]string{"f.txt"}) })
	if err != nil {
		t.Fatal(err)
	}
	// L1 should be blamed on first; L2 on second.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 blame lines, got %d:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], headFirst[:12]) {
		t.Errorf("L1 should be blamed on first commit, got: %s", lines[0])
	}
	if !strings.HasPrefix(lines[1], headSecond[:12]) {
		t.Errorf("L2 should be blamed on second commit, got: %s", lines[1])
	}
}

// 2 -- shortlog
func TestCmd_Shortlog(t *testing.T) {
	setupRepo(t)
	for i := 0; i < 3; i++ {
		writeFile(t, "f.txt", string(rune('a'+i)))
		mustCommit(t, "c"+string(rune('1'+i)))
	}
	out, err := capture(t, func() error { return cmdShortlog(nil) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "(3)") {
		t.Errorf("expected '(3)' commits, got: %s", out)
	}
	for _, msg := range []string{"c1", "c2", "c3"} {
		if !strings.Contains(out, msg) {
			t.Errorf("shortlog missing %s in:\n%s", msg, out)
		}
	}
}

// 3 -- whatchanged
func TestCmd_Whatchanged(t *testing.T) {
	setupRepo(t)
	writeFile(t, "f.txt", "alpha\n")
	mustCommit(t, "init")
	writeFile(t, "f.txt", "alpha\nbeta\n")
	mustCommit(t, "add beta")
	out, err := capture(t, func() error { return cmdWhatchanged([]string{"-n", "1"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "+beta") {
		t.Errorf("whatchanged should show '+beta' diff:\n%s", out)
	}
	if !strings.Contains(out, "add beta") {
		t.Errorf("whatchanged should show subject 'add beta':\n%s", out)
	}
}

// 4 -- merge-base
func TestCmd_MergeBase(t *testing.T) {
	setupRepo(t)
	writeFile(t, "f.txt", "1")
	mustCommit(t, "base")
	base := mustHead(t)
	if err := cmdBranch([]string{"feature"}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, "f.txt", "2")
	mustCommit(t, "main work")
	if err := cmdSwitch([]string{"feature"}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, "g.txt", "feat")
	mustCommit(t, "feature work")
	out, err := capture(t, func() error { return cmdMergeBase([]string{"main", "feature"}) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != base {
		t.Errorf("merge-base = %s, want %s", strings.TrimSpace(out), base)
	}
}

// 5 -- bisect
func TestCmd_Bisect(t *testing.T) {
	setupRepo(t)
	for i := 1; i <= 7; i++ {
		writeFile(t, "f.txt", string(rune('0'+i)))
		mustCommit(t, "c"+string(rune('0'+i)))
	}
	good, _ := capture(t, func() error { return cmdLog([]string{"-n", "7"}) })
	_ = good
	r, _ := FindRepo(".")
	headFile := filepath.Join(r.HelixDir, "BISECT", "ORIG_HEAD")

	if err := cmdBisect([]string{"start"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(headFile); err != nil {
		t.Fatalf("ORIG_HEAD not saved: %v", err)
	}

	// Need the very first commit's hash. Walk to it.
	hash := mustHead(t)
	for {
		c := mustReadCommit(t, hash)
		if len(c.Parents) == 0 {
			break
		}
		hash = c.Parents[0]
	}
	if err := cmdBisect([]string{"good", hash}); err != nil {
		t.Fatal(err)
	}
	if err := cmdBisect([]string{"bad", "HEAD"}); err == nil {
		// HEAD is the bisect midpoint now, not the original; this is just stress
	}
	if err := cmdBisect([]string{"reset"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.HelixDir, "BISECT")); !os.IsNotExist(err) {
		t.Error("BISECT dir should be cleaned up after reset")
	}
}

// 6 -- grep
func TestCmd_Grep(t *testing.T) {
	setupRepo(t)
	writeFile(t, "a.txt", "hello world\nfoo bar\nbaz quux\n")
	writeFile(t, "b.txt", "another line with hello\n")
	out, err := capture(t, func() error { return cmdGrep([]string{"hello"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a.txt:1:hello world") {
		t.Errorf("grep missing match in a.txt:\n%s", out)
	}
	if !strings.Contains(out, "b.txt:1:another line with hello") {
		t.Errorf("grep missing match in b.txt:\n%s", out)
	}
}

// 7 -- notes
func TestCmd_Notes(t *testing.T) {
	setupRepo(t)
	writeFile(t, "f.txt", "x")
	mustCommit(t, "first")
	if err := cmdNotes([]string{"add", "HEAD", "this", "is", "a", "note"}); err != nil {
		t.Fatal(err)
	}
	out, err := capture(t, func() error { return cmdNotes([]string{"show", "HEAD"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "this is a note") {
		t.Errorf("notes show didn't return content: %s", out)
	}
	if err := cmdNotes([]string{"remove", "HEAD"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdNotes([]string{"show", "HEAD"}); err == nil {
		t.Error("expected error after note removed")
	}
}

// 8 -- reflog
func TestCmd_Reflog(t *testing.T) {
	setupRepo(t)
	writeFile(t, "f.txt", "1")
	mustCommit(t, "first")
	writeFile(t, "f.txt", "2")
	mustCommit(t, "second")
	out, err := capture(t, func() error { return cmdReflog(nil) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "first") || !strings.Contains(out, "second") {
		t.Errorf("reflog missing entries: %s", out)
	}
}

// 9 -- gc
func TestCmd_Gc(t *testing.T) {
	setupRepo(t)
	writeFile(t, "f.txt", "1")
	mustCommit(t, "init")
	r, _ := FindRepo(".")

	// Write an unreachable orphan blob.
	orphan, err := r.WriteObject(&Object{Kind: KindBlob, Body: []byte("orphan")})
	if err != nil {
		t.Fatal(err)
	}
	orphanPath := filepath.Join(r.ObjectsDir(), orphan[:2], orphan[2:])
	if _, err := os.Stat(orphanPath); err != nil {
		t.Fatalf("orphan not on disk: %v", err)
	}
	if err := cmdGc(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Errorf("gc didn't delete orphan blob")
	}
}

// 10 -- fsck
func TestCmd_Fsck(t *testing.T) {
	setupRepo(t)
	writeFile(t, "f.txt", "1")
	mustCommit(t, "init")
	if err := cmdFsck(nil); err != nil {
		t.Fatalf("fsck on clean repo: %v", err)
	}
	// Corrupt an object: rename it to a random hash.
	r, _ := FindRepo(".")
	objDir := r.ObjectsDir()
	var sample string
	filepath.Walk(objDir, func(p string, info os.FileInfo, err error) error {
		if !info.IsDir() && sample == "" {
			sample = p
		}
		return nil
	})
	if sample == "" {
		t.Fatal("no objects on disk?")
	}
	bogus := filepath.Join(filepath.Dir(sample), "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	if err := os.Rename(sample, bogus); err != nil {
		t.Fatal(err)
	}
	if err := cmdFsck(nil); err == nil {
		t.Error("expected fsck to detect hash mismatch")
	}
	os.Rename(bogus, sample)
}

// 11 -- archive
func TestCmd_Archive(t *testing.T) {
	setupRepo(t)
	writeFile(t, "a.txt", "hello\n")
	writeFile(t, "sub/b.txt", "world\n")
	mustCommit(t, "init")
	out := filepath.Join(t.TempDir(), "out.tar")
	if err := cmdArchive([]string{"-o", out, "HEAD"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 100 {
		t.Errorf("archive file is suspiciously small: %d bytes", info.Size())
	}
}

// 12 -- worktree
func TestCmd_Worktree(t *testing.T) {
	setupRepo(t)
	writeFile(t, "f.txt", "x")
	mustCommit(t, "init")

	wtPath := filepath.Join(t.TempDir(), "wt")
	if err := cmdWorktree([]string{"add", wtPath}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wtPath, "f.txt")); err != nil {
		t.Errorf("worktree didn't create file: %v", err)
	}
	out, err := capture(t, func() error { return cmdWorktree([]string{"list"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, wtPath) {
		t.Errorf("worktree list missing %s:\n%s", wtPath, out)
	}
	if err := cmdWorktree([]string{"remove", wtPath}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("worktree dir should be removed")
	}
}

// 13 -- format-patch
func TestCmd_FormatPatch(t *testing.T) {
	setupRepo(t)
	writeFile(t, "f.txt", "1\n")
	mustCommit(t, "first")
	since := mustHead(t)
	writeFile(t, "f.txt", "1\n2\n")
	mustCommit(t, "add 2")

	patchDir := t.TempDir()
	if err := cmdFormatPatch([]string{"-o", patchDir, since}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(patchDir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 patch file, got %d", len(entries))
	}
	data, _ := os.ReadFile(filepath.Join(patchDir, entries[0].Name()))
	for _, want := range []string{"From: ", "Subject: [PATCH 1/1] add 2", "Change-Id:", "+2"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("patch missing %q:\n%s", want, data)
		}
	}
}

// 14 -- am
func TestCmd_Am(t *testing.T) {
	// Build a patch from a source repo, apply in dest.
	srcDir := t.TempDir()
	chdir(t, srcDir)
	cmdInit(nil)
	writeFile(t, "f.txt", "1\n")
	mustCommit(t, "init")
	since := mustHead(t)
	writeFile(t, "f.txt", "1\n2\n")
	mustCommit(t, "add 2")
	srcChangeID := mustReadCommit(t, mustHead(t)).ChangeID

	patchDir := t.TempDir()
	if err := cmdFormatPatch([]string{"-o", patchDir, since}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(patchDir)
	patchFile := filepath.Join(patchDir, entries[0].Name())

	// Fresh dest with same base.
	dstDir := t.TempDir()
	chdir(t, dstDir)
	cmdInit(nil)
	writeFile(t, "f.txt", "1\n")
	mustCommit(t, "init")
	if err := cmdAm([]string{patchFile}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile("f.txt")
	if string(got) != "1\n2\n" {
		t.Errorf("am content mismatch: %q", got)
	}
	dstChangeID := mustReadCommit(t, mustHead(t)).ChangeID
	if dstChangeID != srcChangeID {
		t.Errorf("change-id should round-trip via format-patch + am: src=%s dst=%s", srcChangeID, dstChangeID)
	}
}

// 15 -- apply
func TestCmd_Apply(t *testing.T) {
	srcDir := t.TempDir()
	chdir(t, srcDir)
	cmdInit(nil)
	writeFile(t, "f.txt", "alpha\nbeta\n")
	mustCommit(t, "init")
	writeFile(t, "f.txt", "alpha\nBETA\ngamma\n")
	diffFile := filepath.Join(t.TempDir(), "wip.diff")
	f, _ := os.Create(diffFile)
	old := os.Stdout
	os.Stdout = f
	cmdDiff(nil)
	os.Stdout = old
	f.Close()

	// Reset working tree on src and apply.
	writeFile(t, "f.txt", "alpha\nbeta\n")
	if err := cmdApply([]string{diffFile}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile("f.txt")
	if string(got) != "alpha\nBETA\ngamma\n" {
		t.Errorf("apply result: %q", got)
	}
}
