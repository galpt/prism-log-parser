// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

// Package compare combines several parsed PRISM-FRAME logs into column sets
// that a chart can draw side by side.
//
// Every scheduler of every log becomes one column, so the same scheduler
// measured in two logs stays two columns. Scenarios, metric rows and
// population families are matched by name across the logs, and a cell that a
// log does not have is left invalid instead of being guessed.
package compare

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/galpt/prism-log-parser/internal/report"
)

// Column is one scheduler of one log. Log is the index of the log in the slice
// given to New, and Label is unique within the Set.
type Column struct {
	Log       int
	Scheduler string
	Label     string
}

// Set is the combination of several logs. It is read-only after New, and the
// slices it returns must not be modified.
type Set struct {
	columns   []Column
	scenarios []ScenarioTable
	summary   SummaryTable
	diffs     []string
}

// logColumns locates the columns of one log inside the Set's column list.
type logColumns struct {
	first int
	index map[string]int
}

// New combines the logs in the order given, which is the order of the columns.
func New(reports []*report.Report) (*Set, error) {
	if len(reports) == 0 {
		return nil, fmt.Errorf("no logs to compare")
	}
	for i, r := range reports {
		if r == nil {
			return nil, fmt.Errorf("log %d is nil", i)
		}
	}
	s := &Set{}
	logs := s.buildColumns(reports)
	s.buildScenarios(reports, logs)
	summary, err := buildSummary(reports, s.columns, logs, s.scenarios)
	if err != nil {
		return nil, err
	}
	s.summary = summary
	s.diffs = differences(reports)
	return s, nil
}

// Columns returns every (log, scheduler) pair: logs in the order given, then
// the schedulers of each log in the order they first appear in its tables.
func (s *Set) Columns() []Column { return s.columns }

// Scenarios returns one table per scenario name, in order of first appearance.
func (s *Set) Scenarios() []ScenarioTable { return s.scenarios }

// Summary returns the montauk verdicts of every log.
func (s *Set) Summary() SummaryTable { return s.summary }

// Differences lists the preamble fields that are not the same in every log, one
// line per field, such as "CPUS differs: 16 (a.log), 8 (b.log)". It is empty
// when the logs agree. Logs are named by their Source.
func (s *Set) Differences() []string { return s.diffs }

// schedulers returns the schedulers of one log, the union over its scenarios in
// first-seen order.
func schedulers(r *report.Report) []string {
	var names []string
	seen := map[string]bool{}
	for _, sc := range r.Scenarios {
		for _, n := range sc.Schedulers {
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	return names
}

// buildColumns lays out the columns and gives each a label.
//
// A name that only one log has is used as it is. Otherwise the log's timestamp
// is added, so the same scheduler in two runs reads "EEVDF (20260101-120000)".
// Passing one log twice still leaves two equal labels, and those get " #k"
// with k the 1-based position of the log.
//
// A scheduler name can hold spaces, parentheses and digits, so a plain name can
// look exactly like a stamped or numbered label of another column, and those
// three rules alone can not promise distinct labels. The last pass does. It
// visits the columns in order and keeps the set of every label handed out so
// far. A column whose label is in the set gets " #k" after it, and if that is
// taken too, "-2", "-3" and so on after the number. The candidates for one
// column are pairwise different strings and the set is finite, so one of them
// is free and the search ends. Each label enters the set only after it was
// found absent, so no two columns end up with the same label. When the first
// rules already gave every column its own label, this pass changes nothing.
func (s *Set) buildColumns(reports []*report.Report) []logColumns {
	logs := make([]logColumns, len(reports))
	holders := map[string]int{}
	for i, r := range reports {
		names := schedulers(r)
		logs[i] = logColumns{first: len(s.columns), index: map[string]int{}}
		for j, n := range names {
			logs[i].index[n] = j
			holders[n]++
			s.columns = append(s.columns, Column{Log: i, Scheduler: n})
		}
	}
	labeled := map[string]int{}
	for i := range s.columns {
		c := &s.columns[i]
		c.Label = c.Scheduler
		if holders[c.Scheduler] > 1 {
			c.Label += " (" + reports[c.Log].Meta.Timestamp + ")"
		}
		labeled[c.Label]++
	}
	for i := range s.columns {
		c := &s.columns[i]
		if labeled[c.Label] > 1 {
			c.Label += " #" + strconv.Itoa(c.Log+1)
		}
	}
	taken := map[string]bool{}
	for i := range s.columns {
		c := &s.columns[i]
		c.Label = freeLabel(taken, c.Label, c.Log+1)
		taken[c.Label] = true
	}
	return logs
}

// freeLabel returns label if no earlier column has it. Otherwise it returns the
// first of "label #k", "label #k-2", "label #k-3" and so on that is free.
func freeLabel(taken map[string]bool, label string, k int) string {
	if !taken[label] {
		return label
	}
	numbered := label + " #" + strconv.Itoa(k)
	if !taken[numbered] {
		return numbered
	}
	for n := 2; ; n++ {
		if candidate := numbered + "-" + strconv.Itoa(n); !taken[candidate] {
			return candidate
		}
	}
}

// differences compares the preamble fields that make two runs incomparable.
func differences(reports []*report.Report) []string {
	fields := []struct {
		name  string
		value func(m report.Meta) string
	}{
		{"VERSION", func(m report.Meta) string { return m.Version }},
		{"COMMIT", func(m report.Meta) string {
			if m.Dirty {
				return m.Commit + " (dirty)"
			}
			return m.Commit
		}},
		{"CPUS", func(m report.Meta) string { return strconv.Itoa(m.CPUs) }},
		{"ITERATIONS", func(m report.Meta) string { return strconv.Itoa(m.Iterations) }},
	}
	var out []string
	for _, f := range fields {
		var values []string
		sources := map[string][]string{}
		for _, r := range reports {
			v := f.value(r.Meta)
			if _, ok := sources[v]; !ok {
				values = append(values, v)
			}
			sources[v] = append(sources[v], r.Source)
		}
		if len(values) < 2 {
			continue
		}
		parts := make([]string, len(values))
		for i, v := range values {
			parts[i] = v + " (" + strings.Join(sources[v], ", ") + ")"
		}
		out = append(out, f.name+" differs: "+strings.Join(parts, ", "))
	}
	return out
}
