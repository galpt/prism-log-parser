// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package figure

import (
	"fmt"
	"math"
)

// maxSide is the largest side that render accepts.
const maxSide = 16384

// Geometry shared by both figures, in px.
const (
	margin   = 40
	titleTop = 20
	// sectionGap separates the blocks of a figure from each other.
	sectionGap = 18
	// The legend draws a 24x13 swatch, then the text 5px after it, then
	// leaves ECharts' default gap of 10px to the next item.
	legendIconW   = 24
	legendIconH   = 13
	legendTextPad = 5
	legendItemGap = 10
	legendRowH    = 24
	// maxTextW keeps lines of prose short enough to read on a wide figure.
	maxTextW = 1400
)

// Geometry of a scenario figure, in px.
const (
	// minBand is the narrowest slot a bar gets. Four columns then come to
	// about 1600px with the panel chrome around them.
	minBand = 76
	// headerLineH is one line of panel title above a plot.
	headerLineH  = 22
	headerPad    = 8
	plotH        = 200
	panelFootH   = 14
	panelGapV    = 26
	panelGapH    = 30
	deltaLabelsH = 2*16 + 12
	// labelHead is the room above a whisker that its value label takes.
	labelHead = 20
	maxPerRow = 4
	// minWidth keeps a figure of one or two narrow panels wide enough for its
	// title, legend and caption to read as a figure rather than a column.
	minWidth = 760
)

// scenarioSpec is what the layout of a scenario figure depends on. The
// strings are given rather than their widths because the layout wraps some of
// them to the width it settles on.
type scenarioSpec struct {
	columns int
	// band is the width of one column's slot in a metric panel, and axisW is
	// the room left of every plot for its tick labels.
	band, axisW float64
	// titleW is the widest panel title, the delta panel's included.
	titleW  float64
	panels  int
	title   string
	delta   *deltaSpec
	legend  []string
	caption []string
}

// deltaSpec describes the change panel: metrics groups of columns bars, each
// bar in a slot of slot px, with one empty slot between groups.
type deltaSpec struct {
	metrics, columns int
	slot             float64
}

func (d *deltaSpec) plotW() float64 { return float64(d.metrics*(d.columns+1)) * d.slot }

// scenarioLayout places every block of a scenario figure. All y values are
// from the top of the image.
type scenarioLayout struct {
	width, height int
	perRow        int
	plotW         float64
	cellW, cellH  float64
	headerH       float64
	legendRows    [][]string
	legendTop     float64
	panelsTop     float64
	// deltaX and deltaTop are the top-left corner of the delta panel's
	// header. Both are zero without one.
	deltaX, deltaTop float64
	captionTop       float64
	caption          []string
}

// panelOrigin returns the top-left corner of the header of metric panel i.
func (l scenarioLayout) panelOrigin(i int) (x, y float64) {
	return margin + float64(i%l.perRow)*l.cellW, l.panelsTop + float64(i/l.perRow)*l.cellH
}

// layoutScenario sizes a scenario figure.
//
// The width is 2*margin + perRow*cellW - panelGapH, where cellW is axisW +
// plotW + panelGapH and plotW is columns*band, so it grows linearly with the
// number of columns. A plot is widened past columns*band only when a panel
// title would not fit, and the delta panel or minWidth may set a larger width.
// perRow starts at maxPerRow and drops while the image would be wider than
// render allows. The height is the sum of the title, the legend rows, the
// panel rows, the delta panel when it has a row of its own, and the caption
// lines, each wrapped to the width found first.
func layoutScenario(s scenarioSpec, m Measure) (scenarioLayout, error) {
	var l scenarioLayout
	l.plotW = math.Max(float64(s.columns)*s.band, s.titleW-s.axisW)
	l.cellW = s.axisW + l.plotW + panelGapH
	l.headerH = headerLineH + headerPad

	inner := 0.0
	for l.perRow = min(maxPerRow, max(1, s.panels)); ; l.perRow-- {
		inner = float64(l.perRow)*l.cellW - panelGapH
		if s.delta != nil {
			inner = math.Max(inner, s.axisW+s.delta.plotW())
		}
		inner = math.Max(inner, minWidth-2*margin)
		inner = math.Max(inner, m(s.title, figureTitleSize, true))
		if 2*margin+inner <= maxSide {
			break
		}
		if l.perRow == 1 {
			return l, fmt.Errorf("%d columns need an image %.0f px wide, more than the %d px that render allows", s.columns, 2*margin+inner, maxSide)
		}
	}
	l.width = int(math.Ceil(2*margin + inner))

	y := titleTop + lineHeight(figureTitleSize) + sectionGap
	l.legendTop = y
	l.legendRows = legendRows(s.legend, inner, m)
	y += float64(len(l.legendRows))*legendRowH + sectionGap

	l.panelsTop = y
	l.cellH = l.headerH + plotH + panelFootH + panelGapV
	rows := (s.panels + l.perRow - 1) / l.perRow
	y += float64(rows) * l.cellH
	if s.delta != nil {
		// The delta panel goes into the free end of the last panel row when
		// it fits there, and gets a row of its own otherwise.
		last := s.panels - (rows-1)*l.perRow
		if x := float64(last) * l.cellW; rows > 0 && x+s.axisW+s.delta.plotW() <= inner {
			l.deltaX, l.deltaTop = margin+x, y-l.cellH
			y += deltaLabelsH - panelFootH
		} else {
			l.deltaX, l.deltaTop = margin, y
			y += l.headerH + plotH + deltaLabelsH + panelGapV
		}
	}

	l.captionTop = y
	for _, p := range s.caption {
		l.caption = append(l.caption, wrap(p, math.Min(inner, maxTextW), bodySize, false, m)...)
	}
	y += float64(len(l.caption))*lineHeight(bodySize) + margin
	l.height = int(math.Ceil(y))
	if l.height > maxSide {
		return l, fmt.Errorf("the figure needs an image %d px tall, more than the %d px that render allows", l.height, maxSide)
	}
	return l, nil
}

// legendRows packs legend items into rows no wider than width, the way the
// ECharts legend would. Each row becomes its own legend component, so a
// difference in text measurement can never make ECharts wrap a row a second
// time and push it into the panels below.
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
