// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package render

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"strings"
	"testing"
)

// The plot area is pinned by the grid below so that the tests can compute
// where things land. With an 800x450 canvas it spans x 60..780 and y 60..410,
// the y axis is fixed to 0..100, and each of the 3 categories is 240px wide.
const (
	gridLeft, gridTop, gridRight, gridBottom = 60, 60, 20, 40
	testW, testH                             = 800, 450
)

// testOption has a title, two bar series and an error bar on the first
// category that runs from 55 to 70, above the bars, which reach 40 and 50.
const testOption = `{
  "backgroundColor": "#ffffff",
  "animation": false,
  "textStyle": {"fontFamily": "Liberation Serif"},
  "title": {"text": "Scheduler comparison", "left": "center", "top": 10,
            "textStyle": {"color": "#000000", "fontSize": 24}},
  "grid": {"left": 60, "top": 60, "right": 20, "bottom": 40},
  "xAxis": {"type": "category", "data": ["frame time", "input lag", "compile"]},
  "yAxis": {"type": "value", "min": 0, "max": 100},
  "series": [
    {"type": "bar", "name": "A", "data": [40, 60, 30], "itemStyle": {"color": "#9ecae1"}},
    {"type": "bar", "name": "B", "data": [50, 45, 35], "itemStyle": {"color": "#fdae6b"}},
    {"type": "custom", "name": "spread", "renderItem": "errorBar",
     "itemStyle": {"borderColor": "#111111", "borderWidth": 2},
     "data": [[0, 55, 70], [1, 62, 80]]}
  ]
}`

var shared *Renderer

func TestMain(m *testing.M) {
	r, err := New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "New:", err)
		os.Exit(1)
	}
	shared = r
	code := m.Run()
	if err := r.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "Close:", err)
		code = 1
	}
	os.Exit(code)
}

func decode(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	return img
}

// dark reports whether a pixel is close to black, which no fill or gridline in
// testOption is.
func dark(img image.Image, x, y int) bool {
	r, g, b, _ := img.At(x, y).RGBA()
	return (r+g+b)/3 < 0x5000
}

// valueY maps a y axis value in testOption to a pixel row.
func valueY(v float64) int {
	return testH - gridBottom - int(v/100*float64(testH-gridTop-gridBottom))
}

func TestRenderChart(t *testing.T) {
	data, err := shared.Render([]byte(testOption), testW, testH)
	if err != nil {
		t.Fatal(err)
	}
	img := decode(t, data)
	if b := img.Bounds(); b.Dx() != testW || b.Dy() != testH {
		t.Fatalf("size = %dx%d, want %dx%d", b.Dx(), b.Dy(), testW, testH)
	}

	titleInk := 0
	for y := 0; y < gridTop; y++ {
		for x := 0; x < testW; x++ {
			if dark(img, x, y) {
				titleInk++
			}
		}
	}
	if titleInk < 100 {
		t.Errorf("title band has %d dark pixels, want text", titleInk)
	}

	// The category center of the first band is where the whisker is drawn,
	// and its top is above the taller bar, which reaches 50.
	center := gridLeft + (testW-gridLeft-gridRight)/6
	barTop, whiskerTop := valueY(50), valueY(70)
	ink := 0
	for y := whiskerTop; y < barTop-2; y++ {
		if dark(img, center, y) {
			ink++
		}
	}
	if want := (barTop - 2 - whiskerTop) * 3 / 4; ink < want {
		t.Errorf("column %d has %d dark pixels above the bar, want at least %d", center, ink, want)
	}
	if dark(img, center, whiskerTop-6) {
		t.Errorf("whisker reaches above its high value at row %d", whiskerTop-6)
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	first, err := shared.Render([]byte(testOption), testW, testH)
	if err != nil {
		t.Fatal(err)
	}
	second, err := shared.Render([]byte(testOption), testW, testH)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Error("two renders of the same option differ")
	}
}

func TestRenderSeveralSizes(t *testing.T) {
	for _, size := range [][2]int{{400, 300}, {1024, 600}, {400, 300}} {
		data, err := shared.Render([]byte(testOption), size[0], size[1])
		if err != nil {
			t.Fatalf("%dx%d: %v", size[0], size[1], err)
		}
		if b := decode(t, data).Bounds(); b.Dx() != size[0] || b.Dy() != size[1] {
			t.Errorf("size = %dx%d, want %dx%d", b.Dx(), b.Dy(), size[0], size[1])
		}
	}
}

