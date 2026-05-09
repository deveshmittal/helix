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
		err = cmdMergeReal(args)
	case "config":
		err = cmdConfig(args)
	case "remote":
		err = cmdRemote(args)
	case "clone":
		err = cmdClone(args)
	case "fetch":
		err = cmdFetch(args)
	case "pull":
		err = cmdPull(args)
	case "push":
		err = cmdPush(args)
	case "rebase":
		err = cmdRebase(args)
	case "stash":
		err = cmdStash(args)
	// next 15
	case "blame":
		err = cmdBlame(args)
	case "shortlog":
		err = cmdShortlog(args)
	case "whatchanged":
		err = cmdWhatchanged(args)
	case "merge-base":
		err = cmdMergeBase(args)
	case "bisect":
		err = cmdBisect(args)
	case "grep":
		err = cmdGrep(args)
	case "notes":
		err = cmdNotes(args)
	case "reflog":
		err = cmdReflog(args)
	case "gc":
		err = cmdGc(args)
	case "fsck":
		err = cmdFsck(args)
	case "archive":
		err = cmdArchive(args)
	case "worktree":
		err = cmdWorktree(args)
	case "format-patch":
		err = cmdFormatPatch(args)
	case "am":
		err = cmdAm(args)
	case "apply":
		err = cmdApply(args)
	// remaining stubs
	case "submodule":
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
  merge [--no-ff] <branch>              real 3-way merge (writes <<<<<<< markers on conflict)
  rebase <new-base>                     replay commits onto a new base (preserves change-id)
  stash [push|pop|apply|list|drop]      save WIP as a commit
  config <key> [<value>] | --list | --unset <key>
  remote [add|remove|-v] [args]

remote (local file:// transport):
  clone <src-path> [dest]               clone from another helix repo
  fetch [<remote>]                      fetch refs and objects
  push [<remote> [<branch>]]            push current (or named) branch
  pull [<remote>]                       fetch + fast-forward

plumbing:
  hash-object [-w] <file>               hash and (optionally) store a file as a blob
  cat-object [-p|-t] <hash>             read an object back (alias: cat-file)
  ls-files                              list tracked files
  ls-tree [-r] <tree-or-commit>         list a tree's contents
  rev-parse <ref>...                    resolve refs to hashes

history:
  blame <file>                          per-line attribution
  shortlog                              commits per author
  whatchanged [-n N]                    log + per-commit diff
  merge-base <a> <b>                    common ancestor

state / search:
  bisect start|good|bad|reset|status    binary-search for a regression
  grep [-i] <pattern> [path...]         regex over working tree
  notes add|show|remove|list            commit-attached notes
  reflog [<ref>]                        HEAD movement history

maintenance:
  gc [-n]                               sweep unreachable objects
  fsck                                  verify object integrity
  archive [--format tar|zip] [-o file] <commit>   tar/zip of a tree
  worktree add|list|remove              multiple working trees

patches:
  format-patch [-1] [-o dir] <since>    write mbox patch files
  am <patch-file>...                    apply mbox patches as commits
  apply <diff-file>                     apply a unified diff (no commit)

recognized but not implemented (informative error):
  submodule

meta:
  version                               print version
  help                                  show this help

This is an MVP. See DESIGN.md for the full design.
`)
}
