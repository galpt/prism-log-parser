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
	"github.com/go-echarts/go-echarts/v2/types"

	"github.com/galpt/prism-log-parser/internal/compare"
	"github.com/galpt/prism-log-parser/internal/report"
)

const (
	logMark   = "log scale"
	naText    = "n/a"
	deltaName = "Change vs EEVDF (%)"
	// deltaFile is the slug of the delta image. It leaves out the "(%)" of
	// deltaName, which would add a "-pct" that says nothing in a file name.
	deltaFile = "change-vs-eevdf"
)

// Caption sentences of the chart images.
const (
	encodingText = "Bars show the mean and whiskers ±1 sample standard deviation over each log's ITERATIONS runs, " +
		"and the number above a bar is the mean as the log printed it."
	naCaption     = "n/a marks a column whose log has no value for this metric."
	clampLinear   = "A whisker that would reach below zero stops at zero."
	clampLog      = "A whisker that would reach zero or below stops at the mean, since the log scale cannot show it."
	deltaEncoding = "Each bar is the percent change of a mean against the EEVDF column of the same log, " +
		"so each log is measured against its own EEVDF and bars from different logs do not share a baseline."
)

// meanText is the mean of a cell exactly as the log printed it.
func meanText(c report.Cell) string {
	mean, _, _ := strings.Cut(c.Text, " ±")
	return mean
}

// scenarioFigures returns the images of one scenario: one per metric row in
// table order, then the delta image when the scenario has a valid delta.
// colors maps a column label to its position in the Set, which picks its
// color and pattern, and sources are the caption lines that name the logs.
//
// The number in front of each file name is zero padded to the width of the
// image count, so the files sort in table order, and it alone already makes
// every name unique.
func scenarioFigures(t compare.ScenarioTable, colors map[string]int, sources []string, m Measure) ([]Figure, error) {
	var cols []int
	var axis axisRange
	if len(t.Deltas) > 0 {
		cols, axis = deltaColumns(t.Deltas)
	}
	count := len(t.Rows)
	if len(cols) > 0 {
		count++
	}
	dir := scenarioDir(t.Name)
	digits := len(strconv.Itoa(count))
	path := func(i int, s string) string { return fmt.Sprintf("%s/%0*d-%s.png", dir, digits, i+1, s) }

	var figs []Figure
	for i, row := range t.Rows {
		f, err := metricFigure(t, row, colors, sources, m)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", row.Metric.Label, err)
		}
		f.Path = path(i, slug(row.Metric.Label))
		figs = append(figs, f)
	}
	if len(cols) > 0 {
		f, err := deltaFigure(t, cols, axis, colors, sources, m)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", deltaName, err)
		}
		f.Path = path(len(t.Rows), deltaFile)
		figs = append(figs, f)
	}
	return figs, nil
}

// axisWidth is the room left of a plot for its tick labels, which sit 8px
// left of the axis line, after the 5px ticks.
func axisWidth(ticks []string, m Measure) float64 {
	w := 0.0
	for _, s := range ticks {
		w = math.Max(w, m(s, tickSize, false))
	}
	return w + 18
}

// titleWidth is the width of an image title, with the log scale mark after it
// when the axis is logarithmic.
func titleWidth(title string, log bool, m Measure) float64 {
	w := m(title, titleSize, true)
	if log {
		w += logGap + m(logMark, legendSize, false)
	}
	return w
}

// deltaGroupGap is the empty space between two groups of the delta image,
// in slots.
const deltaGroupGap = 0.75

// logGap is the space between a title and its log scale mark.
const logGap = 12

func columnLabels(cols []compare.Column) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.Label
	}
	return out
}

