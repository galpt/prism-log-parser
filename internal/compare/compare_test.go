// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package compare

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/galpt/prism-log-parser/internal/report"
)

var (
	fpsMetric  = report.Metric{Label: "AVG FPS", Direction: report.HigherIsBetter}
	ftMetric   = report.Metric{Label: "FRAME TIME p99", Unit: "ms", Direction: report.LowerIsBetter}
	lateMetric = report.Metric{Label: "LATE FRAMES", Unit: "%", Direction: report.LowerIsBetter}
)

// cell makes a valid cell whose value is easy to recognize in a failure.
func cell(v float64) report.Cell {
	return report.Cell{Mean: v, SD: v / 10, Text: fmt.Sprintf("%.2f", v), Valid: true}
}

func row(m report.Metric, values ...float64) report.Row {
	r := report.Row{Metric: m}
	for _, v := range values {
		r.Cells = append(r.Cells, cell(v))
	}
	return r
}

func delta(m report.Metric, percents ...float64) report.Delta {
	d := report.Delta{Metric: m}
	for _, p := range percents {
		c := report.DeltaCell{Percent: p, Text: fmt.Sprintf("%+.1f%%", p), Valid: p != 0}
		d.Cells = append(d.Cells, c)
	}
	return d
}

func scenario(name string, schedulers []string, rows ...report.Row) report.Scenario {
	return report.Scenario{Name: name, Schedulers: schedulers, Rows: rows}
}

func logOf(source, timestamp string, scenarios ...report.Scenario) *report.Report {
	return &report.Report{
		Source:    source,
		Meta:      report.Meta{Timestamp: timestamp, Version: "1.0.0", Commit: "abc1234", CPUs: 8, Iterations: 5},
		Scenarios: scenarios,
	}
}

