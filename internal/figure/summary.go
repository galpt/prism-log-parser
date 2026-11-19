// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package figure

import (
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"

	"github.com/galpt/prism-log-parser/internal/compare"
	"github.com/galpt/prism-log-parser/internal/render"
	"github.com/galpt/prism-log-parser/internal/report"
)

// Geometry of the summary images, in layout px.
const (
	tableRowH = 28
	tablePadX = 24
	// A verdict cell holds two lines of cellSize text.
	cellH       = 50
	cellPadX    = 18
	minCellW    = 120
	headingPad  = 8
	minSummaryW = 900
)

// state is how a verdict cell is drawn. The colors are an ordinal blue scale
// that stays in order when printed in grayscale, with a neutral gray for a
// cell that has no verdict at all.
type state struct {
	name, fill, text string
	bold             bool
}

var states = []state{
	{"best", "#08519c", white, true},
	{"tied", "#6baed6", ink, false},
	{"not best", "#c6dbef", ink, false},
	{"no verdict", noneCell, muted, false},
}

const (
	stateBest = iota
	stateTied
	stateNotBest
	stateNone
)

// verdictCell returns the state and the text of one heatmap cell. The winner
// of a group has no pair line, so its cell says what it is. Every other cell
// shows how the winner compares with it.
func verdictCell(s compare.Standing) (int, string) {
	if !s.Present {
		return stateNone, "–"
	}
	st := stateBest
	switch s.Verdict {
	case report.Tied:
		st = stateTied
	case report.NotBest:
		st = stateNotBest
	}
	if s.Versus == nil {
		if st == stateBest {
			return st, "best"
		}
		return st, "top mean"
	}
	return st, fmt.Sprintf("d=%+.2f %s\np=%.3f", s.Versus.Cliff, s.Versus.Magnitude, s.Versus.P)
}

// titlePad is the padding that ECharts puts around every title, which
// opts.Title has no field to change.
const titlePad = 5

// textAt is a line or block of text placed with its top-left corner at x, y.
// The title is moved up and left by its padding, so that the text itself
// starts at x, y where the layout measured it.
func textAt(text string, x, y, size float64, bold bool, color string) opts.Title {
	weight := "normal"
	if bold {
		weight = "bold"
	}
	return opts.Title{Title: text, Left: px(x - titlePad), Top: px(y - titlePad), TitleStyle: &opts.TextStyle{
		FontFamily: fontFamily, FontSize: int(size), FontWeight: weight, Color: color, LineHeight: int(lineHeight(size)),
	}}
}

// runTable is the metadata table, one row per log, as text cells.
func runTable(reports []*report.Report) (header []string, rows [][]string) {
	header = []string{"Log", "VERSION", "COMMIT", "TIMESTAMP", "CPUS", "ITERATIONS", "CALIBRATION (iterations/µs)", "DRIFT"}
	for _, r := range reports {
		drift := "n/a"
		if r.Meta.HasDrift {
			drift = fmt.Sprintf("%+.1f%%", r.Meta.Drift)
		}
		rows = append(rows, []string{
			r.Source, r.Meta.Version, commitText(r.Meta), r.Meta.Timestamp,
			strconv.Itoa(r.Meta.CPUs), strconv.Itoa(r.Meta.Iterations),
			strconv.FormatFloat(r.Meta.Calibration, 'f', 3, 64), drift,
		})
	}
	return header, rows
}

