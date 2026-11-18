// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package report

import (
	"fmt"
	"math"
	"math/rand"
	"slices"
	"strings"
	"testing"
)

// tableRow is a Go copy of table_row in pandemonium_common.py as prism-frame
// calls it, with a label width of 26 and a column width of 20. Like the
// original it pads and never truncates.
func tableRow(label string, cells ...string) string {
	line := fmt.Sprintf("%-*s", labelWidth, label)
	for _, c := range cells {
		line += fmt.Sprintf(" %*s", colWidth, c)
	}
	return line
}

func check[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func randomMeasurement(rng *rand.Rand) string {
	// Magnitudes from 1e-3 to 1e30 make cells that are narrower than, equal to
	// and much wider than the 20 character column.
	mean := math.Pow(10, rng.Float64()*33-3)
	sd := mean * rng.Float64()
	if rng.Intn(4) == 0 {
		mean = -mean
	}
	prec := rng.Intn(3)
	return fmt.Sprintf("%.*f ±%.*f", prec, mean, prec, sd)
}

func randomDelta(rng *rand.Rand) string {
	pct := math.Pow(10, rng.Float64()*25-2)
	if rng.Intn(2) == 0 {
		pct = -pct
	}
	return fmt.Sprintf("%+.1f%%", pct)
}

func randomName(rng *rand.Rand) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz_0123456789éß"
	runes := []rune(alphabet)
	n := 1 + rng.Intn(40)
	name := make([]rune, n)
	for i := range name {
		name[i] = runes[rng.Intn(len(runes))]
	}
	return string(name)
}

func randomLabel(rng *rand.Rand) string {
	switch rng.Intn(3) {
	case 0:
		return knownLabels[rng.Intn(len(knownLabels))]
	case 1:
		return "AVG FPS (x)"
	}
	return fmt.Sprintf("CUSTOM %c THING %d", 'A'+rng.Intn(26), rng.Intn(100))
}

// TestDecodeRowRoundTrip checks that the decoder inverts the writer for random
// rows of every width, including cells much wider than their column.
func TestDecodeRowRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(20261119))
	for i := 0; i < 20000; i++ {
		ncols := 1 + rng.Intn(6)
		label := randomLabel(rng)
		cells := make([]string, ncols)
		for c := range cells {
			switch rng.Intn(6) {
			case 0:
				cells[c] = "-"
			case 1:
				cells[c] = ""
			case 2:
				cells[c] = randomDelta(rng)
			default:
				cells[c] = randomMeasurement(rng)
			}
		}
		line := tableRow(label, cells...)
		for _, trimmed := range []string{line, strings.TrimRight(line, " ")} {
			gotLabel, got, err := decodeRow(trimmed, ncols)
			if err != nil {
				t.Fatalf("row %d: %v\n%q", i, err, trimmed)
			}
			if gotLabel != label || !slices.Equal(got, cells) {
				t.Fatalf("row %d: got %q %q, want %q %q\n%q", i, gotLabel, got, label, cells, trimmed)
			}
		}
	}
}

// TestDecodeRowEveryWidth puts a cell of each width from 1 to 45 characters
// into each position, so the widths around the 20 character column and the
// blank columns next to a wide cell are all covered.
func TestDecodeRowEveryWidth(t *testing.T) {
	for width := 1; width <= 45; width++ {
		cell := strings.Repeat("7", width)
		for pos := 0; pos < 3; pos++ {
			cells := []string{"", "", ""}
			cells[pos] = cell
			line := tableRow("AVG FPS", cells...)
			_, got, err := decodeRow(line, 3)
			if err != nil || !slices.Equal(got, cells) {
				t.Errorf("width %d pos %d: got %q, %v", width, pos, got, err)
			}
		}
	}
}

func TestDecodeHeaderRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 2000; i++ {
		names := make([]string, 1+rng.Intn(8))
		for j := range names {
			names[j] = randomName(rng)
		}
		label, got, err := decodeRow(tableRow("METRIC", names...), -1)
		if err != nil || label != "METRIC" || !slices.Equal(got, names) {
			t.Fatalf("header %q: got %q %q, %v", names, label, got, err)
		}
	}
}

// TestDecodeRowLongLabel covers a label wider than 26 characters, where the
// writer drops the padding and the first column follows after one space.
func TestDecodeRowLongLabel(t *testing.T) {
	long := "A SYNTHETIC LABEL OF THIRTY-ONE"
	saved := knownLabels
	t.Cleanup(func() { knownLabels = saved })
	knownLabels = append([]string{long}, saved...)
	cells := []string{"1.00 ±0.10", "", "22222222222222222222222.0 ±1.0", "-"}
	label, got, err := decodeRow(tableRow(long, cells...), len(cells))
	if err != nil || label != long || !slices.Equal(got, cells) {
		t.Fatalf("got %q %q, %v", label, got, err)
	}
}

func TestDecodeRowRejects(t *testing.T) {
	good := tableRow("AVG FPS", "1.00 ±0.10", "2.00 ±0.20")
	cases := []struct {
		name  string
		line  string
		ncols int
	}{
		{"cell shifted left", strings.Replace(good, "  2.00", " 2.00 ", 1), 2},
		{"cell shifted right", strings.Replace(good, "1.00 ±0.10 ", " 1.00 ±0.10", 1), 2},
		{"extra column", good + " " + fmt.Sprintf("%20s", "3.00 ±0.30"), 2},
		{"text after the last column", good + " x", 2},
		{"no label", strings.Repeat(" ", 30) + "1.00 ±0.10", 1},
		{"tab in a gap", strings.Replace(good, "  2.00", "\t 2.00", 1), 2},
	}
	for _, c := range cases {
		if _, _, err := decodeRow(c.line, c.ncols); err == nil {
			t.Errorf("%s: no error for %q", c.name, c.line)
		}
	}
}
