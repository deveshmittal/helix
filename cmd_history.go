package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// blame: for each line in <file> at HEAD, find the most recent commit in
// the file's history that introduced (or last touched) a matching line.
// Approximation: walks first-parent history; matches lines by exact content.
func cmdBlame(args []string) error {
	fs := flag.NewFlagSet("blame", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: helix blame <file>")
	}
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	path := fs.Arg(0)

	currentLines, err := fileLinesAt(r, "HEAD", path)
	if err != nil {
		return err
	}
	if currentLines == nil {
		return fmt.Errorf("%s: not in HEAD", path)
	}
	blame := make([]string, len(currentLines))

	// Walk first-parent chain.
	hash, _ := r.ResolveHead()
	for hash != "" {
		obj, err := r.ReadObject(hash)
		if err != nil {
			break
		}
		c, err := DecodeCommit(obj.Body)
		if err != nil {
			break
		}
		commitLines, _ := fileLinesAt(r, hash, path)
		var parent string
		if len(c.Parents) > 0 {
			parent = c.Parents[0]
		}
		parentLines, _ := fileLinesAt(r, parent, path)
		parentSet := map[string]bool{}
		for _, l := range parentLines {
			parentSet[l] = true
		}
		// For each line in commitLines that isn't in parentLines, mark it
		// in the blame map for any current-line match still unblamed.
		for _, line := range commitLines {
			if parentSet[line] {
				continue
			}
			for i, cl := range currentLines {
				if blame[i] == "" && cl == line {
					blame[i] = hash
				}
			}
		}
		// Stop once everything is blamed.
		anyEmpty := false
		for _, b := range blame {
			if b == "" {
				anyEmpty = true
				break
			}
		}
		if !anyEmpty {
			break
		}
		if parent == "" {
			break
		}
		hash = parent
	}

	// Print: <short-hash> (<author> <date>) <line>
	authorCache := map[string]string{}
	dateCache := map[string]string{}
	for i, line := range currentLines {
		h := blame[i]
		short := "????????????"
		auth := "unknown"
		date := ""
		if h != "" {
			short = h[:12]
			if c, ok := authorCache[h]; ok {
				auth = c
				date = dateCache[h]
			} else if obj, err := r.ReadObject(h); err == nil {
				if c, err := DecodeCommit(obj.Body); err == nil {
					auth = c.Author
					date = c.AuthorAt.Format("2006-01-02")
					authorCache[h] = auth
					dateCache[h] = date
				}
			}
		}
		fmt.Printf("%s (%-30s %s) %3d) %s\n", short, truncate(auth, 30), date, i+1, strings.TrimRight(line, "\n"))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n-1] + "…"
	}
	return s
}

func fileLinesAt(r *Repo, ref, path string) ([]string, error) {
	if ref == "" {
		return nil, nil
	}
	hash := ref
	if ref == "HEAD" {
		h, err := r.ResolveHead()
		if err != nil {
			return nil, err
		}
		hash = h
	}
	obj, err := r.ReadObject(hash)
	if err != nil {
		return nil, err
	}
	if obj.Kind != KindCommit {
		return nil, fmt.Errorf("not a commit: %s", hash)
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
		if e.Path == path {
			blob, err := r.ReadObject(e.Hash)
			if err != nil {
				return nil, err
			}
			return splitKeepNewline(string(blob.Body)), nil
		}
	}
	return nil, nil
}

// shortlog: count commits per author.
func cmdShortlog(args []string) error {
	fs := flag.NewFlagSet("shortlog", flag.ExitOnError)
	fs.Parse(args)
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	hash, _ := r.ResolveHead()
	counts := map[string]int{}
	subjects := map[string][]string{}
	visited := map[string]bool{}
	stack := []string{hash}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == "" || visited[cur] {
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
		counts[c.Author]++
		subjects[c.Author] = append(subjects[c.Author], firstLine(c.Message))
		stack = append(stack, c.Parents...)
	}
	authors := make([]string, 0, len(counts))
	for a := range counts {
		authors = append(authors, a)
	}
	sort.Slice(authors, func(i, j int) bool { return counts[authors[i]] > counts[authors[j]] })
	for _, a := range authors {
		fmt.Printf("%s (%d):\n", a, counts[a])
		for _, s := range subjects[a] {
			fmt.Printf("    %s\n", s)
		}
		fmt.Println()
	}
	return nil
}

// whatchanged: log + per-commit diff (basically `log -p`).
func cmdWhatchanged(args []string) error {
	fs := flag.NewFlagSet("whatchanged", flag.ExitOnError)
	max := fs.Int("n", 0, "max commits")
	fs.Parse(args)
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	hash, _ := r.ResolveHead()
	if hash == "" {
		fmt.Println("No commits.")
		return nil
	}
	count := 0
	for hash != "" {
		obj, err := r.ReadObject(hash)
		if err != nil {
			return err
		}
		c, err := DecodeCommit(obj.Body)
		if err != nil {
			return err
		}
		fmt.Printf("commit %s\n", hash)
		fmt.Printf("change %s\n", c.ChangeID)
		fmt.Printf("author %s\n", c.Author)
		fmt.Printf("date   %s\n\n", c.AuthorAt.Format(time.RFC3339))
		for _, line := range splitLines(c.Message) {
			fmt.Printf("    %s\n", line)
		}
		fmt.Println()

		var parentEntries []IndexEntry
		if len(c.Parents) > 0 {
			parentEntries, _ = flattenCommitEntries(r, c.Parents[0])
		}
		curEntries, _ := r.FlattenTree(c.Tree, "")
		printDiff(r, parentEntries, curEntries)
		fmt.Println()
		count++
		if *max > 0 && count >= *max {
			break
		}
		if len(c.Parents) == 0 {
			break
		}
		hash = c.Parents[0]
	}
	return nil
}

func flattenCommitEntries(r *Repo, hash string) ([]IndexEntry, error) {
	if hash == "" {
		return nil, nil
	}
	obj, err := r.ReadObject(hash)
	if err != nil {
		return nil, err
	}
	c, err := DecodeCommit(obj.Body)
	if err != nil {
		return nil, err
	}
	return r.FlattenTree(c.Tree, "")
}

// merge-base: print the common ancestor of two refs.
func cmdMergeBase(args []string) error {
	fs := flag.NewFlagSet("merge-base", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: helix merge-base <ref1> <ref2>")
	}
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	a, err := r.ResolveAny(fs.Arg(0))
	if err != nil {
		return err
	}
	b, err := r.ResolveAny(fs.Arg(1))
	if err != nil {
		return err
	}
	base := commonAncestor(r, a, b)
	if base == "" {
		fmt.Fprintln(os.Stderr, "no common ancestor")
		os.Exit(1)
	}
	fmt.Println(base)
	return nil
}