// metricFigure draws one metric of a scenario with one bar per column.
func metricFigure(t compare.ScenarioTable, row compare.RowSet, colors map[string]int, sources []string, m Measure) (Figure, error) {
	axis := metricAxis(row.Cells, plotH, labelHead)
	// Each column's slot must hold its widest value label without touching
	// the labels of its neighbors.
	band := float64(minBand)
	hasNA, clamped := false, false
	for _, c := range row.Cells {
		text := naText
		if c.Valid {
			text = meanText(c)
			if low, _ := whisker(c, axis.log); low != c.Mean-c.SD {
				clamped = true
			}
		} else {
			hasNA = true
		}
		band = math.Max(band, m(text, valueSize, false)+bandPad)
	}

	caption := []string{encodingText}
	if hasNA {
		caption = append(caption, naCaption)
	}
	switch {
	case clamped && axis.log:
		caption = append(caption, clampLog)
	case clamped:
		caption = append(caption, clampLinear)
	}
	caption = append(caption, sources...)

	title := t.Name + " · " + metricName(row.Metric)
	axisW := axisWidth(axis.ticks(), m)
	l, err := layoutChart(chartSpec{
		minPlotW: float64(len(t.Columns)) * band, axisW: axisW, footH: plotFoot,
		titleW: titleWidth(title, axis.log, m), legend: columnLabels(t.Columns), caption: caption,
	}, m)
	if err != nil {
		return Figure{}, err
	}

	b := newChartBuilder(l, colors, m)
	b.header(title, axis.log)
	categories := columnLabels(t.Columns)
	b.axes(opts.XAxis{
		Type: "category", Data: categories, AxisLine: axisLine(),
		AxisTick: &opts.AxisTick{Show: opts.Bool(false)}, AxisLabel: &opts.AxisLabel{Show: opts.Bool(false)},
	}, valueAxis(axis))
	b.logSeparators(t.Columns)

	// One series per column puts the column in the legend. Every series
	// holds a single bar at its own category, and a gap of -100% stacks the
	// empty slots of the others on top of it so the bar stays centered.
	var whiskers []opts.CustomData
	var labels []opts.ScatterData
	for c, col := range t.Columns {
		data := make([]opts.BarData, len(t.Columns))
		for k := range data {
			data[k] = opts.BarData{Value: "-"}
		}
		cell := row.Cells[c]
		if cell.Valid {
			data[c] = opts.BarData{Value: cell.Mean}
			low, high := whisker(cell, axis.log)
			whiskers = append(whiskers, opts.CustomData{Value: []float64{float64(c), low, high}})
			labels = append(labels, opts.ScatterData{Name: meanText(cell), Value: []float64{float64(c), high}})
		} else {
			labels = append(labels, opts.ScatterData{Name: naText, Value: []float64{float64(c), axis.min}})
		}
		b.addBar(col.Label, data, charts.WithBarChartOpts(opts.BarChart{BarGap: "-100%", BarWidth: "58%"}))
	}
	b.bar.MultiSeries = append(b.bar.MultiSeries,
		charts.SingleSeries{
			Type: types.ChartCustom, RenderItem: "errorBar", Data: whiskers,
			ItemStyle: &opts.ItemStyle{BorderColor: ink, BorderWidth: 1.2},
		},
		valueLabels(labels),
	)
	return b.finish()
}

// deltaColumns returns the columns that have at least one delta, which are
// the non-baseline ones, and an axis for every delta with room for the labels
// above positive bars and below negative ones.
func deltaColumns(deltas []compare.DeltaSet) ([]int, axisRange) {
	var cols []int
	lo, hi := 0.0, 0.0
	for c := range deltas[0].Cells {
		used := false
		for _, d := range deltas {
			if cell := d.Cells[c]; cell.Valid {
				used = true
				lo, hi = math.Min(lo, cell.Percent), math.Max(hi, cell.Percent)
			}
		}
		if used {
			cols = append(cols, c)
		}
	}
	return cols, linearAxis(lo, hi, plotH, labelHead, labelHead)
}

