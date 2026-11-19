// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

// Package render turns an ECharts option into PNG bytes without a browser.
//
// The option is rendered to an SVG string by ECharts 5.6.0 in server-side
// rendering mode, running inside goja. The SVG is then rasterized by resvg,
// which runs as WebAssembly on wazero. Both are pure Go, so the package builds
// with CGO_ENABLED=0 and needs neither a network nor any installed font at
// runtime.
//
// Text is the part that has to agree between the two halves. ECharts lays
// text out from the widths it is told, and resvg draws the glyphs later, so
// one embedded font (Liberation Serif) serves both. Whatever family a chart
// names is mapped to it, which also makes the output the same on every host.
package render

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dop251/goja"
	resvg "github.com/kanrichan/resvg-go"
	"github.com/tetratelabs/wazero"
)

//go:embed assets/echarts.min.js
var echartsJS string

//go:embed assets/ssr.js
var ssrJS string

// Scale is the number of output pixels per layout px on each side. ECharts
// lays a chart out in CSS px, and resvg draws it Scale times larger, like a
// screenshot of a high density screen, so text stays sharp when a viewer
// zooms in.
const Scale = 2

// maxPixels bounds each side of the pixmap. A pixmap is 4 bytes per pixel
// inside the wasm linear memory, which is limited to 4 GiB, so a larger
// request would exhaust it partway through a render.
const maxPixels = 16384

// MaxSide is the largest side in layout px that Render accepts, the pixmap
// limit divided by Scale.
const MaxSide = maxPixels / Scale

// compiled holds the machine code of the resvg module once the first
// Renderer has compiled it, which is most of the time and memory that a
// Renderer costs. Every later Renderer in the process reuses it.
var compiled = wazero.NewCompilationCache()

// Renderer renders ECharts options to PNG. Loading ECharts and starting the
// resvg worker is the slow part, so a Renderer is meant to be created once and
// reused for many Render calls.
//
// A Renderer is not safe for concurrent use. The goja runtime and the resvg
// worker are both single threaded, so callers that render from several
// goroutines need their own Renderer per goroutine or a lock around Render.
type Renderer struct {
	vm        *goja.Runtime
	renderSVG goja.Callable
	worker    *resvg.Worker
	fonts     *resvg.FontDB
	measure   *measurer
}

// New loads ECharts into a fresh goja runtime and starts the resvg worker.
func New() (*Renderer, error) {
	m, err := newMeasurer()
	if err != nil {
		return nil, err
	}

	vm := goja.New()
	// The shim goes first because it installs the timer stubs that ECharts
	// expects to find when it loads.
	if _, err := vm.RunString(ssrJS); err != nil {
		return nil, fmt.Errorf("load shim: %w", jsError(err))
	}
	if _, err := vm.RunString(echartsJS); err != nil {
		return nil, fmt.Errorf("load echarts: %w", jsError(err))
	}
	install, ok := goja.AssertFunction(vm.Get("installPlatform"))
	if !ok {
		return nil, errors.New("shim does not define installPlatform")
	}
	if _, err := install(goja.Undefined(), vm.ToValue(m.width)); err != nil {
		return nil, fmt.Errorf("install platform: %w", jsError(err))
	}
	renderSVG, ok := goja.AssertFunction(vm.Get("renderSVG"))
	if !ok {
		return nil, errors.New("shim does not define renderSVG")
	}

	worker, err := resvg.NewWorker(context.Background(), wazero.NewRuntimeConfig().WithCompilationCache(compiled))
	if err != nil {
		return nil, fmt.Errorf("start resvg: %w", err)
	}
	fonts, err := newFontDB(worker)
	if err != nil {
		_ = worker.Close()
		return nil, err
	}
	return &Renderer{vm: vm, renderSVG: renderSVG, worker: worker, fonts: fonts, measure: m}, nil
}

