// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package report

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// baseline is the scheduler every "vs EEVDF" row is measured against.
const baseline = "EEVDF"

// scenario returns the block being filled. It is looked up by position each
// time because appending a new scenario moves the slice.
func (p *parser) scenario() *Scenario {
	return &p.rep.Scenarios[len(p.rep.Scenarios)-1]
}

// scenarioStart expects the next SCENARIO line, or the footer line once the
// last table is done.
func scenarioStart(p *parser, line string) (stateFn, error) {
	if line == footerLine {
		if len(p.rep.Scenarios) == 0 {
			return nil, p.fail("footer before any SCENARIO block")
		}
		p.footerSeen = true
		return footer, nil
	}
	name, ok := strings.CutPrefix(line, "SCENARIO ")
	if !ok || name == "" || strings.ContainsFunc(name, unicode.IsSpace) {
		return nil, p.fail("expected \"SCENARIO <NAME>\" or the footer line")
	}
	for _, s := range p.rep.Scenarios {
		if s.Name == name {
			return nil, p.fail("scenario %s appears twice", name)
		}
	}
	p.rep.Scenarios = append(p.rep.Scenarios, Scenario{Name: name})
	p.seen = map[string]bool{}
	return scenarioBody, nil
}

// pendingHeader is a header row whose scheduler names are not known yet. The
// names can only be told apart once the rows below the header give the column
// count, so the line waits here, with its position for error messages.
type pendingHeader struct {
	line  int
	text  string
	split *headerSplit
}

// scenarioBody handles the header row, then the metric rows and the delta rows
// up to the blank line that closes the table. The header is resolved when the
// first row arrives, or at the blank line if the table has no rows.
func scenarioBody(p *parser, line string) (stateFn, error) {
	sc := p.scenario()
	if line == "" {
		switch {
		case p.head != nil:
			return scenarioStart, p.resolveHeader(sc, -1)
		case sc.Schedulers == nil:
			return nil, p.fail("scenario %s has no METRIC header", sc.Name)
		}
		return scenarioStart, nil
	}
	if sc.Schedulers == nil && p.head == nil {
		return scenarioBody, p.header(line)
	}
	if p.head != nil {
		if err := p.firstRow(sc, line); err != nil {
			return nil, err
		}
	}
	return scenarioBody, p.tableRow(sc, line)
}

// header checks the METRIC label and keeps the rest of the line for later.
func (p *parser) header(line string) error {
	runes := []rune(line)
	label := strings.TrimRight(string(runes[:min(labelWidth, len(runes))]), " ")
	if label != "METRIC" {
		return p.fail("expected the METRIC header row")
	}
	rest := runes[min(labelWidth, len(runes)):]
	if len(rest) == 0 {
		return p.fail("header has no scheduler columns")
	}
	split, err := newHeaderSplit(rest)
	if err != nil {
		return p.fail("bad header row: %v", err)
	}
	if split.count(-1) == 0 {
		return p.fail("bad header row: cannot split the header into scheduler columns")
	}
	p.head = &pendingHeader{line: p.line, text: p.text, split: split}
	return nil
}

// firstRow settles the header with the column count of the first row. A delta
// row can lose its blank trailing columns, so only a metric row is trusted to
// give the count. After a delta row the header has to have one cut overall.
func (p *parser) firstRow(sc *Scenario, line string) error {
	label, cells, err := decodeRow(line, -1)
	if err != nil {
		return p.fail("bad row: %v", err)
	}
	n := len(cells)
	if rest, ok := strings.CutPrefix(label, deltaPrefix); ok && rest != "" {
		n = -1
	}
	return p.resolveHeader(sc, n)
}