// summaryNotes gathers the lines that the logs carry besides their tables.
func summaryNotes(reports []*report.Report, set *compare.Set) []string {
	var notes []string
	withPopulation := 0
	for _, r := range reports {
		if len(r.Population) > 0 {
			withPopulation++
		}
	}
	for _, r := range reports {
		for _, n := range r.Notes {
			notes = append(notes, r.Source+": AUDIO: "+n)
		}
		for _, s := range r.Skipped {
			notes = append(notes, r.Source+": SKIPPED "+s.Scheduler+": "+s.Reason)
		}
		for _, t := range r.PopulationText {
			notes = append(notes, r.Source+": montauk: "+t)
		}
		if withPopulation > 0 && len(r.Population) == 0 {
			notes = append(notes, r.Source+" has no POPULATION data, so its columns have no verdict.")
		}
	}
	if withPopulation == 0 {
		notes = append(notes, "No log has POPULATION data, so there are no MCB verdicts. "+
			"prism-frame adds them when ITERATIONS is at least 2 and montauk is installed.")
	}
	notes = append(notes, set.Differences()...)
	if len(notes) == 0 {
		none := "None: no AUDIO or SKIPPED lines and no montauk messages."
		if len(reports) > 1 {
			none += " The logs agree on VERSION, COMMIT, CPUS and ITERATIONS."
		}
		notes = append(notes, none)
	}
	return notes
}

const verdictExplanation = "Each row is one metric and each column one scheduler of one log. " +
	"The color is montauk's MCB verdict (multiple comparisons with the best), computed within each log over its iterations, " +
	"so columns of different logs are never compared with each other. " +
	"In the other cells, d is Cliff's delta of the row's winner in that log against the column, with its magnitude, " +
	"and p is the exact two-sided permutation p-value of the same pair."

// textBlock lays text out top to bottom from y, wrapped to a width, and
// collects it as titles.
type textBlock struct {
	m      Measure
	width  float64
	y      float64
	titles []opts.Title
}

func (t *textBlock) heading(text string) {
	t.titles = append(t.titles, textAt(text, margin, t.y, headingSize, true, ink))
	t.y += lineHeight(headingSize) + headingPad
}

func (t *textBlock) paragraph(text string, size float64) {
	lines := wrap(text, math.Min(t.width, maxTextW), size, false, t.m)
	t.titles = append(t.titles, textAt(strings.Join(lines, "\n"), margin, t.y, size, false, ink))
	t.y += float64(len(lines)) * lineHeight(size)
}

// runsFigure lays out the run table and the notes, top to bottom.
func runsFigure(reports []*report.Report, set *compare.Set, m Measure) (Figure, error) {
	header, rows := runTable(reports)
	colW := make([]float64, len(header))
	for i, h := range header {
		colW[i] = m(h, bodySize, true)
		for _, r := range rows {
			colW[i] = math.Max(colW[i], m(r[i], bodySize, false))
		}
		colW[i] += tablePadX
	}
	tableW := 0.0
	for _, w := range colW {
		tableW += w
	}
	title := "PRISM-FRAME · RUNS"
	inner := math.Max(tableW, math.Max(minSummaryW-2*margin, m(title, titleSize, true)))
	width := int(math.Ceil(2*margin + inner))
	if width > render.MaxSide {
		return Figure{}, fmt.Errorf("the run table needs an image %d px wide, more than the %d px that render allows", width, render.MaxSide)
	}

	hm := charts.NewHeatMap()
	hm.SetGlobalOptions(append(baseOptions(), hiddenAxes()...)...)
	var miss missing
	t := &textBlock{m: m, width: inner}
	t.titles = append(t.titles, textAt(title, margin, titleTop, titleSize, true, ink))
	t.y = titleTop + lineHeight(titleSize) + sectionGap

	// A booktabs table: heavier rules above and below, a light one under the
	// header, and no vertical lines.
	miss.graphic = append(miss.graphic, ruleLine(margin, t.y, margin+tableW, t.y, 1.5))
	for r := -1; r < len(rows); r++ {
		cells, bold := header, true
		if r >= 0 {
			cells, bold = rows[r], false
		}
		x := float64(margin)
		for i, text := range cells {
			t.titles = append(t.titles, textAt(text, x, t.y+(tableRowH-lineHeight(bodySize))/2, bodySize, bold, ink))
			x += colW[i]
		}
		t.y += tableRowH
		if r == -1 {
			miss.graphic = append(miss.graphic, ruleLine(margin, t.y, margin+tableW, t.y, 0.8))
		}
	}
	miss.graphic = append(miss.graphic, ruleLine(margin, t.y, margin+tableW, t.y, 1.5))
	t.y += 2 * sectionGap

	t.heading("Notes")
	for _, n := range summaryNotes(reports, set) {
		t.paragraph(n, bodySize)
		t.y += 6
	}
	height := int(math.Ceil(t.y + margin))
	if height > render.MaxSide {
		return Figure{}, fmt.Errorf("the notes need an image %d px tall, more than the %d px that render allows", height, render.MaxSide)
	}
	miss.titles = t.titles
	miss.yTick = opts.AxisTick{Show: opts.Bool(false)}
	option, err := miss.apply(hm)
	if err != nil {
		return Figure{}, err
	}
	return Figure{Path: path.Join(summaryDir, "runs.png"), Width: width, Height: height, Option: option}, nil
}

