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
