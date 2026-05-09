package main

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// IndexEntry tracks a path and the hash of its content as last seen.
// Helix's "working-copy-as-commit" model means the index is mostly a cache
// of what the working tree currently hashes to — we recompute eagerly on commit.
type IndexEntry struct {
	Path string
	Mode string
	Hash string
}

type Index struct {
	Entries []IndexEntry
}

func (r *Repo) ReadIndex() (*Index, error) {
	data, err := os.ReadFile(r.IndexFile())
	if err != nil {
		if os.IsNotExist(err) {
			return &Index{}, nil
		}
		return nil, err
	}
	idx := &Index{}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("malformed index line: %s", line)
		}
		idx.Entries = append(idx.Entries, IndexEntry{
			Mode: parts[0], Hash: parts[1], Path: parts[2],
		})
	}
	return idx, sc.Err()
}

func (r *Repo) WriteIndex(idx *Index) error {
	sort.Slice(idx.Entries, func(i, j int) bool {
		return idx.Entries[i].Path < idx.Entries[j].Path
	})
	var b strings.Builder
	for _, e := range idx.Entries {
		fmt.Fprintf(&b, "%s\t%s\t%s\n", e.Mode, e.Hash, e.Path)
	}
	return os.WriteFile(r.IndexFile(), []byte(b.String()), 0o644)
}

// ScanWorkingTree walks the repo root and returns hashed entries for each file,
// excluding .helix and respecting a minimal ignore set.
func (r *Repo) ScanWorkingTree() ([]IndexEntry, error) {
	var out []IndexEntry
	rootLen := len(r.Root) + 1
	err := filepath.WalkDir(r.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path == r.Root {
				return nil
			}
			if name == helixDir || name == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip symlinks for the MVP; they need their own handling.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		rel := path[rootLen:]
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		obj := &Object{Kind: KindBlob, Body: data}
		hash, err := r.WriteObject(obj)
		if err != nil {
			return err
		}
		mode := "100644"
		if fi, err := d.Info(); err == nil && fi.Mode()&0o111 != 0 {
			mode = "100755"
		}
		out = append(out, IndexEntry{Path: rel, Mode: mode, Hash: hash})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// BuildTree converts a flat list of (path, mode, hash) into a tree object graph,
// writes all the trees, and returns the root tree's hash.
func (r *Repo) BuildTree(entries []IndexEntry) (string, error) {
	type node struct {
		children map[string]*node
		entry    *IndexEntry // non-nil for leaves
	}
	root := &node{children: map[string]*node{}}
	for i := range entries {
		e := &entries[i]
		parts := strings.Split(e.Path, "/")
		cur := root
		for j, p := range parts {
			if j == len(parts)-1 {
				cur.children[p] = &node{entry: e}
			} else {
				if _, ok := cur.children[p]; !ok {
					cur.children[p] = &node{children: map[string]*node{}}
				}
				cur = cur.children[p]
			}
		}
	}
	var build func(n *node) (string, error)
	build = func(n *node) (string, error) {
		var te []TreeEntry
		names := make([]string, 0, len(n.children))
		for name := range n.children {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			c := n.children[name]
			if c.entry != nil {
				te = append(te, TreeEntry{Mode: c.entry.Mode, Name: name, Hash: c.entry.Hash})
			} else {
				h, err := build(c)
				if err != nil {
					return "", err
				}
				te = append(te, TreeEntry{Mode: "40000", Name: name, Hash: h})
			}
		}
		obj := &Object{Kind: KindTree, Body: EncodeTree(te)}
		return r.WriteObject(obj)
	}
	return build(root)
}

// FlattenTree walks a tree object recursively and returns leaf entries.
func (r *Repo) FlattenTree(treeHash, prefix string) ([]IndexEntry, error) {
	if treeHash == "" {
		return nil, nil
	}
	obj, err := r.ReadObject(treeHash)
	if err != nil {
		return nil, err
	}
	if obj.Kind != KindTree {
		return nil, fmt.Errorf("expected tree, got %s", obj.Kind)
	}
	te, err := DecodeTree(obj.Body)
	if err != nil {
		return nil, err
	}
	var out []IndexEntry
	for _, e := range te {
		path := e.Name
		if prefix != "" {
			path = prefix + "/" + e.Name
		}
		if e.Mode == "40000" {
			sub, err := r.FlattenTree(e.Hash, path)
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
		} else {
			out = append(out, IndexEntry{Path: path, Mode: e.Mode, Hash: e.Hash})
		}
	}
	return out, nil
}
