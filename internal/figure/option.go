// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package figure

import (
	"encoding/json"
	"fmt"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"
)

// missing collects the parts of an option that go-echarts v2.7.3 cannot
// express, and apply is the one place where they are written into the option
// map that go-echarts builds.
//
// BaseConfiguration holds a single title and a single legend while ECharts
// takes a list of each, which is how every panel gets its own title and how a
// legend is split into rows that never wrap on their own. There is no global
// textStyle, no graphic component, no decal on opts.ItemStyle (and the decal
// list of opts.AriaDecal has the wrong element type), and opts.YAxis has no
// axisTick, which ECharts hides by default on a value axis.
type missing struct {
	titles  []opts.Title
	legends []opts.Legend
	graphic []graphic
	// decals is keyed by series index.
	decals map[int]*decal
	// yTick is given to every y axis.
	yTick opts.AxisTick
}

// graphic is one line of the ECharts graphic component, placed in px.
type graphic struct {
	Type  string         `json:"type"`
	Shape map[string]any `json:"shape"`
	Style graphicStyle   `json:"style"`
	Z     int            `json:"z,omitempty"`
}

type graphicStyle struct {
	Stroke    string    `json:"stroke,omitempty"`
	LineWidth float64   `json:"lineWidth,omitempty"`
	LineDash  []float64 `json:"lineDash,omitempty"`
}

func ruleLine(x1, y1, x2, y2, width float64) graphic {
	return graphic{
		Type:  "line",
		Shape: map[string]any{"x1": x1, "y1": y1, "x2": x2, "y2": y2},
		Style: graphicStyle{Stroke: ink, LineWidth: width},
	}
}

// decoratedSeries adds a decal to the item style of a series. The itemStyle
// field here is shallower than the one embedded in SingleSeries, so it is the
// one encoding/json writes.
type decoratedSeries struct {
	charts.SingleSeries
	ItemStyle decoratedStyle `json:"itemStyle"`
}

type decoratedStyle struct {
	opts.ItemStyle
	Decal *decal `json:"decal"`
}

type tickedYAxis struct {
	opts.YAxis
	AxisTick opts.AxisTick `json:"axisTick"`
}

// chart is what the go-echarts chart types share through BaseConfiguration.
type chart interface {
	JSON() map[string]interface{}
}

// apply encodes the option of c with the missing parts added.
func (m missing) apply(c chart) ([]byte, error) {
	option := c.JSON()
	option["textStyle"] = opts.TextStyle{FontFamily: fontFamily, FontSize: bodySize, Color: ink}
	option["title"] = m.titles
	option["legend"] = m.legends
	if len(m.graphic) > 0 {
		option["graphic"] = m.graphic
	}

	series, ok := option["series"].(charts.MultiSeries)
	if !ok {
		return nil, fmt.Errorf("go-echarts series have type %T", option["series"])
	}
	out := make([]any, len(series))
	for i, s := range series {
		out[i] = s
		if d := m.decals[i]; d != nil {
			var style opts.ItemStyle
			if s.ItemStyle != nil {
				style = *s.ItemStyle
			}
			out[i] = decoratedSeries{SingleSeries: s, ItemStyle: decoratedStyle{ItemStyle: style, Decal: d}}
		}
	}
	option["series"] = out

	axes, ok := option["yAxis"].([]opts.YAxis)
	if !ok {
		return nil, fmt.Errorf("go-echarts y axes have type %T", option["yAxis"])
	}
	ticked := make([]tickedYAxis, len(axes))
	for i, a := range axes {
		ticked[i] = tickedYAxis{YAxis: a, AxisTick: m.yTick}
	}
	option["yAxis"] = ticked
	return json.Marshal(option)
}

// baseOptions are the settings every figure shares. The palette is set
// although every series names its color, so that nothing can fall back to the
// ECharts default colors.
func baseOptions() []charts.GlobalOpts {
	return []charts.GlobalOpts{
		charts.WithInitializationOpts(opts.Initialization{BackgroundColor: white}),
		charts.WithAnimation(false),
		charts.WithToolboxOpts(opts.Toolbox{Show: opts.Bool(false)}),
		charts.WithTooltipOpts(opts.Tooltip{Show: opts.Bool(false)}),
		charts.WithColorsOpts(palette),
	}
}
