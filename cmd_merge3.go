package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// cmdMergeReal upgrades the FF-only merge to a real 3-way file+line merge.
// It replaces cmdMerge in dispatch.
func cmdMergeReal(args []string) error {
	fs := flag.NewFlagSet("merge", flag.ExitOnError)
	noFF := fs.Bool("no-ff", false, "always create a merge commit even if FF is possible")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: helix merge [--no-ff] <branch>")
	}
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	target, err := r.ResolveAny(fs.Arg(0))
	if err != nil {
		return err
	}
	headHash, _ := r.ResolveHead()
	if headHash == "" {
		if err := updateHeadRef(r, target); err != nil {
			return err
		}
		return r.CheckoutCommit(target)
	}
	if headHash == target {
		fmt.Println("Already up to date.")
		return nil
	}
	if !*noFF && isAncestor(r, headHash, target) {
		if err := updateHeadRef(r, target); err != nil {
			return err
		}
		if err := r.CheckoutCommit(target); err != nil {
			return err
		}
		fmt.Printf("Fast-forwarded to %s\n", target[:12])
		return nil
	}
	if isAncestor(r, target, headHash) {
		fmt.Println("Already up to date.")
		return nil
	}

	// Real 3-way merge.
	base := commonAncestor(r, headHash, target)
	if base == "" {
		return fmt.Errorf("no common ancestor between HEAD and %s", fs.Arg(0))
	}
	mergedEntries, conflicts, err := threeWayMergeTrees(r, base, headHash, target)
	if err != nil {
		return err
	}
	// Write merged content to working tree.
	current, err := r.ScanWorkingTree()
	if err != nil {
		return err
	}
	curMap := map[string]bool{}
	for _, e := range current {
		curMap[e.Path] = true
	}
	mergedMap := map[string]bool{}
	for _, e := range mergedEntries {
		mergedMap[e.Path] = true
	}
	// Remove files that disappeared.
	for p := range curMap {
		if !mergedMap[p] {
			os.Remove(filepath.Join(r.Root, p))
		}
	}
	// Write all merged files.
	for _, e := range mergedEntries {
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
	pruneEmptyDirs(r.Root)

	if conflicts > 0 {
		fmt.Fprintf(os.Stderr, "Merge has %d conflict(s); fix and run 'helix commit -m \"merge\"'\n", conflicts)
		// Record MERGE_HEAD so a follow-up commit can pick up the second parent.
		os.WriteFile(filepath.Join(r.HelixDir, "MERGE_HEAD"), []byte(target+"\n"), 0o644)
		return fmt.Errorf("merge conflicts in %d file(s)", conflicts)
	}

	// Auto-commit the merge.
	scanned, err := r.ScanWorkingTree()
	if err != nil {
		return err
	}
	tree, err := r.BuildTree(scanned)
	if err != nil {
		return err
	}
	c := &Commit{
		Tree:     tree,
		Parents:  []string{headHash, target},
		Author:   defaultAuthor(),
		AuthorAt: time.Now(),
		ChangeID: NewChangeID(),
		Message:  fmt.Sprintf("Merge %s into %s\n", fs.Arg(0), branchOrDetached(r)),
	}
	obj := &Object{Kind: KindCommit, Body: c.Encode()}
	newHash, err := r.WriteObject(obj)
	if err != nil {
		return err
	}
	if err := updateHeadRef(r, newHash); err != nil {
		return err
	}
	fmt.Printf("[%s %s] merged %s\n", branchOrDetached(r), newHash[:12], fs.Arg(0))
	return nil
}

func commonAncestor(r *Repo, a, b string) string {
	ancestorsA := map[string]bool{}
	stack := []string{a}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if ancestorsA[cur] {
			continue
		}
		ancestorsA[cur] = true
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
	// BFS from b to find first ancestor that's in ancestorsA.
	visited := map[string]bool{}
	queue := []string{b}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if visited[cur] {
			continue
		}
		visited[cur] = true
		if ancestorsA[cur] {
			return cur
		}
		obj, err := r.ReadObject(cur)
		if err != nil {
			continue
		}
		c, err := DecodeCommit(obj.Body)
		if err != nil {
			continue
		}
		queue = append(queue, c.Parents...)
	}
	return ""
}