// deltaFigure draws the change of every delta metric against EEVDF, one group
// of bars per metric and one bar per column in cols.
func deltaFigure(t compare.ScenarioTable, cols []int, axis axisRange, colors map[string]int, sources []string, m Measure) (Figure, error) {
	// A slot holds one bar and must fit the widest percent label, since the
	// labels of neighboring bars sit side by side. Each group leaves a gap of
	// deltaGroupGap slots to the next.
	slot := 32.0
	for _, d := range t.Deltas {
		for _, c := range d.Cells {
			if c.Valid {
				slot = math.Max(slot, m(c.Text, valueSize, false)+6)
			}
		}
	}
	group := (float64(len(cols)) + deltaGroupGap) * slot
	// The category names are wrapped here, so ECharts never wraps them
	// differently and the room under the plot is known.
	categories := make([]string, len(t.Deltas))
	lines := 1
	for k, d := range t.Deltas {
		wrapped := wrap(metricName(d.Metric), group-8, tickSize, false, m)
		lines = max(lines, len(wrapped))
		categories[k] = strings.Join(wrapped, "\n")
	}
	legend := make([]string, len(cols))
	for i, c := range cols {
		legend[i] = t.Columns[c].Label
	}

	title := t.Name + " · " + deltaName
	l, err := layoutChart(chartSpec{
		minPlotW: float64(len(t.Deltas)) * group, axisW: axisWidth(axis.ticks(), m),
		footH: 10 + float64(lines)*lineHeight(tickSize), titleW: titleWidth(title, false, m),
		legend: legend, caption: append([]string{deltaEncoding}, sources...),
	}, m)
	if err != nil {
		return Figure{}, err
	}

	b := newChartBuilder(l, colors, m)
	b.miss.xLineHeight = lineHeight(tickSize)
	b.header(title, false)
	b.axes(opts.XAxis{
		Type: "category", Data: categories, AxisLine: axisLine(),
		AxisTick: &opts.AxisTick{Show: opts.Bool(false)},
		AxisLabel: &opts.AxisLabel{
			Color: ink, FontSize: tickSize, FontFamily: fontFamily, Interval: "0", Margin: 10,
		},
	}, valueAxis(axis))

	for n, c := range cols {
		data := make([]opts.BarData, len(t.Deltas))
		for k, d := range t.Deltas {
			cell := d.Cells[c]
			if !cell.Valid {
				data[k] = opts.BarData{Value: "-"}
				continue
			}
			pos := "top"
			if math.Signbit(cell.Percent) {
				pos = "bottom"
			}
			data[k] = opts.BarData{Value: cell.Percent, Label: &opts.Label{
				Show: opts.Bool(true), Position: pos, Formatter: types.FuncStr(cell.Text),
				FontSize: valueSize, FontFamily: fontFamily, Color: ink,
			}}
		}
		// The bars of a group touch the slots of their neighbors only through
		// the 16% gap, which keeps the labels above them apart.
		options := []charts.SeriesOpts{charts.WithBarChartOpts(opts.BarChart{BarWidth: px(slot / 1.16), BarGap: "16%"})}
		if n == 0 {
			// The zero line is drawn over the bars so that it reads as the
			// baseline even where a bar starts at it.
			options = append(options,
				charts.WithMarkLineNameYAxisItemOpts(opts.MarkLineNameYAxisItem{YAxis: 0}),
				charts.WithMarkLineStyleOpts(opts.MarkLineStyle{
					Symbol: []string{"none", "none"}, Label: &opts.Label{Show: opts.Bool(false)},
					LineStyle: &opts.LineStyle{Color: ink, Width: 1, Type: "solid"},
				}))
		}
		b.addBar(t.Columns[c].Label, data, options...)
	}
	return b.finish()
}

// chartBuilder accumulates the option of one chart image.
type chartBuilder struct {
	bar     *charts.Bar
	measure Measure
	colors  map[string]int
	layout  chartLayout
	miss    missing
}

func newChartBuilder(l chartLayout, colors map[string]int, m Measure) *chartBuilder {
	b := &chartBuilder{bar: charts.NewBar(), measure: m, colors: colors, layout: l,
		miss: missing{decals: map[int]*decal{}, yTick: opts.AxisTick{Show: opts.Bool(true), LineStyle: &opts.LineStyle{Color: ink, Width: 1}}}}
	b.bar.SetGlobalOptions(baseOptions()...)
	return b
}

// header adds the title, the log scale mark after it when log is set, and the
// legend rows.
func (b *chartBuilder) header(title string, log bool) {
	b.miss.titles = append(b.miss.titles, textAt(title, margin, titleTop, titleSize, true, ink))
	if log {
		// The mark is its own title because ECharts sets the plain part of a
		// rich title in its default style, which would lose the bold.
		x := margin + b.measure(title, titleSize, true) + logGap
		mark := textAt(logMark, x, titleTop+(titleSize-legendSize)/2+1, legendSize, false, ink)
		mark.TitleStyle.FontStyle = "italic"
		b.miss.titles = append(b.miss.titles, mark)
	}
	for i, row := range b.layout.legendRows {
		b.miss.legends = append(b.miss.legends, opts.Legend{
			// Without padding the swatches start at the margin, where the
			// rows were measured from.
			Data: row, Left: px(margin), Top: px(b.layout.legendTop + float64(i)*legendRowH), Padding: 0,
			Icon: "rect", ItemWidth: legendIconW, ItemHeight: legendIconH,
			TextStyle: &opts.TextStyle{FontFamily: fontFamily, FontSize: legendSize, Color: ink},
		})
	}
}

