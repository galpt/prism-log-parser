// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package compare

import (
	"fmt"
	"strings"

	"github.com/galpt/prism-log-parser/internal/report"
)

// SummaryTable lays the montauk verdicts of every log side by side. It has
// one row per population family and scenario.
type SummaryTable struct {
	Columns []Column
	Rows    []SummaryRow
}

// SummaryRow is one population family in one scenario. Family is the montauk
// family name. Metric is the table metric the family measures, taken from the
// registry by the family key, or a bare label when the key is unknown.
// Scenario is the scenario name as the tables print it. Cells is parallel to
// SummaryTable.Columns.
type SummaryRow struct {
	Family   string
	Metric   report.Metric
	Scenario string
	Cells    []Standing
}

// Standing is the verdict of one column. Present is false when the column's
// log has no verdict for it, either because the log has no POPULATION section
// or because montauk had nothing to compare for that cell. Versus is the pair
// line that compares this column with the winner of its group, or nil for the
// winner itself.
type Standing struct {
	Present bool
	Verdict report.Verdict
	Versus  *report.Pair
}

// familyScenario identifies one population cell inside a log.
type familyScenario struct {
	family   string
	scenario string
}

// buildSummary matches the population groups of every log to the table
// scenarios and columns. It fails on a group it cannot place, since a verdict
// attached to the wrong scheduler would be worse than no verdict.
func buildSummary(reports []*report.Report, columns []Column, logs []logColumns, scenarios []ScenarioTable) (SummaryTable, error) {
	groups := make([]map[familyScenario]*report.Group, len(reports))
	var families []*report.Family
	famSeen := map[string]bool{}
	for i, r := range reports {
		var err error
		if groups[i], err = placeGroups(r); err != nil {
			return SummaryTable{}, fmt.Errorf("%s: %w", r.Source, err)
		}
		for j := range r.Population {
			if f := &r.Population[j]; !famSeen[f.Name] {
				famSeen[f.Name] = true
				families = append(families, f)
			}
		}
		if err := checkSchedulers(r, logs[i]); err != nil {
			return SummaryTable{}, fmt.Errorf("%s: %w", r.Source, err)
		}
	}
	t := SummaryTable{Columns: columns}
	for _, f := range families {
		metric, ok := report.MetricByKey(f.Key)
		if !ok {
			metric = report.Metric{Label: f.Name, Direction: f.Direction}
		}
		for _, sc := range scenarios {
			row := SummaryRow{Family: f.Name, Metric: metric, Scenario: sc.Name, Cells: make([]Standing, len(columns))}
			found := false
			for c, col := range columns {
				if g := groups[col.Log][familyScenario{f.Name, sc.Name}]; g != nil {
					row.Cells[c] = standing(g, col.Scheduler)
					found = true
				}
			}
			if found {
				t.Rows = append(t.Rows, row)
			}
		}
	}
	return t, nil
}

// placeGroups indexes the groups of one log by family and by the name of the
// table scenario they belong to. The group's "scenario" label is lowercase
// while the table prints it upper-case, so the two are matched ignoring case.
func placeGroups(r *report.Report) (map[familyScenario]*report.Group, error) {
	placed := map[familyScenario]*report.Group{}
	for i := range r.Population {
		f := &r.Population[i]
		for j := range f.Groups {
			g := &f.Groups[j]
			label, ok := g.Labels["scenario"]
			if !ok {
				return nil, fmt.Errorf("a %s group has no scenario label", f.Name)
			}
			name := scenarioName(r, label)
			if name == "" {
				return nil, fmt.Errorf("%s scenario %q has no table", f.Name, label)
			}
			key := familyScenario{f.Name, name}
			if placed[key] != nil {
				return nil, fmt.Errorf("%s has two groups for scenario %s", f.Name, name)
			}
			placed[key] = g
		}
	}
	return placed, nil
}

func scenarioName(r *report.Report, label string) string {
	for _, sc := range r.Scenarios {
		if strings.EqualFold(sc.Name, label) {
			return sc.Name
		}
	}
	return ""
}

// checkSchedulers makes sure every scheduler montauk names is a column of the
// same log. The population text is lowercase, so names match ignoring case.
func checkSchedulers(r *report.Report, lc logColumns) error {
	known := map[string]bool{}
	for n := range lc.index {
		known[strings.ToLower(n)] = true
	}
	for _, f := range r.Population {
		for _, g := range f.Groups {
			var names []string
			for _, p := range g.Pairs {
				names = append(names, p.A, p.B)
			}
			for _, o := range g.Only {
				if o.Scheduler != "" {
					names = append(names, o.Scheduler)
				}
			}
			for _, st := range g.Standings {
				names = append(names, st.Scheduler)
			}
			for _, n := range names {
				if !known[strings.ToLower(n)] {
					return fmt.Errorf("%s names scheduler %q, which has no column in the tables", f.Name, n)
				}
			}
		}
	}
	return nil
}

// standing builds the cell of one scheduler from its group.
func standing(g *report.Group, scheduler string) Standing {
	var st Standing
	for _, s := range g.Standings {
		if strings.EqualFold(s.Scheduler, scheduler) {
			st.Present, st.Verdict = true, s.Verdict
		}
	}
	for i := range g.Pairs {
		if strings.EqualFold(g.Pairs[i].B, scheduler) {
			st.Versus = &g.Pairs[i]
		}
	}
	return st
}
