// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package report

import (
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// headerLineNumber is where synthLog puts the header row of its only table.
const headerLineNumber = "10"

// synthLog is a log with one scenario whose header holds the given names, one
// metric row of made-up cells, the given SKIPPED lines and, when population is
// not empty, a POPULATION section with that body.
func synthLog(names, skipped []string, population string) string {
	full := fullLog()
	var b strings.Builder
	b.WriteString(full[:strings.Index(full, "SCENARIO ALPHA")])
	b.WriteString("SCENARIO ALPHA\n")
	b.WriteString(tableRow("METRIC", names...) + "\n")
	cells := make([]string, len(names))
	for i := range cells {
		cells[i] = "1.00 ±0.10"
	}
	b.WriteString(tableRow("AVG FPS", cells...) + "\n\n")
	b.WriteString(footerLine + "\n")
	for _, s := range skipped {
		b.WriteString("SKIPPED: " + s + "\n")
	}
	if population != "" {
		b.WriteString("\n" + populationLine + "\n" + population)
	}
	return b.String()
}

// The header of a real log has scheduler names with spaces, and
// "PANDEMONIUM (ADAPTIVE)" is wider than the column. The line below is that
// header with the names of public schedulers.
const realHeader = "METRIC                                    EEVDF    PANDEMONIUM (BPF) PANDEMONIUM (ADAPTIVE)            scx_panda             scx_flow"

// TestParseRealHeaderShape checks the header that holds both a spaced name and
// an overflowing one. The last names can also be joined by their padding, which
// makes four columns and three. Only the column count of the rows tells the
// readings apart.
func TestParseRealHeaderShape(t *testing.T) {
	five := []string{"EEVDF", "PANDEMONIUM (BPF)", "PANDEMONIUM (ADAPTIVE)", "scx_panda", "scx_flow"}
	check(t, "the copy of the writer's output", tableRow("METRIC", five...), realHeader)

	build := func(cells ...string) string {
		text := synthLog(five, nil, "")
		return strings.Replace(text, tableRow("AVG FPS", "1.00 ±0.10", "1.00 ±0.10", "1.00 ±0.10", "1.00 ±0.10", "1.00 ±0.10"),
			tableRow("AVG FPS", cells...), 1)
	}
	got := mustParse(t, build("1.00 ±0.10", "2.00 ±0.10", "3.00 ±0.10", "4.00 ±0.10", "5.00 ±0.10")).Scenarios[0]
	if !reflect.DeepEqual(got.Schedulers, five) {
		t.Errorf("five columns: schedulers = %q", got.Schedulers)
	}
	check(t, "cells", len(got.Rows[0].Cells), 5)

	four := mustParse(t, build("1.00 ±0.10", "2.00 ±0.10", "3.00 ±0.10", "4.00 ±0.10")).Scenarios[0]
	want := []string{"EEVDF", "PANDEMONIUM (BPF)", "PANDEMONIUM (ADAPTIVE)            scx_panda", "scx_flow"}
	if !reflect.DeepEqual(four.Schedulers, want) {
		t.Errorf("four columns: schedulers = %q", four.Schedulers)
	}

	// With no rows the header is accepted only if it has one cut overall, and
	// this one has three.
	table := synthLog(five, nil, "")
	noRows := table[:strings.Index(table, tableRow("AVG FPS", "1.00 ±0.10", "1.00 ±0.10", "1.00 ±0.10", "1.00 ±0.10", "1.00 ±0.10"))] +
		"\n" + table[strings.Index(table, footerLine):]
	wantErr(t, noRows, "bad.log:"+headerLineNumber+": ", "in more than one way")

	two := build("1.00 ±0.10", "2.00 ±0.10")
	wantErr(t, two, "bad.log:"+headerLineNumber+": ", "cannot split the header into 2 scheduler columns")
}

// TestParseHeaderWithoutRows covers a table that has a header and nothing
// else. A header with one possible cut is accepted.
func TestParseHeaderWithoutRows(t *testing.T) {
	names := []string{"EEVDF", "sched_alpha", "alpha (bpf)"}
	table := synthLog(names, nil, "")
	cells := make([]string, len(names))
	for i := range cells {
		cells[i] = "1.00 ±0.10"
	}
	text := strings.Replace(table, tableRow("AVG FPS", cells...)+"\n", "", 1)
	r := mustParse(t, text)
	if !reflect.DeepEqual(r.Scenarios[0].Schedulers, names) {
		t.Errorf("schedulers = %q", r.Scenarios[0].Schedulers)
	}
	check(t, "rows", len(r.Scenarios[0].Rows), 0)
}

// TestParseHeaderThenDeltaRow covers a first row that is a delta row, whose
// blank last column is trimmed and so can not give the column count.
func TestParseHeaderThenDeltaRow(t *testing.T) {
	names := []string{"EEVDF", "sched_alpha", "alpha (bpf)"}
	table := synthLog(names, nil, "")
	text := strings.Replace(table, tableRow("AVG FPS", "1.00 ±0.10", "1.00 ±0.10", "1.00 ±0.10"),
		tableRow("vs EEVDF AVG FPS", "", "+1.0%", ""), 1)
	r := mustParse(t, text)
	if !reflect.DeepEqual(r.Scenarios[0].Schedulers, names) {
		t.Errorf("schedulers = %q", r.Scenarios[0].Schedulers)
	}
	check(t, "deltas", len(r.Scenarios[0].Deltas), 1)
}

// TestHeaderSplitEveryLength puts each name length from 1 to 30 first, middle
// and last in a header of three columns, with inner runs of spaces.
func TestHeaderSplitEveryLength(t *testing.T) {
	for n := 1; n <= 30; n++ {
		name := strings.Repeat("a", n)
		if n > 2 {
			name = "a" + strings.Repeat(" ", n-2) + "a"
		}
		for _, names := range [][]string{{name, "b", "c"}, {"b", name, "c"}, {"b", "c", name}} {
			h, err := newHeaderSplit(headerRest(names))
			if err != nil {
				t.Fatal(err)
			}
			if c := h.count(3); c == 0 || (c == 1 && !slices.Equal(h.names(3), names)) {
				t.Errorf("names %q: count %d, names %q", names, c, h.names(3))
			}
		}
	}
}

func TestHeaderSplitRejects(t *testing.T) {
	cases := []struct{ name, rest string }{
		{"no leading space", "EEVDF"},
		{"cell shifted left", strings.TrimPrefix(tableRow("", "EEVDF"), strings.Repeat(" ", labelWidth+1))},
		{"blank column", tableRow("", "a", "", "b")[labelWidth:]},
		{"trailing text", tableRow("", "a")[labelWidth:] + "  b"},
	}
	for _, c := range cases {
		h, err := newHeaderSplit([]rune(c.rest))
		if err != nil {
			t.Fatal(err)
		}
		if n := h.count(-1); n != 0 {
			t.Errorf("%s: %d cuts for %q", c.name, n, c.rest)
		}
	}
	if _, err := newHeaderSplit([]rune(strings.Repeat(" a", 10000))); err == nil {
		t.Error("no error for a header of absurd length")
	}
}

func TestParseSkippedWithSpaces(t *testing.T) {
	text := synthLog([]string{"EEVDF", "alpha (bpf)"}, []string{
		"PANDEMONIUM (ADAPTIVE) -- crashed (during ALPHA)",
		"two  spaces -- fell through to EEVDF -- twice",
	}, "")
	r := mustParse(t, text)
	want := []Skip{
		{"PANDEMONIUM (ADAPTIVE)", "crashed (during ALPHA)"},
		{"two  spaces", "fell through to EEVDF -- twice"},
	}
	if !reflect.DeepEqual(r.Skipped, want) {
		t.Errorf("Skipped = %q, want %q", r.Skipped, want)
	}
	wantErr(t, synthLog([]string{"EEVDF", "alpha (bpf)"}, []string{"no separator here"}, ""), "bad.log:", "expected SKIPPED:")
}

// spacedPopulation is a POPULATION body whose names hold spaces, an equals sign
// and " vs ". The schedulers are in spacedNames.
const spacedPopulation = `METRIC pandemonium_frame_higher_fps
  [scenario=alpha]
    alpha (bpf) vs eevdf [N=5/5]: mean 1.2 (1.1..1.3) vs 1.4 (1.3..1.5) | cliff -1.00 (large) | perm p=0.008 | power 2
    a vs b vs k=v [N=5/4]: mean 1.2 (1.1..1.3) vs 1.4 (1.3..1.5) | cliff -0.50 (medium) | perm p=0.100 | power >20
    k=v vs alpha (adaptive) [N=4/5]: mean 1.2 (1.1..1.3) vs 1.4 (1.3..1.5) | cliff -0.50 (medium) | perm p=0.100 | power 3
    MCB (higher=better): eevdf=not-best alpha (adaptive)=not-best alpha (bpf)=not-best a vs b=tied k=v=best
  [scenario=beta]
    only a vs b [N=1]: mean 12.5 (12.5..12.5) -- no comparison
  [scenario=gamma]
    only alpha (adaptive) [N=2]: mean 12.5 (12.5..13.5) -- no comparison
`

var spacedNames = []string{"EEVDF", "alpha (bpf)", "alpha (adaptive)", "a vs b", "k=v"}

func TestParsePopulationWithSpaces(t *testing.T) {
	r := mustParse(t, synthLog(spacedNames, nil, spacedPopulation))
	groups := r.Population[0].Groups
	pairs := groups[0].Pairs
	check(t, "pairs", len(pairs), 3)
	check(t, "pair 1 A", pairs[0].A, "alpha (bpf)")
	check(t, "pair 1 B", pairs[0].B, "eevdf")
	check(t, "pair 1 cliff", pairs[0].Cliff, -1.0)
	check(t, "pair 2 A", pairs[1].A, "a vs b")
	check(t, "pair 2 B", pairs[1].B, "k=v")
	check(t, "pair 2 power above", pairs[1].PowerAbove, true)
	check(t, "pair 3 A", pairs[2].A, "k=v")
	check(t, "pair 3 B", pairs[2].B, "alpha (adaptive)")
	check(t, "pair 3 NB", pairs[2].NB, 5)

	want := []Standing{
		{"eevdf", NotBest}, {"alpha (adaptive)", NotBest}, {"alpha (bpf)", NotBest}, {"a vs b", Tied}, {"k=v", Best},
	}
	if !reflect.DeepEqual(groups[0].Standings, want) {
		t.Errorf("Standings = %v, want %v", groups[0].Standings, want)
	}
	check(t, "only name", groups[1].Only[0].Scheduler, "a vs b")
	check(t, "only name with parentheses", groups[2].Only[0].Scheduler, "alpha (adaptive)")
	check(t, "only max", groups[2].Only[0].Max, 13.5)
}

// TestParsePopulationRejectsNames checks that a name which is not a scheduler of
// the log's tables is an error, in each kind of line, and that so is a line
// that can be cut in two ways.
func TestParsePopulationRejectsNames(t *testing.T) {
	tail := ": mean 1.2 (1.1..1.3) vs 1.4 (1.3..1.5) | cliff -1.00 (large) | perm p=0.008 | power 2"
	cases := []struct {
		name, line, want string
		names            []string
	}{
		{"unknown first name", "    nope vs eevdf [N=5/5]" + tail, "unknown line", spacedNames},
		{"unknown second name", "    eevdf vs nope [N=5/5]" + tail, "unknown line", spacedNames},
		{"unknown only name", "    only nope [N=1]: mean 12.5 (12.5..12.5) -- no comparison", "unknown line", spacedNames},
		{"unknown MCB name", "    MCB (higher=better): eevdf=best nope=tied", "unreadable MCB entry", spacedNames},
		{"MCB with a double space", "    MCB (higher=better): eevdf=best  k=v=tied", "unreadable MCB entry", spacedNames},
		{"MCB without a verdict", "    MCB (higher=better): eevdf=best k=v", "unreadable MCB entry", spacedNames},
		{"MCB verdict glued to the next name", "    MCB (higher=better): eevdf=bestk=v=tied", "unreadable MCB entry", spacedNames},
		{"MCB name cut inside a longer one", "    MCB (higher=better): alpha=best", "unreadable MCB entry", spacedNames},
		{
			"two ways to cut a pair",
			"    a vs b vs c [N=5/5]" + tail, "more than one way",
			[]string{"EEVDF", "a", "b", "c", "a vs b", "b vs c"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := "METRIC pandemonium_frame_higher_fps\n  [scenario=alpha]\n" + c.line + "\n"
			text := synthLog(c.names, nil, body)
			line := strings.Count(text[:strings.Index(text, c.line)], "\n") + 1
			wantErr(t, text, "bad.log:"+strconv.Itoa(line)+": ", c.want)
		})
	}
}