// hiddenAxes gives the default axes of a chart without a plot a grid that is
// hidden, instead of leaving them to ECharts' default placement.
func hiddenAxes() []charts.GlobalOpts {
	return []charts.GlobalOpts{
		charts.WithGridOpts(opts.Grid{Show: opts.Bool(false), Left: "0", Top: "0", Width: "1", Height: "1"}),
		charts.WithXAxisOpts(opts.XAxis{Show: opts.Bool(false)}),
		charts.WithYAxisOpts(opts.YAxis{Show: opts.Bool(false)}),
	}
}

// verdictFigure draws the montauk verdicts of one scenario, one row per
// metric and one column per column of the Set. It reports false when the
// scenario has no verdict rows, and then there is no image.
func verdictFigure(summary compare.SummaryTable, scenario string, m Measure) (Figure, bool, error) {
	var rows []compare.SummaryRow
	for _, r := range summary.Rows {
		if r.Scenario == scenario {
			rows = append(rows, r)
		}
	}
	if len(rows) == 0 {
		return Figure{}, false, nil
	}
	cellW, labelW := float64(minCellW), 0.0
	rowLabels := make([]string, len(rows))
	for i, row := range rows {
		rowLabels[i] = metricName(row.Metric)
		labelW = math.Max(labelW, m(rowLabels[i], bodySize, false))
		for _, s := range row.Cells {
			st, text := verdictCell(s)
			for _, line := range strings.Split(text, "\n") {
				cellW = math.Max(cellW, m(line, cellSize, states[st].bold)+cellPadX)
			}
		}
	}
	// The column labels are wrapped to the cell width here, so that ECharts
	// never wraps them on its own and the room above the cells is known.
	cols := make([]string, len(summary.Columns))
	lines := 1
	for i, c := range summary.Columns {
		wrapped := wrap(c.Label, cellW-8, bodySize, false, m)
		lines = max(lines, len(wrapped))
		cols[i] = strings.Join(wrapped, "\n")
	}
	columnHeadH := float64(lines)*lineHeight(bodySize) + 10
	gridLeft := margin + labelW + 14
	gridW := float64(len(cols)) * cellW

	title := scenario + " · MCB verdicts (montauk)"
	inner := math.Max(gridLeft-margin+gridW, math.Max(minSummaryW-2*margin, m(title, titleSize, true)))
	width := int(math.Ceil(2*margin + inner))
	if width > render.MaxSide {
		return Figure{}, false, fmt.Errorf("%d columns need an image %d px wide, more than the %d px that render allows", len(cols), width, render.MaxSide)
	}

	hm := charts.NewHeatMap()
	hm.SetGlobalOptions(baseOptions()...)
	var miss missing
	t := &textBlock{m: m, width: inner}
	t.titles = append(t.titles, textAt(title, margin, titleTop, titleSize, true, ink))
	t.y = titleTop + lineHeight(titleSize) + sectionGap
	t.paragraph(verdictExplanation, captionSize)
	t.y += sectionGap

	names := make([]string, len(states))
	for i, s := range states {
		names[i] = s.name
	}
	miss.legends = append(miss.legends, opts.Legend{
		Data: names, Left: px(margin), Top: px(t.y), Padding: 0, Icon: "rect", ItemWidth: legendIconW, ItemHeight: legendIconH,
		TextStyle: &opts.TextStyle{FontFamily: fontFamily, FontSize: legendSize, Color: ink},
		ItemStyle: &opts.ItemStyle{BorderColor: muted, BorderWidth: 0.6},
	})
	t.y += legendRowH + sectionGap
	top := t.y + columnHeadH
	bottom := top + float64(len(rows))*cellH
	verdictGrid(hm, rows, cols, rowLabels, gridLeft, top, cellW)
	// A line between the columns of two logs, from the column labels to the
	// bottom row, groups the verdicts that were computed together.
	for c := 1; c < len(summary.Columns); c++ {
		if summary.Columns[c].Log != summary.Columns[c-1].Log {
			x := gridLeft + float64(c)*cellW
			sep := ruleLine(x, t.y, x, bottom, 1.5)
			// Above the cells, which would otherwise cover it.
			sep.Z = 10
			miss.graphic = append(miss.graphic, sep)
		}
	}
	height := int(math.Ceil(bottom + margin))
	if height > render.MaxSide {
		return Figure{}, false, fmt.Errorf("%d rows need an image %d px tall, more than the %d px that render allows", len(rows), height, render.MaxSide)
	}
	miss.titles = t.titles
	miss.yTick = opts.AxisTick{Show: opts.Bool(false)}
	miss.xLineHeight = lineHeight(bodySize)
	option, err := miss.apply(hm)
	if err != nil {
		return Figure{}, false, err
	}
	return Figure{Path: verdictPath(scenario), Width: width, Height: height, Option: option}, true, nil
}

