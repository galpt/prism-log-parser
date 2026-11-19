// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package figure

import (
	"math"
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
)

// panel is one metric of a scenario figure.
type panel struct {
	title string
	row   compare.RowSet
	axis  axisRange
}

// meanText is the mean of a cell exactly as the log printed it.
func meanText(c report.Cell) string {
	mean, _, _ := strings.Cut(c.Text, " ±")
	return mean
}

// scenarioFigure draws one scenario as small multiples. colors maps a column
// label to its position in the Set, which picks its color and pattern.
func scenarioFigure(t compare.ScenarioTable, colors map[string]int, caption []string, m Measure) (Figure, error) {
	panels := make([]panel, len(t.Rows))
	// Each column's slot must hold its widest value label without touching
	// the labels of its neighbors.
	band := float64(minBand)
	ticks := []string{}
	titleW := 0.0
	logTitleW := m(logMark, bodySize, false) + 8
	for i, row := range t.Rows {
		p := panel{title: panelLetter(i) + " " + metricName(row.Metric), row: row, axis: panelAxis(row.Cells, plotH, labelHead)}
		panels[i] = p
		w := m(p.title, panelTitleSize, true)
		if p.axis.log {
			w += logTitleW
		}
		titleW = math.Max(titleW, w)
		ticks = append(ticks, p.axis.ticks()...)
		for _, c := range row.Cells {
			text := naText
			if c.Valid {
				text = meanText(c)
			}
			band = math.Max(band, m(text, valueSize, false)+14)
		}
	}

	var delta *deltaSpec
	var deltaCols []int
	var deltaAxis axisRange
	deltaTitle := panelLetter(len(panels)) + " " + deltaName
	if len(t.Deltas) > 0 {
		deltaCols, deltaAxis = deltaColumns(t.Deltas)
		slot := 24.0
		for _, d := range t.Deltas {
			for _, c := range d.Cells {
				if c.Valid {
					slot = math.Max(slot, m(c.Text, valueSize, false)+6)
				}
			}
		}
		delta = &deltaSpec{metrics: len(t.Deltas), columns: len(deltaCols), slot: slot}
		ticks = append(ticks, deltaAxis.ticks()...)
		titleW = math.Max(titleW, m(deltaTitle, panelTitleSize, true))
	}
	// The tick labels sit 8px left of the axis line and the ticks take 5px.
	axisW := 16.0
	for _, s := range ticks {
		axisW = math.Max(axisW, m(s, bodySize, false)+16)
	}

	title := "PRISM-FRAME · SCENARIO " + t.Name
	legend := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		legend[i] = c.Label
	}
	l, err := layoutScenario(scenarioSpec{
		columns: len(t.Columns), band: band, axisW: axisW, titleW: titleW,
		panels: len(panels), title: title, delta: delta, legend: legend, caption: caption,
	}, m)
	if err != nil {
		return Figure{}, err
	}

	b := &scenarioBuilder{bar: charts.NewBar(), measure: m, colors: colors, columns: t.Columns, layout: l, axisW: axisW,
		miss: missing{decals: map[int]*decal{}, yTick: opts.AxisTick{Show: opts.Bool(true), LineStyle: &opts.LineStyle{Color: ink, Width: 1}}}}
	b.bar.SetGlobalOptions(baseOptions()...)
	b.miss.titles = append(b.miss.titles, opts.Title{
		Title: title, Left: px(margin), Top: px(titleTop),
		TitleStyle: &opts.TextStyle{FontFamily: fontFamily, FontSize: figureTitleSize, FontWeight: "bold", Color: ink},
	})
	b.legend()
	for i, p := range panels {
		b.metricPanel(i, p)
	}
	if delta != nil {
		b.deltaPanel(len(panels), deltaTitle, t.Deltas, deltaCols, deltaAxis, delta)
	}
	b.miss.titles = append(b.miss.titles, opts.Title{
		Title: strings.Join(l.caption, "\n"), Left: px(margin), Top: px(l.captionTop),
		TitleStyle: &opts.TextStyle{FontFamily: fontFamily, FontSize: bodySize, FontWeight: "normal", Color: ink, LineHeight: int(lineHeight(bodySize))},
	})
	// A scenario block may hold a header and no rows, which leaves the chart
	// with only the default axes of go-echarts and nothing on them.
	if len(b.grids) > 0 {
		b.bar.SetGlobalOptions(charts.WithGridOpts(b.grids...))
		b.bar.SetGlobalOptions(charts.WithXAxisOpts(b.xAxes[0], 0), charts.WithYAxisOpts(b.yAxes[0], 0))
		b.bar.ExtendXAxis(b.xAxes[1:]...)
		b.bar.ExtendYAxis(b.yAxes[1:]...)
	}
	option, err := b.miss.apply(b.bar)
	if err != nil {
		return Figure{}, err
	}
	return Figure{Name: fileName(t.Name), Width: l.width, Height: l.height, Option: option}, nil
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

