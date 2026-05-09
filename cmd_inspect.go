package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

func cmdShow(args []string) error {
	fs := flag.NewFlagSet("show", flag.ExitOnError)
	fs.Parse(args)
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	target := "HEAD"
	if fs.NArg() == 1 {
		target = fs.Arg(0)
	}
	hash, err := r.ResolveAny(target)
	if err != nil {
		return err
	}
	obj, err := r.ReadObject(hash)
	if err != nil {
		return err
	}
	if obj.Kind != KindCommit {
		fmt.Printf("%s %s\n", obj.Kind, hash)
		os.Stdout.Write(obj.Body)
		return nil
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

	// Diff against first parent (or empty tree).
	var parentEntries []IndexEntry
	if len(c.Parents) > 0 {
		pobj, err := r.ReadObject(c.Parents[0])
		if err != nil {
			return err
		}
		pc, err := DecodeCommit(pobj.Body)
		if err != nil {
			return err
		}
		parentEntries, err = r.FlattenTree(pc.Tree, "")
		if err != nil {
			return err
		}
	}
	cur, err := r.FlattenTree(c.Tree, "")
	if err != nil {
		return err
	}
	return printDiff(r, parentEntries, cur)
}

func cmdDiff(args []string) error {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	cached := fs.Bool("cached", false, "no-op (helix has no staging)")
	_ = cached
	fs.Parse(args)
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	var headEntries []IndexEntry
	if h, _ := r.ResolveHead(); h != "" {
		obj, err := r.ReadObject(h)
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
	cur, err := r.ScanWorkingTree()
	if err != nil {
		return err
	}
	return printDiff(r, headEntries, cur)
}

func printDiff(r *Repo, oldList, newList []IndexEntry) error {
	oldMap := map[string]IndexEntry{}
	for _, e := range oldList {
		oldMap[e.Path] = e
	}
	newMap := map[string]IndexEntry{}
	for _, e := range newList {
		newMap[e.Path] = e
	}
	all := map[string]bool{}
	for k := range oldMap {
		all[k] = true
	}
	for k := range newMap {
		all[k] = true
	}
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, p := range keys {
		o, hasOld := oldMap[p]
		n, hasNew := newMap[p]
		if hasOld && hasNew && o.Hash == n.Hash {
			continue
		}
		var oldText, newText string
		if hasOld {
			obj, err := r.ReadObject(o.Hash)
			if err != nil {
				return err
			}
			oldText = string(obj.Body)
		}
		if hasNew {
			obj, err := r.ReadObject(n.Hash)
			if err != nil {
				return err
			}
			newText = string(obj.Body)
		}
		fmt.Printf("--- a/%s\n", p)
		fmt.Printf("+++ b/%s\n", p)
		printUnifiedLines(oldText, newText)
	}
	return nil
}

// printUnifiedLines is a simple LCS-based diff. Not Git-format-precise but readable.
func printUnifiedLines(a, b string) {
	al := splitKeepNewline(a)
	bl := splitKeepNewline(b)
	// LCS
	n, m := len(al), len(bl)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if al[i] == bl[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	i, j := 0, 0
	for i < n && j < m {
		if al[i] == bl[j] {
			fmt.Printf(" %s", ensureNL(al[i]))
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			fmt.Printf("-%s", ensureNL(al[i]))
			i++
		} else {
			fmt.Printf("+%s", ensureNL(bl[j]))
			j++
		}
	}
	for ; i < n; i++ {
		fmt.Printf("-%s", ensureNL(al[i]))
	}
	for ; j < m; j++ {
		fmt.Printf("+%s", ensureNL(bl[j]))
	}
}

func splitKeepNewline(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func ensureNL(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}
