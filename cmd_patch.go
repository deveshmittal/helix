package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// format-patch: write one mbox-style patch file per commit in a range.
//
//	helix format-patch <since>             # commits in <since>..HEAD
//	helix format-patch -1 [<commit>]       # just one commit
func cmdFormatPatch(args []string) error {
	fs := flag.NewFlagSet("format-patch", flag.ExitOnError)
	one := fs.Bool("1", false, "format only one commit")
	out := fs.String("o", ".", "output directory")
	fs.Parse(args)
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}

	var commits []string
	if *one {
		ref := "HEAD"
		if fs.NArg() == 1 {
			ref = fs.Arg(0)
		}
		h, err := r.ResolveAny(ref)
		if err != nil {
			return err
		}
		commits = []string{h}
	} else {
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: helix format-patch <since>")
		}
		since, err := r.ResolveAny(fs.Arg(0))
		if err != nil {
			return err
		}
		head, _ := r.ResolveHead()
		commits, err = commitsBetween(r, since, head)
		if err != nil {
			return err
		}
	}

	for i, h := range commits {
		obj, err := r.ReadObject(h)
		if err != nil {
			return err
		}
		c, err := DecodeCommit(obj.Body)
		if err != nil {
			return err
		}
		subject := firstLine(c.Message)
		fname := fmt.Sprintf("%04d-%s.patch", i+1, sanitize(subject))
		full := filepath.Join(*out, fname)
		f, err := os.Create(full)
		if err != nil {
			return err
		}
		fmt.Fprintf(f, "From %s Mon Sep 17 00:00:00 2001\n", h)
		fmt.Fprintf(f, "From: %s\n", c.Author)
		fmt.Fprintf(f, "Date: %s\n", c.AuthorAt.Format(time.RFC1123Z))
		fmt.Fprintf(f, "Subject: [PATCH %d/%d] %s\n", i+1, len(commits), subject)
		if c.ChangeID != "" {
			fmt.Fprintf(f, "Change-Id: %s\n", c.ChangeID)
		}
		fmt.Fprintf(f, "\n")
		// Body of message after the first line.
		body := c.Message
		if i := strings.Index(body, "\n"); i >= 0 {
			body = body[i+1:]
		} else {
			body = ""
		}
		if body != "" {
			fmt.Fprintf(f, "%s\n", strings.TrimSpace(body))
		}
		fmt.Fprintf(f, "---\n")

		// Diff vs first parent.
		var parentEntries []IndexEntry
		if len(c.Parents) > 0 {
			parentEntries, _ = flattenCommitEntries(r, c.Parents[0])
		}
		curEntries, _ := r.FlattenTree(c.Tree, "")
		writePatchDiff(f, r, parentEntries, curEntries)
		fmt.Fprintf(f, "-- \nhelix %s\n", version)
		f.Close()
		fmt.Println(full)
	}
	return nil
}

func sanitize(s string) string {
	out := make([]byte, 0, len(s))
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			out = append(out, byte(c))
		case c == ' ' || c == '_' || c == '-':
			out = append(out, '-')
		}
	}
	if len(out) > 50 {
		out = out[:50]
	}
	return string(out)
}

func writePatchDiff(w *os.File, r *Repo, oldList, newList []IndexEntry) {
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
	var keys []string
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
		fmt.Fprintf(w, "diff --helix a/%s b/%s\n", p, p)
		fmt.Fprintf(w, "--- a/%s\n", p)
		fmt.Fprintf(w, "+++ b/%s\n", p)
		var oldText, newText string
		if hasOld {
			obj, _ := r.ReadObject(o.Hash)
			oldText = string(obj.Body)
		}
		if hasNew {
			obj, _ := r.ReadObject(n.Hash)
			newText = string(obj.Body)
		}
		writeUnified(w, oldText, newText)
	}
}

// writeUnified writes a single hunk per file: @@ -1,n +1,m @@ followed by full content.
// Real format-patch produces multiple hunks; this MVP gives one hunk per file.
func writeUnified(w *os.File, a, b string) {
	al := splitKeepNewline(a)
	bl := splitKeepNewline(b)
	fmt.Fprintf(w, "@@ -1,%d +1,%d @@\n", len(al), len(bl))
	// LCS-driven walk.
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
			fmt.Fprintf(w, " %s", ensureNL(al[i]))
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			fmt.Fprintf(w, "-%s", ensureNL(al[i]))
			i++
		} else {
			fmt.Fprintf(w, "+%s", ensureNL(bl[j]))
			j++
		}
	}
	for ; i < n; i++ {
		fmt.Fprintf(w, "-%s", ensureNL(al[i]))
	}
	for ; j < m; j++ {
		fmt.Fprintf(w, "+%s", ensureNL(bl[j]))
	}
}

