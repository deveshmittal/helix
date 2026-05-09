package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func cmdBranch(args []string) error {
	fs := flag.NewFlagSet("branch", flag.ExitOnError)
	del := fs.String("d", "", "delete branch")
	force := fs.Bool("f", false, "force")
	fs.Parse(args)
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	if *del != "" {
		_ = force
		if err := r.DeleteRef("branches/" + *del); err != nil {
			return err
		}
		fmt.Printf("Deleted branch %s\n", *del)
		return nil
	}
	if fs.NArg() == 1 {
		// create branch at HEAD
		name := fs.Arg(0)
		hash, err := r.ResolveHead()
		if err != nil {
			return err
		}
		if hash == "" {
			return fmt.Errorf("cannot create branch %s: no commits yet", name)
		}
		if err := r.WriteRef("branches/"+name, hash); err != nil {
			return err
		}
		fmt.Printf("Created branch %s at %s\n", name, hash[:12])
		return nil
	}
	if fs.NArg() == 2 {
		// create branch at given ref
		name, ref := fs.Arg(0), fs.Arg(1)
		hash, err := r.ResolveAny(ref)
		if err != nil {
			return err
		}
		if err := r.WriteRef("branches/"+name, hash); err != nil {
			return err
		}
		fmt.Printf("Created branch %s at %s\n", name, hash[:12])
		return nil
	}
	// list
	branches, err := r.ListBranches()
	if err != nil {
		return err
	}
	sort.Strings(branches)
	cur, _ := r.CurrentBranch()
	for _, b := range branches {
		marker := "  "
		if b == cur {
			marker = "* "
		}
		fmt.Printf("%s%s\n", marker, b)
	}
	return nil
}

func cmdSwitch(args []string) error {
	fs := flag.NewFlagSet("switch", flag.ExitOnError)
	create := fs.Bool("c", false, "create branch first")
	force := fs.Bool("f", false, "discard local changes (DESTRUCTIVE)")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: helix switch [-c] [-f] <branch>")
	}
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	name := fs.Arg(0)

	// Check working tree clean.
	headHash, _ := r.ResolveHead()
	if !*force && headHash != "" {
		dirty, err := r.IsDirty(headHash)
		if err != nil {
			return err
		}
		if dirty {
			return fmt.Errorf("working tree has uncommitted changes; commit or use -f to discard")
		}
	}

	if *create {
		hash, err := r.ResolveHead()
		if err != nil {
			return err
		}
		if hash == "" {
			return fmt.Errorf("cannot create branch %s: no commits yet", name)
		}
		if err := r.WriteRef("branches/"+name, hash); err != nil {
			return err
		}
	}

	target, err := r.ReadRef("branches/" + name)
	if err != nil || target == "" {
		return fmt.Errorf("branch not found: %s", name)
	}

	if err := r.CheckoutCommit(target); err != nil {
		return err
	}
	if err := os.WriteFile(r.HeadFile(), []byte("ref: branches/"+name+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Printf("Switched to branch %s\n", name)
	return nil
}

// checkout is kept as an alias for switch, with a note.
func cmdCheckout(args []string) error {
	fmt.Fprintln(os.Stderr, "note: 'checkout' is a Git compatibility alias; prefer 'helix switch' or 'helix restore'")
	return cmdSwitch(args)
}

func cmdTag(args []string) error {
	fs := flag.NewFlagSet("tag", flag.ExitOnError)
	del := fs.String("d", "", "delete tag")
	fs.Parse(args)
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	if *del != "" {
		if err := r.DeleteRef("tags/" + *del); err != nil {
			return err
		}
		fmt.Printf("Deleted tag %s\n", *del)
		return nil
	}
	if fs.NArg() >= 1 {
		name := fs.Arg(0)
		var hash string
		if fs.NArg() == 2 {
			hash, err = r.ResolveAny(fs.Arg(1))
			if err != nil {
				return err
			}
		} else {
			hash, err = r.ResolveHead()
			if err != nil {
				return err
			}
		}
		if hash == "" {
			return fmt.Errorf("cannot create tag at empty HEAD")
		}
		if err := r.WriteRef("tags/"+name, hash); err != nil {
			return err
		}
		fmt.Printf("Created tag %s -> %s\n", name, hash[:12])
		return nil
	}
	tags, err := r.ListTags()
	if err != nil {
		return err
	}
	sort.Strings(tags)
	for _, t := range tags {
		fmt.Println(t)
	}
	return nil
}

// CheckoutCommit replaces working-tree contents with those of the given commit.
// It removes files no longer present, writes files from the target tree,
// and prunes empty directories.
func (r *Repo) CheckoutCommit(commitHash string) error {
	obj, err := r.ReadObject(commitHash)
	if err != nil {
		return err
	}
	c, err := DecodeCommit(obj.Body)
	if err != nil {
		return err
	}
	target, err := r.FlattenTree(c.Tree, "")
	if err != nil {
		return err
	}
	current, err := r.ScanWorkingTree()
	if err != nil {
		return err
	}
	targetMap := map[string]IndexEntry{}
	for _, e := range target {
		targetMap[e.Path] = e
	}
	// Remove files in current but not in target.
	for _, e := range current {
		if _, ok := targetMap[e.Path]; !ok {
			_ = os.Remove(filepath.Join(r.Root, e.Path))
		}
	}
	// Write target files.
	for _, e := range target {
		obj, err := r.ReadObject(e.Hash)
		if err != nil {
			return err
		}
		path := filepath.Join(r.Root, e.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if e.Mode == "100755" {
			mode = 0o755
		}
		if err := os.WriteFile(path, obj.Body, mode); err != nil {
			return err
		}
	}
	// Prune now-empty directories under root.
	pruneEmptyDirs(r.Root)
	return nil
}

func pruneEmptyDirs(root string) {
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() || path == root {
			return nil
		}
		if filepath.Base(path) == helixDir {
			return filepath.SkipDir
		}
		entries, _ := os.ReadDir(path)
		if len(entries) == 0 {
			os.Remove(path)
		}
		return nil
	})
}

// IsDirty returns true if the working tree differs from the given commit's tree.
func (r *Repo) IsDirty(commitHash string) (bool, error) {
	obj, err := r.ReadObject(commitHash)
	if err != nil {
		return false, err
	}
	c, err := DecodeCommit(obj.Body)
	if err != nil {
		return false, err
	}
	committed, err := r.FlattenTree(c.Tree, "")
	if err != nil {
		return false, err
	}
	current, err := r.ScanWorkingTree()
	if err != nil {
		return false, err
	}
	if len(committed) != len(current) {
		return true, nil
	}
	cm := map[string]IndexEntry{}
	for _, e := range committed {
		cm[e.Path] = e
	}
	for _, e := range current {
		c, ok := cm[e.Path]
		if !ok || c.Hash != e.Hash || c.Mode != e.Mode {
			return true, nil
		}
	}
	return false, nil
}