// verdictGrid adds the heatmap with one series per state, which gives each
// state its color and its legend entry without a visualMap.
func verdictGrid(hm *charts.HeatMap, rows []compare.SummaryRow, cols, rowLabels []string, left, top, cellW float64) {
	hm.SetGlobalOptions(
		charts.WithGridOpts(opts.Grid{
			Left: px(left), Top: px(top), Width: px(float64(len(cols)) * cellW), Height: px(float64(len(rows)) * cellH),
		}),
		charts.WithXAxisOpts(opts.XAxis{
			Type: "category", Position: "top", Data: cols,
			AxisLine: &opts.AxisLine{Show: opts.Bool(false)}, AxisTick: &opts.AxisTick{Show: opts.Bool(false)},
			AxisLabel: &opts.AxisLabel{
				Color: ink, FontSize: bodySize, FontFamily: fontFamily, Interval: "0", Margin: 8,
			},
		}),
		charts.WithYAxisOpts(opts.YAxis{
			Type: "category", Inverse: opts.Bool(true), Data: rowLabels,
			AxisLine:  &opts.AxisLine{Show: opts.Bool(false)},
			AxisLabel: &opts.AxisLabel{Color: ink, FontSize: bodySize, FontFamily: fontFamily, Interval: "0", Margin: 12},
		}),
	)
	data := make([][]opts.HeatMapData, len(states))
	for r, row := range rows {
		for c, s := range row.Cells {
			st, text := verdictCell(s)
			data[st] = append(data[st], opts.HeatMapData{Name: text, Value: []int{c, r, st}})
		}
	}
	for i, s := range states {
		weight := "normal"
		if s.bold {
			weight = "bold"
		}
		hm.AddSeries(s.name, data[i],
			charts.WithItemStyleOpts(opts.ItemStyle{Color: s.fill, BorderColor: white, BorderWidth: 2}),
			charts.WithLabelOpts(opts.Label{
				Show: opts.Bool(true), Formatter: "{b}", Color: s.text, FontWeight: weight,
				FontSize: cellSize, FontFamily: fontFamily, LineHeight: float32(lineHeight(cellSize)),
			}),
		)
	}
}
