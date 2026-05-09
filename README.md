# Helix

A next-generation distributed version control system with code review built into the core data model.

This repository contains:

- [`DESIGN.md`](DESIGN.md) — the full design document (architecture, data model, CLI, review system, security, migration, comparison with Git/Gerrit/etc.).
- The **MVP slice** of the client in Go: a working CLI that demonstrates the core ideas — content-addressed object store, native `change-id` in commit headers (no `commit-msg` hook), and working-tree status.

> **Status: MVP slice, not the full design.**
> This is roughly 1% of the work described in `DESIGN.md`. It exists to ground the design in real running code and to be a starting point a team can iterate on. See [What works](#what-works) and [What does not yet work](#what-does-not-yet-work) below before assuming.

## Build

Requires Go 1.22+.

```sh
go build -o helix .
```

This produces a single static binary, `helix`.

## Try it

```sh
mkdir demo && cd demo
helix init
echo 'hello' > greeting.txt
helix status
helix commit -m "Add greeting"
helix log
```

You will see a `change-id: cs-...` printed alongside the commit. Amend the commit:

```sh
echo 'hello, world' > greeting.txt
helix commit -m "Add greeting (rev2)" --amend
helix log -n 2
```

The `change-id` is preserved across the amend. This is the property Gerrit gets via a `commit-msg` hook; Helix puts it natively in the commit header.

## Commands

### Implemented (real semantics)

| Category | Commands |
|---|---|
| Basic | `init`, `status`, `commit -m <msg> [--amend]`, `log [-n]`, `diff`, `show` |
| Files | `add`, `rm`, `mv`, `ls-files`, `ls-tree [-r]` |
| Branching | `branch`, `switch [-c] [-f]`, `checkout` (alias), `tag` |
| State | `restore --source`, `reset [--hard|--soft]`, `clean -n|-f` |
| Combine | `cherry-pick`, `revert`, `merge` (fast-forward only) |
| Config | `config`, `remote add|remove` |
| Plumbing | `hash-object [-w]`, `cat-object [-p|-t]` (alias `cat-file`), `rev-parse` |

`--amend` preserves the change-id — the property the design highlights as Gerrit's killer feature, made native.

### Recognized but not implemented (informative error)

`clone fetch pull push rebase stash blame bisect submodule worktree reflog gc fsck archive format-patch am apply shortlog grep notes whatchanged`

Running any of these prints what it would do and points to the relevant DESIGN.md section. They are not silent no-ops.

Hashes are SHA-256, displayed as full 64-char hex. Short prefixes (≥ 4 chars) work where unambiguous.

## What's real, what's not

**Real:**
- SHA-256 content-addressed object store under `.helix/objects/aa/bbcc...`.
- Three object kinds (`blob`, `tree`, `commit`) plus refs (`branches/`, `tags/`, `HEAD`).
- Stable `change-id` in the commit header, preserved across `--amend`.
- Working-tree scan with added/modified/deleted detection vs HEAD.
- Switching branches that updates the working tree (refusing if dirty without `-f`).
- Cherry-pick and revert with file-level conflict detection (errors if working tree differs from the expected base).
- Fast-forward merges with ancestor checks.
- Unified diff (LCS-based) for `diff` and `show`.
- File-based config and remote storage.

**Not real (yet):**
- No server, no review, no comments, no approvals, no submit rules.
- No network protocol — anything remote (`clone`, `push`, `pull`, `fetch`) is a stub.
- No 3-way merge — `merge` only handles fast-forward; non-FF prints the design reference.
- No reftable (refs are flat files); no pack files; no compression.
- No op-log / `undo`, no Git interop, no large-file chunking, no partial clone, no signing.
- No `rebase`, no `stash` (intentional — see design), no `blame`, no `bisect`.

These are listed honestly so you can see the gap between this MVP slice and the full design. Each stub command prints its own DESIGN.md reference when invoked.

## Layout

```
.
├── DESIGN.md            # full design document
├── README.md            # this file
├── go.mod
├── main.go              # CLI dispatch
├── repo.go              # repo discovery and init
├── object.go            # object kinds, hashing, write/read, tree encoding
├── refs.go              # HEAD and refs/branches/* read/write
├── index.go             # working-tree scan, tree builder, tree flatten
├── commit.go            # commit encoding and change-id generation
├── commands.go          # init, status, commit, log, hash-object, cat-object
├── cmd_files.go         # add, rm, mv, ls-files, ls-tree, rev-parse
├── cmd_branch.go        # branch, switch, checkout, tag, working-tree checkout
├── cmd_inspect.go       # show, diff (with LCS-based unified diff)
├── cmd_state.go         # restore, reset, clean, cherry-pick, revert, merge, config, remote
├── cmd_stubs.go         # informative errors for unimplemented git commands
└── object_test.go       # unit tests for object encoding round-trip
```

## Tests

```sh
go test ./...
```

Five unit tests cover hash stability, hash differentiation by content and kind, and round-trip encoding for trees and commits. The end-to-end CLI flow (init → status → commit → log → amend) has been verified manually.

## License

To be decided. The design document recommends Apache 2.0 for the core; this repository ships under the same intent until the project formalizes a license.
