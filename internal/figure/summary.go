// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package figure

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"

	"github.com/galpt/prism-log-parser/internal/compare"
	"github.com/galpt/prism-log-parser/internal/report"
)

// Geometry of the summary figure, in px.
const (
	tableRowH   = 24
	tablePadX   = 22
	cellH       = 42
	cellPadX    = 18
	minCellW    = 104
	headingPad  = 6
	columnHeadH = 26
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

// textAt is a line or block of text placed with its top-left corner at x, y.
func textAt(text string, x, y, size float64, bold bool, color string) opts.Title {
	weight := "normal"
	if bold {
		weight = "bold"
	}
	return opts.Title{Title: text, Left: px(x), Top: px(y), TitleStyle: &opts.TextStyle{
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

const verdictExplanation = "Each row is one metric in one scenario, and each column one scheduler of one log. " +
	"The color is montauk's MCB verdict (multiple comparisons with the best), computed within each log over its iterations, " +
	"so columns of different logs are never compared with each other. " +
	"In the other cells, d is Cliff's delta of the row's winner in that log against the column, with its magnitude, " +
	"and p is the exact two-sided permutation p-value of the same pair."

// summaryFigure lays out the run table, the verdict heatmap when there is
// one, and the notes, top to bottom.
func summaryFigure(reports []*report.Report, set *compare.Set, m Measure) (Figure, error) {
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

	summary := set.Summary()
	cellW, labelW := float64(minCellW), 0.0
	rowLabels := make([]string, len(summary.Rows))
	for i, row := range summary.Rows {
		rowLabels[i] = metricName(row.Metric) + " · " + row.Scenario
		labelW = math.Max(labelW, m(rowLabels[i], bodySize, false))
		for _, s := range row.Cells {
			_, text := verdictCell(s)
			for _, line := range strings.Split(text, "\n") {
				cellW = math.Max(cellW, m(line, valueSize, true)+cellPadX)
			}
		}
	}
	for _, c := range summary.Columns {
		cellW = math.Max(cellW, m(c.Label, bodySize, false)+cellPadX)
	}
	gridLeft := margin + labelW + 14
	heatW := 0.0
	if len(summary.Rows) > 0 {
		heatW = gridLeft - margin + float64(len(summary.Columns))*cellW
	}

	title := "PRISM-FRAME · RUN SUMMARY"
	inner := math.Max(math.Max(tableW, heatW), math.Max(minSummaryW-2*margin, m(title, figureTitleSize, true)))
	width := 2*margin + inner
	if width > maxSide {
		return Figure{}, fmt.Errorf("%d columns need an image %.0f px wide, more than the %d px that render allows", len(summary.Columns), width, maxSide)
	}

	hm := charts.NewHeatMap()
	hm.SetGlobalOptions(baseOptions()...)
	var miss missing
	miss.titles = append(miss.titles, textAt(title, margin, titleTop, figureTitleSize, true, ink))
	y := titleTop + lineHeight(figureTitleSize) + sectionGap

	heading := func(text string) {
		miss.titles = append(miss.titles, textAt(text, margin, y, headingSize, true, ink))
		y += lineHeight(headingSize) + headingPad
	}
	paragraph := func(text string, color string) {
		lines := wrap(text, math.Min(inner, maxTextW), bodySize, false, m)
		miss.titles = append(miss.titles, textAt(strings.Join(lines, "\n"), margin, y, bodySize, false, color))
		y += float64(len(lines)) * lineHeight(bodySize)
	}

	// A booktabs table: heavier rules above and below, a light one under the
	// header, and no vertical lines.
	heading("Runs")
	miss.graphic = append(miss.graphic, ruleLine(margin, y, margin+tableW, y, 1.5))
	for r := -1; r < len(rows); r++ {
		cells, bold := header, true
		if r >= 0 {
			cells, bold = rows[r], false
		}
		x := float64(margin)
		for i, text := range cells {
			miss.titles = append(miss.titles, textAt(text, x, y+1, bodySize, bold, ink))
			x += colW[i]
		}
		y += tableRowH
		if r == -1 {
			miss.graphic = append(miss.graphic, ruleLine(margin, y, margin+tableW, y, 0.8))
		}
	}
	miss.graphic = append(miss.graphic, ruleLine(margin, y, margin+tableW, y, 1.5))
	y += 2 * sectionGap

	if len(summary.Rows) > 0 {
		heading("MCB verdicts (montauk)")
		paragraph(verdictExplanation, ink)
		y += headingPad
		names := make([]string, len(states))
		for i, s := range states {
			names[i] = s.name
		}
		miss.legends = append(miss.legends, opts.Legend{
			Data: names, Left: px(margin), Top: px(y), Icon: "rect", ItemWidth: legendIconW, ItemHeight: legendIconH,
			TextStyle: &opts.TextStyle{FontFamily: fontFamily, FontSize: legendSize, Color: ink},
			ItemStyle: &opts.ItemStyle{BorderColor: muted, BorderWidth: 0.6},
		})
		y += legendRowH + headingPad
		top := y + columnHeadH
		verdictGrid(hm, summary, rowLabels, gridLeft, top, cellW)
		// A line between the columns of two logs, from the column labels to
		// the bottom row, groups the verdicts that were computed together.
		for c := 1; c < len(summary.Columns); c++ {
			if summary.Columns[c].Log != summary.Columns[c-1].Log {
				x := gridLeft + float64(c)*cellW
				sep := ruleLine(x, y, x, top+float64(len(summary.Rows))*cellH, 1.5)
				// Above the cells, which would otherwise cover it.
				sep.Z = 10
				miss.graphic = append(miss.graphic, sep)
			}
		}
		y += columnHeadH + float64(len(summary.Rows))*cellH + 2*sectionGap
	} else {
		// A heatmap chart always has its default axes, so they are given a
		// grid that is hidden instead of left to ECharts' default placement.
		hm.SetGlobalOptions(
			charts.WithGridOpts(opts.Grid{Show: opts.Bool(false), Left: "0", Top: "0", Width: "1", Height: "1"}),
			charts.WithXAxisOpts(opts.XAxis{Show: opts.Bool(false)}),
			charts.WithYAxisOpts(opts.YAxis{Show: opts.Bool(false)}),
		)
	}

	heading("Notes")
	for _, n := range summaryNotes(reports, set) {
		paragraph(n, ink)
		y += 4
	}
	height := int(math.Ceil(y + margin))
	if height > maxSide {
		return Figure{}, fmt.Errorf("the figure needs an image %d px tall, more than the %d px that render allows", height, maxSide)
	}
	miss.yTick = opts.AxisTick{Show: opts.Bool(false)}
	option, err := miss.apply(hm)
	if err != nil {
		return Figure{}, err
	}
	return Figure{Name: "prism-summary.png", Width: int(math.Ceil(width)), Height: height, Option: option}, nil
}

// verdictGrid adds the heatmap with one series per state, which gives each
// state its color and its legend entry without a visualMap.
func verdictGrid(hm *charts.HeatMap, t compare.SummaryTable, rowLabels []string, left, top, cellW float64) {
	cols := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		cols[i] = c.Label
	}
	hm.SetGlobalOptions(
		charts.WithGridOpts(opts.Grid{
			Left: px(left), Top: px(top), Width: px(float64(len(cols)) * cellW), Height: px(float64(len(t.Rows)) * cellH),
		}),
		charts.WithXAxisOpts(opts.XAxis{
			Type: "category", Position: "top", Data: cols,
			AxisLine: &opts.AxisLine{Show: opts.Bool(false)}, AxisTick: &opts.AxisTick{Show: opts.Bool(false)},
			AxisLabel: &opts.AxisLabel{Color: ink, FontSize: bodySize, FontFamily: fontFamily, Interval: "0", Margin: 8},
		}),
		charts.WithYAxisOpts(opts.YAxis{
			Type: "category", Inverse: opts.Bool(true), Data: rowLabels,
			AxisLine:  &opts.AxisLine{Show: opts.Bool(false)},
			AxisLabel: &opts.AxisLabel{Color: ink, FontSize: bodySize, FontFamily: fontFamily, Interval: "0", Margin: 10},
		}),
	)
	data := make([][]opts.HeatMapData, len(states))
	for r, row := range t.Rows {
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
				FontSize: valueSize, FontFamily: fontFamily, LineHeight: 14,
			}),
		)
	}
}
