package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const helixDir = ".helix"

type Repo struct {
	Root    string
	HelixDir string
}

func FindRepo(start string) (*Repo, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	for {
		hx := filepath.Join(dir, helixDir)
		if fi, err := os.Stat(hx); err == nil && fi.IsDir() {
			return &Repo{Root: dir, HelixDir: hx}, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, errors.New("not in a helix repository (run 'helix init')")
		}
		dir = parent
	}
}

func InitRepo(at string) (*Repo, error) {
	root, err := filepath.Abs(at)
	if err != nil {
		return nil, err
	}
	hx := filepath.Join(root, helixDir)
	if _, err := os.Stat(hx); err == nil {
		return nil, fmt.Errorf("already a helix repository: %s", hx)
	}
	dirs := []string{
		hx,
		filepath.Join(hx, "objects"),
		filepath.Join(hx, "refs"),
		filepath.Join(hx, "refs", "branches"),
		filepath.Join(hx, "ops"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	if err := os.WriteFile(filepath.Join(hx, "HEAD"), []byte("ref: branches/main\n"), 0o644); err != nil {
		return nil, err
	}
	cfg := "[core]\nversion = 1\n"
	if err := os.WriteFile(filepath.Join(hx, "config"), []byte(cfg), 0o644); err != nil {
		return nil, err
	}
	return &Repo{Root: root, HelixDir: hx}, nil
}

func (r *Repo) ObjectsDir() string { return filepath.Join(r.HelixDir, "objects") }
func (r *Repo) RefsDir() string    { return filepath.Join(r.HelixDir, "refs") }
func (r *Repo) HeadFile() string   { return filepath.Join(r.HelixDir, "HEAD") }
func (r *Repo) IndexFile() string  { return filepath.Join(r.HelixDir, "index") }