// am: apply a series of mbox-format .patch files (typically produced by format-patch).
//
//	helix am <patch-file>...
func cmdAm(args []string) error {
	fs := flag.NewFlagSet("am", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: helix am <patch-file>...")
	}
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	for i := 0; i < fs.NArg(); i++ {
		path := fs.Arg(i)
		if err := applyOnePatch(r, path); err != nil {
			return fmt.Errorf("am: %s: %w", path, err)
		}
		fmt.Printf("Applied %s\n", path)
	}
	return nil
}

func applyOnePatch(r *Repo, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)

	headers := map[string]string{}
	var bodyLines []string
	var diffLines []string
	state := 0 // 0=headers, 1=body, 2=diff
	for sc.Scan() {
		line := sc.Text()
		switch state {
		case 0:
			if line == "" {
				state = 1
				continue
			}
			if k, v, ok := strings.Cut(line, ": "); ok {
				headers[k] = v
			}
		case 1:
			if strings.TrimSpace(line) == "---" {
				state = 2
				continue
			}
			bodyLines = append(bodyLines, line)
		case 2:
			diffLines = append(diffLines, line)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}

	// Apply diff to working tree.
	if err := applyDiffLines(r, diffLines); err != nil {
		return err
	}

	// Build commit from headers.
	subject := strings.TrimPrefix(headers["Subject"], "[PATCH] ")
	// Strip "[PATCH n/m] " if present.
	if i := strings.Index(subject, "] "); i >= 0 && strings.HasPrefix(subject, "[") {
		subject = subject[i+2:]
	}
	body := strings.TrimSpace(strings.Join(bodyLines, "\n"))
	msg := subject
	if body != "" {
		msg = subject + "\n\n" + body
	}
	current, _ := r.ScanWorkingTree()
	tree, err := r.BuildTree(current)
	if err != nil {
		return err
	}
	headHash, _ := r.ResolveHead()
	parents := []string{}
	if headHash != "" {
		parents = []string{headHash}
	}
	c := &Commit{
		Tree: tree, Parents: parents,
		Author:   headers["From"],
		AuthorAt: time.Now(),
		ChangeID: orDefault(headers["Change-Id"], NewChangeID()),
		Message:  msg + "\n",
	}
	obj := &Object{Kind: KindCommit, Body: c.Encode()}
	newHash, err := r.WriteObject(obj)
	if err != nil {
		return err
	}
	return updateHeadRef(r, newHash)
}

func orDefault(s, dflt string) string {
	if s == "" {
		return dflt
	}
	return s
}

// apply: apply a unified diff to the working tree (no commit).
func cmdApply(args []string) error {
	fs := flag.NewFlagSet("apply", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: helix apply <diff-file>")
	}
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	return applyDiffLines(r, strings.Split(string(data), "\n"))
}

// applyDiffLines parses a sequence of file headers + hunks and applies them.
// Supports the "single-hunk-per-file" output from our writeUnified.
// Hunks with @@ -A,B +C,D @@ where the counts match the pre/post line counts.
func applyDiffLines(r *Repo, lines []string) error {
	type hunk struct {
		path    string
		oldText string
		newText string
		isNew   bool
	}
	var hunks []hunk
	i := 0
	for i < len(lines) {
		l := lines[i]
		if strings.HasPrefix(l, "diff ") {
			// Read --- and +++ headers.
			i++
			if i >= len(lines) {
				break
			}
			oldHdr := lines[i]
			i++
			if i >= len(lines) {
				break
			}
			newHdr := lines[i]
			i++
			path := strings.TrimPrefix(newHdr, "+++ b/")
			oldPath := strings.TrimPrefix(oldHdr, "--- a/")
			isNew := strings.Contains(oldHdr, "/dev/null")
			_ = oldPath

			// Now scan hunks for this file.
			var oldB, newB strings.Builder
			for i < len(lines) && !strings.HasPrefix(lines[i], "diff ") && !strings.HasPrefix(lines[i], "-- ") {
				ln := lines[i]
				i++
				if strings.HasPrefix(ln, "@@") {
					continue
				}
				if strings.HasPrefix(ln, " ") {
					oldB.WriteString(ln[1:] + "\n")
					newB.WriteString(ln[1:] + "\n")
				} else if strings.HasPrefix(ln, "-") {
					oldB.WriteString(ln[1:] + "\n")
				} else if strings.HasPrefix(ln, "+") {
					newB.WriteString(ln[1:] + "\n")
				}
			}
			hunks = append(hunks, hunk{path: path, oldText: oldB.String(), newText: newB.String(), isNew: isNew})
			continue
		}
		i++
	}

	for _, h := range hunks {
		full := filepath.Join(r.Root, h.path)
		if h.newText == "" {
			os.Remove(full)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		// Verify oldText matches current file (best-effort).
		if !h.isNew {
			cur, _ := os.ReadFile(full)
			if string(cur) != "" && string(cur) != h.oldText {
				// Continue anyway; this isn't a perfect 3-way patcher.
			}
		}
		if err := os.WriteFile(full, []byte(h.newText), 0o644); err != nil {
			return err
		}
	}
	_ = strconv.Atoi // unused-import guard if any path strips
	return nil
}
