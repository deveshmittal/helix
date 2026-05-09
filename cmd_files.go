package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Helix doesn't have a separate index; files are picked up automatically.
// `add` simply asserts that the given files exist (and aren't ignored), so
// scripts ported from git keep working.
func cmdAdd(args []string) error {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	all := fs.Bool("A", false, "add all files (no-op in helix; files are auto-tracked)")
	fs.Parse(args)
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	if *all || fs.NArg() == 0 {
		entries, err := r.ScanWorkingTree()
		if err != nil {
			return err
		}
		fmt.Printf("helix tracks %d files (no staging needed; commit when ready)\n", len(entries))
		return nil
	}
	for i := 0; i < fs.NArg(); i++ {
		p := fs.Arg(i)
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	}
	fmt.Printf("helix has no staging area — files are tracked automatically. %d path(s) verified.\n", fs.NArg())
	return nil
}

func cmdRm(args []string) error {
	fs := flag.NewFlagSet("rm", flag.ExitOnError)
	cached := fs.Bool("cached", false, "untrack but keep on disk (no-op: helix has no staging)")
	force := fs.Bool("f", false, "force")
	fs.Parse(args)
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: helix rm [-f] <path>...")
	}
	if *cached {
		return fmt.Errorf("helix has no staging area; use a .helixignore (TODO) or just don't track the file")
	}
	for i := 0; i < fs.NArg(); i++ {
		p := fs.Arg(i)
		if _, err := os.Stat(p); err != nil {
			if !*force {
				return fmt.Errorf("%s: %w", p, err)
			}
			continue
		}
		if err := os.Remove(p); err != nil {
			return err
		}
		fmt.Printf("removed %s\n", p)
	}
	return nil
}

func cmdMv(args []string) error {
	fs := flag.NewFlagSet("mv", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: helix mv <src> <dst>")
	}
	src, dst := fs.Arg(0), fs.Arg(1)
	if fi, err := os.Stat(dst); err == nil && fi.IsDir() {
		dst = filepath.Join(dst, filepath.Base(src))
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	fmt.Printf("renamed %s -> %s\n", src, dst)
	return nil
}

func cmdLsFiles(args []string) error {
	fs := flag.NewFlagSet("ls-files", flag.ExitOnError)
	fs.Parse(args)
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	entries, err := r.ScanWorkingTree()
	if err != nil {
		return err
	}
	for _, e := range entries {
		fmt.Println(e.Path)
	}
	return nil
}

func cmdLsTree(args []string) error {
	fs := flag.NewFlagSet("ls-tree", flag.ExitOnError)
	recursive := fs.Bool("r", false, "recurse into sub-trees")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: helix ls-tree [-r] <tree-or-commit>")
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
	if obj.Kind == KindCommit {
		c, err := DecodeCommit(obj.Body)
		if err != nil {
			return err
		}
		hash = c.Tree
		obj, err = r.ReadObject(hash)
		if err != nil {
			return err
		}
	}
	if obj.Kind != KindTree {
		return fmt.Errorf("not a tree or commit: %s (kind=%s)", fs.Arg(0), obj.Kind)
	}
	if *recursive {
		flat, err := r.FlattenTree(hash, "")
		if err != nil {
			return err
		}
		sort.Slice(flat, func(i, j int) bool { return flat[i].Path < flat[j].Path })
		for _, e := range flat {
			fmt.Printf("%s blob %s\t%s\n", e.Mode, e.Hash[:12], e.Path)
		}
		return nil
	}
	te, err := DecodeTree(obj.Body)
	if err != nil {
		return err
	}
	for _, e := range te {
		kind := "blob"
		if e.Mode == "40000" {
			kind = "tree"
		}
		fmt.Printf("%s %s %s\t%s\n", e.Mode, kind, e.Hash[:12], e.Name)
	}
	return nil
}

func cmdRevParse(args []string) error {
	fs := flag.NewFlagSet("rev-parse", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: helix rev-parse <ref>...")
	}
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	for i := 0; i < fs.NArg(); i++ {
		ref := fs.Arg(i)
		if ref == "HEAD" || ref == "@" {
			h, err := r.ResolveHead()
			if err != nil {
				return err
			}
			fmt.Println(h)
			continue
		}
		hash, err := r.ResolveAny(ref)
		if err != nil {
			return err
		}
		fmt.Println(hash)
	}
	return nil
}