// axes places the one plot of the image.
func (b *chartBuilder) axes(x opts.XAxis, y opts.YAxis) {
	l := b.layout
	b.bar.SetGlobalOptions(
		charts.WithGridOpts(opts.Grid{Left: px(l.plotLeft), Top: px(l.plotTop), Width: px(l.plotW), Height: px(plotH)}),
		charts.WithXAxisOpts(x, 0), charts.WithYAxisOpts(y, 0),
	)
}

// logSeparators draws a dashed line between the columns of two logs, so that
// a reader sees which bars were measured together.
func (b *chartBuilder) logSeparators(cols []compare.Column) {
	l := b.layout
	band := l.plotW / float64(len(cols))
	for c := 1; c < len(cols); c++ {
		if cols[c].Log != cols[c-1].Log {
			x := l.plotLeft + float64(c)*band
			sep := ruleLine(x, l.plotTop, x, l.plotTop+plotH, 1)
			sep.Style.Stroke, sep.Style.LineDash = muted, []float64{5, 4}
			b.miss.graphic = append(b.miss.graphic, sep)
		}
	}
}

// addBar adds a bar series in the color and pattern of its column.
func (b *chartBuilder) addBar(label string, data []opts.BarData, options ...charts.SeriesOpts) {
	col := b.colors[label]
	if d := columnDecal(col); d != nil {
		b.miss.decals[len(b.bar.MultiSeries)] = d
	}
	b.bar.AddSeries(label, data, append(options, charts.WithItemStyleOpts(opts.ItemStyle{Color: columnColor(col), BorderColor: barEdge, BorderWidth: 0.8}))...)
}

// finish adds the caption and encodes the option.
func (b *chartBuilder) finish() (Figure, error) {
	l := b.layout
	b.miss.titles = append(b.miss.titles, textAt(strings.Join(l.caption, "\n"), margin, l.captionTop, captionSize, false, ink))
	option, err := b.miss.apply(b.bar)
	if err != nil {
		return Figure{}, err
	}
	return Figure{Width: l.width, Height: l.height, Option: option}, nil
}

func axisLine() *opts.AxisLine {
	return &opts.AxisLine{Show: opts.Bool(true), OnZero: opts.Bool(false), LineStyle: &opts.LineStyle{Color: ink, Width: 1}}
}

func splitLine() *opts.SplitLine {
	return &opts.SplitLine{Show: opts.Bool(true), LineStyle: &opts.LineStyle{Color: rule, Width: 1}}
}

// valueAxis is the y axis of a plot with the ends chosen by axisRange.
func valueAxis(a axisRange) opts.YAxis {
	y := opts.YAxis{
		Type: "value", Min: a.min, Max: a.max,
		AxisLine: axisLine(), SplitLine: splitLine(),
		AxisLabel: &opts.AxisLabel{Color: ink, FontSize: tickSize, FontFamily: fontFamily, Margin: 8},
	}
	if a.log {
		y.Type = "log"
	} else {
		// Pinning both interval bounds makes ECharts tick at the step the
		// ends were rounded to.
		y.MinInterval, y.MaxInterval = a.step, a.step
	}
	return y
}

// valueLabels is an invisible scatter series that carries the value labels.
// Putting the labels on points at the whisker tops, rather than on the bars,
// is what keeps every label clear of its whisker cap. A scatter series is
// drawn at an opacity of 0.8 by default, and its labels with it, which would
// turn the black text gray.
func valueLabels(data []opts.ScatterData) charts.SingleSeries {
	return charts.SingleSeries{
		Type: types.ChartScatter, Data: data, SymbolSize: 0, ItemStyle: &opts.ItemStyle{Opacity: opts.Float(1)},
		Label: &opts.Label{Show: opts.Bool(true), Position: "top", Formatter: "{b}", FontSize: valueSize, FontFamily: fontFamily, Color: ink},
	}
}
