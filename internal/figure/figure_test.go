// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package figure

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/galpt/prism-log-parser/internal/compare"
	"github.com/galpt/prism-log-parser/internal/report"
)

// measure stands in for the font: every character is 0.55 em, or 0.6 em in
// bold. The layout only needs widths that grow with the text.
func measure(text string, size float64, bold bool) float64 {
	em := 0.55
	if bold {
		em = 0.6
	}
	return float64(utf8.RuneCountInString(text)) * size * em
}

var (
	fps   = report.Metric{Label: "AVG FPS", Direction: report.HigherIsBetter}
	ft99  = report.Metric{Label: "FRAME TIME p99", Unit: "ms", Direction: report.LowerIsBetter}
	wake  = report.Metric{Label: "JOB WAKE p99", Unit: "us", Direction: report.LowerIsBetter}
	ticks = report.Metric{Label: "REGIME TICKS LIGHT"}
)

func cell(mean, sd float64) report.Cell {
	return report.Cell{Mean: mean, SD: sd, Text: fmt.Sprintf("%.2f ±%.2f", mean, sd), Valid: true}
}

func row(m report.Metric, cells ...report.Cell) report.Row {
	return report.Row{Metric: m, Cells: cells}
}

func deltaRow(m report.Metric, percents ...float64) report.Delta {
	d := report.Delta{Metric: m, Cells: []report.DeltaCell{{}}}
	for _, p := range percents {
		d.Cells = append(d.Cells, report.DeltaCell{Percent: p, Text: fmt.Sprintf("%+.1f%%", p), Valid: true})
	}
	return d
}

// synthetic is a log with four schedulers. Its HEAVY scenario has a wake
// metric that spans three decades with a whisker that would cross zero, an
// invalid cell and two deltas. Its PACED scenario has no deltas and a metric
// with a zero mean.
func synthetic(source, stamp string) *report.Report {
	names := []string{"EEVDF", "sched_a", "sched_b", "sched_c"}
	return &report.Report{
		Source: "/some/dir/" + source,
		Meta:   report.Meta{Version: "1.2.3", Commit: "abc1234", Timestamp: stamp, CPUs: 8, Iterations: 5, Calibration: 1000.5, Drift: 0.1, HasDrift: true},
		Scenarios: []report.Scenario{
			{
				Name: "HEAVY", Schedulers: names,
				Rows: []report.Row{
					row(fps, cell(300, 2), cell(350, 1), cell(352, 3), report.Cell{Text: "-"}),
					row(ft99, cell(3.8, 0.05), cell(2.9, 0.02), cell(2.8, 0.01), cell(2.7, 0.01)),
					row(wake, cell(950, 15), cell(20, 0.7), cell(13, 0.1), cell(45, 46)),
				},
				Deltas: []report.Delta{deltaRow(fps, 16.7, 17.3, 0), deltaRow(ft99, -23.7, -26.3, -28.9)},
			},
			{
				Name: "PACED", Schedulers: names,
				Rows: []report.Row{
					row(fps, cell(144, 0), cell(144, 0), cell(144, 0), cell(144, 0)),
					row(ticks, cell(0, 0), cell(-2, 1), cell(5, 1), cell(1, 0.5)),
				},
			},
		},
	}
}

