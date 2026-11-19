// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package figure

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/galpt/prism-log-parser/internal/compare"
	"github.com/galpt/prism-log-parser/internal/render"
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
		t.Fatalf("%s: option is not valid JSON", f.Path)
	}
	var o option
	if err := json.Unmarshal(f.Option, &o); err != nil {
		t.Fatalf("%s: %v", f.Path, err)
	}
	return o
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

func TestWrap(t *testing.T) {
	lines := wrap("one two three four five", measure("one two", 10, false), 10, false, measure)
	if strings.Join(lines, "|") != "one two|three|four|five" {
		t.Errorf("wrap = %q", lines)
	}
	if lines := wrap("abcdefghij", measure("abcd", 10, false), 10, false, measure); strings.Join(lines, "|") != "abcd|efgh|ij" {
		t.Errorf("long word wraps as %q", lines)
	}
}

func paths(figs []Figure) []string {
	out := make([]string, len(figs))
	for i, f := range figs {
		out[i] = f.Path
	}
	return out
}

// byPath returns the figure saved under path.
func byPath(t *testing.T, figs []Figure, path string) Figure {
	t.Helper()
	for _, f := range figs {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("no figure %s in %q", path, paths(figs))
	return Figure{}
}

// captionText is the caption of a chart image, which is its last title.
func captionText(o option) string { return o.Title[len(o.Title)-1].Text }

// withPopulation adds verdicts for AVG FPS in HEAVY and nothing else.
func withPopulation(r *report.Report) *report.Report {
	r.Population = []report.Family{{
		Name: "pandemonium_frame_higher_fps", Key: "fps", Direction: report.HigherIsBetter,
		Groups: []report.Group{{
			Labels: map[string]string{"scenario": "heavy"},
			Pairs: []report.Pair{{A: "sched_b", B: "eevdf", Cliff: 1, Magnitude: "large", P: 0.008},
				{A: "sched_b", B: "sched_a", Cliff: 0.2, Magnitude: "small", P: 0.5}},
			Standings: []report.Standing{{Scheduler: "eevdf", Verdict: report.NotBest}, {Scheduler: "sched_a", Verdict: report.Tied}, {Scheduler: "sched_b", Verdict: report.Best}},
		}},
	}}
	return r
}

func TestBuildPaths(t *testing.T) {
	figs := mustBuild(t, withPopulation(synthetic("a.log", "20300101-000001")))
	want := []string{
		"scenario-heavy/1-avg-fps.png",
		"scenario-heavy/2-frame-time-p99.png",
		"scenario-heavy/3-job-wake-p99.png",
		"scenario-heavy/4-change-vs-eevdf.png",
		"scenario-paced/1-avg-fps.png",
		"scenario-paced/2-regime-ticks-light.png",
		"summary/runs.png",
		"summary/verdicts-heavy.png",
	}
	if got := paths(figs); !slices.Equal(got, want) {
		t.Errorf("paths = %q, want %q", got, want)
	}
	// Without POPULATION there is no verdict image at all.
	if got := paths(mustBuild(t, synthetic("a.log", "20300101-000001"))); slices.ContainsFunc(got, func(p string) bool {
		return strings.Contains(p, "verdicts")
	}) {
		t.Errorf("paths without population = %q", got)
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"AVG FPS":              "avg-fps",
		"1% LOW FPS (p99)":     "1pct-low-fps-p99",
		"0.1% LOW FPS (p99.9)": "0-1pct-low-fps-p99-9",
		"JOB WAKE p99.9":       "job-wake-p99-9",
		"LATE FRAMES %":        "late-frames-pct",
		"  ((x))  ":            "x",
		"µs only µ":            "s-only",
		"...":                  "metric",
		"":                     "metric",
	}
	for in, want := range cases {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestFileNumbering checks the zero padding and that labels with the same
// slug still get distinct names.
func TestFileNumbering(t *testing.T) {
	r := synthetic("a.log", "20300101-000001")
	sc := &r.Scenarios[1]
	for i := range 9 {
		// "LOW (A)" and "LOW A" share a slug.
		label := "LOW A"
		if i%2 == 0 {
			label = "LOW (A)"
		}
		sc.Rows = append(sc.Rows, row(report.Metric{Label: label, Unit: fmt.Sprint(i)}, cell(1, 0), cell(2, 0), cell(3, 0), cell(4, 0)))
	}
	var names []string
	seen := map[string]bool{}
	for _, f := range mustBuild(t, r) {
		if dir, name, _ := strings.Cut(f.Path, "/"); dir == "scenario-paced" {
			names = append(names, name)
			if seen[name] {
				t.Errorf("%s is used twice", name)
			}
			seen[name] = true
		}
	}
	if len(names) != 11 || names[0] != "01-avg-fps.png" || names[2] != "03-low-a.png" || names[10] != "11-low-a.png" {
		t.Errorf("names = %q", names)
	}
	if !slices.IsSorted(names) {
		t.Errorf("names do not sort in table order: %q", names)
	}
}

func TestScenarioDirectories(t *testing.T) {
	if got := scenarioDir("EACH_CONTENDED"); got != "scenario-each_contended" {
		t.Errorf("scenarioDir = %s", got)
	}
	if got := scenarioDir("../X"); got != "scenario----x" {
		t.Errorf("scenarioDir = %s", got)
	}
	if got := scenarioDir("summary"); got == summaryDir {
		t.Errorf("scenario SUMMARY takes the directory %s", got)
	}
	r := synthetic("a.log", "20300101-000001")
	r.Scenarios[1].Name = "HE/AVY"
	r.Scenarios[0].Name = "HE AVY"
	if _, err := Build([]*report.Report{r}, mustSet(t, r), measure); err == nil || !strings.Contains(err.Error(), "scenario-he-avy") {
		t.Errorf("two scenarios in one directory give %v", err)
	}
}

func TestMetricImages(t *testing.T) {
	figs := mustBuild(t, synthetic("a.log", "20300101-000001"))
	for _, f := range figs {
		if !strings.HasPrefix(f.Path, "scenario-") {
			continue
		}
		o := decode(t, f)
		if len(o.Grid) != 1 || len(o.YAxis) != 1 {
			t.Errorf("%s has %d grids and %d y axes, want one plot", f.Path, len(o.Grid), len(o.YAxis))
		}
		if strings.HasSuffix(f.Path, deltaFile+".png") {
			continue
		}
		// One bar per column, the whiskers and the labels.
		if len(o.Series) != 4+2 {
			t.Errorf("%s has %d series, want 6", f.Path, len(o.Series))
		}
		legend := 0
		for _, l := range o.Legend {
			legend += len(l.Data)
		}
		if legend != 4 {
			t.Errorf("%s has %d legend entries, want 4", f.Path, legend)
		}
	}
	titles := []string{}
	for _, p := range []string{"scenario-heavy/1-avg-fps.png", "scenario-heavy/2-frame-time-p99.png", "scenario-heavy/3-job-wake-p99.png", "scenario-paced/2-regime-ticks-light.png"} {
		titles = append(titles, decode(t, byPath(t, figs, p)).Title[0].Text)
	}
	want := []string{"HEAVY · AVG FPS ↑", "HEAVY · FRAME TIME p99 (ms) ↓", "HEAVY · JOB WAKE p99 (µs) ↓", "PACED · REGIME TICKS LIGHT"}
	if !slices.Equal(titles, want) {
		t.Errorf("titles = %q, want %q", titles, want)
	}
}

func TestDeltaImage(t *testing.T) {
	figs := mustBuild(t, synthetic("a.log", "20300101-000001"))
	o := decode(t, byPath(t, figs, "scenario-heavy/4-change-vs-eevdf.png"))
	// One bar series per non-baseline column, and EEVDF has no deltas.
	var names []string
	for _, s := range o.Series {
		if s.Type != "bar" || len(s.Data) != 2 {
			t.Errorf("delta series %s is %s with %d items", s.Name, s.Type, len(s.Data))
		}
		names = append(names, s.Name)
	}
	if !slices.Equal(names, []string{"sched_a", "sched_b", "sched_c"}) {
		t.Errorf("delta series = %q", names)
	}
	if o.Title[0].Text != "HEAVY · "+deltaName || !strings.Contains(captionText(o), "its own EEVDF") {
		t.Errorf("delta title %q, caption %q", o.Title[0].Text, captionText(o))
	}

	// A scenario whose deltas are all invalid has nothing to draw.
	r := synthetic("a.log", "20300101-000001")
	r.Scenarios[1].Deltas = []report.Delta{{Metric: fps, Cells: make([]report.DeltaCell, 4)}}
	for _, p := range paths(mustBuild(t, r)) {
		if strings.HasPrefix(p, "scenario-paced/") && strings.Contains(p, deltaFile) {
			t.Errorf("invalid deltas give %s", p)
		}
	}
}

func TestCaptions(t *testing.T) {
	figs := mustBuild(t, synthetic("a.log", "20300101-000001"))
	cases := []struct {
		path        string
		clamp, na   bool
		clampString string
	}{
		{"scenario-heavy/1-avg-fps.png", false, true, ""},
		{"scenario-heavy/2-frame-time-p99.png", false, false, ""},
		{"scenario-heavy/3-job-wake-p99.png", true, false, clampLog},
		{"scenario-paced/2-regime-ticks-light.png", false, false, ""},
	}
	for _, c := range cases {
		text := captionText(decode(t, byPath(t, figs, c.path)))
		flat := strings.ReplaceAll(text, "\n", " ")
		if !strings.Contains(flat, "±1 sample standard deviation") || !strings.Contains(flat, "Source: /some/dir/a.log (VERSION 1.2.3, COMMIT abc1234, CPUS 8, ITERATIONS 5).") {
			t.Errorf("%s caption lacks the encoding or the source: %q", c.path, flat)
		}
		if got := strings.Contains(flat, "stops at"); got != c.clamp {
			t.Errorf("%s: clamp sentence = %v, want %v", c.path, got, c.clamp)
		}
		if c.clamp && !strings.Contains(flat, c.clampString) {
			t.Errorf("%s caption lacks %q", c.path, c.clampString)
		}
		if got := strings.Contains(flat, naCaption); got != c.na {
			t.Errorf("%s: n/a sentence = %v, want %v", c.path, got, c.na)
		}
	}

	// A linear axis clamps at zero.
	r := synthetic("a.log", "20300101-000001")
	r.Scenarios[1].Rows[1].Cells[2] = cell(5, 9)
	text := strings.ReplaceAll(captionText(decode(t, byPath(t, mustBuild(t, r), "scenario-paced/2-regime-ticks-light.png"))), "\n", " ")
	if !strings.Contains(text, clampLinear) {
		t.Errorf("linear clamp caption = %q", text)
	}

	// Logs that differ get a source each and the difference lines.
	other := synthetic("b.log", "20300101-000002")
	other.Meta.CPUs = 16
	text = strings.ReplaceAll(captionText(decode(t, mustBuild(t, synthetic("a.log", "20300101-000001"), other)[0])), "\n", " ")
	for _, want := range []string{"Sources: /some/dir/a.log (VERSION 1.2.3, COMMIT abc1234, CPUS 8, ITERATIONS 5); /some/dir/b.log (", "Note: CPUS differs: 8 (/some/dir/a.log), 16 (/some/dir/b.log)."} {
		if !strings.Contains(text, want) {
			t.Errorf("caption %q lacks %q", text, want)
		}
	}
}

// TestLogAxisValuesArePositive checks every value sent to a log axis: bars,
// both whisker ends and the label points.
func TestLogAxisValuesArePositive(t *testing.T) {
	logAxes := 0
	for _, f := range mustBuild(t, synthetic("a.log", "20300101-000001")) {
		o := decode(t, f)
		for i, a := range o.YAxis {
			if a.Type != "log" {
				continue
			}
			logAxes++
			if a.Min <= 0 || a.Max <= a.Min {
				t.Errorf("%s: log axis spans %v..%v", f.Path, a.Min, a.Max)
			}
			for _, s := range o.Series {
				if s.YAxisIndex != i {
					continue
				}
				for _, raw := range s.Data {
					for _, v := range values(t, raw, s.Type) {
						if v <= 0 || v < a.Min || v > a.Max {
							t.Errorf("%s: %s series has value %v outside %v..%v", f.Path, s.Type, v, a.Min, a.Max)
						}
					}
				}
			}
			if !strings.Contains(string(f.Option), logMark) {
				t.Errorf("%s: the log axis has no mark", f.Path)
			}
		}
	}
	// Only the wake metric spreads over 20x.
	if logAxes != 1 {
		t.Errorf("%d log axes, want 1", logAxes)
	}
}

// spec is a metric image like the real ones, with a legend entry per column.
func spec(columns int) chartSpec {
	legend := make([]string, columns)
	for i := range legend {
		legend[i] = fmt.Sprintf("sched_%d", i)
	}
	return chartSpec{
		minPlotW: float64(columns * minBand), axisW: 50, footH: plotFoot,
		titleW: measure("HEAVY · FRAME TIME p99 (ms) ↓", titleSize, true), legend: legend,
		caption: []string{encodingText, "Source: a.log (VERSION 1.2.3, COMMIT abc1234, CPUS 8, ITERATIONS 5)."},
	}
}

// TestLayoutWidthGrowsWithColumns checks that the width grows strictly from
// 4 to 60 columns. Up to 3 columns minWidth sets it instead.
func TestLayoutWidthGrowsWithColumns(t *testing.T) {
	prev := 0
	widths := map[int]int{}
	for c := 1; c <= 60; c++ {
		l, err := layoutChart(spec(c), measure)
		if err != nil {
			t.Fatal(err)
		}
		widths[c] = l.width
		if c <= 3 && l.width != minWidth {
			t.Errorf("%d columns give width %d, want minWidth %d", c, l.width, minWidth)
		}
		if c >= 4 && l.width <= prev {
			t.Errorf("%d columns give width %d, not more than %d", c, l.width, prev)
		}
		if want := 2*margin + 50 + c*minBand; c >= 4 && l.width != want {
			t.Errorf("%d columns give width %d, want %d", c, l.width, want)
		}
		prev = l.width
	}
	if w := widths[4]; w < 600 || w > 800 {
		t.Errorf("4 columns give width %d, want 600 to 800", w)
	}
	if widths[8] < widths[4]*3/2 {
		t.Errorf("8 columns give width %d, not much more than %d for 4", widths[8], widths[4])
	}
}

func TestLayoutHeightGrows(t *testing.T) {
	base, err := layoutChart(spec(4), measure)
	if err != nil {
		t.Fatal(err)
	}
	if len(base.legendRows) != 1 {
		t.Errorf("4 columns take %d legend rows, want 1", len(base.legendRows))
	}
	more := spec(4)
	more.caption = append(more.caption, "Note: CPUS differs.")
	taller, err := layoutChart(more, measure)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := taller.height-base.height, int(lineHeight(captionSize)); got != want {
		t.Errorf("one more caption line adds %d px, want %d", got, want)
	}
	crowded := spec(4)
	for i := range 12 {
		crowded.legend = append(crowded.legend, fmt.Sprintf("sched_%d (20300101-000001)", i))
	}
	wide, err := layoutChart(crowded, measure)
	if err != nil {
		t.Fatal(err)
	}
	if len(wide.legendRows) < 2 || wide.height != base.height+(len(wide.legendRows)-1)*legendRowH {
		t.Errorf("16 legend entries take %d rows and height %d, from %d", len(wide.legendRows), wide.height, base.height)
	}
	if wide.plotTop <= base.plotTop || wide.captionTop-wide.plotTop != base.captionTop-base.plotTop {
		t.Errorf("legend rows move the plot from %v to %v", base.plotTop, wide.plotTop)
	}
}

func TestLayoutTooWide(t *testing.T) {
	_, err := layoutChart(spec(100), measure)
	if err == nil || !strings.Contains(err.Error(), strconv.Itoa(render.MaxSide)) {
		t.Errorf("100 columns give %v, want an error naming the limit", err)
	}
}

func TestManyColumnsGetPatterns(t *testing.T) {
	var reports []*report.Report
	for i := range 4 {
		reports = append(reports, synthetic(fmt.Sprintf("%d.log", i), fmt.Sprintf("2030010%d-000001", i+1)))
	}
	o := decode(t, mustBuild(t, reports...)[0])
	seen := map[string]bool{}
	for _, s := range o.Series {
		if s.Type != "bar" {
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
		t.Errorf("%d distinct bar styles, want 16", len(seen))
	}
	legend := 0
	for _, l := range o.Legend {
		legend += len(l.Data)
	}
	if legend != 16 || len(o.Legend) < 2 {
		t.Errorf("legend has %d entries in %d rows, want 16 in several", legend, len(o.Legend))
	}
}

func TestSummary(t *testing.T) {
	other := synthetic("b.log", "20300101-000002")
	other.Notes = []string{"the audio thread ran late"}
	figs := mustBuild(t, withPopulation(synthetic("a.log", "20300101-000001")), other)
	v := byPath(t, figs, "summary/verdicts-heavy.png")
	o := decode(t, v)
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
	for _, want := range []string{"HEAVY · MCB verdicts (montauk)", `d=+1.00 large\np=0.008`, "AVG FPS ↑", "multiple comparisons with the best"} {
		if !strings.Contains(string(v.Option), want) {
			t.Errorf("verdict option lacks %q", want)
		}
	}

	runs := string(byPath(t, figs, "summary/runs.png").Option)
	for _, want := range []string{"b.log: AUDIO: the audio thread ran late", "b.log has no POPULATION data", "abc1234", "+0.1%", "1000.500"} {
		if !strings.Contains(runs, want) {
			t.Errorf("runs option lacks %q", want)
		}
	}

	runs = string(byPath(t, mustBuild(t, synthetic("a.log", "20300101-000001")), "summary/runs.png").Option)
	if !strings.Contains(runs, "No log has POPULATION data") {
		t.Error("runs without population does not say so")
	}
}

func TestEmptyScenario(t *testing.T) {
	r := synthetic("a.log", "20300101-000001")
	r.Scenarios = append(r.Scenarios, report.Scenario{Name: "EMPTY", Schedulers: r.Scenarios[0].Schedulers})
	for _, p := range paths(mustBuild(t, r)) {
		if strings.Contains(p, "empty") {
			t.Errorf("a scenario without rows gives %s", p)
		}
	}
}