func TestRenderErrors(t *testing.T) {
	tests := []struct {
		name          string
		option        string
		width, height int
		want          string
	}{
		{"unknown renderItem", `{"series": [{"type": "custom", "renderItem": "nope", "data": []}]}`, 100, 100, `unknown renderItem "nope"`},
		{"inherited property name", `{"series": [{"type": "custom", "renderItem": "constructor", "data": []}]}`, 100, 100, `unknown renderItem "constructor"`},
		{"invalid JSON", `{"series": [`, 100, 100, "not valid JSON"},
		{"not an object", `[1, 2]`, 100, 100, "must be a JSON object"},
		{"zero width", testOption, 0, 100, "outside"},
		{"zero height", testOption, 100, 0, "outside"},
		{"width too large", testOption, 20000, 100, "outside"},
		{"height too large", testOption, 100, 16385, "outside"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := shared.Render([]byte(tt.option), tt.width, tt.height)
			if err == nil {
				t.Fatalf("got %d bytes, want an error", len(data))
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

// A failed render must not leave the Renderer unusable.
func TestRenderRecoversAfterError(t *testing.T) {
	if _, err := shared.Render([]byte(`{"series": [{"type": "custom", "renderItem": "nope"}]}`), 100, 100); err == nil {
		t.Fatal("want an error")
	}
	if _, err := shared.Render([]byte(testOption), testW, testH); err != nil {
		t.Fatalf("render after error: %v", err)
	}
}

// Two grids, a log axis and non-default axis indexes, which the error bar
// must follow because it asks the series' own coordinate system for positions.
func TestErrorBarOnSecondGridWithLogAxis(t *testing.T) {
	option := `{
	  "backgroundColor": "#ffffff",
	  "animation": false,
	  "grid": [{"left": 40, "top": 20, "width": 300, "height": 160}, {"left": 400, "top": 20, "width": 300, "height": 160}],
	  "xAxis": [{"type": "category", "gridIndex": 0, "data": ["a", "b"]}, {"type": "category", "gridIndex": 1, "data": ["a", "b"]}],
	  "yAxis": [{"type": "value", "gridIndex": 0}, {"type": "log", "gridIndex": 1, "min": 1, "max": 1000}],
	  "series": [
	    {"type": "bar", "xAxisIndex": 1, "yAxisIndex": 1, "data": [10, 20]},
	    {"type": "custom", "renderItem": "errorBar", "xAxisIndex": 1, "yAxisIndex": 1,
	     "itemStyle": {"borderColor": "#000000", "borderWidth": 2},
	     "data": [[0, 100, 500]]}
	  ]
	}`
	data, err := shared.Render([]byte(option), 760, 220)
	if err != nil {
		t.Fatal(err)
	}
	img := decode(t, data)
	// The first band of the second grid is 150px wide, so its center is at
	// 400+75. The log axis spans 1..1000 over the 160px plot height.
	x := 475
	logY := func(v float64) int { return 20 + 160 - int(160*math.Log10(v)/3) }
	top, bottom := logY(500), logY(100)
	ink := 0
	for y := top + 2; y < bottom-2; y++ {
		if dark(img, x, y) {
			ink++
		}
	}
	if ink < (bottom-top-4)*3/4 {
		t.Errorf("whisker on the second grid has %d dark pixels in rows %d..%d", ink, top, bottom)
	}
	for y := 20; y < 180; y++ {
		if dark(img, 40+75, y) {
			t.Fatalf("first grid has ink at row %d, the whisker was drawn on the wrong grid", y)
		}
	}
}

func TestTextWidth(t *testing.T) {
	text := "FRAME TIME p99 (ms)"
	regular := shared.TextWidth(text, 12, false)
	if want := shared.measure.width(text, "12px sans-serif"); regular != want {
		t.Errorf("TextWidth = %v, want the measurer's %v", regular, want)
	}
	if bold := shared.TextWidth(text, 12, true); bold <= regular {
		t.Errorf("bold width %v is not wider than regular %v", bold, regular)
	}
	if double := shared.TextWidth(text, 24, false); math.Abs(double-2*regular) > 0.01*double {
		t.Errorf("24px width %v is not twice the 12px width %v", double, regular)
	}
	if w := shared.TextWidth(text, 12.5, false); w <= regular {
		t.Errorf("12.5px width %v is not wider than 12px width %v", w, regular)
	}
}
