package main

import (
	"reflect"
	"testing"
)

func TestObjectHashStability(t *testing.T) {
	o := &Object{Kind: KindBlob, Body: []byte("hello\n")}
	h1 := o.Hash()
	h2 := o.Hash()
	if h1 != h2 {
		t.Fatalf("hash not stable: %s vs %s", h1, h2)
	}
	if len(h1) != 64 {
		t.Fatalf("expected 64-char sha256 hex, got %d", len(h1))
	}
}

func TestObjectHashDiffers(t *testing.T) {
	a := (&Object{Kind: KindBlob, Body: []byte("a")}).Hash()
	b := (&Object{Kind: KindBlob, Body: []byte("b")}).Hash()
	if a == b {
		t.Fatalf("different content produced same hash")
	}
}

func TestKindAffectsHash(t *testing.T) {
	a := (&Object{Kind: KindBlob, Body: []byte("x")}).Hash()
	b := (&Object{Kind: KindTree, Body: []byte("x")}).Hash()
	if a == b {
		t.Fatalf("different kind should change hash")
	}
}

func TestTreeRoundTrip(t *testing.T) {
	in := []TreeEntry{
		{Mode: "100644", Name: "README.md", Hash: "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"},
		{Mode: "40000", Name: "src", Hash: "ffeeddccbbaa99887766554433221100ffeeddccbbaa99887766554433221100"},
	}
	encoded := EncodeTree(in)
	out, err := DecodeTree(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round-trip mismatch:\nin=%+v\nout=%+v", in, out)
	}
}

func TestCommitRoundTrip(t *testing.T) {
	body := []byte("tree abc\nparent def\nauthor x <x@y> 1700000000\nchange-id cs-foo\n\nhello\n")
	c, err := DecodeCommit(body)
	if err != nil {
		t.Fatal(err)
	}
	if c.Tree != "abc" || len(c.Parents) != 1 || c.Parents[0] != "def" {
		t.Fatalf("decoded commit wrong: %+v", c)
	}
	if c.ChangeID != "cs-foo" {
		t.Fatalf("change-id lost: %q", c.ChangeID)
	}
	if c.Message != "hello\n" {
		t.Fatalf("message wrong: %q", c.Message)
	}
}
