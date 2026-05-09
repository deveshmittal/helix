package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ObjKind string

const (
	KindBlob   ObjKind = "blob"
	KindTree   ObjKind = "tree"
	KindCommit ObjKind = "commit"
)

type Object struct {
	Kind ObjKind
	Body []byte
}

// Encoded layout: "<kind> <size>\n" + body. Hash is sha256 of the whole thing.
func (o *Object) encode() []byte {
	header := fmt.Sprintf("%s %d\n", o.Kind, len(o.Body))
	out := make([]byte, 0, len(header)+len(o.Body))
	out = append(out, header...)
	out = append(out, o.Body...)
	return out
}

func (o *Object) Hash() string {
	sum := sha256.Sum256(o.encode())
	return hex.EncodeToString(sum[:])
}

func (r *Repo) WriteObject(o *Object) (string, error) {
	hash := o.Hash()
	path := r.objectPath(hash)
	if _, err := os.Stat(path); err == nil {
		return hash, nil // already stored
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, o.encode(), 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return hash, nil
}

func (r *Repo) ReadObject(hash string) (*Object, error) {
	if len(hash) != 64 {
		full, err := r.resolveShortHash(hash)
		if err != nil {
			return nil, err
		}
		hash = full
	}
	data, err := os.ReadFile(r.objectPath(hash))
	if err != nil {
		return nil, err
	}
	nl := -1
	for i, b := range data {
		if b == '\n' {
			nl = i
			break
		}
	}
	if nl < 0 {
		return nil, errors.New("malformed object: no header")
	}
	header := string(data[:nl])
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return nil, errors.New("malformed object header")
	}
	size, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("malformed object size: %w", err)
	}
	body := data[nl+1:]
	if len(body) != size {
		return nil, fmt.Errorf("size mismatch: header %d, body %d", size, len(body))
	}
	return &Object{Kind: ObjKind(parts[0]), Body: body}, nil
}

func (r *Repo) objectPath(hash string) string {
	return filepath.Join(r.ObjectsDir(), hash[:2], hash[2:])
}

func (r *Repo) resolveShortHash(prefix string) (string, error) {
	if len(prefix) < 4 {
		return "", fmt.Errorf("hash prefix too short: %s", prefix)
	}
	if len(prefix) >= 64 {
		return prefix[:64], nil
	}
	dir := filepath.Join(r.ObjectsDir(), prefix[:2])
	rest := prefix[2:]
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("no object matching %s", prefix)
	}
	var matches []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), rest) {
			matches = append(matches, prefix[:2]+e.Name())
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no object matching %s", prefix)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous hash prefix %s (%d matches)", prefix, len(matches))
	}
	return matches[0], nil
}

// Tree object: each entry encoded as "<mode> <name>\0<32-byte-binary-hash>"
// Modes: "100644" file, "100755" exec, "40000" tree.
type TreeEntry struct {
	Mode string
	Name string
	Hash string // hex
}

func EncodeTree(entries []TreeEntry) []byte {
	out := make([]byte, 0, 64*len(entries))
	for _, e := range entries {
		out = append(out, e.Mode...)
		out = append(out, ' ')
		out = append(out, e.Name...)
		out = append(out, 0)
		raw, _ := hex.DecodeString(e.Hash)
		out = append(out, raw...)
	}
	return out
}

func DecodeTree(body []byte) ([]TreeEntry, error) {
	var entries []TreeEntry
	for i := 0; i < len(body); {
		sp := i
		for sp < len(body) && body[sp] != ' ' {
			sp++
		}
		if sp == len(body) {
			return nil, errors.New("malformed tree: no space")
		}
		mode := string(body[i:sp])
		nul := sp + 1
		for nul < len(body) && body[nul] != 0 {
			nul++
		}
		if nul == len(body) {
			return nil, errors.New("malformed tree: no nul")
		}
		name := string(body[sp+1 : nul])
		if nul+1+32 > len(body) {
			return nil, errors.New("malformed tree: short hash")
		}
		hash := hex.EncodeToString(body[nul+1 : nul+1+32])
		entries = append(entries, TreeEntry{Mode: mode, Name: name, Hash: hash})
		i = nul + 1 + 32
	}
	return entries, nil
}
