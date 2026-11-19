// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package report

import (
	"fmt"
	"strings"
)

// maxSplitCells bounds the table that headerSplit fills. A real header is a
// few hundred characters with a handful of columns, so this only stops an
// absurd line from exhausting memory.
const maxSplitCells = 1 << 24

// A scheduler name is printed as one column, the field " " + name.rjust(20).
// Python never truncates, so a name of 20 characters or fewer fills a field of
// exactly colWidth+1 characters, and a longer name makes the field " " + name.
// A name has no leading or trailing space but may hold any number of inner
// spaces, so where one field ends and the next begins can not be read off the
// line alone. headerSplit finds every way to cut the line into fields, and the
// column count of the rows below the header then picks the right one.
type headerSplit struct {
	runes []rune
	// ways[k][p] is the number of ways to cut runes[p:] into exactly k fields.
	// It saturates at 2, since callers only need to tell none, one and many.
	ways [][]uint8
}

// newHeaderSplit prepares the cuts of the part of a header row that follows
// the label. A field is at least colWidth+1 runes long, which bounds how many
// fields the line can hold.
//
// The count for k fields at position p adds two cases. A field of exactly
// colWidth+1 runes starts with a space and ends in a non-space. An overflow
// field starts with a space, has a non-space right after it, ends in a
// non-space and is longer than colWidth+1 runes. After either kind the line
// ends or the next field begins, and a field always begins with a space, so
// the count of the next position is zero unless it starts with one. The
// overflow case may end at any later position, so a running sum of the counts
// at the positions where a field can end keeps each step constant time.
func newHeaderSplit(runes []rune) (*headerSplit, error) {
	maxFields := len(runes) / (colWidth + 1)
	if (maxFields+1)*(len(runes)+1) > maxSplitCells {
		return nil, fmt.Errorf("header is too long to split into columns")
	}
	h := &headerSplit{runes: runes, ways: make([][]uint8, maxFields+1)}
	for k := range h.ways {
		h.ways[k] = make([]uint8, len(runes)+1)
	}
	h.ways[0][len(runes)] = 1
	for k := 1; k <= maxFields; k++ {
		prev, cur := h.ways[k-1], h.ways[k]
		var overflow uint8
		for p := len(runes) - 1; p >= 0; p-- {
			if end := p + colWidth + 2; end <= len(runes) && runes[end-1] != ' ' {
				overflow = min(2, overflow+prev[end])
			}
			if runes[p] != ' ' {
				continue
			}
			var n uint8
			if h.fixedFits(p) {
				n += prev[p+colWidth+1]
			}
			if p+1 < len(runes) && runes[p+1] != ' ' {
				n += overflow
			}
			cur[p] = min(2, n)
		}
	}
	return h, nil
}

// fixedFits reports whether a field of colWidth+1 runes can start at p.
func (h *headerSplit) fixedFits(p int) bool {
	end := p + colWidth + 1
	return h.runes[p] == ' ' && end <= len(h.runes) && h.runes[end-1] != ' '
}

// count returns how many cuts have n fields, saturated at 2. A negative n
// counts the cuts of every width. The empty cut is left out, because a header
// without a scheduler is refused before the line gets here.
func (h *headerSplit) count(n int) int {
	if n >= 0 {
		if n >= len(h.ways) {
			return 0
		}
		return int(h.ways[n][0])
	}
	total := 0
	for k := 1; k < len(h.ways); k++ {
		total += int(h.ways[k][0])
	}
	return min(2, total)
}

// names returns the cut with n fields, or for a negative n the only cut of any
// width. The caller has checked that count(n) is exactly 1, so at each step
// exactly one field leads on to a complete cut.
func (h *headerSplit) names(n int) []string {
	if n < 0 {
		for k := 1; k < len(h.ways); k++ {
			if h.ways[k][0] > 0 {
				n = k
			}
		}
	}
	var names []string
	p := 0
	for left := n; left > 0; left-- {
		next := h.ways[left-1]
		if h.fixedFits(p) && next[p+colWidth+1] > 0 {
			end := p + colWidth + 1
			names = append(names, strings.TrimLeft(string(h.runes[p+1:end]), " "))
			p = end
			continue
		}
		for end := p + colWidth + 2; end <= len(h.runes); end++ {
			if h.runes[p] == ' ' && h.runes[p+1] != ' ' && h.runes[end-1] != ' ' && next[end] > 0 {
				names = append(names, string(h.runes[p+1:end]))
				p = end
				break
			}
		}
	}
	return names
}
