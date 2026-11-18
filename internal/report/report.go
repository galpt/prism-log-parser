// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

// Package report parses one PANDEMONIUM PRISM-FRAME benchmark log into typed
// values.
//
// The log is what write_report in tests/prism-frame.py produces: a preamble,
// one fixed-width table per scenario, a footer, and optionally the population
// comparison text that montauk prints. The tables are padded with Python
// format widths, so a cell wider than its column pushes everything after it to
// the right. Cells are therefore recovered by column position and never by
// splitting on spaces. Parsing is strict on purpose: a line the parser does not
// understand is an error, because a silently skipped row would put a wrong
// number in a chart.
package report

// Direction tells whether a larger value of a metric is the better one.
type Direction int

// The zero value is Unknown so that a metric missing from the registry never
// claims a direction it does not have.
const (
	Unknown Direction = iota
	HigherIsBetter
	LowerIsBetter
)

// Verdict is montauk's multiple-comparisons-with-the-best result for one
// scheduler. The zero value is Best, so a Standing is only meaningful when its
// Present field is set.
type Verdict int

const (
	Best Verdict = iota
	Tied
	NotBest
)

// Report is one parsed log.
type Report struct {
	// Source is the name the log was parsed under, normally its path.
	Source    string
	Meta      Meta
	Scenarios []Scenario
	// Notes holds the text of each AUDIO line, without the "AUDIO: " tag.
	Notes      []string
	Skipped    []Skip
	Population []Family
	// PopulationText holds the lines montauk printed instead of a comparison,
	// which prism-frame pastes from montauk's stderr when its stdout is empty.
	PopulationText []string
}

// Meta is the preamble of the log.
type Meta struct {
	Version string
	Commit  string
	// Dirty is set when the commit carried the " (dirty)" suffix, which is
	// stripped from Commit.
	Dirty      bool
	Timestamp  string
	CPUs       int
	Iterations int
	// Calibration is in iterations per microsecond.
	Calibration float64
	// Drift is the end-of-run calibration drift in percent, as printed
	// (+0.1 means +0.1%). It is only meaningful when HasDrift is set.
	Drift    float64
	HasDrift bool
}

// Scenario is one SCENARIO block. Name is the upper-case name as printed.
type Scenario struct {
	Name       string
	Schedulers []string
	Rows       []Row
	Deltas     []Delta
}

// Row is one metric line. Cells is parallel to Scenario.Schedulers.
type Row struct {
	Metric Metric
	Cells  []Cell
}

// Delta is one "vs EEVDF" line. Cells is parallel to Scenario.Schedulers and
// the EEVDF column is always Valid=false, since it is the baseline.
type Delta struct {
	Metric Metric
	Cells  []DeltaCell
}

// Metric names a table row. Label excludes the unit, so "FRAME TIME p99 ms"
// is Label "FRAME TIME p99" with Unit "ms".
type Metric struct {
	Label     string
	Unit      string
	Direction Direction
}

// Cell is one "mean ±sd" measurement. Text is exactly what was printed. Valid
// is false for a "-" cell and for a value that is not finite.
type Cell struct {
	Mean, SD float64
	Text     string
	Valid    bool
}

// DeltaCell is one percent change against EEVDF, such as "+12.8%". Percent
// keeps the sign of a "-0.0%" cell. Valid is false for a blank cell and for a
// value that is not finite.
type DeltaCell struct {
	Percent float64
	Text    string
	Valid   bool
}

// Skip is one SKIPPED footer line.
type Skip struct {
	Scheduler, Reason string
}

// Family is one METRIC block of the POPULATION section. Key and Direction come
// from the family name pandemonium_frame_{higher|lower}_<key> and are empty
// and Unknown for any other name.
type Family struct {
	Name      string
	Key       string
	Direction Direction
	Groups    []Group
}

// Group is one cell of a family, such as one scenario. Labels holds the
// k=v pairs of the "[...]" header and is empty for "(no labels)".
type Group struct {
	Labels    map[string]string
	Pairs     []Pair
	Only      []Only
	Standings []Standing
}

// Pair is one "A vs B" line. In montauk's vs-best selection A is the winner
// of the group.
type Pair struct {
	A, B              string
	NA, NB            int
	MeanA, MinA, MaxA float64
	MeanB, MinB, MaxB float64
	Cliff             float64
	Magnitude         string
	P                 float64
	// Power is the run count montauk estimates for 80% power. PowerAbove is
	// set when montauk printed ">20", and Power is then zero.
	Power      int
	PowerAbove bool
}

// Only is a cell with a single group and so nothing to compare. Scheduler is
// empty for the "only (none)" form, which has no other fields.
type Only struct {
	Scheduler      string
	N              int
	Mean, Min, Max float64
}

// Standing is the MCB verdict for one scheduler of a group.
type Standing struct {
	Scheduler string
	Verdict   Verdict
}
