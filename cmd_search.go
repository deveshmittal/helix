package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// bisect: persistent state in .helix/BISECT/.
//   helix bisect start          → begin a session
//   helix bisect bad             → mark current commit bad
//   helix bisect good <ref>      → mark <ref> good
//   helix bisect reset           → end and restore HEAD
//
// After each good/bad it picks the midpoint and checks it out.
func cmdBisect(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: helix bisect <start|good|bad|reset|status>")
	}
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	dir := filepath.Join(r.HelixDir, "BISECT")
	switch args[0] {
	case "start":
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		// Save current HEAD state so 'reset' can restore it.
		head, _ := r.ReadHead()
		var saved string
		if head.Symbolic {
			saved = "ref: " + head.Ref
		} else {
			saved = head.Hash
		}
		os.WriteFile(filepath.Join(dir, "ORIG_HEAD"), []byte(saved), 0o644)
		fmt.Println("Bisect started. Mark commits with 'helix bisect good' / 'bad'.")
		return nil
	case "reset":
		orig, err := os.ReadFile(filepath.Join(dir, "ORIG_HEAD"))
		if err == nil {
			s := strings.TrimSpace(string(orig))
			if strings.HasPrefix(s, "ref: ") {
				ref := strings.TrimPrefix(s, "ref: ")
				if h, _ := r.ReadRef(ref); h != "" {
					r.CheckoutCommit(h)
				}
				os.WriteFile(r.HeadFile(), []byte("ref: "+ref+"\n"), 0o644)
			} else {
				r.CheckoutCommit(s)
				os.WriteFile(r.HeadFile(), []byte(s+"\n"), 0o644)
			}
		}
		os.RemoveAll(dir)
		fmt.Println("Bisect reset.")
		return nil
	case "good":
		ref := "HEAD"
		if len(args) >= 2 {
			ref = args[1]
		}
		hash, err := r.ResolveAny(ref)
		if err != nil {
			return err
		}
		if err := appendLine(filepath.Join(dir, "good"), hash); err != nil {
			return err
		}
		return bisectStep(r, dir)
	case "bad":
		ref := "HEAD"
		if len(args) >= 2 {
			ref = args[1]
		}
		hash, err := r.ResolveAny(ref)
		if err != nil {
			return err
		}
		if err := appendLine(filepath.Join(dir, "bad"), hash); err != nil {
			return err
		}
		return bisectStep(r, dir)
	case "status":
		good, _ := os.ReadFile(filepath.Join(dir, "good"))
		bad, _ := os.ReadFile(filepath.Join(dir, "bad"))
		fmt.Printf("good:\n%s\nbad:\n%s\n", good, bad)
		return nil
	}
	return fmt.Errorf("unknown bisect op: %s", args[0])
}

func appendLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line + "\n")
	return err
}

func readLines(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(string(data), "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func bisectStep(r *Repo, dir string) error {
	good := readLines(filepath.Join(dir, "good"))
	bad := readLines(filepath.Join(dir, "bad"))
	if len(good) == 0 || len(bad) == 0 {
		fmt.Println("Need at least one good and one bad commit to begin bisecting.")
		return nil
	}
	// Distances from any bad ref (BFS over parents).
	badDist := map[string]int{}
	for _, b := range bad {
		bfsParents(r, b, badDist)
	}
	// Exclude good ancestors (they're "before" the regression and can't be the first bad commit).
	goodAnc := map[string]bool{}
	for _, g := range good {
		walkAncestors(r, g, goodAnc)
	}
	type cand struct {
		hash string
		dist int
	}
	var cands []cand
	for h, d := range badDist {
		if goodAnc[h] {
			continue
		}
		// Don't pick bad refs themselves as a candidate to checkout.
		isBadRef := false
		for _, b := range bad {
			if b == h {
				isBadRef = true
				break
			}
		}
		if isBadRef {
			continue
		}
		cands = append(cands, cand{h, d})
	}
	if len(cands) == 0 {
		fmt.Println("Bisect complete: a bad ref is the first bad commit.")
		return nil
	}
	// Pick the candidate with median distance — splits the search space in half.
	for i := 1; i < len(cands); i++ {
		for j := i; j > 0 && cands[j-1].dist > cands[j].dist; j-- {
			cands[j-1], cands[j] = cands[j], cands[j-1]
		}
	}
	mid := cands[len(cands)/2].hash
	if err := r.CheckoutCommit(mid); err != nil {
		return err
	}
	// Detached HEAD-style: write the hash directly, don't move the branch.
	if err := os.WriteFile(r.HeadFile(), []byte(mid+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Printf("Bisecting: %d commit(s) remaining; checked out %s\n", len(cands), mid[:12])
	return nil
}

func bfsParents(r *Repo, start string, dist map[string]int) {
	type node struct {
		h string
		d int
	}
	queue := []node{{start, 0}}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if d, ok := dist[n.h]; ok && d <= n.d {
			continue
		}
		dist[n.h] = n.d
		obj, err := r.ReadObject(n.h)
		if err != nil {
			continue
		}
		c, err := DecodeCommit(obj.Body)
		if err != nil {
			continue
		}
		for _, p := range c.Parents {
			queue = append(queue, node{p, n.d + 1})
		}
	}
}

func walkAncestors(r *Repo, hash string, out map[string]bool) {
	stack := []string{hash}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if out[cur] {
			continue
		}
		out[cur] = true
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
}

func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j-1] > ss[j]; j-- {
			ss[j-1], ss[j] = ss[j], ss[j-1]
		}
	}
}

