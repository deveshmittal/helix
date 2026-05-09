package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HEAD is either "ref: branches/<name>\n" (symbolic) or a raw hash.

type HeadState struct {
	Symbolic bool
	Ref      string // e.g. "branches/main"
	Hash     string // populated when detached
}

func (r *Repo) ReadHead() (HeadState, error) {
	data, err := os.ReadFile(r.HeadFile())
	if err != nil {
		return HeadState{}, err
	}
	s := strings.TrimSpace(string(data))
	if strings.HasPrefix(s, "ref: ") {
		return HeadState{Symbolic: true, Ref: strings.TrimPrefix(s, "ref: ")}, nil
	}
	return HeadState{Hash: s}, nil
}

func (r *Repo) ResolveHead() (string, error) {
	h, err := r.ReadHead()
	if err != nil {
		return "", err
	}
	if !h.Symbolic {
		return h.Hash, nil
	}
	return r.ReadRef(h.Ref)
}

func (r *Repo) ReadRef(ref string) (string, error) {
	path := filepath.Join(r.RefsDir(), filepath.FromSlash(ref))
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // unborn
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (r *Repo) WriteRef(ref, hash string) error {
	path := filepath.Join(r.RefsDir(), filepath.FromSlash(ref))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(hash+"\n"), 0o644)
}

// ResolveAny tries (in order): "HEAD", branches/<x>, tags/<x>, raw hash prefix.
func (r *Repo) ResolveAny(ref string) (string, error) {
	if ref == "HEAD" || ref == "@" {
		return r.ResolveHead()
	}
	if h, err := r.ReadRef("branches/" + ref); err == nil && h != "" {
		return h, nil
	}
	if h, err := r.ReadRef("tags/" + ref); err == nil && h != "" {
		return h, nil
	}
	// Try as a hash prefix.
	if len(ref) >= 4 && len(ref) <= 64 {
		if full, err := r.resolveShortHash(ref); err == nil {
			return full, nil
		}
	}
	return "", fmt.Errorf("unknown ref: %s", ref)
}

// ListBranches returns all branch names (sorted).
func (r *Repo) ListBranches() ([]string, error) {
	dir := filepath.Join(r.RefsDir(), "branches")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// ListTags returns all tag names.
func (r *Repo) ListTags() ([]string, error) {
	dir := filepath.Join(r.RefsDir(), "tags")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

func (r *Repo) DeleteRef(ref string) error {
	path := filepath.Join(r.RefsDir(), filepath.FromSlash(ref))
	return os.Remove(path)
}

func (r *Repo) CurrentBranch() (string, error) {
	h, err := r.ReadHead()
	if err != nil {
		return "", err
	}
	if !h.Symbolic {
		return "", fmt.Errorf("HEAD is detached")
	}
	if !strings.HasPrefix(h.Ref, "branches/") {
		return "", fmt.Errorf("HEAD does not point to a branch: %s", h.Ref)
	}
	return strings.TrimPrefix(h.Ref, "branches/"), nil
}
