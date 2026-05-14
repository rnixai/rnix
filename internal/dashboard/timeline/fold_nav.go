package timeline

import "sort"

// FoldGroup represents a collapsible range in the filtered events list.
// Key is the ExpandedAggGroups map key (first step number of the group).
type FoldGroup struct {
	StartIdx int
	EndIdx   int
	Key      int
}

// BuildVisibleIndices returns the sorted filtered indices that are visible
// given the current fold state. Collapsed groups contribute only their
// StartIdx; expanded groups (or non-group items) contribute all indices.
func BuildVisibleIndices(filteredLen int, expandedGroups map[int]bool, groups []FoldGroup) []int {
	skip := make(map[int]bool)
	for _, g := range groups {
		if !expandedGroups[g.Key] {
			for i := g.StartIdx + 1; i < g.EndIdx; i++ {
				skip[i] = true
			}
		}
	}
	visible := make([]int, 0, filteredLen-len(skip))
	for i := range filteredLen {
		if !skip[i] {
			visible = append(visible, i)
		}
	}
	return visible
}

// NextVisibleIndex moves delta steps through visibleIndices from the
// current filtered index. Clamps to first/last visible on overflow.
// If current is not on a visible index (inside a collapsed group),
// it snaps to the nearest visible in the direction of movement.
func NextVisibleIndex(current int, visibleIndices []int, delta int) int {
	if len(visibleIndices) == 0 {
		return current
	}
	pos := sort.SearchInts(visibleIndices, current)
	if pos < len(visibleIndices) && visibleIndices[pos] == current {
		newPos := pos + delta
		return visibleIndices[clampIdx(newPos, len(visibleIndices))]
	}
	// current is inside a collapsed group — snap to nearest visible
	if delta >= 0 {
		return visibleIndices[clampIdx(pos, len(visibleIndices))]
	}
	return visibleIndices[clampIdx(pos-1, len(visibleIndices))]
}

// VisiblePosition returns the position within visibleIndices that
// corresponds to the given filtered index (or the nearest preceding one).
func VisiblePosition(current int, visibleIndices []int) int {
	if len(visibleIndices) == 0 {
		return 0
	}
	pos := sort.SearchInts(visibleIndices, current)
	if pos < len(visibleIndices) && visibleIndices[pos] == current {
		return pos
	}
	if pos > 0 {
		return pos - 1
	}
	return 0
}

// FindGroupBoundary returns the StartIdx of the next (direction>0) or
// previous (direction<0) group relative to current. Returns current if
// no group boundary is found in the requested direction.
func FindGroupBoundary(current int, groups []FoldGroup, direction int) int {
	if direction > 0 {
		for _, g := range groups {
			if g.StartIdx > current {
				return g.StartIdx
			}
		}
	} else {
		for i := len(groups) - 1; i >= 0; i-- {
			if groups[i].StartIdx < current {
				return groups[i].StartIdx
			}
		}
	}
	return current
}

func clampIdx(i, n int) int {
	if i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}