func mustNew(t *testing.T, reports ...*report.Report) *Set {
	t.Helper()
	s, err := New(reports)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func labels(s *Set) []string {
	var out []string
	for _, c := range s.Columns() {
		out = append(out, c.Label)
	}
	return out
}

func TestColumnLabels(t *testing.T) {
	heavy := func(names ...string) report.Scenario { return scenario("HEAVY", names) }
	cases := []struct {
		name    string
		reports []*report.Report
		want    []string
	}{
		{
			"names unique to each log stay plain",
			[]*report.Report{logOf("a", "20260101-000001", heavy("x", "y")), logOf("b", "20260101-000002", heavy("z"))},
			[]string{"x", "y", "z"},
		},
		{
			"a name in two logs gets the timestamp of each",
			[]*report.Report{
				logOf("a", "20260101-000001", heavy("EEVDF", "x")),
				logOf("b", "20260101-000002", heavy("EEVDF", "y")),
			},
			[]string{"EEVDF (20260101-000001)", "x", "EEVDF (20260101-000002)", "y"},
		},
		{
			"the same log twice is numbered by position",
			[]*report.Report{logOf("a", "20260101-000001", heavy("EEVDF", "x")), logOf("a", "20260101-000001", heavy("EEVDF", "x"))},
			[]string{"EEVDF (20260101-000001) #1", "x (20260101-000001) #1", "EEVDF (20260101-000001) #2", "x (20260101-000001) #2"},
		},
		{
			"only the colliding labels are numbered",
			[]*report.Report{
				logOf("a", "20260101-000001", heavy("EEVDF", "x")),
				logOf("b", "20260101-000002", heavy("EEVDF")),
				logOf("a", "20260101-000001", heavy("EEVDF", "x")),
			},
			[]string{
				"EEVDF (20260101-000001) #1", "x (20260101-000001) #1",
				"EEVDF (20260101-000002)",
				"EEVDF (20260101-000001) #3", "x (20260101-000001) #3",
			},
		},
		{
			"two logs from the same second collide and are numbered",
			[]*report.Report{logOf("a", "20260101-000001", heavy("EEVDF")), logOf("b", "20260101-000001", heavy("EEVDF"))},
			[]string{"EEVDF (20260101-000001) #1", "EEVDF (20260101-000001) #2"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := labels(mustNew(t, c.reports...)); !reflect.DeepEqual(got, c.want) {
				t.Errorf("labels = %q\nwant     %q", got, c.want)
			}
		})
	}
}

// TestColumnLabelsAreUnique draws many random sets of logs, with small pools of
// names and timestamps so that collisions are common, and checks that every
// label is distinct and every column keeps its own log and scheduler.
func TestColumnLabelsAreUnique(t *testing.T) {
	rng := rand.New(rand.NewSource(1234))
	pool := []string{"EEVDF", "scx_a", "scx_b", "scx_c"}
	stamps := []string{"20260101-000001", "20260101-000002", "20260101-000003"}
	for i := 0; i < 5000; i++ {
		var reports []*report.Report
		for l := rng.Intn(6) + 1; l > 0; l-- {
			var names []string
			for _, n := range pool {
				if rng.Intn(2) == 0 {
					names = append(names, n)
				}
			}
			if len(names) == 0 {
				names = pool[:1]
			}
			reports = append(reports, logOf("log", stamps[rng.Intn(len(stamps))], scenario("HEAVY", names)))
		}
		s := mustNew(t, reports...)
		seen := map[string]bool{}
		for _, c := range s.Columns() {
			if seen[c.Label] {
				t.Fatalf("label %q is not unique in %q", c.Label, labels(s))
			}
			seen[c.Label] = true
			if c.Log >= len(reports) || !strings.HasPrefix(c.Label, c.Scheduler) {
				t.Fatalf("column %+v does not belong to its log", c)
			}
		}
	}
}

func TestSchedulersAreTheUnionOverScenarios(t *testing.T) {
	r := logOf("a", "20260101-000001",
		scenario("HEAVY", []string{"EEVDF", "x"}, row(fpsMetric, 1, 2)),
		scenario("PACED", []string{"EEVDF", "y"}, row(fpsMetric, 3, 4)))
	s := mustNew(t, r)
	if got := labels(s); !reflect.DeepEqual(got, []string{"EEVDF", "x", "y"}) {
		t.Fatalf("labels = %q", got)
	}
	heavy, paced := s.Scenarios()[0], s.Scenarios()[1]
	if got := []bool{heavy.Rows[0].Cells[0].Valid, heavy.Rows[0].Cells[1].Valid, heavy.Rows[0].Cells[2].Valid}; !reflect.DeepEqual(got, []bool{true, true, false}) {
		t.Errorf("HEAVY validity = %v", got)
	}
	if got := []float64{paced.Rows[0].Cells[0].Mean, paced.Rows[0].Cells[2].Mean}; !reflect.DeepEqual(got, []float64{3, 4}) {
		t.Errorf("PACED values = %v", got)
	}
	if paced.Rows[0].Cells[1].Valid {
		t.Error("x has no PACED value but its cell is valid")
	}
}

func TestScenarioUnionWithMissingScenarios(t *testing.T) {
	a := logOf("a", "20260101-000001",
		scenario("HEAVY", []string{"EEVDF", "x"}, row(fpsMetric, 10, 11)),
		scenario("PACED", []string{"EEVDF", "x"}, row(fpsMetric, 20, 21)))
	b := logOf("b", "20260101-000002",
		scenario("CONTENDED", []string{"EEVDF", "y"}, row(fpsMetric, 30, 31)),
		scenario("PACED", []string{"EEVDF", "y"}, row(fpsMetric, 40, 41)),
		scenario("HEAVY", []string{"EEVDF", "y"}, row(fpsMetric, 50, 51)))
	s := mustNew(t, a, b)
	var names []string
	for _, sc := range s.Scenarios() {
		names = append(names, sc.Name)
	}
	if want := []string{"HEAVY", "PACED", "CONTENDED"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("scenario order = %q, want %q", names, want)
	}
	byName := map[string]ScenarioTable{}
	for _, sc := range s.Scenarios() {
		byName[sc.Name] = sc
	}
	values := func(sc ScenarioTable) []float64 {
		var out []float64
		for _, c := range sc.Rows[0].Cells {
			out = append(out, c.Mean)
		}
		return out
	}
	if got := len(byName["CONTENDED"].Columns); got != 2 {
		t.Errorf("CONTENDED has %d columns, want only the 2 of log b", got)
	}
	for _, c := range byName["CONTENDED"].Columns {
		if c.Log != 1 {
			t.Errorf("CONTENDED holds a column of log %d", c.Log)
		}
	}
	if got, want := values(byName["CONTENDED"]), []float64{30, 31}; !reflect.DeepEqual(got, want) {
		t.Errorf("CONTENDED values = %v, want %v", got, want)
	}
	if got, want := values(byName["HEAVY"]), []float64{10, 11, 50, 51}; !reflect.DeepEqual(got, want) {
		t.Errorf("HEAVY values = %v, want %v", got, want)
	}
	if got, want := values(byName["PACED"]), []float64{20, 21, 40, 41}; !reflect.DeepEqual(got, want) {
		t.Errorf("PACED values = %v, want %v", got, want)
	}
	if got := labels(s); !reflect.DeepEqual(got, []string{"EEVDF (20260101-000001)", "x", "EEVDF (20260101-000002)", "y"}) {
		t.Errorf("labels = %q", got)
	}
}

func TestRowAndDeltaUnion(t *testing.T) {
	names := []string{"EEVDF", "x"}
	sa := scenario("PACED", names, row(fpsMetric, 100, 110), row(ftMetric, 4, 3))
	sa.Deltas = []report.Delta{delta(fpsMetric, 0, 10), delta(ftMetric, 0, -25)}
	sb := scenario("PACED", []string{"EEVDF", "y"}, row(fpsMetric, 90, 99), row(lateMetric, 0.5, 0.1))
	sb.Deltas = []report.Delta{delta(fpsMetric, 0, 10), delta(lateMetric, 0, -80)}
	s := mustNew(t, logOf("a", "20260101-000001", sa), logOf("b", "20260101-000002", sb))
	paced := s.Scenarios()[0]

	var metrics []report.Metric
	for _, r := range paced.Rows {
		metrics = append(metrics, r.Metric)
	}
	if want := []report.Metric{fpsMetric, ftMetric, lateMetric}; !reflect.DeepEqual(metrics, want) {
		t.Fatalf("rows = %+v, want %+v", metrics, want)
	}
	valid := func(r RowSet) []bool {
		var out []bool
		for _, c := range r.Cells {
			out = append(out, c.Valid)
		}
		return out
	}
	if got := valid(paced.Rows[0]); !reflect.DeepEqual(got, []bool{true, true, true, true}) {
		t.Errorf("AVG FPS validity = %v", got)
	}
	if got := valid(paced.Rows[1]); !reflect.DeepEqual(got, []bool{true, true, false, false}) {
		t.Errorf("FRAME TIME validity = %v", got)
	}
	if got := valid(paced.Rows[2]); !reflect.DeepEqual(got, []bool{false, false, true, true}) {
		t.Errorf("LATE FRAMES validity = %v", got)
	}
	check := func(what string, got, want float64) {
		t.Helper()
		if got != want {
			t.Errorf("%s = %v, want %v", what, got, want)
		}
	}
	check("log b EEVDF FPS", paced.Rows[0].Cells[2].Mean, 90)
	check("log b y late", paced.Rows[2].Cells[3].Mean, 0.1)

	if len(paced.Deltas) != 3 {
		t.Fatalf("%d delta rows, want 3", len(paced.Deltas))
	}
	// Each delta stays relative to the EEVDF of its own log.
	check("log a x fps delta", paced.Deltas[0].Cells[1].Percent, 10)
	check("log b y fps delta", paced.Deltas[0].Cells[3].Percent, 10)
	check("log a x ft delta", paced.Deltas[1].Cells[1].Percent, -25)
	if paced.Deltas[1].Cells[3].Valid || paced.Deltas[2].Cells[1].Valid {
		t.Error("a delta the log does not have is valid")
	}
	check("log b y late delta", paced.Deltas[2].Cells[3].Percent, -80)
	if paced.Deltas[0].Cells[0].Valid || paced.Deltas[0].Cells[2].Valid {
		t.Error("a baseline cell is valid")
	}
}

// populated is a log with a population section for the family fps in the
// scenario HEAVY, where sched_x wins and eevdf and sched_y are compared to it.
func populated() *report.Report {
	r := logOf("a", "20260101-000001", scenario("HEAVY", []string{"EEVDF", "sched_x", "sched_y"}, row(fpsMetric, 1, 2, 3)))
	r.Population = []report.Family{{
		Name: "pandemonium_frame_higher_fps", Key: "fps", Direction: report.HigherIsBetter,
		Groups: []report.Group{{
			Labels: map[string]string{"scenario": "heavy", "commit": "abc1234"},
			Pairs: []report.Pair{
				{A: "sched_x", B: "eevdf", P: 0.008},
				{A: "sched_x", B: "sched_y", P: 0.5},
			},
			Standings: []report.Standing{
				{Scheduler: "eevdf", Verdict: report.NotBest},
				{Scheduler: "sched_x", Verdict: report.Best},
				{Scheduler: "sched_y", Verdict: report.Tied},
			},
		}},
	}}
	return r
}

func TestSummaryWiring(t *testing.T) {
	a := populated()
	// The second log has no POPULATION section and a different scenario set.
	b := logOf("b", "20260101-000002", scenario("HEAVY", []string{"EEVDF", "sched_z"}, row(fpsMetric, 1, 2)),
		scenario("PACED", []string{"EEVDF", "sched_z"}, row(fpsMetric, 1, 2)))
	s := mustNew(t, a, b)
	sum := s.Summary()
	if len(sum.Columns) != 5 {
		t.Fatalf("summary has %d columns, want 5", len(sum.Columns))
	}
	if len(sum.Rows) != 1 {
		t.Fatalf("summary has %d rows, want 1", len(sum.Rows))
	}
	sr := sum.Rows[0]
	if sr.Family != "pandemonium_frame_higher_fps" || sr.Scenario != "HEAVY" {
		t.Errorf("row = %s/%s", sr.Family, sr.Scenario)
	}
	if sr.Metric != fpsMetricFromRegistry() {
		t.Errorf("Metric = %+v", sr.Metric)
	}
	if len(sr.Cells) != 5 {
		t.Fatalf("%d cells, want 5", len(sr.Cells))
	}
	for i, want := range []struct {
		present bool
		verdict report.Verdict
		versus  string
	}{
		{true, report.NotBest, "sched_x vs eevdf"},
		{true, report.Best, ""},
		{true, report.Tied, "sched_x vs sched_y"},
		{false, 0, ""},
		{false, 0, ""},
	} {
		c := sr.Cells[i]
		if c.Present != want.present || (c.Present && c.Verdict != want.verdict) {
			t.Errorf("cell %d = %+v, want present=%v verdict=%v", i, c, want.present, want.verdict)
		}
		got := ""
		if c.Versus != nil {
			got = c.Versus.A + " vs " + c.Versus.B
		}
		if got != want.versus {
			t.Errorf("cell %d versus = %q, want %q", i, got, want.versus)
		}
	}
	if sr.Cells[0].Versus != &a.Population[0].Groups[0].Pairs[0] {
		t.Error("Versus does not point at the pair of the report")
	}
}

func fpsMetricFromRegistry() report.Metric {
	m, _ := report.MetricByKey("fps")
	return m
}

func TestSummaryOrder(t *testing.T) {
	mk := func(src, ts string, families ...string) *report.Report {
		r := logOf(src, ts, scenario("PACED", []string{"EEVDF"}), scenario("HEAVY", []string{"EEVDF"}))
		for _, f := range families {
			r.Population = append(r.Population, report.Family{Name: f, Groups: []report.Group{
				{Labels: map[string]string{"scenario": "heavy"}},
				{Labels: map[string]string{"scenario": "paced"}},
			}})
		}
		return r
	}
	s := mustNew(t, mk("a", "20260101-000001", "fam_b", "fam_a"), mk("b", "20260101-000002", "fam_c", "fam_a"))
	var got []string
	for _, r := range s.Summary().Rows {
		got = append(got, r.Family+"/"+r.Scenario)
		if r.Metric.Label != r.Family {
			t.Errorf("unknown family %s has label %q", r.Family, r.Metric.Label)
		}
	}
	want := []string{"fam_b/PACED", "fam_b/HEAVY", "fam_a/PACED", "fam_a/HEAVY", "fam_c/PACED", "fam_c/HEAVY"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %q\nwant   %q", got, want)
	}
}

func TestSummaryErrors(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(r *report.Report)
		want   string
	}{
		{"scheduler without a column", func(r *report.Report) {
			r.Population[0].Groups[0].Standings[2].Scheduler = "sched_missing"
		}, `"sched_missing"`},
		{"pair names an unknown scheduler", func(r *report.Report) {
			r.Population[0].Groups[0].Pairs[0].B = "ghost"
		}, `"ghost"`},
		{"only names an unknown scheduler", func(r *report.Report) {
			r.Population[0].Groups[0].Only = []report.Only{{Scheduler: "ghost"}}
		}, `"ghost"`},
		{"scenario without a table", func(r *report.Report) {
			r.Population[0].Groups[0].Labels["scenario"] = "nowhere"
		}, `"nowhere"`},
		{"group without a scenario label", func(r *report.Report) {
			delete(r.Population[0].Groups[0].Labels, "scenario")
		}, "no scenario label"},
		{"two groups for one scenario", func(r *report.Report) {
			g := r.Population[0].Groups[0]
			r.Population[0].Groups = append(r.Population[0].Groups, g)
		}, "two groups"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := populated()
			c.mutate(r)
			_, err := New([]*report.Report{r})
			if err == nil || !strings.Contains(err.Error(), c.want) || !strings.HasPrefix(err.Error(), "a: ") {
				t.Errorf("error = %v, want one naming the log and containing %s", err, c.want)
			}
		})
	}
	// The match ignores case, because tables print EEVDF and montauk eevdf.
	if _, err := New([]*report.Report{populated()}); err != nil {
		t.Errorf("case-insensitive match failed: %v", err)
	}
}

