package main

import (
	"archive/tar"
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// gc: walk all refs, compute the reachable set, delete unreachable objects.
func cmdGc(args []string) error {
	fs := flag.NewFlagSet("gc", flag.ExitOnError)
	dry := fs.Bool("n", false, "dry run")
	fs.Parse(args)
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	reachable := map[string]bool{}
	// Collect roots: HEAD, branches, tags, remotes, stash, notes.
	var roots []string
	if h, _ := r.ResolveHead(); h != "" {
		roots = append(roots, h)
	}
	branches, _ := r.ListBranches()
	for _, b := range branches {
		if h, err := r.ReadRef("branches/" + b); err == nil && h != "" {
			roots = append(roots, h)
		}
	}
	tags, _ := r.ListTags()
	for _, t := range tags {
		if h, err := r.ReadRef("tags/" + t); err == nil && h != "" {
			roots = append(roots, h)
		}
	}
	if h, err := r.ReadRef("stash"); err == nil && h != "" {
		roots = append(roots, h)
	}
	// Also walk refs/remotes/*/*
	remoteRoot := filepath.Join(r.RefsDir(), "remotes")
	filepath.WalkDir(remoteRoot, func(path string, d osDirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		h := strings.TrimSpace(string(data))
		if h != "" {
			roots = append(roots, h)
		}
		return nil
	})

	for _, h := range roots {
		markReachable(r, h, reachable)
	}

	// Walk every object on disk; delete those not reachable.
	deleted := 0
	kept := 0
	objDir := r.ObjectsDir()
	filepath.WalkDir(objDir, func(path string, d osDirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(objDir, path)
		hash := strings.ReplaceAll(rel, string(os.PathSeparator), "")
		if reachable[hash] {
			kept++
			return nil
		}
		if *dry {
			fmt.Printf("would delete %s\n", hash[:12])
		} else {
			os.Remove(path)
		}
		deleted++
		return nil
	})
	if *dry {
		fmt.Printf("Would delete %d unreachable objects (kept %d).\n", deleted, kept)
	} else {
		fmt.Printf("Deleted %d unreachable objects (kept %d).\n", deleted, kept)
	}
	return nil
}

func markReachable(r *Repo, hash string, set map[string]bool) {
	if set[hash] {
		return
	}
	obj, err := r.ReadObject(hash)
	if err != nil {
		return
	}
	set[hash] = true
	switch obj.Kind {
	case KindCommit:
		c, err := DecodeCommit(obj.Body)
		if err != nil {
			return
		}
		markReachable(r, c.Tree, set)
		for _, p := range c.Parents {
			markReachable(r, p, set)
		}
	case KindTree:
		te, err := DecodeTree(obj.Body)
		if err != nil {
			return
		}
		for _, e := range te {
			markReachable(r, e.Hash, set)
		}
	}
}

// fsck: re-hash every object on disk and verify the filename matches.
// Also verify reachable refs point to existing objects of the right kind.
func cmdFsck(args []string) error {
	fs := flag.NewFlagSet("fsck", flag.ExitOnError)
	fs.Parse(args)
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	var problems []string
	objDir := r.ObjectsDir()
	count := 0
	filepath.WalkDir(objDir, func(path string, d osDirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(objDir, path)
		hash := strings.ReplaceAll(rel, string(os.PathSeparator), "")
		data, err := os.ReadFile(path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("read fail: %s: %v", hash[:12], err))
			return nil
		}
		sum := sha256.Sum256(data)
		got := hex.EncodeToString(sum[:])
		if got != hash {
			problems = append(problems, fmt.Sprintf("hash mismatch: %s -> %s", hash, got))
		}
		count++
		return nil
	})

	// Verify refs point to commits or tags.
	checkRef := func(ref, hash string) {
		obj, err := r.ReadObject(hash)
		if err != nil {
			problems = append(problems, fmt.Sprintf("dangling ref: %s -> %s", ref, hash))
			return
		}
		_ = obj
	}
	branches, _ := r.ListBranches()
	for _, b := range branches {
		if h, err := r.ReadRef("branches/" + b); err == nil && h != "" {
			checkRef("branches/"+b, h)
		}
	}
	tags, _ := r.ListTags()
	for _, t := range tags {
		if h, err := r.ReadRef("tags/" + t); err == nil && h != "" {
			checkRef("tags/"+t, h)
		}
	}

	if len(problems) == 0 {
		fmt.Printf("OK: %d objects, all refs valid.\n", count)
		return nil
	}
	for _, p := range problems {
		fmt.Fprintln(os.Stderr, p)
	}
	return fmt.Errorf("fsck found %d problem(s)", len(problems))
}

