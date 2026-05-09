package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func cmdRestore(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	source := fs.String("source", "HEAD", "commit to restore from")
	fs.Parse(args)
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: helix restore [--source <ref>] <path>...")
	}
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	hash, err := r.ResolveAny(*source)
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
	idx := map[string]IndexEntry{}
	for _, e := range flat {
		idx[e.Path] = e
	}
	for i := 0; i < fs.NArg(); i++ {
		p := fs.Arg(i)
		e, ok := idx[p]
		if !ok {
			return fmt.Errorf("%s: not in %s", p, *source)
		}
		blob, err := r.ReadObject(e.Hash)
		if err != nil {
			return err
		}
		full := filepath.Join(r.Root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if e.Mode == "100755" {
			mode = 0o755
		}
		if err := os.WriteFile(full, blob.Body, mode); err != nil {
			return err
		}
		fmt.Printf("restored %s\n", p)
	}
	return nil
}

func cmdReset(args []string) error {
	fs := flag.NewFlagSet("reset", flag.ExitOnError)
	hard := fs.Bool("hard", false, "reset working tree to target commit (DESTRUCTIVE)")
	soft := fs.Bool("soft", false, "move HEAD only")
	fs.Parse(args)
	target := "HEAD"
	if fs.NArg() == 1 {
		target = fs.Arg(0)
	}
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	hash, err := r.ResolveAny(target)
	if err != nil {
		return err
	}
	prev, _ := r.ResolveHead()
	head, err := r.ReadHead()
	if err != nil {
		return err
	}
	if head.Symbolic {
		if err := r.WriteRef(head.Ref, hash); err != nil {
			return err
		}
	} else {
		if err := os.WriteFile(r.HeadFile(), []byte(hash+"\n"), 0o644); err != nil {
			return err
		}
	}
	mode := "soft"
	if *hard {
		mode = "hard"
	}
	r.LogRef("HEAD", prev, hash, "reset --"+mode, "")
	if *hard {
		if err := r.CheckoutCommit(hash); err != nil {
			return err
		}
		fmt.Printf("HEAD is now at %s (working tree replaced)\n", hash[:12])
		return nil
	}
	if *soft {
		fmt.Printf("HEAD moved to %s (working tree unchanged)\n", hash[:12])
		return nil
	}
	fmt.Printf("HEAD moved to %s (working tree unchanged; pass --hard to also reset files)\n", hash[:12])
	return nil
}

func cmdClean(args []string) error {
	fs := flag.NewFlagSet("clean", flag.ExitOnError)
	dryRun := fs.Bool("n", false, "dry run")
	force := fs.Bool("f", false, "actually delete")
	fs.Parse(args)
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	if !*dryRun && !*force {
		return fmt.Errorf("refusing to clean without -n (dry-run) or -f (force)")
	}
	headHash, _ := r.ResolveHead()
	tracked := map[string]bool{}
	if headHash != "" {
		obj, err := r.ReadObject(headHash)
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
		for _, e := range flat {
			tracked[e.Path] = true
		}
	}
	current, err := r.ScanWorkingTree()
	if err != nil {
		return err
	}
	for _, e := range current {
		if !tracked[e.Path] {
			if *dryRun {
				fmt.Printf("would remove %s\n", e.Path)
			} else {
				path := filepath.Join(r.Root, e.Path)
				if err := os.Remove(path); err != nil {
					return err
				}
				fmt.Printf("removed %s\n", e.Path)
			}
		}
	}
	if !*dryRun {
		pruneEmptyDirs(r.Root)
	}
	return nil
}

