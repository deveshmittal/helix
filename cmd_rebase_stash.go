package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Rebase: replay commits from current branch onto a new base.
// Linear only; aborts on conflict.
func cmdRebase(args []string) error {
	fs := flag.NewFlagSet("rebase", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: helix rebase <new-base>")
	}
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	newBase, err := r.ResolveAny(fs.Arg(0))
	if err != nil {
		return err
	}
	headHash, _ := r.ResolveHead()
	if headHash == "" {
		return fmt.Errorf("no commits to rebase")
	}
	if isAncestor(r, newBase, headHash) {
		// Find commits between newBase and HEAD.
		commits, err := commitsBetween(r, newBase, headHash)
		if err != nil {
			return err
		}
		if len(commits) == 0 {
			fmt.Println("Nothing to rebase.")
			return nil
		}
		// Move HEAD to newBase, checkout, then cherry-pick each.
		if err := updateHeadRef(r, newBase); err != nil {
			return err
		}
		if err := r.CheckoutCommit(newBase); err != nil {
			return err
		}
		for _, ch := range commits {
			if err := cherryPickCommit(r, ch); err != nil {
				return fmt.Errorf("rebase aborted at %s: %w", ch[:12], err)
			}
		}
		fmt.Printf("Rebased %d commit(s) onto %s\n", len(commits), newBase[:12])
		return nil
	}
	if isAncestor(r, headHash, newBase) {
		// HEAD is behind newBase; just fast-forward.
		if err := updateHeadRef(r, newBase); err != nil {
			return err
		}
		return r.CheckoutCommit(newBase)
	}
	// Diverged: find common ancestor and replay.
	mergeBase := commonAncestor(r, headHash, newBase)
	if mergeBase == "" {
		return fmt.Errorf("no common ancestor with %s", fs.Arg(0))
	}
	commits, err := commitsBetween(r, mergeBase, headHash)
	if err != nil {
		return err
	}
	if err := updateHeadRef(r, newBase); err != nil {
		return err
	}
	if err := r.CheckoutCommit(newBase); err != nil {
		return err
	}
	for _, ch := range commits {
		if err := cherryPickCommit(r, ch); err != nil {
			return fmt.Errorf("rebase aborted at %s: %w (manual recovery: helix reset --hard %s)", ch[:12], err, headHash[:12])
		}
	}
	fmt.Printf("Rebased %d commit(s) onto %s\n", len(commits), newBase[:12])
	return nil
}

// commitsBetween returns the list of commits ancestor..tip (exclusive of ancestor),
// in oldest-first order along the first-parent chain.
func commitsBetween(r *Repo, ancestor, tip string) ([]string, error) {
	var chain []string
	cur := tip
	for cur != "" && cur != ancestor {
		chain = append([]string{cur}, chain...)
		obj, err := r.ReadObject(cur)
		if err != nil {
			return nil, err
		}
		c, err := DecodeCommit(obj.Body)
		if err != nil {
			return nil, err
		}
		if len(c.Parents) == 0 {
			break
		}
		cur = c.Parents[0]
	}
	return chain, nil
}

// cherryPickCommit applies a single commit onto HEAD without printing.
func cherryPickCommit(r *Repo, hash string) error {
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
		Author:   src.Author,
		AuthorAt: time.Now(),
		ChangeID: src.ChangeID, // preserve change-id across rebase!
		Message:  src.Message,
	}
	obj := &Object{Kind: KindCommit, Body: c.Encode()}
	newHash, err := r.WriteObject(obj)
	if err != nil {
		return err
	}
	return updateHeadRef(r, newHash)
}

// Stash: save WIP as a commit pointed at by refs/stash, then reset working tree to HEAD.
func cmdStash(args []string) error {
	if len(args) == 0 {
		args = []string{"push"}
	}
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	switch args[0] {
	case "push", "save":
		return stashPush(r, args[1:])
	case "pop":
		return stashPop(r, true)
	case "apply":
		return stashPop(r, false)
	case "list":
		return stashList(r)
	case "drop":
		return r.DeleteRef("stash")
	default:
		return fmt.Errorf("usage: helix stash [push|pop|apply|list|drop]")
	}
}

func stashPush(r *Repo, args []string) error {
	headHash, _ := r.ResolveHead()
	if headHash == "" {
		return fmt.Errorf("nothing to stash: no commits yet")
	}
	dirty, err := r.IsDirty(headHash)
	if err != nil {
		return err
	}
	if !dirty {
		fmt.Println("No local changes to stash.")
		return nil
	}
	current, err := r.ScanWorkingTree()
	if err != nil {
		return err
	}
	tree, err := r.BuildTree(current)
	if err != nil {
		return err
	}
	msg := "WIP on " + branchOrDetached(r)
	if len(args) > 0 {
		msg = strings.Join(args, " ")
	}
	c := &Commit{
		Tree:     tree,
		Parents:  []string{headHash},
		Author:   defaultAuthor(),
		AuthorAt: time.Now(),
		ChangeID: NewChangeID(),
		Message:  msg + "\n",
	}
	obj := &Object{Kind: KindCommit, Body: c.Encode()}
	stashHash, err := r.WriteObject(obj)
	if err != nil {
		return err
	}
	if err := r.WriteRef("stash", stashHash); err != nil {
		return err
	}
	if err := r.CheckoutCommit(headHash); err != nil {
		return err
	}
	fmt.Printf("Stashed working tree at %s\n", stashHash[:12])
	fmt.Println("(Helix's design suggests committing WIP directly; stash is a compatibility convenience.)")
	return nil
}

func stashPop(r *Repo, drop bool) error {
	hash, err := r.ReadRef("stash")
	if err != nil || hash == "" {
		return fmt.Errorf("no stash entry")
	}
	// Apply: cherry-pick the diff from stash's parent to stash onto HEAD.
	if err := func() error {
		entries, err := r.applyCommitDiff(hash, false)
		if err != nil {
			return err
		}
		// We don't create a commit; just write entries to working tree.
		current, err := r.ScanWorkingTree()
		if err != nil {
			return err
		}
		curMap := map[string]bool{}
		for _, e := range current {
			curMap[e.Path] = true
		}
		newMap := map[string]bool{}
		for _, e := range entries {
			newMap[e.Path] = true
		}
		for p := range curMap {
			if !newMap[p] {
				os.Remove(filepath.Join(r.Root, p))
			}
		}
		for _, e := range entries {
			obj, err := r.ReadObject(e.Hash)
			if err != nil {
				return err
			}
			full := filepath.Join(r.Root, e.Path)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(0o644)
			if e.Mode == "100755" {
				mode = 0o755
			}
			if err := os.WriteFile(full, obj.Body, mode); err != nil {
				return err
			}
		}
		return nil
	}(); err != nil {
		return err
	}
	if drop {
		r.DeleteRef("stash")
	}
	fmt.Println("Stash applied.")
	return nil
}

func stashList(r *Repo) error {
	hash, _ := r.ReadRef("stash")
	if hash == "" {
		return nil
	}
	obj, err := r.ReadObject(hash)
	if err != nil {
		return err
	}
	c, err := DecodeCommit(obj.Body)
	if err != nil {
		return err
	}
	fmt.Printf("stash@{0}: %s\n", strings.TrimSpace(c.Message))
	return nil
}
