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

### The 15 most-used git commands — all real

| Command | Status |
|---|---|
| `init` | ✓ creates a repo |
| `status` | ✓ added/modified/deleted vs HEAD |
| `add` | ✓ (no-op — helix has no staging) |
| `commit -m <msg> [--amend]` | ✓ `--amend` preserves change-id |
| `log [-n]` | ✓ walks parents from HEAD |
| `diff` | ✓ working tree vs HEAD (LCS-based unified) |
| `branch` | ✓ list / create / delete |
| `switch` / `checkout` | ✓ with working-tree update + dirty check |
| `merge` | ✓ **real 3-way line-level merge with conflict markers** |
| `clone` | ✓ filesystem source (file://) |
| `fetch` | ✓ from local remote |
| `pull` | ✓ fetch + fast-forward |
| `push` | ✓ to local remote, with non-FF refused |
| `rebase` | ✓ linear, **preserves change-id across replay** |
| `stash` (push/pop/list/drop) | ✓ saved as a WIP commit pointed at by `refs/stash` |
| `reset [--hard|--soft]` | ✓ |

### Also implemented

| Category | Commands |
|---|---|
| Inspection | `show`, `tag`, `ls-files`, `ls-tree [-r]`, `rev-parse` |
| Files | `rm`, `mv` |
| Restore | `restore --source`, `clean -n|-f` |
| Combine | `cherry-pick`, `revert` |
| Config | `config`, `remote add|remove` |
| Plumbing | `hash-object [-w]`, `cat-object [-p|-t]` (alias `cat-file`) |

### Next 15 — also real

| Command | What it does |
|---|---|
| `blame <file>` | Per-line attribution; walks first-parent history |
| `shortlog` | Commit count + subjects grouped by author |
| `whatchanged [-n N]` | Like `log -p` — log entries with per-commit diffs |
| `merge-base <a> <b>` | Print the common ancestor of two refs |
| `bisect <start\|good\|bad\|reset\|status>` | Binary-search for a regression; midpoint by topological distance, detached HEAD during search |
| `grep [-i] <pattern> [path...]` | Regex search across the working tree |
| `notes <add\|show\|remove\|list>` | Attach key/value notes to commits |
| `reflog [<ref>]` | History of HEAD/branch movements (commit, switch, reset) |
| `gc [-n]` | Mark-and-sweep unreachable objects |
| `fsck` | Verify object integrity by re-hashing every object on disk |
| `archive [--format tar\|zip] [-o file] <commit>` | Tar/zip of a tree |
| `worktree <add\|list\|remove>` | Multiple working trees (clone-backed in this MVP) |
| `format-patch [-1] [-o dir] <since>` | Emit mbox-style .patch files; **change-id is in the patch header** |
| `am <file>...` | Apply mbox patches as commits, **preserving change-id** |
| `apply <diff-file>` | Apply a unified diff to the working tree (no commit) |

### Recognized but not implemented (informative error)

`submodule`

The design (§10.4) replaces submodules with scope-imports. The command is kept as an informative stub so users running `helix submodule` see why.

Hashes are SHA-256, displayed as full 64-char hex. Short prefixes (≥ 4 chars) work where unambiguous.

## What's real, what's not

**Real:**
- SHA-256 content-addressed object store under `.helix/objects/aa/bbcc...`.
- Three object kinds (`blob`, `tree`, `commit`) plus refs (`branches/`, `tags/`, `HEAD`, `remotes/`).
- Stable `change-id` in the commit header, preserved across `--amend` AND across `rebase`.
- Working-tree scan with added/modified/deleted detection vs HEAD.
- Switching branches that updates the working tree (refusing if dirty without `-f`).
- **Real 3-way line-level merge** with `<<<<<<< / ======= / >>>>>>>` conflict markers and merge commits with two parents.
- **Local-filesystem remotes** — `clone`, `fetch`, `push`, `pull` against another helix repo on disk, the same file:// transport git supports. Push refuses non-fast-forward.
- **Linear rebase** that cherry-picks each commit onto a new base, preserving change-ids.
- **Stash** as save-WIP-commit / pop / list / drop. (The design suggests committing WIP directly; stash is a compatibility convenience, and the command says so.)
- Cherry-pick and revert with file-level conflict detection.
- Unified diff (LCS-based) for `diff` and `show`.
- File-based config and remote storage.

**Not real (yet):**
- No server, no review, no comments, no approvals, no submit rules.
- **No network protocol over the wire** — `clone`/`fetch`/`push`/`pull` only work against local paths. The design's gRPC transport (§3.8) replaces this; the file-based version above is the same shape minus the network.
- No reftable (refs are flat files); no pack files; no compression.
- No op-log / `undo`, no Git interop, no large-file chunking, no partial clone, no signing.
- No `blame`, `bisect`, `submodule`, `gc`, `fsck`.

These are listed honestly so you can see the gap between this MVP slice and the full design. Each unimplemented command prints its own DESIGN.md reference when invoked.

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
├── cmd_state.go         # restore, reset, clean, cherry-pick, revert, config, remote
├── cmd_remote.go        # clone, fetch, push, pull (local-filesystem transport)
├── cmd_merge3.go        # real 3-way merge command + tree merge logic
├── merge3.go            # line-level diff3 algorithm
├── cmd_rebase_stash.go  # rebase (linear, preserving change-ids), stash push/pop/list/drop
├── cmd_stubs.go         # informative errors for unimplemented git commands
└── object_test.go       # unit tests for object encoding round-trip
```

## Tests

```sh
go test ./...
go test -v          # see each test name
```

- **5 unit tests** in `object_test.go` — hash stability, kind/content differentiation, tree and commit round-tripping.
- **30 integration tests** total: 15 in `integration_test.go` for the top-15 verbs (init, status, add, commit, log, diff, branch, switch, merge, clone, fetch, pull, push, rebase, stash) and 15 in `integration_extra_test.go` for the next-15 (blame, shortlog, whatchanged, merge-base, bisect, grep, notes, reflog, gc, fsck, archive, worktree, format-patch, am, apply).

Specifically verified invariants:
  - `commit --amend` preserves `change-id`.
  - `rebase` preserves `change-id` across replay.
  - `format-patch` + `am` round-trip preserves `change-id` across repos.
  - `merge` produces a two-parent commit.
  - `push` refuses non-fast-forward updates.
  - `switch` refuses to change branches with a dirty working tree.
  - `gc` deletes orphan blobs but keeps reachable objects.
  - `fsck` detects hash mismatches.
  - `bisect` reduces the candidate set by ~half each step (topological median).

See [EXAMPLES.md](EXAMPLES.md) for the corresponding worked examples.

## License

To be decided. The design document recommends Apache 2.0 for the core; this repository ships under the same intent until the project formalizes a license.