// newFontDB builds the font database that every render shares. It holds only
// the embedded fonts, so no system font can leak into the output.
func newFontDB(worker *resvg.Worker) (*resvg.FontDB, error) {
	db, err := worker.NewFontDBDefault()
	if err != nil {
		return nil, fmt.Errorf("create font database: %w", err)
	}
	for _, data := range [][]byte{serifRegular, serifBold} {
		if err := db.LoadFontData(data); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("load font: %w", err)
		}
	}
	// SVG font-family lists usually end in a generic name, and a list with no
	// match at all would otherwise fall through to a font that is not there.
	setters := []func(string) error{
		db.SetSerifFamily,
		db.SetSansSerifFamily,
		db.SetCursiveFamily,
		db.SetFantasyFamily,
		db.SetMonospaceFamily,
	}
	for _, set := range setters {
		if err := set(fontFamily); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("set font family: %w", err)
		}
	}
	return db, nil
}

// Render lays an ECharts option, given as JSON, out on a width by height
// canvas in layout px and returns a PNG that is Scale times as large on each
// side. The option must set backgroundColor if the image is not
// to be transparent. A custom series may name its renderItem by string, and
// "errorBar" is the one name that is known. Its data items are [x, low, high],
// where x is a category index or an axis value. Each side must be between 1
// and MaxSide.
//
// Render is not safe for concurrent use.
func (r *Renderer) Render(option []byte, width, height int) ([]byte, error) {
	if width < 1 || width > MaxSide || height < 1 || height > MaxSide {
		return nil, fmt.Errorf("size %dx%d is outside 1..%d", width, height, MaxSide)
	}
	// Rejecting bad JSON here gives the caller a plain Go error and keeps the
	// option out of the JS runtime until it is known to parse.
	if !json.Valid(option) {
		return nil, errors.New("option is not valid JSON")
	}

	v, err := r.renderSVG(goja.Undefined(), r.vm.ToValue(string(option)), r.vm.ToValue(width), r.vm.ToValue(height))
	if err != nil {
		return nil, fmt.Errorf("render svg: %w", jsError(err))
	}
	return r.rasterize(v.String(), width, height)
}

// rasterize draws an SVG string of width by height layout px onto a pixmap
// Scale times as large and encodes it. Scaling the drawing rather than the
// SVG keeps every coordinate that ECharts computed as it is, and resvg then
// draws the glyph outlines and the lines at the full output resolution.
func (r *Renderer) rasterize(svg string, width, height int) ([]byte, error) {
	tree, err := r.worker.NewTreeFromData([]byte(svg), &resvg.Options{Dpi: 96, FontFamily: fontFamily, FontSize: 12})
	if err != nil {
		return nil, fmt.Errorf("parse svg: %w", err)
	}
	defer tree.Close()
	if err := tree.ConvertText(r.fonts); err != nil {
		return nil, fmt.Errorf("convert text: %w", err)
	}
	pixmap, err := r.worker.NewPixmap(uint32(width*Scale), uint32(height*Scale))
	if err != nil {
		return nil, fmt.Errorf("allocate pixmap: %w", err)
	}
	defer pixmap.Close()
	if err := tree.Render(resvg.TransformFromScale(Scale, Scale), pixmap); err != nil {
		return nil, fmt.Errorf("rasterize: %w", err)
	}
	png, err := pixmap.EncodePNG()
	if err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}
	return png, nil
}

// TextWidth returns the advance in px of text set in the embedded font at
// sizePx, which is the width ECharts lays the same text out with. A caller
// that sizes a chart around its labels gets the numbers the render will use.
func (r *Renderer) TextWidth(text string, sizePx float64, bold bool) float64 {
	// A size that is not positive gets the default size, as it does in a
	// font string.
	if !(sizePx > 0) {
		sizePx = defaultFontSize
	}
	return r.measure.advance(text, faceKey{bold: bold, size: sizePx})
}

// Close releases the resvg worker and its wasm memory. The Renderer must not
// be used afterwards.
func (r *Renderer) Close() error {
	return errors.Join(r.fonts.Close(), r.worker.Close())
}

// jsError reduces a goja exception to the message the script threw. The full
// text of a goja.Exception carries a stack trace that points into a minified
// bundle and says nothing useful to a caller.
func jsError(err error) error {
	var exc *goja.Exception
	if !errors.As(err, &exc) {
		return err
	}
	if obj, ok := exc.Value().(*goja.Object); ok {
		if msg := obj.Get("message"); msg != nil {
			return errors.New(msg.String())
		}
	}
	return errors.New(exc.Value().String())
}