// applyCommitDiff applies the file-level diff between (parent → commit) on top of HEAD.
// If a file in the diff already differs from the parent's version, returns an error.
func (r *Repo) applyCommitDiff(commitHash string, invert bool) ([]IndexEntry, error) {
	obj, err := r.ReadObject(commitHash)
	if err != nil {
		return nil, err
	}
	c, err := DecodeCommit(obj.Body)
	if err != nil {
		return nil, err
	}
	var parentTree string
	if len(c.Parents) > 0 {
		pobj, err := r.ReadObject(c.Parents[0])
		if err != nil {
			return nil, err
		}
		pc, err := DecodeCommit(pobj.Body)
		if err != nil {
			return nil, err
		}
		parentTree = pc.Tree
	}
	parentList, err := r.FlattenTree(parentTree, "")
	if err != nil {
		return nil, err
	}
	cList, err := r.FlattenTree(c.Tree, "")
	if err != nil {
		return nil, err
	}
	parentMap := map[string]IndexEntry{}
	for _, e := range parentList {
		parentMap[e.Path] = e
	}
	cMap := map[string]IndexEntry{}
	for _, e := range cList {
		cMap[e.Path] = e
	}

	current, err := r.ScanWorkingTree()
	if err != nil {
		return nil, err
	}
	curMap := map[string]IndexEntry{}
	for _, e := range current {
		curMap[e.Path] = e
	}

	// All files touched in the commit.
	touched := map[string]bool{}
	for k := range parentMap {
		if cMap[k].Hash != parentMap[k].Hash {
			touched[k] = true
		}
	}
	for k := range cMap {
		if cMap[k].Hash != parentMap[k].Hash {
			touched[k] = true
		}
	}

	for path := range touched {
		from := parentMap[path]
		to := cMap[path]
		if invert {
			from, to = to, from
		}
		curEntry, exists := curMap[path]
		// "from" is the version we expect to see.
		if from.Hash == "" {
			// Adding a file. Conflict if file already exists with different content.
			if exists && curEntry.Hash != to.Hash {
				return nil, fmt.Errorf("conflict: %s already exists in working tree", path)
			}
		} else if to.Hash == "" {
			// Deleting a file. Conflict if file's current content differs from "from".
			if !exists || curEntry.Hash != from.Hash {
				return nil, fmt.Errorf("conflict: %s differs from expected base", path)
			}
		} else {
			// Modifying. Current must equal "from" or we conflict.
			if !exists || curEntry.Hash != from.Hash {
				return nil, fmt.Errorf("conflict on %s", path)
			}
		}
	}

	// Apply.
	for path := range touched {
		from := parentMap[path]
		to := cMap[path]
		if invert {
			from, to = to, from
		}
		full := filepath.Join(r.Root, path)
		if to.Hash == "" {
			os.Remove(full)
			continue
		}
		blob, err := r.ReadObject(to.Hash)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return nil, err
		}
		mode := os.FileMode(0o644)
		if to.Mode == "100755" {
			mode = 0o755
		}
		if err := os.WriteFile(full, blob.Body, mode); err != nil {
			return nil, err
		}
		_ = from
	}
	pruneEmptyDirs(r.Root)
	return r.ScanWorkingTree()
}

func cmdCherryPick(args []string) error {
	fs := flag.NewFlagSet("cherry-pick", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: helix cherry-pick <commit>")
	}
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	hash, err := r.ResolveAny(fs.Arg(0))
	if err != nil {
		return err
	}
	srcObj, err := r.ReadObject(hash)
	if err != nil {
		return err
	}
	src, err := DecodeCommit(srcObj.Body)
	if err != nil {
		return err
	}
	entries, err := r.applyCommitDiff(hash, false)
	if err != nil {
		return err
	}
	tree, err := r.BuildTree(entries)
	if err != nil {
		return err
	}
	headHash, _ := r.ResolveHead()
	parents := []string{}
	if headHash != "" {
		parents = []string{headHash}
	}
	c := &Commit{
		Tree:     tree,
		Parents:  parents,
		Author:   defaultAuthor(),
		AuthorAt: time.Now(),
		ChangeID: NewChangeID(),
		Message:  src.Message + "\n(cherry picked from commit " + hash + ")\n",
	}
	obj := &Object{Kind: KindCommit, Body: c.Encode()}
	newHash, err := r.WriteObject(obj)
	if err != nil {
		return err
	}
	if err := updateHeadRef(r, newHash); err != nil {
		return err
	}
	fmt.Printf("[%s %s] cherry-picked %s\n", branchOrDetached(r), newHash[:12], hash[:12])
	return nil
}

func cmdRevert(args []string) error {
	fs := flag.NewFlagSet("revert", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: helix revert <commit>")
	}
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	hash, err := r.ResolveAny(fs.Arg(0))
	if err != nil {
		return err
	}
	srcObj, err := r.ReadObject(hash)
	if err != nil {
		return err
	}
	src, err := DecodeCommit(srcObj.Body)
	if err != nil {
		return err
	}
	entries, err := r.applyCommitDiff(hash, true)
	if err != nil {
		return err
	}
	tree, err := r.BuildTree(entries)
	if err != nil {
		return err
	}
	headHash, _ := r.ResolveHead()
	parents := []string{}
	if headHash != "" {
		parents = []string{headHash}
	}
	msg := "Revert: " + firstLine(src.Message) + "\n\nThis reverts commit " + hash + ".\n"
	c := &Commit{
		Tree:     tree,
		Parents:  parents,
		Author:   defaultAuthor(),
		AuthorAt: time.Now(),
		ChangeID: NewChangeID(),
		Message:  msg,
	}
	obj := &Object{Kind: KindCommit, Body: c.Encode()}
	newHash, err := r.WriteObject(obj)
	if err != nil {
		return err
	}
	if err := updateHeadRef(r, newHash); err != nil {
		return err
	}
	fmt.Printf("[%s %s] reverted %s\n", branchOrDetached(r), newHash[:12], hash[:12])
	return nil
}

