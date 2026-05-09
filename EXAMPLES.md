# Helix examples

One worked example per command, in the order most users encounter them. Outputs shown are real (captured from this MVP build).

Each example is also covered by an automated integration test in [`integration_test.go`](integration_test.go).

---

## 1. `init` — create a repository

```sh
$ mkdir myproject && cd myproject
$ helix init
Initialized empty helix repository in /path/to/myproject/.helix
```

Creates `.helix/{HEAD,config,objects/,refs/branches/,refs/}`. Running it twice in the same directory is an error.

---

## 2. `status` — show working-tree changes

```sh
$ echo "hello" > greeting.txt
$ helix status
On branch main

Added:
  + greeting.txt

$ helix commit -m "First commit"
[main 4a8b2... ] First commit
change-id: cs-...

$ helix status
On branch main
Working tree clean.

$ echo "modified" > greeting.txt
$ helix status
On branch main

Modified:
  ~ greeting.txt
```

No staging area to reason about. Categories are exactly: Added / Modified / Deleted.

---

## 3. `add` — track files (no-op in helix)

```sh
$ echo "hello" > a.txt
$ helix add a.txt
helix has no staging area — files are tracked automatically. 1 path(s) verified.

$ helix add -A
helix tracks 1 files (no staging needed; commit when ready)
```

`add` is kept for muscle-memory and for scripts ported from git. It verifies files exist but doesn't record any state — Helix has no index to update.

---

## 4. `commit` — snapshot, with `--amend` preserving change-id

```sh
$ echo "hello" > greeting.txt
$ helix commit -m "Initial"
[main d7c2... ] Initial
change-id: cs-egw5mzypgg

$ echo "hello, world" > greeting.txt
$ helix commit -m "Initial (better)" --amend
[main 89f0... ] Initial (better)
change-id: cs-egw5mzypgg     ← same change-id, new commit hash
```