// scenarioBuilder accumulates the grids, axes and series of a scenario
// figure. Panel i owns grid i and axis pair i.
type scenarioBuilder struct {
	bar     *charts.Bar
	measure Measure
	colors  map[string]int
	columns []compare.Column
	layout  scenarioLayout
	axisW   float64
	grids   []opts.Grid
	xAxes   []opts.XAxis
	yAxes   []opts.YAxis
	miss    missing
}

func (b *scenarioBuilder) legend() {
	for i, row := range b.layout.legendRows {
		b.miss.legends = append(b.miss.legends, opts.Legend{
			Data: row, Left: px(margin), Top: px(b.layout.legendTop + float64(i)*legendRowH),
			Icon: "rect", ItemWidth: legendIconW, ItemHeight: legendIconH,
			TextStyle: &opts.TextStyle{FontFamily: fontFamily, FontSize: legendSize, Color: ink},
		})
	}
}

// addPanel places the grid and the axes of one panel and its title.
func (b *scenarioBuilder) addPanel(x, y, plotW float64, title string, xa opts.XAxis, ya opts.YAxis) {
	b.grids = append(b.grids, opts.Grid{
		Left: px(x + b.axisW), Top: px(y + b.layout.headerH), Width: px(plotW), Height: px(plotH),
	})
	b.xAxes = append(b.xAxes, xa)
	b.yAxes = append(b.yAxes, ya)
	b.miss.titles = append(b.miss.titles, opts.Title{
		Title: title, Left: px(x), Top: px(y),
		TitleStyle: &opts.TextStyle{FontFamily: fontFamily, FontSize: panelTitleSize, FontWeight: "bold", Color: ink},
	})
}

func axisLine() *opts.AxisLine {
	return &opts.AxisLine{Show: opts.Bool(true), OnZero: opts.Bool(false), LineStyle: &opts.LineStyle{Color: ink, Width: 1}}
}

func splitLine() *opts.SplitLine {
	return &opts.SplitLine{Show: opts.Bool(true), LineStyle: &opts.LineStyle{Color: rule, Width: 1}}
}