func isAncestor(r *Repo, ancestor, descendant string) bool {
	visited := map[string]bool{}
	stack := []string{descendant}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == ancestor {
			return true
		}
		if visited[cur] {
			continue
		}
		visited[cur] = true
		obj, err := r.ReadObject(cur)
		if err != nil {
			continue
		}
		c, err := DecodeCommit(obj.Body)
		if err != nil {
			continue
		}
		stack = append(stack, c.Parents...)
	}
	return false
}

func updateHeadRef(r *Repo, hash string) error {
	head, err := r.ReadHead()
	if err != nil {
		return err
	}
	if head.Symbolic {
		return r.WriteRef(head.Ref, hash)
	}
	return os.WriteFile(r.HeadFile(), []byte(hash+"\n"), 0o644)
}

// --- config and remote (simple file-based storage) ---

func cmdConfig(args []string) error {
	fs := flag.NewFlagSet("config", flag.ExitOnError)
	list := fs.Bool("list", false, "list all config")
	unset := fs.Bool("unset", false, "remove a key")
	fs.Parse(args)
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	cfgPath := filepath.Join(r.HelixDir, "config")
	cfg, _ := os.ReadFile(cfgPath)
	lines := strings.Split(string(cfg), "\n")
	if *list {
		for _, l := range lines {
			if l != "" {
				fmt.Println(l)
			}
		}
		return nil
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: helix config <key> [value] | --list | --unset <key>")
	}
	key := fs.Arg(0)
	if *unset {
		var out []string
		for _, l := range lines {
			if !strings.HasPrefix(l, key+" = ") {
				out = append(out, l)
			}
		}
		return os.WriteFile(cfgPath, []byte(strings.Join(out, "\n")), 0o644)
	}
	if fs.NArg() == 1 {
		// get
		for _, l := range lines {
			if strings.HasPrefix(l, key+" = ") {
				fmt.Println(strings.TrimPrefix(l, key+" = "))
				return nil
			}
		}
		return fmt.Errorf("not set: %s", key)
	}
	value := strings.Join(fs.Args()[1:], " ")
	var out []string
	found := false
	for _, l := range lines {
		if strings.HasPrefix(l, key+" = ") {
			out = append(out, key+" = "+value)
			found = true
		} else {
			out = append(out, l)
		}
	}
	if !found {
		out = append(out, key+" = "+value)
	}
	return os.WriteFile(cfgPath, []byte(strings.Join(out, "\n")), 0o644)
}

func cmdRemote(args []string) error {
	fs := flag.NewFlagSet("remote", flag.ExitOnError)
	fs.Parse(args)
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	rpath := filepath.Join(r.HelixDir, "remotes")
	if fs.NArg() == 0 {
		data, _ := os.ReadFile(rpath)
		for _, l := range strings.Split(string(data), "\n") {
			if l != "" {
				if name, _, ok := strings.Cut(l, " "); ok {
					fmt.Println(name)
				}
			}
		}
		return nil
	}
	op := fs.Arg(0)
	switch op {
	case "add":
		if fs.NArg() != 3 {
			return fmt.Errorf("usage: helix remote add <name> <url>")
		}
		name, url := fs.Arg(1), fs.Arg(2)
		data, _ := os.ReadFile(rpath)
		lines := strings.Split(string(data), "\n")
		var out []string
		for _, l := range lines {
			if l == "" {
				continue
			}
			if !strings.HasPrefix(l, name+" ") {
				out = append(out, l)
			}
		}
		out = append(out, name+" "+url)
		return os.WriteFile(rpath, []byte(strings.Join(out, "\n")+"\n"), 0o644)
	case "remove", "rm":
		if fs.NArg() != 2 {
			return fmt.Errorf("usage: helix remote remove <name>")
		}
		name := fs.Arg(1)
		data, _ := os.ReadFile(rpath)
		lines := strings.Split(string(data), "\n")
		var out []string
		for _, l := range lines {
			if l != "" && !strings.HasPrefix(l, name+" ") {
				out = append(out, l)
			}
		}
		return os.WriteFile(rpath, []byte(strings.Join(out, "\n")+"\n"), 0o644)
	case "-v":
		data, _ := os.ReadFile(rpath)
		for _, l := range strings.Split(string(data), "\n") {
			if l != "" {
				fmt.Println(l)
			}
		}
		return nil
	}
	return fmt.Errorf("unknown remote op: %s", op)
}
