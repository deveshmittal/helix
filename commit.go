package main

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"os"
	"strings"
	"time"
)

type Commit struct {
	Tree     string
	Parents  []string
	Author   string
	AuthorAt time.Time
	ChangeID string
	Message  string
}

func (c *Commit) Encode() []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "tree %s\n", c.Tree)
	for _, p := range c.Parents {
		fmt.Fprintf(&b, "parent %s\n", p)
	}
	fmt.Fprintf(&b, "author %s %d\n", c.Author, c.AuthorAt.Unix())
	fmt.Fprintf(&b, "change-id %s\n", c.ChangeID)
	b.WriteString("\n")
	b.WriteString(c.Message)
	if !strings.HasSuffix(c.Message, "\n") {
		b.WriteString("\n")
	}
	return []byte(b.String())
}

func DecodeCommit(body []byte) (*Commit, error) {
	c := &Commit{}
	s := string(body)
	parts := strings.SplitN(s, "\n\n", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("malformed commit: no message separator")
	}
	c.Message = parts[1]
	for _, line := range strings.Split(parts[0], "\n") {
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, " ")
		if !ok {
			return nil, fmt.Errorf("malformed commit header: %s", line)
		}
		switch k {
		case "tree":
			c.Tree = v
		case "parent":
			c.Parents = append(c.Parents, v)
		case "author":
			// "<author> <unix-ts>"
			i := strings.LastIndex(v, " ")
			if i < 0 {
				c.Author = v
			} else {
				c.Author = v[:i]
				var ts int64
				fmt.Sscanf(v[i+1:], "%d", &ts)
				c.AuthorAt = time.Unix(ts, 0)
			}
		case "change-id":
			c.ChangeID = v
		}
	}
	return c, nil
}

// Generate a 26-char ULID-style identifier (random; not time-sortable for MVP).
func NewChangeID() string {
	var b [16]byte
	rand.Read(b[:])
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	return "cs-" + strings.ToLower(enc.EncodeToString(b[:]))[:10]
}

func defaultAuthor() string {
	if a := os.Getenv("HELIX_AUTHOR"); a != "" {
		return a
	}
	name := os.Getenv("USER")
	if name == "" {
		name = "anonymous"
	}
	return fmt.Sprintf("%s <%s@local>", name, name)
}