func mustSet(t *testing.T, reports ...*report.Report) *compare.Set {
	t.Helper()
	s, err := compare.New(reports)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustBuild(t *testing.T, reports ...*report.Report) []Figure {
	t.Helper()
	figs, err := Build(reports, mustSet(t, reports...), measure)
	if err != nil {
		t.Fatal(err)
	}
	return figs
}

// option is the part of an ECharts option that the tests look at.
type option struct {
	Grid   []json.RawMessage         `json:"grid"`
	Title  []struct{ Text string }   `json:"title"`
	Legend []struct{ Data []string } `json:"legend"`
	YAxis  []struct {
		Type string
		Min  float64
		Max  float64
	} `json:"yAxis"`
	Series []struct {
		Type       string
		Name       string
		YAxisIndex int
		Data       []json.RawMessage
		ItemStyle  struct{ Decal *decal } `json:"itemStyle"`
	} `json:"series"`
}

func decode(t *testing.T, f Figure) option {
	t.Helper()
	if !json.Valid(f.Option) {
		t.Fatalf("%s: option is not valid JSON", f.Name)
	}
	var o option
	if err := json.Unmarshal(f.Option, &o); err != nil {
		t.Fatalf("%s: %v", f.Name, err)
	}
	return o
}

func panelTitles(o option) []string {
	var out []string
	for _, t := range o.Title {
		if strings.HasPrefix(t.Text, "(") {
			out = append(out, t.Text)
		}
	}
	return out
}

func TestBuildNamesAndCounts(t *testing.T) {
	figs := mustBuild(t, synthetic("a.log", "20300101-000001"))
	var names []string
	for _, f := range figs {
		names = append(names, f.Name)
	}
	if got, want := strings.Join(names, " "), "prism-scenario-heavy.png prism-scenario-paced.png prism-summary.png"; got != want {
		t.Fatalf("names = %s, want %s", got, want)
	}

	heavy := decode(t, figs[0])
	// Three metric panels and the delta panel.
	if len(heavy.Grid) != 4 || len(heavy.YAxis) != 4 {
		t.Errorf("heavy has %d grids and %d y axes, want 4 and 4", len(heavy.Grid), len(heavy.YAxis))
	}
	wantTitles := []string{"(a) AVG FPS ↑", "(b) FRAME TIME p99 (ms) ↓", "(c) JOB WAKE p99 (µs) ↓", "(d) " + deltaName}
	if got := panelTitles(heavy); strings.Join(got, "|") != strings.Join(wantTitles, "|") {
		t.Errorf("panel titles = %q, want %q", got, wantTitles)
	}
	// Per metric panel: one bar per column, the whiskers and the labels.
	// The delta panel has one bar series per non-baseline column.
	if want := 3*(4+2) + 3; len(heavy.Series) != want {
		t.Errorf("heavy has %d series, want %d", len(heavy.Series), want)
	}

	paced := decode(t, figs[1])
	if got := panelTitles(paced); len(got) != 2 || got[1] != "(b) REGIME TICKS LIGHT" {
		t.Errorf("paced panel titles = %q", got)
	}
	if len(paced.Grid) != 2 {
		t.Errorf("paced has %d grids, want 2 without deltas", len(paced.Grid))
	}
	for _, tt := range paced.Title {
		if strings.Contains(tt.Text, "EEVDF column") {
			t.Error("paced caption explains deltas it does not have")
		}
	}
}

func TestDeltaPanelOnlyWithDeltas(t *testing.T) {
	for _, f := range mustBuild(t, synthetic("a.log", "20300101-000001"))[:2] {
		o := decode(t, f)
		has := false
		for _, title := range panelTitles(o) {
			has = has || strings.HasSuffix(title, deltaName)
		}
		if want := f.Name == "prism-scenario-heavy.png"; has != want {
			t.Errorf("%s: delta panel = %v, want %v", f.Name, has, want)
		}
	}
}

func TestUseLog(t *testing.T) {
	cases := []struct {
		name  string
		cells []report.Cell
		want  bool
	}{
		{"wide spread", []report.Cell{cell(1000, 1), cell(10, 1)}, true},
		{"exactly the ratio", []report.Cell{cell(20, 1), cell(1, 1)}, true},
		{"narrow spread", []report.Cell{cell(19, 1), cell(1, 1)}, false},
		{"a zero mean", []report.Cell{cell(1000, 1), cell(0, 0)}, false},
		{"a negative mean", []report.Cell{cell(1000, 1), cell(-1, 0)}, false},
		{"invalid cells are ignored", []report.Cell{cell(1000, 1), {Mean: -5}, cell(10, 1)}, true},
		{"a single value", []report.Cell{cell(1000, 1)}, false},
	}
	for _, c := range cases {
		if got := useLog(c.cells); got != c.want {
			t.Errorf("%s: useLog = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestLogAxisValuesArePositive checks every value sent to a log axis: bars,
// both whisker ends and the label points.
func TestLogAxisValuesArePositive(t *testing.T) {
	f := mustBuild(t, synthetic("a.log", "20300101-000001"))[0]
	o := decode(t, f)
	logAxes := 0
	for i, a := range o.YAxis {
		if a.Type != "log" {
			continue
		}
		logAxes++
		if a.Min <= 0 || a.Max <= a.Min {
			t.Errorf("log axis %d spans %v..%v", i, a.Min, a.Max)
		}
		for _, s := range o.Series {
			if s.YAxisIndex != i {
				continue
			}
			for _, raw := range s.Data {
				for _, v := range values(t, raw, s.Type) {
					if v <= 0 || v < a.Min || v > a.Max {
						t.Errorf("%s series on log axis %d has value %v outside %v..%v", s.Type, i, v, a.Min, a.Max)
					}
				}
			}
		}
	}
	// Only the wake panel spreads over 20x.
	if logAxes != 1 {
		t.Errorf("%d log axes, want 1", logAxes)
	}
	if !strings.Contains(string(f.Option), logMark) {
		t.Error("the log axis has no mark")
	}
}

// values returns the y values of one data item. A bar item is a number, "-"
// or an object with a value. Whisker and label items carry x first.
func values(t *testing.T, raw json.RawMessage, kind string) []float64 {
	t.Helper()
	var item struct{ Value []float64 }
	switch {
	case string(raw) == `"-"`, string(raw) == `{"value":"-"}`:
		return nil
	case kind == "bar":
		var bar struct{ Value float64 }
		if err := json.Unmarshal(raw, &bar); err != nil {
			t.Fatalf("bar item %s: %v", raw, err)
		}
		return []float64{bar.Value}
	case json.Unmarshal(raw, &item) != nil:
		t.Fatalf("item %s", raw)
	}
	return item.Value[1:]
}

func TestWhisker(t *testing.T) {
	cases := []struct {
		c         report.Cell
		log       bool
		low, high float64
	}{
		{cell(45, 46), true, 45, 91},
		{cell(45, 10), true, 35, 55},
		{cell(45, 46), false, 0, 91},
		{cell(-2, 1), false, -3, -1},
	}
	for _, c := range cases {
		if low, high := whisker(c.c, c.log); low != c.low || high != c.high {
			t.Errorf("whisker(%v, log=%v) = %v, %v, want %v, %v", c.c.Text, c.log, low, high, c.low, c.high)
		}
	}
}

func TestLinearAxis(t *testing.T) {
	a := linearAxis(0, 397, 200, 20, 0)
	if a.min != 0 || a.step != 100 || a.max != 500 {
		t.Errorf("0..397 gives %+v, want 0..500 by 100", a)
	}
	// 400 would leave the label above 377 only 11px.
	if a := linearAxis(0, 377, 200, 20, 0); a.max != 500 {
		t.Errorf("0..377 gives max %v, want 500", a.max)
	}
	a = linearAxis(-98.6, 41.4, 200, 20, 20)
	if a.min > -120 || a.max < 41.4 {
		t.Errorf("-98.6..41.4 gives %+v, without room for the labels", a)
	}
	if a := linearAxis(0, 0, 200, 20, 0); a.max <= 0 {
		t.Errorf("an all-zero panel gives %+v", a)
	}
	if got := strings.Join(linearAxis(0, 0.25, 200, 20, 0).ticks(), " "); got != "0.00 0.05 0.10 0.15 0.20 0.25 0.30" {
		t.Errorf("ticks = %s", got)
	}
}

func TestTickText(t *testing.T) {
	cases := map[string]string{
		tickText(10000, 0): "10,000", tickText(0.01, 2): "0.01", tickText(-1500, 0): "-1,500",
		tickText(-0.0001, 1): "0.0", tickText(123, 0): "123",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("tickText = %q, want %q", got, want)
		}
	}
}

func TestPanelLetterAndMetricName(t *testing.T) {
	if got := panelLetter(0) + panelLetter(25) + panelLetter(26); got != "(a)(z)(aa)" {
		t.Errorf("letters = %s", got)
	}
	if got := metricName(ticks); got != "REGIME TICKS LIGHT" {
		t.Errorf("unknown direction gives %q", got)
	}
}

func TestWrap(t *testing.T) {
	lines := wrap("one two three four five", measure("one two", 10, false), 10, false, measure)
	if strings.Join(lines, "|") != "one two|three|four|five" {
		t.Errorf("wrap = %q", lines)
	}
	if lines := wrap("abcdefghij", measure("abcd", 10, false), 10, false, measure); strings.Join(lines, "|") != "abcd|efgh|ij" {
		t.Errorf("long word wraps as %q", lines)
	}
}

// spec is a scenario like the real ones: 13 panels, a delta panel and a
// legend entry per column.
func spec(columns int) scenarioSpec {
	legend := make([]string, columns)
	for i := range legend {
		legend[i] = fmt.Sprintf("sched_%d (20300101-000001)", i)
	}
	return scenarioSpec{
		columns: columns, band: minBand, axisW: 50, titleW: 190, panels: 13,
		title: "PRISM-FRAME · SCENARIO HEAVY", legend: legend,
		delta:   &deltaSpec{metrics: 5, columns: columns - 1, slot: 40},
		caption: []string{strings.Repeat("caption words ", 40)},
	}
}

func TestLayoutWidthGrowsWithColumns(t *testing.T) {
	prev := 0
	widths := map[int]int{}
	for c := 2; c <= 16; c++ {
		l, err := layoutScenario(spec(c), measure)
		if err != nil {
			t.Fatal(err)
		}
		if l.width <= prev {
			t.Errorf("%d columns give width %d, not more than %d", c, l.width, prev)
		}
		prev = l.width
		widths[c] = l.width
	}
	if w := widths[4]; w < 1400 || w > 1800 {
		t.Errorf("4 columns give width %d, want about 1600", w)
	}
	if widths[8] < widths[4]*3/2 {
		t.Errorf("8 columns give width %d, not much more than %d for 4", widths[8], widths[4])
	}
}

func TestLayoutHeightGrows(t *testing.T) {
	base, err := layoutScenario(spec(4), measure)
	if err != nil {
		t.Fatal(err)
	}
	more := spec(4)
	more.panels = 17
	more.caption = append(more.caption, "another caption line")
	taller, err := layoutScenario(more, measure)
	if err != nil {
		t.Fatal(err)
	}
	if taller.height <= base.height {
		t.Errorf("more panels and caption give height %d, not more than %d", taller.height, base.height)
	}
	if base.perRow != maxPerRow || len(base.legendRows) != 1 {
		t.Errorf("4 columns give %d panels per row and %d legend rows", base.perRow, len(base.legendRows))
	}
	crowded := spec(4)
	crowded.legend = spec(30).legend
	wide, err := layoutScenario(crowded, measure)
	if err != nil {
		t.Fatal(err)
	}
	if len(wide.legendRows) < 2 || wide.height <= base.height {
		t.Errorf("30 legend entries take %d rows and height %d", len(wide.legendRows), wide.height)
	}
}

func TestLayoutDropsPanelsPerRow(t *testing.T) {
	s := spec(60)
	s.delta = nil
	l, err := layoutScenario(s, measure)
	if err != nil {
		t.Fatal(err)
	}
	if l.perRow >= maxPerRow || l.width > maxSide {
		t.Errorf("60 columns give %d panels per row and width %d", l.perRow, l.width)
	}
	if want := 2*margin + float64(l.perRow)*l.cellW - panelGapH; math.Abs(float64(l.width)-want) > 1 {
		t.Errorf("width %d, want %v", l.width, want)
	}
}

func TestLayoutTooWide(t *testing.T) {
	_, err := layoutScenario(spec(300), measure)
	if err == nil || !strings.Contains(err.Error(), "16384") {
		t.Errorf("300 columns give %v, want an error naming the limit", err)
	}
}

func TestManyColumnsGetPatterns(t *testing.T) {
	var reports []*report.Report
	for i := range 4 {
		reports = append(reports, synthetic(fmt.Sprintf("%d.log", i), fmt.Sprintf("2030010%d-000001", i+1)))
	}
	figs := mustBuild(t, reports...)
	o := decode(t, figs[0])
	seen := map[string]bool{}
	for _, s := range o.Series {
		if s.Type != "bar" || s.YAxisIndex != 0 {
			continue
		}
		d, _ := json.Marshal(s.ItemStyle.Decal)
		key := columnColor(len(seen)) + string(d)
		if seen[key] {
			t.Errorf("column %s repeats color and pattern %s", s.Name, key)
		}
		seen[key] = true
	}
	if len(seen) != 16 {
		t.Errorf("%d distinct bar styles in the first panel, want 16", len(seen))
	}
	legend := 0
	for _, l := range o.Legend {
		legend += len(l.Data)
	}
	if legend != 16 {
		t.Errorf("legend has %d entries, want 16", legend)
	}
}

func TestSummary(t *testing.T) {
	r := synthetic("a.log", "20300101-000001")
	r.Population = []report.Family{{
		Name: "pandemonium_frame_higher_fps", Key: "fps", Direction: report.HigherIsBetter,
		Groups: []report.Group{{
			Labels: map[string]string{"scenario": "heavy"},
			Pairs: []report.Pair{{A: "sched_b", B: "eevdf", Cliff: 1, Magnitude: "large", P: 0.008},
				{A: "sched_b", B: "sched_a", Cliff: 0.2, Magnitude: "small", P: 0.5}},
			Standings: []report.Standing{{Scheduler: "eevdf", Verdict: report.NotBest}, {Scheduler: "sched_a", Verdict: report.Tied}, {Scheduler: "sched_b", Verdict: report.Best}},
		}},
	}}
	other := synthetic("b.log", "20300101-000002")
	other.Notes = []string{"the audio thread ran late"}
	figs := mustBuild(t, r, other)
	f := figs[len(figs)-1]
	o := decode(t, f)
	var heat []string
	for _, s := range o.Series {
		if s.Type == "heatmap" {
			heat = append(heat, fmt.Sprintf("%s:%d", s.Name, len(s.Data)))
		}
	}
	// One row of eight columns: the winner, one tied, one not best, and
	// the fourth column and all of b.log without a verdict.
	if got := strings.Join(heat, " "); got != "best:1 tied:1 not best:1 no verdict:5" {
		t.Errorf("heatmap series = %s", got)
	}
	text := string(f.Option)
	for _, want := range []string{"AVG FPS ↑ · HEAVY", `d=+1.00 large\np=0.008`, "b.log: AUDIO: the audio thread ran late",
		"b.log has no POPULATION data", "abc1234", "+0.1%", "1000.500"} {
		if !strings.Contains(text, want) {
			t.Errorf("summary option lacks %q", want)
		}
	}

	o = decode(t, mustBuild(t, synthetic("a.log", "20300101-000001"))[2])
	found := false
	for _, tt := range o.Title {
		found = found || strings.Contains(tt.Text, "No log has POPULATION data")
	}
	if !found {
		t.Error("summary without population does not say so")
	}
}

func TestFileName(t *testing.T) {
	if got := fileName("EACH_CONTENDED"); got != "prism-scenario-each_contended.png" {
		t.Errorf("fileName = %s", got)
	}
	if got := fileName("../X"); got != "prism-scenario----x.png" {
		t.Errorf("fileName = %s", got)
	}
	if got := fileName("summary"); got == "prism-summary.png" {
		t.Errorf("scenario SUMMARY takes the summary's file name %s", got)
	}
}

func TestEmptyScenario(t *testing.T) {
	r := synthetic("a.log", "20300101-000001")
	r.Scenarios = append(r.Scenarios, report.Scenario{Name: "EMPTY", Schedulers: r.Scenarios[0].Schedulers})
	figs := mustBuild(t, r)
	if f := figs[2]; f.Name != "prism-scenario-empty.png" || len(decode(t, f).Grid) != 0 {
		t.Errorf("empty scenario gives %s with option %s", f.Name, f.Option)
	}
}
