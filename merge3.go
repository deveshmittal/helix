package main

// merge3 implements line-level 3-way merge.
// Returns the merged content and whether any conflict was emitted.
//
// Algorithm: walk base lines; align with ours and theirs by LCS;
// at each base segment classify into one of:
//   - both unchanged → emit base
//   - ours only changed → emit ours
//   - theirs only changed → emit theirs
//   - both changed identically → emit either
//   - both changed divergently → emit conflict markers around both
//
// This is a simplified but correct version of diff3. Suitable for the MVP;
// not optimized for very large files.

func merge3(base, ours, theirs []string) ([]string, bool) {
	bo := lcs(base, ours)
	bt := lcs(base, theirs)
	// Convert to per-base-line maps: for each base index, what does each side have?
	oursMap := alignByBase(base, ours, bo)
	theirsMap := alignByBase(base, theirs, bt)

	var out []string
	conflict := false
	bi, oi, ti := 0, 0, 0
	// First, collect any leading inserts in ours/theirs before base[0].
	leadingOurs := oursMap.leading
	leadingTheirs := theirsMap.leading
	out = append(out, conflictBlock(leadingOurs, leadingTheirs, &conflict, true)...)
	oi += len(leadingOurs)
	ti += len(leadingTheirs)

	for bi < len(base) {
		// What is base[bi]'s fate on each side?
		oursSlice := oursMap.atBase[bi]
		theirsSlice := theirsMap.atBase[bi]
		bothKept := oursSlice.kept && theirsSlice.kept
		oursDeleted := !oursSlice.kept
		theirsDeleted := !theirsSlice.kept

		if bothKept {
			out = append(out, base[bi])
		} else if oursDeleted && !theirsDeleted {
			// ours removed it; take ours' decision
		} else if !oursDeleted && theirsDeleted {
			// theirs removed it; take theirs' decision
		} else {
			// both deleted — fine, drop the line
		}
		// Append any insertions appearing after base[bi] on each side, before next base line.
		afterOurs := oursMap.afterBase[bi]
		afterTheirs := theirsMap.afterBase[bi]
		out = append(out, conflictBlock(afterOurs, afterTheirs, &conflict, true)...)
		bi++
	}
	return out, conflict
}

// conflictBlock decides how to combine inserted regions from ours/theirs.
// If they are equal, emit once. If only one side has content, emit that side.
// Otherwise, emit a conflict block. mark=false suppresses markers (used internally).
func conflictBlock(ours, theirs []string, conflict *bool, mark bool) []string {
	if len(ours) == 0 && len(theirs) == 0 {
		return nil
	}
	if len(theirs) == 0 {
		return ours
	}
	if len(ours) == 0 {
		return theirs
	}
	if equalSlices(ours, theirs) {
		return ours
	}
	if mark {
		*conflict = true
		var out []string
		out = append(out, "<<<<<<< ours\n")
		out = append(out, ours...)
		out = append(out, "=======\n")
		out = append(out, theirs...)
		out = append(out, ">>>>>>> theirs\n")
		return out
	}
	// Without markers: just take ours.
	return ours
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type sideInfo struct {
	kept bool // whether base line is kept on this side (vs deleted)
}

type aligned struct {
	leading   []string            // inserts before any base line
	atBase    map[int]sideInfo    // per base index, presence info
	afterBase map[int][]string    // inserts immediately after base[i]
}

// alignByBase, given a base list and a side list, plus their LCS,
// reports per-base-index whether the line was kept and what was inserted around it.
func alignByBase(base, side []string, common []lcsPair) aligned {
	a := aligned{
		atBase:    map[int]sideInfo{},
		afterBase: map[int][]string{},
	}
	for i := range base {
		a.atBase[i] = sideInfo{kept: false}
	}
	// Walk side and base in parallel using the common LCS list.
	// Inserts on side that aren't in common are placed relative to the most recent base index.
	bi := 0
	si := 0
	currentAfterBase := -1 // -1 means "before any base line"
	for _, p := range common {
		// All side lines from si..p.b that are inserts (relative to base) go before this match.
		for si < p.b {
			if currentAfterBase == -1 {
				a.leading = append(a.leading, side[si])
			} else {
				a.afterBase[currentAfterBase] = append(a.afterBase[currentAfterBase], side[si])
			}
			si++
		}
		// All base lines from bi..p.a that are NOT in common were deleted on this side.
		for bi < p.a {
			a.atBase[bi] = sideInfo{kept: false}
			currentAfterBase = bi
			bi++
		}
		// p.a == bi, p.b == si: matched.
		a.atBase[bi] = sideInfo{kept: true}
		currentAfterBase = bi
		bi++
		si++
	}
	// Tail base lines after last match: deleted on side.
	for bi < len(base) {
		a.atBase[bi] = sideInfo{kept: false}
		currentAfterBase = bi
		bi++
	}
	// Tail side lines after last match: inserted at end.
	for si < len(side) {
		if currentAfterBase == -1 {
			a.leading = append(a.leading, side[si])
		} else {
			a.afterBase[currentAfterBase] = append(a.afterBase[currentAfterBase], side[si])
		}
		si++
	}
	return a
}

type lcsPair struct{ a, b int }

func lcs(a, b []string) []lcsPair {
	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var out []lcsPair
	i, j := 0, 0
	for i < n && j < m {
		if a[i] == b[j] {
			out = append(out, lcsPair{i, j})
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			i++
		} else {
			j++
		}
	}
	return out
}