// valueAxis is the y axis of a panel with the ends chosen by axisRange.
func valueAxis(grid int, a axisRange) opts.YAxis {
	y := opts.YAxis{
		Type: "value", GridIndex: grid, Min: a.min, Max: a.max,
		AxisLine: axisLine(), SplitLine: splitLine(),
		AxisLabel: &opts.AxisLabel{Color: ink, FontSize: bodySize, FontFamily: fontFamily, Margin: 8},
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

// addBar adds a bar series and records the decal of its column.
func (b *scenarioBuilder) addBar(name string, data []opts.BarData, col int, options ...charts.SeriesOpts) {
	if d := columnDecal(col); d != nil {
		b.miss.decals[len(b.bar.MultiSeries)] = d
	}
	b.bar.AddSeries(name, data, append(options, charts.WithItemStyleOpts(opts.ItemStyle{Color: columnColor(col), BorderColor: barEdge, BorderWidth: 0.8}))...)
}

// logSeparators draws a dashed line between the columns of two logs in the
// plot whose top-left corner is at x, y, so that a reader sees which bars
// were measured together.
func (b *scenarioBuilder) logSeparators(x, y float64) {
	band := b.layout.plotW / float64(len(b.columns))
	for c := 1; c < len(b.columns); c++ {
		if b.columns[c].Log != b.columns[c-1].Log {
			sep := ruleLine(x+float64(c)*band, y, x+float64(c)*band, y+plotH, 1)
			sep.Style.Stroke, sep.Style.LineDash = muted, []float64{4, 3}
			b.miss.graphic = append(b.miss.graphic, sep)
		}
	}
}

func (b *scenarioBuilder) metricPanel(i int, p panel) {
	x, y := b.layout.panelOrigin(i)
	categories := make([]string, len(b.columns))
	for c, col := range b.columns {
		categories[c] = col.Label
	}
	b.addPanel(x, y, b.layout.plotW, p.title, opts.XAxis{
		Type: "category", GridIndex: i, Data: categories, AxisLine: axisLine(),
		AxisTick: &opts.AxisTick{Show: opts.Bool(false)}, AxisLabel: &opts.AxisLabel{Show: opts.Bool(false)},
	}, valueAxis(i, p.axis))
	if p.axis.log {
		// The mark is its own title because ECharts sets the plain part of a
		// rich title in its default style, which would lose the bold.
		mark := textAt(logMark, x+b.measure(p.title, panelTitleSize, true)+8, y+(panelTitleSize-bodySize)/2, bodySize, false, muted)
		mark.TitleStyle.FontStyle, mark.TitleStyle.LineHeight = "italic", 0
		b.miss.titles = append(b.miss.titles, mark)
	}
	b.logSeparators(x+b.axisW, y+b.layout.headerH)

	// One series per column puts the column in the legend. Every series
	// holds a single bar at its own category, and a gap of -100% stacks the
	// empty slots of the others on top of it so the bar stays centered.
	var whiskers []opts.CustomData
	var labels []opts.ScatterData
	for c, col := range b.columns {
		data := make([]opts.BarData, len(b.columns))
		for k := range data {
			data[k] = opts.BarData{Value: "-"}
		}
		cell := p.row.Cells[c]
		if cell.Valid {
			data[c] = opts.BarData{Value: cell.Mean}
			low, high := whisker(cell, p.axis.log)
			whiskers = append(whiskers, opts.CustomData{Value: []float64{float64(c), low, high}})
			labels = append(labels, opts.ScatterData{Name: meanText(cell), Value: []float64{float64(c), high}})
		} else {
			labels = append(labels, opts.ScatterData{Name: naText, Value: []float64{float64(c), p.axis.min}})
		}
		b.addBar(col.Label, data, b.colors[col.Label], charts.WithBarChartOpts(opts.BarChart{
			XAxisIndex: i, YAxisIndex: i, BarGap: "-100%", BarWidth: "62%",
		}))
	}
	b.bar.MultiSeries = append(b.bar.MultiSeries,
		charts.SingleSeries{
			Type: types.ChartCustom, RenderItem: "errorBar", XAxisIndex: i, YAxisIndex: i, Data: whiskers,
			ItemStyle: &opts.ItemStyle{BorderColor: ink, BorderWidth: 1},
		},
		valueLabels(i, labels),
	)
}

// valueLabels is an invisible scatter series that carries the value labels.
// Putting the labels on points at the whisker tops, rather than on the bars,
// is what keeps every label clear of its whisker cap.
func valueLabels(axis int, data []opts.ScatterData) charts.SingleSeries {
	return charts.SingleSeries{
		Type: types.ChartScatter, XAxisIndex: axis, YAxisIndex: axis, Data: data, SymbolSize: 0,
		Label: &opts.Label{Show: opts.Bool(true), Position: "top", Formatter: "{b}", FontSize: valueSize, FontFamily: fontFamily, Color: ink},
	}
}

func (b *scenarioBuilder) deltaPanel(i int, title string, deltas []compare.DeltaSet, cols []int, a axisRange, spec *deltaSpec) {
	x, y := b.layout.deltaX, b.layout.deltaTop
	categories := make([]string, len(deltas))
	for k, d := range deltas {
		categories[k] = metricName(d.Metric)
	}
	b.addPanel(x, y, spec.plotW(), title, opts.XAxis{
		Type: "category", GridIndex: i, Data: categories, AxisLine: axisLine(),
		AxisTick: &opts.AxisTick{Show: opts.Bool(false)},
		AxisLabel: &opts.AxisLabel{
			Color: ink, FontSize: bodySize, FontFamily: fontFamily, Interval: "0", Margin: 10,
			Width: int(float64(spec.columns+1)*spec.slot - 8), Overflow: "break",
		},
	}, valueAxis(i, a))

	for n, c := range cols {
		data := make([]opts.BarData, len(deltas))
		for k, d := range deltas {
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
		col := b.columns[c]
		options := []charts.SeriesOpts{charts.WithBarChartOpts(opts.BarChart{
			XAxisIndex: i, YAxisIndex: i, BarWidth: px(spec.slot * 0.86), BarGap: "16%",
		})}
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
		b.addBar(col.Label, data, b.colors[col.Label], options...)
	}
}
