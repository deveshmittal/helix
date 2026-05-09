package main

import (
	"fmt"
	"os"
)

const version = "0.2.0-mvp"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	var err error
	switch cmd {
	// implemented
	case "init":
		err = cmdInit(args)
	case "status":
		err = cmdStatus(args)
	case "commit":
		err = cmdCommit(args)
	case "log":
		err = cmdLog(args)
	case "hash-object":
		err = cmdHashObject(args)
	case "cat-object", "cat-file":
		err = cmdCatObject(args)
	case "add":
		err = cmdAdd(args)
	case "rm":
		err = cmdRm(args)
	case "mv":
		err = cmdMv(args)
	case "ls-files":
		err = cmdLsFiles(args)
	case "ls-tree":
		err = cmdLsTree(args)
	case "rev-parse":
		err = cmdRevParse(args)
	case "branch":
		err = cmdBranch(args)
	case "switch":
		err = cmdSwitch(args)
	case "checkout":
		err = cmdCheckout(args)
	case "tag":
		err = cmdTag(args)
	case "show":
		err = cmdShow(args)
	case "diff":
		err = cmdDiff(args)
	case "restore":
		err = cmdRestore(args)
	case "reset":
		err = cmdReset(args)
	case "clean":
		err = cmdClean(args)
	case "cherry-pick":
		err = cmdCherryPick(args)
	case "revert":
		err = cmdRevert(args)
	case "merge":
		err = cmdMerge(args)
	case "config":
		err = cmdConfig(args)
	case "remote":
		err = cmdRemote(args)
	// stubs (recognized; not implemented)
	case "clone", "fetch", "pull", "push", "rebase", "stash", "blame", "bisect",
		"submodule", "worktree", "reflog", "gc", "fsck", "archive",
		"format-patch", "am", "apply", "shortlog", "grep", "notes", "whatchanged":
		err = cmdStub(cmd)
	// meta
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

basic:
  init [path]                      initialize a new repository
  status                           show working-tree changes vs HEAD
  add [-A] [<path>...]             (no-op; helix has no staging) verify paths
  rm [-f] <path>...                remove files
  mv <src> <dst>                   rename / move
  commit -m <msg> [--amend]        snapshot the working tree
  log [-n N]                       show commit history
  diff                             working tree vs HEAD
  show [<commit>]                  show a commit and its diff

branching:
  branch [-d <name>] [<name> [<ref>]]   list / create / delete
  switch [-c] [-f] <branch>             change branch (working tree must be clean)
  checkout <branch>                     alias for switch (with warning)
  tag [-d <name>] [<name> [<ref>]]      list / create / delete tags

state:
  restore --source <ref> <path>...      restore files from a commit
  reset [--hard|--soft] [<ref>]         move HEAD; --hard also resets working tree
  clean -n | -f                         remove untracked files

advanced:
  cherry-pick <commit>                  apply a commit's changes
  revert <commit>                       create an inverse commit
  merge <branch>                        fast-forward merge only (in MVP)
  config <key> [<value>] | --list | --unset <key>
  remote [add|remove|-v] [args]

plumbing:
  hash-object [-w] <file>               hash and (optionally) store a file as a blob
  cat-object [-p|-t] <hash>             read an object back (alias: cat-file)
  ls-files                              list tracked files
  ls-tree [-r] <tree-or-commit>         list a tree's contents
  rev-parse <ref>...                    resolve refs to hashes

recognized but not implemented (informative error):
  clone fetch pull push rebase stash blame bisect submodule worktree
  reflog gc fsck archive format-patch am apply shortlog grep notes whatchanged

meta:
  version                               print version
  help                                  show this help

This is an MVP. See DESIGN.md for the full design.
`)
}
