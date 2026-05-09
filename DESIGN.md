# Helix: A Modern Version Control and Code Review System

A design document for a from-scratch rewrite that keeps what Git got right, fixes what it got wrong, and integrates Gerrit-style code review as a first-class primitive.

> **Note on prior art.** Sapling (Meta), Jujutsu (`jj`), Pijul, and Fossil have already explored much of this design space. This document is honest about that overlap. The novel contribution proposed here is the *integration* of a Gerrit-quality review model with a Sapling/jj-quality client UX, on top of a Git-compatible object format — not the invention of new VCS theory. See §19 for whether that integration is worth building.

---

## 1. Executive Summary

**Helix** is a distributed version control system with code review built into the core data model. It treats a *change* (a logically reviewable unit of work) as a first-class object with a stable identity across rewrites, while preserving Git's content-addressed object graph for snapshots.

**Why it should exist.** Git's data model has aged well; its UX, history-rewrite story, monorepo support, and review story have not. Gerrit invented the right review primitives but bolted them onto Git via a commit-message hook and an external server, producing decades of friction. The combination Git + GitHub PRs is dominant but has its own pain: PR-as-branch loses identity across force-pushes, stacked PRs are second-class, and review state lives outside the repository.

**What Helix improves.**
- **Stable change identity** without a `commit-msg` hook.
- **Working-copy-as-commit** model: no staging area, no detached HEAD, no "did I commit before pulling?"
- **First-class stacked changes** with automatic rebase propagation.
- **Conflicts as objects**: a half-merged state is a real, addressable, pushable thing.
- **Reftable-style transactional ref storage** by default.
- **Native large-file and partial-clone support** — not an opt-in extension.
- **Review, approvals, and submit rules** stored in the repository graph, replicable like any other data.
- **A CLI designed by deletion**, with ~15 core verbs instead of Git's ~150.

**Non-goal.** Helix is not a content-distribution platform, an issue tracker, a CI system, or a package registry.

---

## 2. Design Principles

