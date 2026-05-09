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

| Command | What it does |
|---|---|
| `helix init [path]` | Create a new repository. |
| `helix status` | Show added/modified/deleted files vs HEAD. |
| `helix commit -m <msg> [--amend]` | Snapshot the working tree. `--amend` preserves the change-id. |
| `helix log [-n N]` | Walk parents from HEAD. |
| `helix hash-object [-w] <file>` | Compute (and optionally store) a blob's SHA-256 hash. |
| `helix cat-object [-p|-t] <hash>` | Read an object back. `-p` pretty-prints commits/trees; `-t` shows type only. |

Hashes are SHA-256, displayed as full 64-char hex. Short prefixes (≥ 4 chars) work where unambiguous.

## What works

- SHA-256 content-addressed object store on disk under `.helix/objects/aa/bbcc...`.
- Three object kinds: `blob`, `tree`, `commit`.
- Stable `change-id` in commit headers, preserved across `--amend`.
- Repository discovery (walks up from cwd looking for `.helix/`).
- Symbolic-ref `HEAD → refs/branches/main`.
- Working-tree scan with added/modified/deleted detection vs HEAD.
- Tree directory structure (nested directories produce nested tree objects).
- Round-trip-tested encoding for tree and commit objects.

## What does not yet work

Everything else in `DESIGN.md`, notably:

- No server, no review, no comments, no approvals, no submit rules.
- No network protocol — `clone`, `push`, `pull` are not implemented.
- No staging area is intentional, but no `helix stage` opt-in path either yet.
- No reftable (refs are flat files).
- No pack files; every object is its own file.
- No compression. Object bodies are raw bytes — fine for an MVP, expensive at scale.
- No merge, no rebase, no branch switching that touches the working tree.
- No op-log, no `helix undo`.
- No Git interop (import or export).
- No large-file chunking, no partial clone, no sparse checkout.
- No signing.

These are not stubs — they're simply not in this slice. See `DESIGN.md` for what they should look like.

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
├── commands.go          # implementations of each CLI verb
└── object_test.go       # unit tests for object encoding round-trip
```

## Tests

```sh
go test ./...
```

Five unit tests cover hash stability, hash differentiation by content and kind, and round-trip encoding for trees and commits. The end-to-end CLI flow (init → status → commit → log → amend) has been verified manually.

## License

To be decided. The design document recommends Apache 2.0 for the core; this repository ships under the same intent until the project formalizes a license.
