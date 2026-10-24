package tool

import (
	"fmt"
	"strings"
)

const (
	diffContext  = 3
	diffMaxCells = 4_000_000 // guards the O(n*m) table against huge files
)

type diffOp struct {
	kind byte // ' ' context, '-' removed, '+' added
	text string
}

func splitDiffLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// UnifiedDiff returns a unified-style diff between old and updated, or "" when
// they are identical. It is a plain longest-common-subsequence line diff.
func UnifiedDiff(oldName, newName, oldText, updated string) string {
	if oldText == updated {
		return ""
	}
	a, b := splitDiffLines(oldText), splitDiffLines(updated)
	if len(a)*len(b) > diffMaxCells {
		return fmt.Sprintf("(diff too large to preview: %d -> %d lines)", len(a), len(b))
	}
	ops := lcsOps(a, b)

	// Prefix counts let each hunk header report its starting line numbers.
	oldBefore := make([]int, len(ops)+1)
	newBefore := make([]int, len(ops)+1)
	for k, op := range ops {
		oldBefore[k+1], newBefore[k+1] = oldBefore[k], newBefore[k]
		if op.kind != '+' {
			oldBefore[k+1]++
		}
		if op.kind != '-' {
			newBefore[k+1]++
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", oldName, newName)
	for _, r := range hunkRanges(ops) {
		start, end := r[0], r[1]
		oldCount := oldBefore[end] - oldBefore[start]
		newCount := newBefore[end] - newBefore[start]
		oldStart, newStart := oldBefore[start]+1, newBefore[start]+1
		if oldCount == 0 {
			oldStart--
		}
		if newCount == 0 {
			newStart--
		}
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount)
		for _, op := range ops[start:end] {
			fmt.Fprintf(&sb, "%c%s\n", op.kind, op.text)
		}
	}
	return sb.String()
}

// lcsOps aligns a and b with a longest common subsequence.
func lcsOps(a, b []string) []diffOp {
	n, m := len(a), len(b)
	table := make([][]int32, n+1)
	for i := range table {
		table[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				table[i][j] = table[i+1][j+1] + 1
			} else if table[i+1][j] >= table[i][j+1] {
				table[i][j] = table[i+1][j]
			} else {
				table[i][j] = table[i][j+1]
			}
		}
	}
	var ops []diffOp
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{' ', a[i]})
			i++
			j++
		case table[i+1][j] >= table[i][j+1]:
			ops = append(ops, diffOp{'-', a[i]})
			i++
		default:
			ops = append(ops, diffOp{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{'-', a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{'+', b[j]})
	}
	return ops
}

// hunkRanges returns [start,end) index ranges over ops that contain changes plus
// diffContext lines on each side, merging ranges that touch.
func hunkRanges(ops []diffOp) [][2]int {
	var out [][2]int
	for k, op := range ops {
		if op.kind == ' ' {
			continue
		}
		start := max(k-diffContext, 0)
		end := min(k+diffContext+1, len(ops))
		if len(out) > 0 && start <= out[len(out)-1][1] {
			out[len(out)-1][1] = end
		} else {
			out = append(out, [2]int{start, end})
		}
	}
	return out
}
