// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package figure

import (
	"fmt"
	"math"

	"github.com/galpt/prism-log-parser/internal/render"
)

// Geometry shared by every image, in layout px.
const (
	margin   = 28
	titleTop = 20
	// sectionGap separates the blocks of an image from each other.
	sectionGap = 16
	// The legend draws a 26x14 swatch, then the text 5px after it, then
	// leaves ECharts' default gap of 10px to the next item.
	legendIconW   = 26
	legendIconH   = 14
	legendTextPad = 5
	legendItemGap = 10
	legendRowH    = 26
	// maxTextW keeps lines of prose short enough to read on a wide image.
	maxTextW = 1100
)

// Geometry of a chart image, in layout px.
const (
	// minBand is the narrowest slot a bar gets. Four columns then come to
	// about 620px with the axis and the margins around them, and eight to
	// about 1130px.
	minBand = 128
	// bandPad is the room a value label leaves to the labels of the
	// neighboring bars.
	bandPad = 20
	plotH   = 280
	// labelHead is the room above a whisker that its value label takes.
	labelHead = 26
	// plotFoot is the room under a plot whose x axis has no labels.
	plotFoot = 12
	// minWidth keeps an image of one or two columns wide enough for its
	// legend and caption to read as a figure rather than a strip.
	minWidth = 560
)

// chartSpec is what the layout of a chart image depends on. The legend and
// the caption are given as strings because the layout wraps them to the width
// it settles on.
type chartSpec struct {
	// minPlotW is the plot width that the bars need, and axisW the room left
	// of the plot for the tick labels.
	minPlotW, axisW float64
	// footH is the room under the plot for the x axis labels.
	footH float64
	// titleW is the width of the title line, its log scale mark included.
	titleW  float64
	legend  []string
	caption []string
}

// chartLayout places every block of a chart image. All y values are from the
// top of the image.
type chartLayout struct {
	width, height int
	// plotW may be wider than chartSpec.minPlotW when the title or minWidth
	// makes the image wider than its bars.
	plotW, plotLeft float64
	legendRows      [][]string
	legendTop       float64
	plotTop         float64
	captionTop      float64
	caption         []string
}

// layoutChart sizes a chart image.
//
// The width is 2*margin + axisW + plotW, where plotW is at least the minPlotW
// of the spec, which a metric image sets to columns*band, so the width grows
// linearly with the number of columns. The plot is widened to fill the image
// when the title or minWidth asks for more room. The height is the sum of the
// title, the legend rows, the plot with the room under it, and the caption
// lines, the last two wrapped to the width found first.
func layoutChart(s chartSpec, m Measure) (chartLayout, error) {
	var l chartLayout
	inner := math.Max(s.axisW+s.minPlotW, math.Max(minWidth-2*margin, s.titleW))
	l.plotW, l.plotLeft = inner-s.axisW, margin+s.axisW
	l.width = int(math.Ceil(2*margin + inner))
	if l.width > render.MaxSide {
		return l, fmt.Errorf("the image needs to be %d px wide, more than the %d px that render allows", l.width, render.MaxSide)
	}

	y := titleTop + lineHeight(titleSize) + sectionGap
	l.legendTop = y
	l.legendRows = legendRows(s.legend, inner, m)
	y += float64(len(l.legendRows))*legendRowH + sectionGap

	l.plotTop = y
	y += plotH + s.footH + sectionGap

	l.captionTop = y
	for _, p := range s.caption {
		l.caption = append(l.caption, wrap(p, math.Min(inner, maxTextW), captionSize, false, m)...)
	}
	y += float64(len(l.caption))*lineHeight(captionSize) + margin
	l.height = int(math.Ceil(y))
	if l.height > render.MaxSide {
		return l, fmt.Errorf("the image needs to be %d px tall, more than the %d px that render allows", l.height, render.MaxSide)
	}
	return l, nil
}

// legendRows packs legend items into rows no wider than width, the way the
// ECharts legend would. Each row becomes its own legend component, so a
// difference in text measurement can never make ECharts wrap a row a second
// time and push it into the plot below.
func legendRows(items []string, width float64, m Measure) [][]string {
	var rows [][]string
	x := 0.0
	for _, it := range items {
		w := legendIconW + legendTextPad + m(it, legendSize, false)
		if len(rows) == 0 || x+w > width {
			rows = append(rows, nil)
			x = 0
		}
		rows[len(rows)-1] = append(rows[len(rows)-1], it)
		x += w + legendItemGap
	}
	return rows
}
