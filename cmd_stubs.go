package main

import (
	"fmt"
	"os"
)

// stubMsg lists git commands that are recognized but not yet implemented in
// the MVP. Each one prints a clear message pointing to the design doc rather
// than silently no-op'ing.
var stubMsg = map[string]string{
	"clone":      "network protocol not implemented in MVP (DESIGN.md §3.8). For now, copy the .helix directory directly.",
	"fetch":      "network protocol not implemented (DESIGN.md §3.8). No remote object transfer yet.",
	"pull":       "network protocol not implemented (DESIGN.md §3.8). No remote object transfer yet.",
	"push":       "network protocol not implemented (DESIGN.md §3.8). Use the design's gRPC API once the server is built.",
	"rebase":     "rebase is in DESIGN.md §6.6 but not in the MVP. Use cherry-pick + reset for a manual rebase.",
	"stash":      "Helix has no stash by design (DESIGN.md §2). The working copy IS a commit; commit your work in progress and amend later.",
	"blame":      "blame not implemented in MVP. Walks history annotating each line with last-touching commit.",
	"bisect":     "bisect not implemented in MVP. State machine for binary-searching a regression.",
	"submodule":  "Helix replaces submodules with scope-imports (DESIGN.md §10.4). Use those instead.",
	"worktree":   "multi-worktree support not implemented in MVP.",
	"reflog":     "Helix uses an op-log (DESIGN.md §3.3, §6.5) instead of a reflog. `helix op log` once implemented.",
	"gc":         "online GC is in DESIGN.md §5.4 but not in the MVP. Objects accumulate until then.",
	"fsck":       "verify-on-read is enabled; full fsck command not yet wired up.",
	"archive":    "archive not implemented in MVP.",
	"format-patch": "format-patch not implemented; design favors patch-set objects (DESIGN.md §4.6).",
	"am":           "applying mailbox-format patches not implemented; use cherry-pick with a local commit.",
	"apply":        "applying raw diffs not implemented in MVP.",
	"shortlog":     "shortlog not implemented; use log + grep.",
	"describe":     "git describe (find tag relative to HEAD) not implemented; helix describe sets a commit message in DESIGN.md §6.1 (note: not the same verb as git's).",
	"grep":         "grep not implemented in MVP; use ripgrep over the working tree.",
	"notes":        "notes not implemented; comments in helix attach to changes (DESIGN.md §8.4).",
	"whatchanged":  "whatchanged not implemented; use log + show.",
	"reset-hash":   "(internal) — see `helix reset`.",
}

func cmdStub(name string) error {
	msg, ok := stubMsg[name]
	if !ok {
		msg = "not implemented in this MVP"
	}
	fmt.Fprintf(os.Stderr, "helix: '%s' is recognized but not implemented.\n  → %s\n", name, msg)
	return fmt.Errorf("command not implemented")
}
