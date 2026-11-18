// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package compare

import "github.com/galpt/prism-log-parser/internal/report"

// ScenarioTable is one scenario merged over the logs that contain it. Columns
// holds every column of those logs and nothing from the others.
type ScenarioTable struct {
	Name    string
	Columns []Column
	Rows    []RowSet
	Deltas  []DeltaSet
}

// RowSet is one metric across the columns of a ScenarioTable. Cells is
// parallel to ScenarioTable.Columns and is invalid where a log has no value.
type RowSet struct {
	Metric report.Metric
	Cells  []report.Cell
}

// DeltaSet is one "vs EEVDF" metric across the columns. Each log's deltas stay
// relative to its own EEVDF column, so columns of different logs are not
// measured against the same baseline.
type DeltaSet struct {
	Metric report.Metric
	Cells  []report.DeltaCell
}

// member is one log's copy of a scenario and where its columns start in the
// merged table.
type member struct {
	log   int
	sc    *report.Scenario
	start int
}

func (s *Set) buildScenarios(reports []*report.Report, logs []logColumns) {
	var order []string
	byName := map[string][]*member{}
	for i, r := range reports {
		for j := range r.Scenarios {
			sc := &r.Scenarios[j]
			if byName[sc.Name] == nil {
				order = append(order, sc.Name)
			}
			byName[sc.Name] = append(byName[sc.Name], &member{log: i, sc: sc})
		}
	}
	for _, name := range order {
		t := ScenarioTable{Name: name}
		members := byName[name]
		for _, m := range members {
			m.start = len(t.Columns)
			t.Columns = append(t.Columns, s.columns[logs[m.log].first:logs[m.log].first+len(logs[m.log].index)]...)
		}
		rows := map[report.Metric]int{}
		deltas := map[report.Metric]int{}
		for _, m := range members {
			// slot maps a scheduler of this log to its place in the table.
			slot := func(name string) int { return m.start + logs[m.log].index[name] }
			for _, row := range m.sc.Rows {
				k, ok := rows[row.Metric]
				if !ok {
					k = len(t.Rows)
					rows[row.Metric] = k
					t.Rows = append(t.Rows, RowSet{Metric: row.Metric, Cells: make([]report.Cell, len(t.Columns))})
				}
				for c, cell := range row.Cells {
					t.Rows[k].Cells[slot(m.sc.Schedulers[c])] = cell
				}
			}
			for _, d := range m.sc.Deltas {
				k, ok := deltas[d.Metric]
				if !ok {
					k = len(t.Deltas)
					deltas[d.Metric] = k
					t.Deltas = append(t.Deltas, DeltaSet{Metric: d.Metric, Cells: make([]report.DeltaCell, len(t.Columns))})
				}
				for c, cell := range d.Cells {
					t.Deltas[k].Cells[slot(m.sc.Schedulers[c])] = cell
				}
			}
		}
		s.scenarios = append(s.scenarios, t)
	}
}