The `change-id` is stamped into the commit header, not the message. It survives `--amend` (and `rebase`, see #14), so review history stays continuous across rewrites without a `commit-msg` hook.

---

## 5. `log` — walk parents from HEAD

```sh
$ helix log -n 2
commit c5a8...
change cs-2u7f...
author user <user@local>
date   2026-05-09T...

    Add /healthz endpoint

commit 4a8b...
change cs-egw5...
author user <user@local>
date   2026-05-09T...

    Initial scaffold
```

`-n N` limits output. Output includes the change-id alongside the commit hash so you can see review continuity.

---

## 6. `diff` — working-tree changes vs HEAD

```sh
$ echo "alpha" > f.txt && echo "beta" >> f.txt && echo "gamma" >> f.txt
$ helix commit -m "init"
$ sed -i '' 's/beta/BETA/' f.txt
$ helix diff
--- a/f.txt
+++ b/f.txt
 alpha
-beta
+BETA
 gamma
```

LCS-based unified diff. Per-file headers, ` ` for context, `-` for removed, `+` for added.

---

## 7. `branch` — list, create, delete

```sh
$ helix branch                # list
* main

$ helix branch feature        # create at HEAD
Created branch feature at d7c2...

$ helix branch                # list again
  feature
* main

$ helix branch -d feature     # delete
Deleted branch feature
```

The `*` marks the current branch. `branch <name> <ref>` creates at an arbitrary commit / tag.

---

## 8. `switch` — change branches with working-tree update

```sh
$ helix branch feature
$ helix switch feature
Switched to branch feature

$ echo "feature work" > f.txt
$ helix commit -m "feature change"

$ helix switch main           # working tree reverts
Switched to branch main

$ cat f.txt
(content from main, not feature)
```

`switch` refuses if the working tree has uncommitted changes (use `-f` to discard, or commit first). `-c` creates the branch first: `helix switch -c new-feature`.

---

## 9. `merge` — real 3-way line-level merge

```sh
$ printf "L1\nL2\nL3\n" > f.txt && helix commit -m "base"
$ helix branch other
$ printf "OURS-L1\nL2\nL3\n" > f.txt && helix commit -m "ours change"
$ helix switch other
$ printf "L1\nL2\nTHEIRS-L3\n" > f.txt && helix commit -m "theirs change"
$ helix switch main
$ helix merge other
[main 7125... ] merged other

$ cat f.txt
OURS-L1
L2
THEIRS-L3
```

Both branches' edits are present in the merge result. The merge commit has two parents.

When the same line is changed differently on both sides:

```
$ helix merge other
Merge has 1 conflict(s); fix and run 'helix commit -m "merge"'
helix: merge conflicts in 1 file(s)

$ cat f.txt
alpha
<<<<<<< ours
BETA-OURS
=======
BETA-THEIRS
>>>>>>> theirs
gamma
```

Resolve manually, then commit. `MERGE_HEAD` is preserved so the next commit becomes a 2-parent merge.

---

## 10. `clone` — make a working copy

```sh
$ helix clone /path/to/source-repo myclone
Cloned 7 objects, 1 branches into myclone

$ cd myclone
$ helix log -n 1
... full history is present ...
$ helix remote
origin
```

This MVP uses filesystem (`file://`) transport. The design's gRPC transport (DESIGN.md §3.8) has the same shape — refs and objects copied, `origin` configured automatically.

---

## 11. `fetch` — pull refs and objects without merging

```sh
$ helix clone /path/to/source mywork && cd mywork
# (someone commits in source...)
$ helix fetch
Fetched 3 objects, 1 remote branches updated

$ cat .helix/refs/remotes/origin/main      # updated
$ cat .helix/refs/branches/main             # unchanged — local branch hasn't moved
```

`fetch` updates `refs/remotes/<name>/<branch>` only. Your local branch is untouched.

---

## 12. `pull` — fetch + fast-forward

```sh
$ helix pull
Fetched 3 objects, 1 remote branches updated
Fast-forwarded main to dbf6eb652a82
```

Equivalent to `helix fetch && helix merge --ff-only origin/main`. Refuses if the working tree is dirty or if the local branch has diverged (you must merge or rebase manually in that case).

---

## 13. `push` — send commits to a remote

```sh
$ echo "new feature" > f.txt && helix commit -m "Feature"
$ helix push
Pushed 3 objects, main -> origin/main (6d9885ea14da)
```

Push refuses non-fast-forward updates:

```
$ helix push
helix: non-fast-forward push refused; pull and merge first
```

The design does not enable a `--force` flag in this MVP. For a destructive replace, the design proposes `push --replace` (DESIGN.md §7.3) — explicit verb, not a flag.

---

## 14. `rebase` — replay commits on a new base, preserving change-ids

```sh
$ helix log -n 4
commit c5a8...   change cs-feat-2  feature 2
commit 8211...   change cs-feat-1  feature 1
commit 9201...   change cs-base    base

$ helix rebase main
Rebased 2 commit(s) onto 229b5b5a7da0

$ helix log -n 5
commit d10a...   change cs-feat-2  feature 2     ← same change-id
commit f163...   change cs-feat-1  feature 1     ← same change-id
commit 229b...   change cs-main2   main work 2
commit 81af...   change cs-main1   main work 1
commit 9201...   change cs-base    base
```

Commit hashes change (different parents); change-ids do not. Reviewers see "patch set N+1 of the same change" instead of "two new orphan commits".

---

## 15. `stash` — save WIP as a commit

```sh
$ echo "WIP edit" > f.txt
$ helix status
~ f.txt

$ helix stash
Stashed working tree at fce832f3297c

$ cat f.txt              # back to HEAD's content
(committed content)

$ helix stash list
stash@{0}: WIP on main

$ helix stash pop
Stash applied.

$ cat f.txt              # WIP returns
WIP edit
```

The stash is a single ref, `refs/stash`, pointing at a real commit object. The design (§2) argues that committing WIP directly is cleaner — stash is here for compatibility, and the command says so on push.

---

## Running the tests

Each example above has a corresponding test in `integration_test.go`. Run them all:

```sh
$ go test -v
=== RUN   TestCmd_Init
--- PASS: TestCmd_Init
=== RUN   TestCmd_Status
--- PASS: TestCmd_Status
... (15 integration tests + 5 unit tests, all pass) ...
ok      helix    0.2s
```