// grep: regex search across the working tree.
func cmdGrep(args []string) error {
	fs := flag.NewFlagSet("grep", flag.ExitOnError)
	ignoreCase := fs.Bool("i", false, "case-insensitive")
	fs.Parse(args)
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: helix grep [-i] <pattern> [path...]")
	}
	pat := fs.Arg(0)
	if *ignoreCase {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return fmt.Errorf("bad pattern: %w", err)
	}
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	roots := []string{r.Root}
	if fs.NArg() > 1 {
		roots = nil
		for i := 1; i < fs.NArg(); i++ {
			roots = append(roots, filepath.Join(r.Root, fs.Arg(i)))
		}
	}
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d osDirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if d.Name() == helixDir || d.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(r.Root, path)
			lines := strings.Split(string(data), "\n")
			for i, l := range lines {
				if re.MatchString(l) {
					fmt.Printf("%s:%d:%s\n", rel, i+1, l)
				}
			}
			return nil
		})
	}
	return nil
}

// Use io/fs's DirEntry alias to keep signatures readable.
type osDirEntry = fs.DirEntry

// notes: a flat key/value store under .helix/notes/<commit-hash>.
func cmdNotes(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: helix notes <add|show|remove|list> [args]")
	}
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	dir := filepath.Join(r.HelixDir, "notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	switch args[0] {
	case "add":
		if len(args) < 3 {
			return fmt.Errorf("usage: helix notes add <commit> <text>")
		}
		hash, err := r.ResolveAny(args[1])
		if err != nil {
			return err
		}
		text := strings.Join(args[2:], " ")
		return os.WriteFile(filepath.Join(dir, hash), []byte(text+"\n"), 0o644)
	case "show":
		ref := "HEAD"
		if len(args) >= 2 {
			ref = args[1]
		}
		hash, err := r.ResolveAny(ref)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(dir, hash))
		if err != nil {
			return fmt.Errorf("no note for %s", hash[:12])
		}
		fmt.Print(string(data))
		return nil
	case "remove":
		if len(args) < 2 {
			return fmt.Errorf("usage: helix notes remove <commit>")
		}
		hash, err := r.ResolveAny(args[1])
		if err != nil {
			return err
		}
		return os.Remove(filepath.Join(dir, hash))
	case "list":
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if !e.IsDir() {
				fmt.Println(e.Name())
			}
		}
		return nil
	}
	return fmt.Errorf("unknown notes op: %s", args[0])
}

// reflog: append-only log per ref.
// We hook into commit, switch, reset, and other ref-mutating ops via Repo.LogRef().
// Format per line: <old>\t<new>\t<op>\t<unix-ts>\t<message>
func cmdReflog(args []string) error {
	fs := flag.NewFlagSet("reflog", flag.ExitOnError)
	fs.Parse(args)
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	ref := "HEAD"
	if fs.NArg() >= 1 {
		ref = fs.Arg(0)
	}
	path := filepath.Join(r.HelixDir, "logs", ref)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("(no reflog entries)")
			return nil
		}
		return err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	// Print newest-first.
	for i := len(lines) - 1; i >= 0; i-- {
		l := lines[i]
		if l == "" {
			continue
		}
		parts := strings.SplitN(l, "\t", 5)
		if len(parts) != 5 {
			fmt.Println(l)
			continue
		}
		old, neu, op, tsStr, msg := parts[0], parts[1], parts[2], parts[3], parts[4]
		var ts int64
		fmt.Sscanf(tsStr, "%d", &ts)
		when := time.Unix(ts, 0).Format("2006-01-02 15:04:05")
		short := neu
		if len(short) > 12 {
			short = short[:12]
		}
		_ = old
		fmt.Printf("%s %s %s: %s\n", short, when, op, msg)
	}
	return nil
}

// LogRef appends a reflog entry. Empty hashes are written as 64 zeros so
// every line has the same column count (avoids parsing surprises on leading-tab fields).
func (r *Repo) LogRef(ref, oldHash, newHash, op, msg string) error {
	dir := filepath.Join(r.HelixDir, "logs", filepath.Dir(ref))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(r.HelixDir, "logs", ref)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if oldHash == "" {
		oldHash = strings.Repeat("0", 64)
	}
	if newHash == "" {
		newHash = strings.Repeat("0", 64)
	}
	line := fmt.Sprintf("%s\t%s\t%s\t%d\t%s\n", oldHash, newHash, op, time.Now().Unix(), msg)
	_, err = f.WriteString(line)
	return err
}