1. **Make the safe thing the easy thing.** Destructive operations require an explicit verb; everything else is reversible by default via an operation log.
2. **One concept per primitive.** A branch is a movable pointer. A change is a unit of review. A commit is an immutable snapshot. These never blur.
3. **No magic state.** The index, the reflog, the stash, and "detached HEAD" are gone. The working copy *is* a commit.
4. **Server is optional, not central.** Every workflow works offline; the server adds review, ACLs, and replication, not capability.
5. **Compatibility where it pays, divergence where it doesn't.** Helix reads and writes Git pack files and can act as a Git remote. It does *not* preserve Git's ref layout, index format, or hook model.
6. **Boring technology for the load-bearing parts.** SHA-256 (already standard in Git's transition plan), Merkle DAG, Raft for server consensus, well-trodden TLS/OIDC for auth.

**Tradeoffs explicitly accepted.**
- A new tool to learn, even if the verbs are simpler.
- Some Git workflows (e.g. `git rebase -i` muscle memory) won't translate verbatim.
- Native review means more server-side state than a "dumb" Git host.

**Out of scope.**
- Replacing CI systems, issue trackers, or package registries.
- Patch-theoretic merge (Pijul/Darcs). Interesting, but the engineering and pedagogy cost is high and 3-way merge with conflict objects covers 99% of practical needs.
- A new wire format invented from scratch — we use a versioned protocol over HTTP/2 + gRPC.

---

## 3. Core Architecture

### 3.1 Client

```
┌─────────────────────────────────────────────┐
│ CLI / IDE plugin / Web UI                   │
├─────────────────────────────────────────────┤
│ Porcelain (workflow commands)               │
├─────────────────────────────────────────────┤
│ Plumbing (object/ref/change ops)            │
├─────────────────────────────────────────────┤
│ Op-log │ Working copy │ Merge engine        │
├─────────────────────────────────────────────┤
│ Object store │ Ref store (reftable) │ Index │
├─────────────────────────────────────────────┤
│ Transport (HTTP/2 + gRPC, SSH)              │
└─────────────────────────────────────────────┘
```

The client is a single static binary written in **Rust** (the MVP slice in this repository is in Go for bootstrap speed; Rust is the recommended target for production). Plumbing is exposed as a stable library (`libhelix`) so IDE plugins and tooling don't shell out.

### 3.2 Server

```
┌─────────────────────────────────────────────┐
│ Edge: TLS, OIDC, rate limiting              │
├─────────────────────────────────────────────┤
│ API: gRPC + REST (changes, reviews, refs)   │
├─────────────────────────────────────────────┤
│ Review service │ Submit-rule engine │ ACL   │
├─────────────────────────────────────────────┤
│ Repo service: object store, ref txns, packs │
├─────────────────────────────────────────────┤
│ Storage: object backend (FS/S3) + metadata  │
│ DB (Postgres/CockroachDB) + search index    │
└─────────────────────────────────────────────┘
```

Repositories are sharded by repo-id; each shard's ref-store is consensus-replicated (Raft) across at least three nodes for durability and multi-region reads. Object data lives on a content-addressed blob store (filesystem for self-hosted, S3-compatible for cloud).

### 3.3 Repository storage model

A repo is:
- An **object store** of `(hash → object)` entries.
- A **ref store** (reftable) holding branches, tags, and change-refs in a single transactionally-updated file.
- A **change store** holding `Change` and `PatchSet` records (stored as objects, indexed in metadata DB on the server).
- An **op log** of every local-mutating operation (for `helix undo`).

### 3.4 Object model

Six object kinds. The first three are intentionally Git-shaped for compatibility.

| Kind | Purpose |
|---|---|
| `blob` | File contents (chunked for large files; see §5.5). |
| `tree` | Directory entry list. |
| `commit` | Snapshot: tree + parents + author + message + change-id. |
| `change` | Stable identity: id, owner, status, current patch-set, history of patch-sets. |
| `review` | Comments, approvals, submit-records for a change. |
| `op` | Entry in the operation log: parent op, mutation kind, ref-store delta. |

All objects are SHA-256 addressed. A `change` references its patch-sets by commit hash; the change-id is *not* a hash — it's a 128-bit ULID generated at change creation, so identity survives rebase and amend.

### 3.5 Reference model

One unified namespace, not Git's `refs/heads/`, `refs/tags/`, `refs/remotes/...` sprawl:

```
branches/<name>            movable
tags/<name>                immutable, signed-by-default
changes/<change-id>/head   tip of latest patch-set
changes/<change-id>/ps/<n> immutable patch-set marker
ops/<op-id>                op-log entries
```

Stored in a **reftable** (already proven in JGit) for atomic multi-ref updates and O(log n) lookup at any scale.

### 3.6 Review model

A `Change` has:
- `id` (ULID)
- `target` (branch it lands on)
- `status` ∈ {`draft`, `open`, `merged`, `abandoned`, `superseded`}
- `patch_sets[]` — ordered, immutable, each pinning a commit hash
- `reviewers[]`, `cc[]`, `labels[]`
- `submit_requirements[]` evaluated by the submit-rule engine
- `dependencies[]` — explicit or inferred from commit parents

Comments and approvals are separate objects so they replicate independently and can be rate-limited / paginated.

### 3.7 Permission model

ACLs are scoped at three levels: **organization → repository → ref-pattern**. Each level grants capabilities (`read`, `propose`, `review`, `approve:<label>`, `submit`, `force-update`, `admin`). Default deny; explicit grants only. Branch-level rules use glob patterns (`release/*`).

### 3.8 Network protocol

gRPC over HTTP/2 with streaming for object transfer. SSH transport tunnels the same gRPC for environments that prefer it. Wire format is versioned; servers advertise capabilities at handshake. We *also* speak Git's smart-HTTP protocol for read-only Git-client interop (§10).

### 3.9 API surface

A single OpenAPI/protobuf spec covers:
- `Repos.{Create, Get, List, Delete}`
- `Refs.{Get, List, Update}` (transactional batch)
- `Objects.{Get, Put, Has}` (streaming)
- `Changes.{Create, Get, List, Update, Submit, Abandon}`
- `Reviews.{Comment, Vote, GetThread}`
- `Submit.{Evaluate, Run}`
- `Audit.{Query}`

All write endpoints are idempotent given a client-supplied request-id.

---

## 4. Data Model

### 4.1 Commit

```
commit
  tree:       <tree-hash>
  parents:    [<commit-hash>...]
  change_id:  <ulid>          ← native, not in message
  author:     {id, name, email, time, signature?}
  committer:  {id, name, email, time, signature?}
  message:    <utf-8>
```

`change_id` is a header field, not a magic line in the message — no commit-msg hook required, and the message remains pure prose.

### 4.2 Tree

Standard Git-shape: list of `(mode, name, hash, kind)`. Modes restricted to file/exec/symlink/submodule/tree. Names are NFC-normalized UTF-8; case-collisions are rejected at write time (default — overridable per repo for legacy imports).

### 4.3 Blob

Either a raw byte sequence (small) or a **chunk list** (large): `[(chunk-hash, length)...]` produced by content-defined chunking (FastCDC). Clients reassemble transparently.

### 4.4 Tag

Annotated, signed-by-default. Lightweight tags exist but are discouraged in CI policy.

### 4.5 Branch

A named ref pointing at one commit. No tracking metadata in branch state — *upstream* is a property of the workspace, not the branch.

### 4.6 Change, Patch Set, Review, Comment, Approval, Submit Record

```
change          { id, repo, target_branch, owner, created, status,
                  patch_sets[], current_ps, reviewers[], topic? }
patch_set       { number, commit, parent_change?, base, created,
                  description_diff, uploader }
review          { change_id, comments[], votes[], submit_records[] }
comment         { id, author, ts, file?, line?, range?, parent?, body, resolved }
approval        { label, value, voter, ts, patch_set }
submit_record   { rule, status: ok|blocked|needs|na, label?, message }
```

Approvals are **per-patch-set, by default not carried forward** unless the change of patch-set is "trivial rebase" (verified by tree-equivalence on a no-conflict rebase) or "no code change" — same heuristic Gerrit uses, but evaluated server-side and visible in the audit trail.

### 4.7 Audit event

Every state transition (push, comment, vote, submit, ACL edit) emits an immutable, signed audit event into an append-only log keyed by repo + monotonic seq. Replicated like ref data; not garbage-collected.

---

## 5. Storage Design

### 5.1 Content-addressed storage

SHA-256 throughout. Object IDs are 32 bytes; displayed as 12-character prefixes by default with a hash-precision policy that lengthens automatically as collisions become possible.

### 5.2 Pack format

A successor to Git packs:
- **Per-object compression** with zstd (level configurable) instead of zlib.
- **Delta chains bounded** to depth 8 by default — Git's depth-50 chains are a CPU cost users pay forever.
- **Bitmap and reachability indexes** are first-class, not optional add-ons.
- **Multi-pack-index** mandatory; we never expose individual pack lookups.

### 5.3 Deduplication

Three layers:
1. Object-level (every blob deduped by hash).
2. **Chunk-level** for large blobs via FastCDC. A 2 GB asset that changes 4 KB only stores those 4 KB.
3. Pack-level cross-object delta compression at GC time.

### 5.4 Garbage collection

Reachability is computed from refs + retained op-log + retained changes. GC is **online** — runs in background, never blocks pushes. Concurrent-safe via a generational tri-color marker; objects unreferenced for ≥ retention window (default 14 days) become collectible. Aborted pushes don't strand objects beyond that window.

### 5.5 Large files

Native — no separate LFS server. Files above a configurable threshold (default 4 MB) are auto-chunked. The transport streams chunks in parallel; partial fetches and resumable uploads are baked in. There is no separate `helix lfs` subcommand.

### 5.6 Partial clone

A clone declares a **scope**: one or more path globs and an optional history depth. The server materializes a filtered object set; on cache miss, the client fetches lazily. Scope is server-validated to prevent leaking objects outside the user's ACL.

### 5.7 Sparse checkout

The working copy honors a sparse profile, declared in the repo as `.helix/profiles/<name>.profile`. Switching profiles is a single command and rewrites only the file system, not refs. Conflicting writes outside the profile are not silently dropped — they're surfaced.

### 5.8 Monorepo scaling

- **Virtual filesystem mode** (FUSE / ProjFS) for repos > 100 GB.
- **Commit graph indexes** server-side so `helix log path/to/dir` is sub-second on 10M-commit repos.
- **Path-based ACLs** so different teams can have different read scopes.
- **CODEOWNERS-equivalent** is a structured policy file enforced by the submit-rule engine, not a convention.

### 5.9 Integrity

Every object verified on read. Refs include a checksum of the reachability set since the last fsck (lazy Merkle). Server runs continuous background fsck; clients run fsck on `helix verify`.

### 5.10 Backup and recovery

Object store is content-addressed → trivially backup-able to S3 with versioning. Ref store and change DB use point-in-time recovery via WAL shipping. Cross-region backups verified weekly by sampled object-restore drills.

---

## 6. Workflow Design

The unifying idea: **the working copy is a commit**. As you edit, you mutate that commit in place. You don't `add` and you don't `commit` separately — they're the same act, performed automatically by file-system watching or on demand. To start a *new* commit, you say so. This is the jj/Sapling model and it eliminates a category of Git confusion.

### 6.1 Local workflow

```
helix init                       # create repo
edit files...                    # working copy IS commit
helix describe -m "Add foo"      # set message on current commit
helix new                        # start a new commit on top
edit more files...
```

No staging area by default. (`helix stage` exists for users who want index-style hunk selection, but it's opt-in.)

### 6.2 Branch-based workflow

```
helix branch feature/x           # create + switch
edit, describe, new, edit...
helix push origin                # publishes branch
```

### 6.3 Change-based workflow

```
helix change new                 # creates a Change object on the server
edit, describe...
helix push --review              # uploads as patch set 1
edit, describe (amend in place)
helix push --review              # uploads as patch set 2 of same change
helix submit                     # land when approved + CI green
```

The `change-id` is created locally and stamped into the commit header. Amending the commit keeps the same `change-id` — the server recognizes it and adds a new patch-set rather than creating a new change. **No `commit-msg` hook.**

### 6.4 Pull-request-style workflow

For teams that prefer GitHub-style: a *pull request* in Helix is a Change whose patch-sets happen to be a sequence of commits rather than amendments of one commit. Reviewers can review either way; the data model is the same.

### 6.5 Stacked changes

```
helix change new                 # change A
edit; describe -m "Refactor X"
helix change new                 # change B, parent = A
edit; describe -m "Use refactored X"
helix push --review              # publishes both, with A as B's dependency
```

When A is rebased on `main`, B is **automatically rebased** locally and re-uploaded. The server tracks the dependency; B cannot be submitted before A.

### 6.6 Rebase

`helix rebase --onto main` rewrites the change's commits onto a new base. Old patch-sets are preserved on the server; reviewers see the diff between PS-N and PS-N+1, with a "trivial rebase" annotation when the only changes are upstream merges.

### 6.7 Merge

3-way merge by default. **Conflicts are first-class objects** — a half-merged tree containing conflict regions can be committed, pushed, and reviewed. This means CI can run on a conflicted state and report which conflicts block the merge, instead of forcing the developer to resolve before they can even share the WIP.

### 6.8 Cherry-pick / revert

`helix cherry-pick <change-id>` and `helix revert <change-id>` operate on changes, not commits. Reverting a merged change creates a new change whose description references the original.

### 6.9 Conflict resolution

Built-in 3-way TUI plus structural-merge plugins (per-language: tree-sitter-aware merges for braces, imports, function reorders). Merges that succeed structurally but produce a different AST than either parent are flagged for human review.

### 6.10 CI-gated submission

Submit rules (§8.6) declare CI requirements declaratively. `helix submit` is **always a server-side operation** — it evaluates rules atomically against the latest patch-set, fails fast if rules are unmet, and lands via fast-forward or merge-commit per branch policy.

---

## 7. CLI Design

A small set of verbs, organized by what users actually do.

### 7.1 Daily verbs (15)

```
helix init        helix clone       helix status
helix log         helix diff        helix describe
helix new         helix amend       helix branch
helix switch      helix push        helix pull
helix change      helix submit      helix undo
```

### 7.2 Power verbs (advanced)

```
helix rebase      helix merge       helix cherry-pick
helix revert      helix split       helix squash
helix stage       helix bisect      helix verify
helix gc          helix admin
```

### 7.3 Naming improvements over Git

| Git | Helix | Why |
|---|---|---|
| `checkout` (5 different jobs) | `switch` / `restore` / `change checkout` | One job each. |
| `reset` (3 different jobs) | `undo` / `restore` / `branch move` | "reset" hides intent. |
| `add` + `commit` | (automatic) + `describe` / `new` | Staging not needed for 90% of work. |
| `push -f` | `push --replace` (requires explicit ref) | "force" implies "right thing"; "replace" implies "destructive". |
| `git stash` | (none — working copy is a commit) | Stash is a workaround for the index. |
| `pull` (fetch + merge) | `pull` (fetch + rebase by default) | Defaults to clean history. |

### 7.4 Examples

```bash
# Start work
helix clone https://corp/helix/web
helix change new -m "Speed up search"

# Iterate
$EDITOR src/search.rs
helix diff
helix push --review                      # patch set 1
$EDITOR src/search.rs
helix push --review                      # patch set 2

# Land
helix submit                              # only if rules pass

# Recover from a mistake
helix undo                                # reverses last op
helix op log                              # see what you did
helix op restore <op-id>                  # jump to any prior state
```

### 7.5 Safety

Destructive operations:
- Require an explicit verb (`replace`, `delete`, `purge`).
- Print a single-line "preview" of what will be lost.
- Are reversible via op-log for the retention window.

Confirmation prompts are *adaptive* — never asked twice for the same action in a session, never asked for trivially reversible actions.

### 7.6 Edge cases Git users hit

- **"Detached HEAD"**: doesn't exist. Switching to a commit creates an anonymous branch in the op-log.
- **"Forgot to pull before push"**: `push` is rebase-then-push by default and atomic on the server.
- **"Lost commits after rebase"**: commits remain in the op-log for the retention window; `helix undo` reverses any operation.
- **"Wrong branch"**: `helix branch move <commits> <to-branch>` instead of `git reset` + cherry-pick.

---

## 8. Code Review System

### 8.1 Creating changes

`helix push --review` (or `helix change push`) creates or updates a change. The change is keyed by the commit's `change_id` header. New change → new `Change` object; existing → new patch-set on the same `Change`.

### 8.2 Patch sets

Each push that materially differs from the previous tip creates a new patch-set. "Materially different" means tree-not-equal *or* parent-not-equal *or* message-not-equal. Patch-sets are immutable references and never garbage-collected while the change exists.

### 8.3 Reviewers

Assigned by:
1. Explicit `--reviewer` flag at push.
2. Repository-level rules (CODEOWNERS-equivalent: a structured `helix/owners.toml` file).
3. Auto-suggestion from blame + recent reviewer history.

### 8.4 Inline comments

Anchored to `(file, commit, line-range)`. Re-anchored across patch-sets via line-tracking heuristics; if anchoring fails, the comment is shown on the original patch-set with a "comment moved/orphaned" indicator. Threads are explicit objects and resolution is a state, not a tag.

### 8.5 Approvals

Labels are configurable per repo. Default labels:
- **Code-Review**: `-2, -1, 0, +1, +2`
- **Verified** (CI): `-1, 0, +1`

Submit requires `Code-Review = +2` and `Verified = +1` by default; configurable via submit rules. Negative votes are **blocking** and require explicit clearing.

### 8.6 Submit rules

Declarative, written in a sandboxed Rego-like DSL stored in the repo at `helix/submit_rules.rego`. Evaluated server-side on every patch-set update, results streamed to the UI:

```rego
allow_submit if {
  votes["Code-Review"] >= 2
  votes["Verified"] >= 1
  not has_unresolved_comments
  ci_status == "passing"
  owner_approval_present
}
```

Versioned with the repo so historical submits can be re-evaluated for audit.

### 8.7 CI representation

CI systems post `Verified` votes plus a structured `BuildResult` object: `{system, url, status, started, finished, artifacts[]}`. Multiple CI systems vote independently; submit rules combine them.

### 8.8 Dependencies

Stacked changes form a DAG. The server enforces topological submit order. UI shows the full stack on every change. Dependencies are computed from commit parents but can be declared explicitly (`Depends-On: <change-id>`) for cross-repo dependencies.

### 8.9 Lifecycle states

- `draft` — visible only to owner + explicit reviewers
- `open` — under review
- `merged` — landed
- `abandoned` — explicitly closed
- `superseded` — closed because another change subsumed it (linked)

Transitions are audit-logged.

---

## 9. Security Model

### 9.1 Authentication

OIDC for humans (any IdP). Short-lived (≤ 1h) tokens for clients; refresh via OIDC. Service accounts use signed JWTs from a workload identity provider (SPIFFE-compatible). SSH key auth supported but discouraged — keys don't rotate.

### 9.2 Authorization

Capability-based, scoped per (org, repo, ref-pattern). Default deny. Branch protection is an ACL pattern, not a separate feature. Submit is a distinct capability from push — pushing for review and landing are different operations.

### 9.3 Signed commits and changes

Every commit is signed by default (sigstore / Ed25519). Signing keys are bound to identities via OIDC; gitsign-style keyless flow available. Servers reject unsigned commits on protected branches.

A **change** also carries an aggregate signature over its accepted patch-set + approvals + submit-record, so the audit trail of "who reviewed and approved this code" is itself cryptographically verifiable.

### 9.4 Bots

Bot identities are first-class. Each bot has a capability set; bots cannot self-approve their own changes. CI bots can vote `Verified` but not `Code-Review`. Bot actions are audit-tagged distinctly.

### 9.5 Hooks

No arbitrary shell hooks. Server-side policy runs in a **WASM sandbox** with declared capabilities (read repo, read change, post comment, post vote). Client-side hooks are similarly sandboxed and opt-in per repo — cloning a malicious repo cannot execute code.

### 9.6 Credential storage

Use OS keychains (macOS Keychain, Windows Credential Manager, libsecret). Never write credentials to plain config files. Repo-local config is read-only for security-sensitive keys.

### 9.7 Malicious repository protection

- All paths normalized; `..`, NUL, control chars rejected at write time.
- Symlink-out-of-tree on checkout requires explicit confirmation.
- Submodule URLs are validated against an allowlist policy.
- Pack-file parsers fuzzed continuously; OOM-bounded.
- "Unsafe repository" is a real concept: cloning into a directory with unexpected ownership prompts.

### 9.8 Supply chain

- SBOM generation on every submit.
- Provenance records (in-toto/SLSA) attached to merges as audit events.
- Dependency-update changes get a distinct `Dependency-Update` label and stricter submit rules.

---

## 10. Migration and Interoperability

### 10.1 Git import

`helix import git <url>` produces a Helix repo with:
- Object hashes preserved as **legacy SHA-1 mappings** (Helix internally re-addresses to SHA-256 but maintains a bidirectional index).
- Branches → branches.
- Tags → tags (re-signed if requested).
- Each commit gets a synthesized `change_id` based on the existing Gerrit `Change-Id:` trailer if present, else a new ULID.

### 10.2 Git export

`helix export git --to <url>` produces a Git-compatible repo. Helix-specific objects (changes, reviews) are exported as `refs/helix/changes/...` so a Git client sees them but ignores them.

### 10.3 Gerrit migration

A `helix import gerrit` mode reads Gerrit's NoteDB:
- Each Gerrit change → a Helix change with the same change-id.
- Patch sets, comments, votes, submit records → corresponding Helix objects.
- Submit rules expressed in Prolog → translated to the Rego-like DSL where automatic; flagged for review where not.

### 10.4 GitHub/GitLab/Bitbucket migration

Tooling reads via the platform's API:
- Repos and refs → Helix objects (Git-compatible path).
- PRs/MRs → Helix changes (one PR = one change with one or more patch-sets, derived from PR commit history).
- PR comments → Helix comments anchored as best as we can.
- Approvals → Helix approvals on `Code-Review` label.

Not all platforms expose enough fidelity to perfectly preserve PR state. The migration tool produces a fidelity report.

### 10.5 Live interop

Helix servers expose Git smart-HTTP read-only. Existing CI, deploy scripts, and `git clone` keep working during transition. Writes go through Helix.

### 10.6 Hash handling

Internal hashes are SHA-256. Git-imported objects retain a `legacy_sha1` field so external systems quoting the old hash still resolve.

---

## 11. Developer Experience

### 11.1 Onboarding

`helix init` runs an interactive 3-step wizard the first time: identity, signing key, default editor. Subsequent repos inherit. A `helix tour` command walks through making, reviewing, and submitting a change in a sandbox repo — under 5 minutes, no prior Git knowledge assumed.

### 11.2 Error messages

Every error has:
- A code (`E0142`).
- A one-line summary in plain English.
- A "what to do" line.
- A `helix explain E0142` command for depth.

Bad: `error: failed to push some refs`. Good: `E0142: change cs-Q3X8P4 has comments unresolved by the owner — resolve them or run 'helix push --override-resolved' to push anyway`.

### 11.3 Documentation

- **Reference** auto-generated from the protobuf API.
- **Tutorials** task-shaped, not feature-shaped ("How do I revert a bad merge?" not "The revert command").
- **Recipes** for migration scenarios.
- All docs versioned with the binary.

### 11.4 IDE integration

`libhelix` (Rust) with FFI bindings (C, Python, TypeScript, JVM). LSP-style "Helix language server" exposes operations to any editor. VS Code, JetBrains, Neovim plugins are first-party.

### 11.5 Web UI

- Three-pane review: file tree | unified diff | comments thread.
- Range comments span hunks naturally.
- Side-by-side stacked-change view: see the whole stack while reviewing one piece.
- Keyboard-first; full functionality without a mouse.
- Dark mode default. (Sorry.)

### 11.6 Conflict UI

Inline conflict regions rendered as an interactive 3-pane (base | ours | theirs) widget. Per-language structural merge tools render hunks at AST granularity. "Accept ours/theirs/both" + manual edit, all undoable.

### 11.7 Observability

Per-user: op-log inspection, "what changed in my workspace" digest.
Per-admin: Prometheus metrics, structured JSON audit log, OpenTelemetry tracing for every API call, slow-query log.

---

## 12. Administration

### 12.1 Repository creation

`helix admin repo create <name>` with optional template. Templates are themselves Helix repos. Naming, default-branch, ACL inheritance configured at org level.

### 12.2 Access control

YAML ACL files stored *in the repo* under `helix/acl.yaml`, change-controlled like code. Org-level ACL stored in a meta-repo with stricter approval requirements.

### 12.3 Policy

Submit rules, branch protection, required labels, all expressed in repo-versioned config files. Policy changes are themselves changes that go through review.

### 12.4 CI integration

Pluggable. Built-in support for GitHub Actions, GitLab CI, Buildkite, Jenkins via webhooks + a `helix ci-token` short-lived credential.

### 12.5 Replication

Multi-master with Raft for ref store; eventually-consistent for object store (content-addressed, so safe). Each region serves reads locally; writes proxy to the leader for the affected ref-shard.

### 12.6 Multi-site

Active-active by default. WAN-aware fetch (delta + bitmap) so multi-region offices don't full-clone over slow links. Geo-replicated object store with regional read affinity.

### 12.7 Backups and DR

Object store: S3 cross-region replication + versioning.
Ref store: Raft + WAL shipping to a cold region, RPO ≤ 5 minutes, RTO ≤ 1 hour.
Quarterly DR drills with measured restore times.

### 12.8 Monitoring

Standard golden signals (rate, error, latency, saturation) per service. Repo-level "freshness lag" between regions exposed as a metric and visible in the UI.

### 12.9 Audit logs

Append-only, signed, exportable to SIEM. Retention configurable (default 7 years for compliance use cases). Querying via a dedicated read API.

---

## 13. Scalability and Performance

| Dimension | Target |
|---|---|
| Local `status` on 10M-file repo | < 200 ms (with FS watcher) |
| Local `log -- path` on 1M commits | < 500 ms (commit-graph + path index) |
| Server fetch of single small change | < 100 ms p99 |
| Server `clone` of 10 GB repo (LAN) | bounded by NIC, not CPU |
| Submit-rule eval | < 50 ms p99 |
| Concurrent open changes per repo | 100K without UI degradation |
| Refs per repo | 10M (reftable scales) |

### 13.1 Local

- FS watcher (FSEvents/inotify/ReadDirectoryChangesW) keeps working-copy hash up to date incrementally.
- Working-tree diff uses a Merkle index over the file system.
- Most operations are O(changed-files), not O(repo-size).

### 13.2 Server

- Sharding by repo-id; large repos can sub-shard by ref-prefix.
- Cold pack-objects requests served from precomputed bitmap indexes.
- Hot path: single SSD seek + memory-mapped reftable read.

### 13.3 Search

Built-in code search over recent commits, indexed via a Tantivy-based service. Full-history search is opt-in (expensive to maintain); recent-history search is always on.

### 13.4 Storage efficiency

Zstd + delta + chunk dedup typically yields 1.3–2× better ratios than Git-zlib on typical source repos, much higher (10–50×) for repos with large binary turnover.

---

## 14. Testing Strategy

- **Unit**: every plumbing op, every CLI parser, every storage primitive. Target 90% line coverage on plumbing.
- **Integration**: end-to-end CLI scenarios using a hermetic-sandbox harness.
- **Protocol**: golden-file tests for wire formats; cross-version tests against the previous N-2 versions.
- **Compatibility**: Git interop suite — clone Helix repo with stock `git`, push from `git` to Helix, round-trip object preservation. Run nightly against multiple Git versions (2.30, 2.40, 2.50, latest).
- **Security**: continuous fuzzing (cargo-fuzz, oss-fuzz) on every parser, hash verifier, and config loader. Annual third-party pen test.
- **Performance**: fixed benchmark suite (microbenchmarks + macro: clone, fetch, status, log, submit), tracked over time, regression-blocking.
- **Fuzz**: pack files, refs, change objects, ACL configs, submit rules.
- **Migration**: nightly import of a corpus of public Git repos (Linux kernel, LLVM, Chromium-shaped synthetic) with checksum verification.
- **Distributed consistency**: Jepsen-style tests for the ref-store under partition, leader change, and clock skew.

---

## 15. Implementation Plan

### 15.1 MVP (months 0–9)

- Object store, reftable, SHA-256 commit/tree/blob.
- Local CLI: `init`, `clone`, `status`, `diff`, `describe`, `new`, `branch`, `switch`, `push`, `pull`, `log`, `undo`.
- Single-region server with gRPC + minimal review (changes, comments, one approval label, submit).
- Git import (read-only).
- Web UI: list changes, view diff, comment, approve, submit.

**Exit criterion**: a 5-engineer team uses Helix exclusively for a real internal project for 4 weeks without escape hatches.

### 15.2 Phase 1 (9–18 months)

- Stacked changes, submit rules, CI integration, Git export.
- Sparse / partial clone.
- Reftable production-hardened, multi-region read replicas.
- Conflict-as-object.
- Bot identities, signed commits.

### 15.3 Phase 2 (18–30 months)

- Large file native chunking + virtual filesystem mode.
- Gerrit and GitHub migrators with fidelity reports.
- WASM-sandboxed hooks.
- Structural merge for top 5 languages.
- Multi-master Raft writes.

### 15.4 Phase 3 (30+ months)

- Cross-repo dependencies as first-class.
- SBOM/SLSA integration.
- Federation between Helix instances (cross-org changes).
- Code search at full-history scale.

### 15.5 Risks and mitigations

| Risk | Mitigation |
|---|---|
| Adoption inertia (Git is everywhere) | Bidirectional Git interop from day 1; Helix server can host Git-only repos. |
| Performance regressions vs Git | Benchmark suite gates every release; explicit perf budget. |
| Reftable bugs on huge ref counts | Reuse JGit's reftable as a reference; don't reinvent until proven. |
| Submit-rule DSL turning into a programming language | Hard cap on language complexity; written reviews of every new built-in. |
| Security regressions | Mandatory threat-model review for any new server endpoint. |
| Overlap with Sapling / jj | See §19. |

### 15.6 Languages and frameworks

- **Client** in Rust (single static binary, FFI). The MVP slice in this repo is in Go for bootstrap speed.
- **Server** in Rust (or Go if team velocity favors it; Rust preferred for shared code with the client).
- **Web UI** in TypeScript + React + Vite. Server-rendered for the read-heavy review pages.
- **Storage**: S3-compatible (MinIO for self-hosted) for objects; PostgreSQL / CockroachDB for metadata; Tantivy for search.
- **Consensus**: Raft via the `openraft` crate.
- **Wire**: tonic (gRPC).

### 15.7 Team

Initial: 1 PM, 1 designer, 8 engineers split as 3 client / 3 server / 2 web. Add 2 SRE in Phase 1, 2 security in Phase 2.

---

## 16. Comparison

| Dimension | Helix | Git | Gerrit | GitHub PRs | GitLab MRs | Mercurial | Perforce |
|---|---|---|---|---|---|---|---|
| Distributed | ✓ | ✓ | ✓ (via Git) | ✓ (via Git) | ✓ (via Git) | ✓ | partial |
| Stable change ID | ✓ native | ✗ | ✓ via hook | by branch | by branch | ✗ | by changelist |
| Review built-in | ✓ | ✗ | ✓ | server-side | server-side | ✗ | ✓ |
| Stacked changes first-class | ✓ | painful | ✓ | external tools | partial | partial | ✓ |
| Conflicts as objects | ✓ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ |
| No staging area | ✓ | ✗ | ✗ | ✗ | ✗ | ✗ (default) | ✓ |
| Op-log / undo | ✓ | reflog | ✗ | ✗ | ✗ | partial | ✓ |
| Native large files | ✓ | LFS bolt-on | LFS | LFS | LFS | extension | ✓ |
| Monorepo at scale | ✓ | needs partial-clone | ok | weak | weak | with extensions | ✓ |
| Submit rules | ✓ | ✗ | ✓ | branch protection | rules | ✗ | triggers |
| Open source | proposed | ✓ | ✓ | ✗ | partial | ✓ | ✗ |
| Beginner UX | good | poor | poor | good | good | good | medium |

---

## 17. Example Workflows

### 17.1 Create a repository

```bash
$ helix admin repo create acme/web --template service
Created acme/web. Default branch: main. ACL inherited from acme.
$ helix clone https://hx.acme/acme/web
Cloned 1 ref, 0 changes, 0 objects.
```

### 17.2 Make a commit

```bash
$ cd web
$ echo "fn main() {}" > src/main.rs
$ helix status
On branch main (no upstream)
Working copy is commit @abc123 (untitled)
  added: src/main.rs
$ helix describe -m "Initial scaffold"
$ helix push origin
Pushed main → origin/main (1 commit).
```

### 17.3 Create a reviewable change

```bash
$ helix change new -m "Add /healthz endpoint"
Created change cs-K7P9Q2 on top of main.
$ $EDITOR src/main.rs
$ helix push --review
Uploaded patch set 1 of cs-K7P9Q2.
URL: https://hx.acme/acme/web/c/cs-K7P9Q2
Reviewers auto-suggested: alice@, bob@
```

### 17.4 Update a patch set

```bash
$ $EDITOR src/main.rs
$ helix push --review
Uploaded patch set 2 of cs-K7P9Q2.
Diff vs PS1: +3 / -1.
```

### 17.5 Review a change

```bash
$ helix change checkout cs-K7P9Q2
Switched to change cs-K7P9Q2 PS2.
$ helix diff @^
... shows the change ...
$ helix review --comment src/main.rs:42 "Consider using std::process::exit"
$ helix review --vote Code-Review=+1
```

### 17.6 Submit

```bash
$ helix submit cs-K7P9Q2
Evaluating submit rules...
  ✓ Code-Review ≥ +2  (alice +2)
  ✓ Verified ≥ +1     (CI green)
  ✓ No unresolved comments
  ✓ Owner approval present
Submitting via fast-forward... done.
cs-K7P9Q2 merged as commit @def456 on main.
```

### 17.7 Resolve a conflict

```bash
$ helix pull
Conflict in src/main.rs
Working copy is now a conflict commit (you can push it for review if you want).
$ helix resolve src/main.rs
[opens 3-pane TUI; resolve, save]
$ helix continue
Conflict resolved. Working copy is commit @ghi789.
```

### 17.8 Revert

```bash
$ helix revert cs-K7P9Q2
Created change cs-M3N1L8: "Revert: Add /healthz endpoint".
PS1 uploaded. Submit when reviewed.
```

### 17.9 Recover from a mistake

```bash
$ helix amend -m "wrong message"
$ helix op log
@ op-9f2  amend     (just now)
| op-9f1  describe  (2m ago)
| op-9f0  push      (5m ago)
$ helix undo
Reverted op-9f2. Working copy restored to op-9f1.
```

### 17.10 Monorepo

```bash
$ helix clone https://hx.acme/acme/mono --scope services/billing,libs/common
Sparse clone: 14k of 1.2M files materialized.
$ cd mono
$ helix log services/billing
... fast even though repo has 8M commits ...
```

### 17.11 Stacked changes

```bash
$ helix change new -m "Refactor billing client"
... edit ...
$ helix change new -m "Use refactored client in API"
... edit ...
$ helix push --review
Uploaded:
  cs-A1B2C3 PS1: Refactor billing client (no deps)
  cs-D4E5F6 PS1: Use refactored client (depends on cs-A1B2C3)
$ # later, after cs-A1B2C3 is rebased on main:
$ helix sync
Stack rebased automatically. cs-D4E5F6 PS2 uploaded.
```

---

## 18. Risks and Open Questions

### 18.1 Technical

- **Reftable at extreme scale (>100M refs)**: unproven. Mitigate with sharded reftables.
- **Conflict-as-object semantics**: how does `bisect` behave across conflict commits? Open.
- **Cross-repo change dependencies**: introduces distributed-transaction-shaped complexity. Phase 3.
- **Structural merge correctness**: per-language parsers diverge from compilers. Limit to advisory mode in Phase 2.

### 18.2 Adoption

- Git is the entrenched default. Helix needs interop on day 1 *and* a clear "why switch" story. The honest answer is: the value is at the team-and-up level (review fidelity, monorepo scale, audit), not the individual level.
- Migration tooling fidelity is everything. A bad import burns trust permanently.

### 18.3 Migration

- Hash translation between SHA-1 and SHA-256 will surface in CI logs, deploy scripts, bug trackers — every system that pinned a Git SHA. A long deprecation window with both visible is required.
- Gerrit Prolog rules don't always cleanly translate; require manual review for edge cases.

### 18.4 Compatibility

- Some Git workflows (e.g., `git filter-branch` style history surgery) are intentionally not supported. Users who depend on these need a separate path.
- Submodules: deliberately replaced by repo-imports / scope-includes, which means submodule-heavy repos need explicit migration.

### 18.5 UX

- "Working-copy-is-commit" is an adjustment for Git users. Teach it explicitly.
- The op-log creates a new mental model; users need to learn to trust it instead of being afraid of `--force`.

### 18.6 Governance

- Who owns Helix? A foundation is the credible answer (CNCF / SPI). Single-vendor stewardship will limit adoption.
- License: Apache 2.0 for the core. Server features that compete with hosted offerings (advanced replication, SAML) are the natural commercial-edition line — be explicit about it from day 1, don't relicense later.

---

## 19. Final Recommendation

**Build a hybrid.** A Git-compatible *client* plus a new *server*. Concretely:

- **Year 1**: Ship the Helix client as a tool that talks Git protocol to existing Git servers, *and* the new Helix protocol where available. Adoption requires zero server change. The client alone — with op-log, change-id-as-header, working-copy-as-commit, native stacked changes — is a meaningful improvement that stands on its own. This is essentially what Sapling and jj have proven works.
- **Year 2**: Ship the Helix server with native review, submit rules, and replication. Teams adopt server-side when the review value justifies it; the client keeps working against Git in the meantime.
- **Year 3+**: Migration tooling matures. Helix becomes a first-class option for new repos and large migrations.

**Build first**:
1. Client object store + working-copy-as-commit + op-log.
2. Git interop (read and write).
3. Core CLI (15 verbs).
4. Stacked changes with auto-rebase.

That alone justifies the project even if the server never ships.

**Avoid**:
- Reinventing wire formats before the data model is stable. Use gRPC and version it.
- Patch-theoretic merge — academically interesting, practically a five-year detour.
- Day-1 distributed multi-master writes. Single-region write, multi-region read first.
- Vendor lock-in features (proprietary auth flows, closed APIs). The point of replacing Git is to do *better*, including on openness.
- A configurable everything. Each config knob is a future support burden. Strong defaults.

**What makes this meaningfully better than Git + Gerrit**:
1. **One tool, one protocol, one mental model** for both the developer and the reviewer. Today the gap between `git push` and "what happens on Gerrit" is enormous; here they're the same act.
2. **Stable change identity natively**, removing the entire `commit-msg`-hook category of confusion.
3. **Stacked changes that work correctly** without third-party tools (`git-branchless`, `ghstack`, etc.).
4. **Op-log and reversible operations**, eliminating the "I lost my work after rebase" failure mode.
5. **First-class large-file and monorepo support**, removing the LFS/VFS extension tax.
6. **Review state replicated like code**, so your review history is durable, queryable, and migrate-able across hosts.

**Honest caveat**: Sapling already delivers most of (1)–(4) for individual users; jj delivers (1)–(4) cleanly without server tie-in; both lack a Gerrit-quality review server. The unique contribution of this design is therefore the *server side and the integration*, not the client innovations on their own. That's still worth building — but a team starting today should seriously evaluate forking Sapling or jj for the client and focusing engineering effort on the review server, rather than building all three layers from scratch. That choice could cut the timeline in §15 roughly in half.
