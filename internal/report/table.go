// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package report

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// The writer prints a row as f"{label:<26}" followed by f" {cell:>20}" per
// column. Python measures width in characters, so everything here works on
// runes: the "±" in a cell is one character wide but two bytes long.
const (
	labelWidth = 26
	colWidth   = 20
)

// decodeRow inverts the writer for one table line. It returns the label and
// one text per column, with "" for a blank column. A negative ncols decodes a
// header, where the column count is whatever the line holds.
//
// Python never truncates, so a label or a cell longer than its field widens
// the line instead. The label is found first because it fixes where the first
// column starts. Known labels are tried longest first and the fixed width of 26
// is the fallback for any other label.
func decodeRow(line string, ncols int) (label string, cells []string, err error) {
	runes := []rune(line)
	for _, known := range knownLabels {
		if !strings.HasPrefix(line, known) {
			continue
		}
		n := utf8.RuneCountInString(known)
		width := max(labelWidth, n)
		if !blank(runes, n, width) {
			continue
		}
		cells, cerr := decodeCells(runes, width, ncols)
		if cerr == nil {
			return known, cells, nil
		}
		if err == nil {
			err = cerr
		}
	}
	label = strings.TrimRight(string(runes[:min(labelWidth, len(runes))]), " ")
	if label == "" || label[0] == ' ' {
		return "", nil, fmt.Errorf("no label in the first %d characters", labelWidth)
	}
	cells, cerr := decodeCells(runes, labelWidth, ncols)
	if cerr != nil {
		if err == nil {
			err = cerr
		}
		return "", nil, err
	}
	return label, cells, nil
}

// decodeCells walks the columns left to right from position p, where the first
// column starts. A column is a space plus a right-aligned field, so a token
// belongs to the current column exactly when it ends where that field ends.
// Any other token has to belong to a later column, which means the current one
// is blank and the writer filled it with 21 spaces.
func decodeCells(runes []rune, p, ncols int) ([]string, error) {
	var cells []string
	for ncols < 0 || len(cells) < ncols {
		start := p
		for start < len(runes) && runes[start] == ' ' {
			start++
		}
		if start >= len(runes) {
			// Trailing blank columns are trimmed when a file loses its trailing
			// whitespace, so a line that ends early is not an error here.
			for ncols >= 0 && len(cells) < ncols {
				cells = append(cells, "")
			}
			break
		}
		end := tokenEnd(runes, start)
		if end == p+1+max(colWidth, end-start) {
			cells = append(cells, string(runes[start:end]))
			p = end
			continue
		}
		if !blank(runes, p, p+1+colWidth) {
			return nil, fmt.Errorf("cell %q is not aligned with column %d", string(runes[start:end]), len(cells)+1)
		}
		cells = append(cells, "")
		p += 1 + colWidth
	}
	if !blank(runes, p, len(runes)) {
		return nil, fmt.Errorf("text after the last column")
	}
	return cells, nil
}

// tokenEnd returns where the token starting at start ends. A measurement is
// "mean ±sd", which has a space inside it but still occupies one column.
func tokenEnd(runes []rune, start int) int {
	end := start
	for end < len(runes) && runes[end] != ' ' {
		end++
	}
	if end+1 < len(runes) && runes[end+1] == '±' {
		end++
		for end < len(runes) && runes[end] != ' ' {
			end++
		}
	}
	return end
}

// blank reports whether runes[from:to] holds only spaces. Positions past the
// end of the line count as spaces.
func blank(runes []rune, from, to int) bool {
	for i := from; i < to && i < len(runes); i++ {
		if runes[i] != ' ' {
			return false
		}
	}
	return true
}