func threeWayMergeTrees(r *Repo, baseCommit, oursCommit, theirsCommit string) ([]IndexEntry, int, error) {
	baseFiles, err := flattenCommit(r, baseCommit)
	if err != nil {
		return nil, 0, err
	}
	oursFiles, err := flattenCommit(r, oursCommit)
	if err != nil {
		return nil, 0, err
	}
	theirsFiles, err := flattenCommit(r, theirsCommit)
	if err != nil {
		return nil, 0, err
	}
	all := map[string]bool{}
	for k := range baseFiles {
		all[k] = true
	}
	for k := range oursFiles {
		all[k] = true
	}
	for k := range theirsFiles {
		all[k] = true
	}
	conflicts := 0
	var out []IndexEntry
	for path := range all {
		base, baseOK := baseFiles[path]
		ours, oursOK := oursFiles[path]
		theirs, theirsOK := theirsFiles[path]

		// File-level resolution first.
		if !baseOK {
			// Added on either or both sides.
			if oursOK && theirsOK {
				if ours.Hash == theirs.Hash {
					out = append(out, ours)
					continue
				}
				// Both added different content: line-merge with empty base.
				merged, conf, err := mergeFile(r, []byte{}, ours.Hash, theirs.Hash)
				if err != nil {
					return nil, 0, err
				}
				if conf {
					conflicts++
				}
				h, err := r.WriteObject(&Object{Kind: KindBlob, Body: merged})
				if err != nil {
					return nil, 0, err
				}
				out = append(out, IndexEntry{Path: path, Mode: ours.Mode, Hash: h})
				continue
			}
			if oursOK {
				out = append(out, ours)
				continue
			}
			if theirsOK {
				out = append(out, theirs)
				continue
			}
			continue
		}
		// base existed.
		if !oursOK && !theirsOK {
			continue // both deleted
		}
		if !oursOK {
			// we deleted; keep theirs only if unchanged from base
			if theirs.Hash == base.Hash {
				continue
			}
			// modify/delete conflict — keep theirs and flag
			conflicts++
			out = append(out, theirs)
			continue
		}
		if !theirsOK {
			if ours.Hash == base.Hash {
				continue
			}
			conflicts++
			out = append(out, ours)
			continue
		}
		// All three exist.
		if ours.Hash == theirs.Hash {
			out = append(out, ours)
			continue
		}
		if ours.Hash == base.Hash {
			out = append(out, theirs)
			continue
		}
		if theirs.Hash == base.Hash {
			out = append(out, ours)
			continue
		}
		// True 3-way line-level merge.
		baseObj, err := r.ReadObject(base.Hash)
		if err != nil {
			return nil, 0, err
		}
		merged, conf, err := mergeFile(r, baseObj.Body, ours.Hash, theirs.Hash)
		if err != nil {
			return nil, 0, err
		}
		if conf {
			conflicts++
		}
		h, err := r.WriteObject(&Object{Kind: KindBlob, Body: merged})
		if err != nil {
			return nil, 0, err
		}
		out = append(out, IndexEntry{Path: path, Mode: ours.Mode, Hash: h})
	}
	return out, conflicts, nil
}

func mergeFile(r *Repo, base []byte, oursHash, theirsHash string) ([]byte, bool, error) {
	oursObj, err := r.ReadObject(oursHash)
	if err != nil {
		return nil, false, err
	}
	theirsObj, err := r.ReadObject(theirsHash)
	if err != nil {
		return nil, false, err
	}
	bLines := splitKeepNewline(string(base))
	oLines := splitKeepNewline(string(oursObj.Body))
	tLines := splitKeepNewline(string(theirsObj.Body))
	merged, conflict := merge3(bLines, oLines, tLines)
	var out []byte
	for _, l := range merged {
		out = append(out, []byte(l)...)
	}
	return out, conflict, nil
}

func flattenCommit(r *Repo, commitHash string) (map[string]IndexEntry, error) {
	out := map[string]IndexEntry{}
	if commitHash == "" {
		return out, nil
	}
	obj, err := r.ReadObject(commitHash)
	if err != nil {
		return nil, err
	}
	c, err := DecodeCommit(obj.Body)
	if err != nil {
		return nil, err
	}
	flat, err := r.FlattenTree(c.Tree, "")
	if err != nil {
		return nil, err
	}
	for _, e := range flat {
		out[e.Path] = e
	}
	return out, nil
}
