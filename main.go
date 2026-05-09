package main

import (
	"fmt"
	"os"
)

const version = "0.1.0-mvp"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	var err error
	switch cmd {
	case "init":
		err = cmdInit(args)
	case "status":
		err = cmdStatus(args)
	case "hash-object":
		err = cmdHashObject(args)
	case "cat-object":
		err = cmdCatObject(args)
	case "commit":
		err = cmdCommit(args)
	case "log":
		err = cmdLog(args)
	case "version", "--version", "-v":
		fmt.Printf("helix %s\n", version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "helix: unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "helix: %s\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `helix `+version+` — a next-generation VCS (MVP slice)

usage: helix <command> [args]

commands:
  init                       initialize a new repository here
  status                     show working-tree changes vs HEAD
  commit -m <msg>            snapshot the working tree as a commit
  log                        show commit history from HEAD
  hash-object <file>         hash and store a file as a blob
  cat-object <hash>          print a stored object's contents
  version                    print version
  help                       show this help

This is an MVP, not the full design. See DESIGN.md for what's planned.
`)
}