// resolveHeader picks the cut of the pending header with n scheduler columns,
// or the only cut when n is negative, and refuses a cut that is not unique.
func (p *parser) resolveHeader(sc *Scenario, n int) error {
	h := p.head
	p.head = nil
	what := "scheduler columns"
	if n >= 0 {
		what = fmt.Sprintf("%d scheduler columns", n)
	}
	switch h.split.count(n) {
	case 0:
		return p.failAt(h.line, h.text, "cannot split the header into %s", what)
	case 1:
	default:
		return p.failAt(h.line, h.text, "the header splits into %s in more than one way", what)
	}
	names := h.split.names(n)
	seen := map[string]bool{}
	for _, name := range names {
		if seen[strings.ToLower(name)] {
			return p.failAt(h.line, h.text, "scheduler %s appears twice in the header", name)
		}
		seen[strings.ToLower(name)] = true
	}
	sc.Schedulers = names
	return nil
}

func (p *parser) tableRow(sc *Scenario, line string) error {
	label, texts, err := decodeRow(line, len(sc.Schedulers))
	if err != nil {
		return p.fail("bad row: %v", err)
	}
	if p.seen[label] {
		return p.fail("row %q appears twice", label)
	}
	p.seen[label] = true
	if rest, ok := strings.CutPrefix(label, deltaPrefix); ok && rest != "" {
		return p.deltaRow(sc, rest, texts)
	}
	if len(sc.Deltas) > 0 {
		return p.fail("metric row after the vs EEVDF rows")
	}
	cells := make([]Cell, len(texts))
	for i, t := range texts {
		if cells[i], err = parseCell(t); err != nil {
			return p.fail("column %s: %v", sc.Schedulers[i], err)
		}
	}
	sc.Rows = append(sc.Rows, Row{Metric: rowMetric(label), Cells: cells})
	return nil
}

func (p *parser) deltaRow(sc *Scenario, label string, texts []string) error {
	base := slices.Index(sc.Schedulers, baseline)
	if base < 0 {
		return p.fail("vs EEVDF row but no %s column", baseline)
	}
	if texts[base] != "" {
		return p.fail("the %s column of a vs EEVDF row must be blank", baseline)
	}
	cells := make([]DeltaCell, len(texts))
	for i, t := range texts {
		var err error
		if cells[i], err = parseDelta(t); err != nil {
			return p.fail("column %s: %v", sc.Schedulers[i], err)
		}
	}
	sc.Deltas = append(sc.Deltas, Delta{Metric: deltaMetric(label), Cells: cells})
	return nil
}

// parseCell reads "-" or "mean ±sd". A blank is refused because the writer
// prints "-" for a missing measurement.
func parseCell(text string) (Cell, error) {
	if text == "-" {
		return Cell{Text: text}, nil
	}
	if text == "" {
		return Cell{}, fmt.Errorf("empty cell, a column may be missing")
	}
	mean, sd, ok := strings.Cut(text, " ±")
	if !ok {
		return Cell{}, fmt.Errorf("cell %q is not \"-\" or \"mean ±sd\"", text)
	}
	var n numReader
	c := Cell{Mean: n.float(mean), SD: n.float(sd), Text: text}
	if n.err != nil {
		return Cell{}, fmt.Errorf("cell %q has a bad number", text)
	}
	c.Valid = finite(c.Mean) && finite(c.SD)
	return c, nil
}

// parseDelta reads a signed percent such as "+12.8%". A blank is the baseline
// column, or a scheduler that has no result to compare.
func parseDelta(text string) (DeltaCell, error) {
	if text == "" {
		return DeltaCell{}, nil
	}
	num, ok := strings.CutSuffix(text, "%")
	if !ok || num == "" || (num[0] != '+' && num[0] != '-') {
		return DeltaCell{}, fmt.Errorf("cell %q is not a signed percent", text)
	}
	var n numReader
	c := DeltaCell{Percent: n.float(num), Text: text}
	if n.err != nil {
		return DeltaCell{}, fmt.Errorf("cell %q has a bad number", text)
	}
	c.Valid = finite(c.Percent)
	return c, nil
}