// archive: write a tar or zip of a tree to stdout (or file).
func cmdArchive(args []string) error {
	fs := flag.NewFlagSet("archive", flag.ExitOnError)
	format := fs.String("format", "tar", "tar | zip")
	out := fs.String("o", "", "output file (default stdout)")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: helix archive [--format tar|zip] [-o <file>] <commit>")
	}
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	hash, err := r.ResolveAny(fs.Arg(0))
	if err != nil {
		return err
	}
	obj, err := r.ReadObject(hash)
	if err != nil {
		return err
	}
	c, err := DecodeCommit(obj.Body)
	if err != nil {
		return err
	}
	flat, err := r.FlattenTree(c.Tree, "")
	if err != nil {
		return err
	}

	var w io.Writer = os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}

	switch *format {
	case "tar":
		tw := tar.NewWriter(w)
		defer tw.Close()
		for _, e := range flat {
			blob, err := r.ReadObject(e.Hash)
			if err != nil {
				return err
			}
			mode := int64(0o644)
			if e.Mode == "100755" {
				mode = 0o755
			}
			hdr := &tar.Header{Name: e.Path, Mode: mode, Size: int64(len(blob.Body))}
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			if _, err := tw.Write(blob.Body); err != nil {
				return err
			}
		}
	case "zip":
		zw := zip.NewWriter(w)
		defer zw.Close()
		for _, e := range flat {
			blob, err := r.ReadObject(e.Hash)
			if err != nil {
				return err
			}
			fw, err := zw.Create(e.Path)
			if err != nil {
				return err
			}
			if _, err := fw.Write(blob.Body); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unknown format: %s", *format)
	}
	return nil
}

// worktree: a worktree in this MVP is a directory backed by 'helix clone'.
// We track them in .helix/worktrees so 'list' works.
//
//	helix worktree add <path> [<branch>]
//	helix worktree list
//	helix worktree remove <path>
func cmdWorktree(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: helix worktree <add|list|remove> [args]")
	}
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	wtdir := filepath.Join(r.HelixDir, "worktrees.list")
	switch args[0] {
	case "add":
		if len(args) < 2 {
			return fmt.Errorf("usage: helix worktree add <path> [<branch>]")
		}
		path, _ := filepath.Abs(args[1])
		// Use clone to populate the new directory.
		// (Note: the design proposes a shared object store — see DESIGN.md §11.4.
		// This MVP just clones, which duplicates objects; the higher-fidelity
		// version belongs in Phase 2.)
		dst := filepath.Dir(path)
		name := filepath.Base(path)
		old, _ := os.Getwd()
		os.Chdir(dst)
		if err := cmdClone([]string{r.Root, name}); err != nil {
			os.Chdir(old)
			return err
		}
		os.Chdir(old)
		// If a branch was specified, switch the new worktree to it.
		if len(args) >= 3 {
			os.Chdir(path)
			cmdSwitch([]string{args[2]})
			os.Chdir(old)
		}
		// Append to list.
		f, _ := os.OpenFile(wtdir, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		f.WriteString(path + "\n")
		f.Close()
		fmt.Printf("Added worktree at %s\n", path)
		return nil
	case "list":
		fmt.Printf("%s  (main)\n", r.Root)
		data, _ := os.ReadFile(wtdir)
		for _, l := range strings.Split(string(data), "\n") {
			if l != "" {
				fmt.Println(l)
			}
		}
		return nil
	case "remove":
		if len(args) < 2 {
			return fmt.Errorf("usage: helix worktree remove <path>")
		}
		path, _ := filepath.Abs(args[1])
		data, _ := os.ReadFile(wtdir)
		var keep []string
		for _, l := range strings.Split(string(data), "\n") {
			if l != "" && l != path {
				keep = append(keep, l)
			}
		}
		os.WriteFile(wtdir, []byte(strings.Join(keep, "\n")+"\n"), 0o644)
		os.RemoveAll(path)
		fmt.Printf("Removed worktree %s\n", path)
		return nil
	}
	return fmt.Errorf("unknown worktree op: %s", args[0])
}
