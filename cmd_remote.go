package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Local-filesystem remote transport.
// A "remote" is a path on disk pointing at another helix repo's root.
// This is the file:// transport — the same building block git uses.
// The design's gRPC transport (DESIGN.md §3.8) replaces this; the semantics
// here mirror what the network protocol needs to do.

func cmdClone(args []string) error {
	fs := flag.NewFlagSet("clone", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: helix clone <source-path> [dest]")
	}
	src := stripFileScheme(fs.Arg(0))
	dest := filepath.Base(strings.TrimSuffix(src, "/"))
	if fs.NArg() == 2 {
		dest = fs.Arg(1)
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("destination already exists: %s", dest)
	}
	srcRepo, err := openRepoAt(src)
	if err != nil {
		return fmt.Errorf("source: %w", err)
	}
	dstRepo, err := InitRepo(dest)
	if err != nil {
		return err
	}
	// Copy all objects.
	n, err := copyAllObjects(srcRepo, dstRepo)
	if err != nil {
		return err
	}
	// Copy refs/branches/* into refs/remotes/origin/* AND mirror branches locally.
	branches, err := srcRepo.ListBranches()
	if err != nil {
		return err
	}
	for _, b := range branches {
		hash, err := srcRepo.ReadRef("branches/" + b)
		if err != nil || hash == "" {
			continue
		}
		if err := dstRepo.WriteRef("remotes/origin/"+b, hash); err != nil {
			return err
		}
		if err := dstRepo.WriteRef("branches/"+b, hash); err != nil {
			return err
		}
	}
	// Tags
	tags, _ := srcRepo.ListTags()
	for _, t := range tags {
		if h, err := srcRepo.ReadRef("tags/" + t); err == nil && h != "" {
			dstRepo.WriteRef("tags/"+t, h)
		}
	}
	// Set HEAD to the source's HEAD branch if available, else "main".
	head, _ := srcRepo.ReadHead()
	if head.Symbolic {
		os.WriteFile(dstRepo.HeadFile(), []byte("ref: "+head.Ref+"\n"), 0o644)
	}
	// Remember the remote.
	rpath := filepath.Join(dstRepo.HelixDir, "remotes")
	os.WriteFile(rpath, []byte("origin "+absOrSelf(src)+"\n"), 0o644)
	// Materialize working tree from HEAD.
	if h, _ := dstRepo.ResolveHead(); h != "" {
		if err := dstRepo.CheckoutCommit(h); err != nil {
			return err
		}
	}
	fmt.Printf("Cloned %d objects, %d branches into %s\n", n, len(branches), dest)
	return nil
}

func cmdFetch(args []string) error {
	fs := flag.NewFlagSet("fetch", flag.ExitOnError)
	fs.Parse(args)
	remote := "origin"
	if fs.NArg() >= 1 {
		remote = fs.Arg(0)
	}
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	url, err := lookupRemote(r, remote)
	if err != nil {
		return err
	}
	srcRepo, err := openRepoAt(stripFileScheme(url))
	if err != nil {
		return err
	}
	n, err := copyAllObjects(srcRepo, r)
	if err != nil {
		return err
	}
	branches, err := srcRepo.ListBranches()
	if err != nil {
		return err
	}
	updated := 0
	for _, b := range branches {
		hash, err := srcRepo.ReadRef("branches/" + b)
		if err != nil || hash == "" {
			continue
		}
		if err := r.WriteRef("remotes/"+remote+"/"+b, hash); err != nil {
			return err
		}
		updated++
	}
	fmt.Printf("Fetched %d objects, %d remote branches updated\n", n, updated)
	return nil
}

func cmdPush(args []string) error {
	fs := flag.NewFlagSet("push", flag.ExitOnError)
	fs.Parse(args)
	remote := "origin"
	branch := ""
	if fs.NArg() >= 1 {
		remote = fs.Arg(0)
	}
	if fs.NArg() >= 2 {
		branch = fs.Arg(1)
	}
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	if branch == "" {
		branch, err = r.CurrentBranch()
		if err != nil {
			return err
		}
	}
	url, err := lookupRemote(r, remote)
	if err != nil {
		return err
	}
	dstRepo, err := openRepoAt(stripFileScheme(url))
	if err != nil {
		return err
	}
	localHash, err := r.ReadRef("branches/" + branch)
	if err != nil || localHash == "" {
		return fmt.Errorf("local branch %s has no commits", branch)
	}
	remoteHash, _ := dstRepo.ReadRef("branches/" + branch)
	if remoteHash != "" && !isAncestor(r, remoteHash, localHash) {
		return fmt.Errorf("non-fast-forward push refused; pull and merge first")
	}
	n, err := copyAllObjects(r, dstRepo)
	if err != nil {
		return err
	}
	if err := dstRepo.WriteRef("branches/"+branch, localHash); err != nil {
		return err
	}
	fmt.Printf("Pushed %d objects, %s -> %s/%s (%s)\n", n, branch, remote, branch, localHash[:12])
	return nil
}

func cmdPull(args []string) error {
	if err := cmdFetch(args); err != nil {
		return err
	}
	r, err := FindRepo(".")
	if err != nil {
		return err
	}
	branch, err := r.CurrentBranch()
	if err != nil {
		return err
	}
	remote := "origin"
	if len(args) >= 1 {
		remote = args[0]
	}
	remoteHash, _ := r.ReadRef("remotes/" + remote + "/" + branch)
	if remoteHash == "" {
		return fmt.Errorf("no fetched ref for %s/%s", remote, branch)
	}
	headHash, _ := r.ResolveHead()
	if headHash == "" {
		// no local commits — adopt
		if err := r.WriteRef("branches/"+branch, remoteHash); err != nil {
			return err
		}
		return r.CheckoutCommit(remoteHash)
	}
	if headHash == remoteHash {
		fmt.Println("Already up to date.")
		return nil
	}
	if isAncestor(r, headHash, remoteHash) {
		dirty, _ := r.IsDirty(headHash)
		if dirty {
			return fmt.Errorf("cannot fast-forward: working tree dirty; commit or stash first")
		}
		if err := r.WriteRef("branches/"+branch, remoteHash); err != nil {
			return err
		}
		if err := r.CheckoutCommit(remoteHash); err != nil {
			return err
		}
		fmt.Printf("Fast-forwarded %s to %s\n", branch, remoteHash[:12])
		return nil
	}
	return fmt.Errorf("local and remote diverged; run 'helix merge %s/%s' to merge", remote, branch)
}

// --- helpers ---

func stripFileScheme(s string) string {
	return strings.TrimPrefix(s, "file://")
}

func absOrSelf(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

func openRepoAt(path string) (*Repo, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	hx := filepath.Join(abs, helixDir)
	if fi, err := os.Stat(hx); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("not a helix repository: %s", abs)
	}
	return &Repo{Root: abs, HelixDir: hx}, nil
}

func lookupRemote(r *Repo, name string) (string, error) {
	rpath := filepath.Join(r.HelixDir, "remotes")
	data, err := os.ReadFile(rpath)
	if err != nil {
		return "", fmt.Errorf("no remotes configured")
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		n, url, ok := strings.Cut(line, " ")
		if ok && n == name {
			return url, nil
		}
	}
	return "", fmt.Errorf("unknown remote: %s", name)
}

func copyAllObjects(src, dst *Repo) (int, error) {
	srcDir := src.ObjectsDir()
	count := 0
	err := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst.ObjectsDir(), rel)
		if _, err := os.Stat(dstPath); err == nil {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(dstPath)
		if err != nil {
			return err
		}
		defer out.Close()
		if _, err := io.Copy(out, in); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}
