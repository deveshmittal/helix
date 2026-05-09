package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"time"
)

func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	fs.Parse(args)
	target := "."
	if fs.NArg() == 1 {
		target = fs.Arg(0)
	}
	r, err := InitRepo(target)
	if err != nil {
		return err
	}
	fmt.Printf("Initialized empty helix repository in %s\n", r.HelixDir)
	return nil
}

func cmdHashObject(args []string) error {
	fs := flag.NewFlagSet("hash-object", flag.ExitOnError)
	write := fs.Bool("w", false, "store the object")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: helix hash-object [-w] <file>")
	}
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	obj := &Object{Kind: KindBlob, Body: data}
	if *write {
		h, err := r.WriteObject(obj)
		if err != nil {
			return err
		}
		fmt.Println(h)
		return nil
	}
	fmt.Println(obj.Hash())
	return nil
}

func cmdCatObject(args []string) error {
	fs := flag.NewFlagSet("cat-object", flag.ExitOnError)
	pretty := fs.Bool("p", false, "pretty-print")
	typeOnly := fs.Bool("t", false, "show type only")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: helix cat-object [-p|-t] <hash>")
	}
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	obj, err := r.ReadObject(fs.Arg(0))
	if err != nil {
		return err
	}
	if *typeOnly {
		fmt.Println(obj.Kind)
		return nil
	}
	switch obj.Kind {
	case KindBlob:
		os.Stdout.Write(obj.Body)
	case KindTree:
		te, err := DecodeTree(obj.Body)
		if err != nil {
			return err
		}
		for _, e := range te {
			fmt.Printf("%s %s\t%s\n", e.Mode, e.Hash[:12], e.Name)
		}
	case KindCommit:
		if *pretty {
			c, err := DecodeCommit(obj.Body)
			if err != nil {
				return err
			}
			fmt.Printf("tree     %s\n", c.Tree)
			for _, p := range c.Parents {
				fmt.Printf("parent   %s\n", p)
			}
			fmt.Printf("author   %s\n", c.Author)
			fmt.Printf("when     %s\n", c.AuthorAt.Format(time.RFC3339))
			fmt.Printf("change   %s\n", c.ChangeID)
			fmt.Printf("\n%s", c.Message)
		} else {
			os.Stdout.Write(obj.Body)
		}
	}
	return nil
}

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	fs.Parse(args)
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	branch, _ := r.CurrentBranch()
	headHash, _ := r.ResolveHead()

	if branch != "" {
		fmt.Printf("On branch %s\n", branch)
	} else {
		fmt.Println("HEAD detached")
	}

	current, err := r.ScanWorkingTree()
	if err != nil {
		return err
	}

	var headEntries []IndexEntry
	if headHash != "" {
		obj, err := r.ReadObject(headHash)
		if err != nil {
			return err
		}
		c, err := DecodeCommit(obj.Body)
		if err != nil {
			return err
		}
		headEntries, err = r.FlattenTree(c.Tree, "")
		if err != nil {
			return err
		}
	}

	headMap := map[string]IndexEntry{}
	for _, e := range headEntries {
		headMap[e.Path] = e
	}
	curMap := map[string]IndexEntry{}
	for _, e := range current {
		curMap[e.Path] = e
	}

	var added, modified, deleted []string
	for _, e := range current {
		h, ok := headMap[e.Path]
		if !ok {
			added = append(added, e.Path)
		} else if h.Hash != e.Hash || h.Mode != e.Mode {
			modified = append(modified, e.Path)
		}
	}
	for _, e := range headEntries {
		if _, ok := curMap[e.Path]; !ok {
			deleted = append(deleted, e.Path)
		}
	}
	sort.Strings(added)
	sort.Strings(modified)
	sort.Strings(deleted)

	if len(added)+len(modified)+len(deleted) == 0 {
		if headHash == "" {
			fmt.Println("No commits yet. Working tree is empty.")
		} else {
			fmt.Println("Working tree clean.")
		}
		return nil
	}
	if len(added) > 0 {
		fmt.Println("\nAdded:")
		for _, p := range added {
			fmt.Printf("  + %s\n", p)
		}
	}
	if len(modified) > 0 {
		fmt.Println("\nModified:")
		for _, p := range modified {
			fmt.Printf("  ~ %s\n", p)
		}
	}
	if len(deleted) > 0 {
		fmt.Println("\nDeleted:")
		for _, p := range deleted {
			fmt.Printf("  - %s\n", p)
		}
	}
	return nil
}

func cmdCommit(args []string) error {
	fs := flag.NewFlagSet("commit", flag.ExitOnError)
	msg := fs.String("m", "", "commit message")
	amend := fs.Bool("amend", false, "amend the previous commit (preserves change-id)")
	fs.Parse(args)
	if *msg == "" {
		return fmt.Errorf("commit message required (-m)")
	}
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	current, err := r.ScanWorkingTree()
	if err != nil {
		return err
	}
	if len(current) == 0 {
		return fmt.Errorf("nothing to commit (working tree empty)")
	}
	tree, err := r.BuildTree(current)
	if err != nil {
		return err
	}
	headHash, _ := r.ResolveHead()
	var parents []string
	changeID := NewChangeID()
	if headHash != "" {
		if *amend {
			// reuse parents and change-id from current HEAD
			obj, err := r.ReadObject(headHash)
			if err != nil {
				return err
			}
			prev, err := DecodeCommit(obj.Body)
			if err != nil {
				return err
			}
			parents = prev.Parents
			if prev.ChangeID != "" {
				changeID = prev.ChangeID
			}
		} else {
			parents = []string{headHash}
		}
	}
	c := &Commit{
		Tree:     tree,
		Parents:  parents,
		Author:   defaultAuthor(),
		AuthorAt: time.Now(),
		ChangeID: changeID,
		Message:  *msg,
	}
	obj := &Object{Kind: KindCommit, Body: c.Encode()}
	hash, err := r.WriteObject(obj)
	if err != nil {
		return err
	}
	head, err := r.ReadHead()
	if err != nil {
		return err
	}
	prevHead := headHash
	if head.Symbolic {
		if err := r.WriteRef(head.Ref, hash); err != nil {
			return err
		}
	} else {
		if err := os.WriteFile(r.HeadFile(), []byte(hash+"\n"), 0o644); err != nil {
			return err
		}
	}
	op := "commit"
	if *amend {
		op = "commit (amend)"
	}
	r.LogRef("HEAD", prevHead, hash, op, firstLine(*msg))
	if err := r.WriteIndex(&Index{Entries: current}); err != nil {
		return err
	}
	short := hash[:12]
	fmt.Printf("[%s %s] %s\n", branchOrDetached(r), short, firstLine(*msg))
	fmt.Printf("change-id: %s\n", changeID)
	return nil
}

func cmdLog(args []string) error {
	fs := flag.NewFlagSet("log", flag.ExitOnError)
	max := fs.Int("n", 0, "maximum entries to show (0 = all)")
	fs.Parse(args)
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	hash, err := r.ResolveHead()
	if err != nil {
		return err
	}
	if hash == "" {
		fmt.Println("No commits yet.")
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
		fmt.Printf("date   %s\n", c.AuthorAt.Format(time.RFC3339))
		fmt.Println()
		for _, line := range splitLines(c.Message) {
			fmt.Printf("    %s\n", line)
		}
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

func branchOrDetached(r *Repo) string {
	b, err := r.CurrentBranch()
	if err != nil {
		return "detached"
	}
	return b
}

func firstLine(s string) string {
	for i, c := range s {
		if c == '\n' {
			return s[:i]
		}
	}
	return s
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out
}