func TestNewRejectsBadInput(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Error("no error for zero logs")
	}
	if _, err := New([]*report.Report{populated(), nil}); err == nil {
		t.Error("no error for a nil log")
	}
}

func TestDifferences(t *testing.T) {
	mk := func(src, version, commit string, dirty bool, cpus, iters int) *report.Report {
		r := logOf(src, "20260101-000001", scenario("HEAVY", []string{"EEVDF"}))
		r.Meta.Version, r.Meta.Commit, r.Meta.Dirty, r.Meta.CPUs, r.Meta.Iterations = version, commit, dirty, cpus, iters
		return r
	}
	same := mustNew(t, mk("a.log", "1", "c1", false, 8, 5), mk("b.log", "1", "c1", false, 8, 5))
	if d := same.Differences(); len(d) != 0 {
		t.Errorf("identical logs differ: %q", d)
	}
	if d := mustNew(t, mk("a.log", "1", "c1", false, 8, 5)).Differences(); len(d) != 0 {
		t.Errorf("a single log differs: %q", d)
	}
	cpus := mustNew(t, mk("a.log", "1", "c1", false, 16, 5), mk("b.log", "1", "c1", false, 8, 5))
	if got, want := cpus.Differences(), []string{"CPUS differs: 16 (a.log), 8 (b.log)"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Differences = %q, want %q", got, want)
	}
	many := mustNew(t,
		mk("a.log", "1", "c1", false, 8, 5),
		mk("b.log", "2", "c1", true, 8, 5),
		mk("c.log", "1", "c1", false, 8, 3),
		mk("d.log", "1", "c1", false, 8, 5))
	want := []string{
		"VERSION differs: 1 (a.log, c.log, d.log), 2 (b.log)",
		"COMMIT differs: c1 (a.log, c.log, d.log), c1 (dirty) (b.log)",
		"ITERATIONS differs: 5 (a.log, b.log, d.log), 3 (c.log)",
	}
	if got := many.Differences(); !reflect.DeepEqual(got, want) {
		t.Errorf("Differences = %q\nwant         %q", got, want)
	}
}
