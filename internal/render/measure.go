// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package render

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

//go:embed assets/LiberationSerif-Regular.ttf
var serifRegular []byte

//go:embed assets/LiberationSerif-Bold.ttf
var serifBold []byte

// fontFamily is the one family that is ever drawn. Every family name that a
// chart asks for resolves to it, both here and inside resvg, so the width
// measured by ECharts is the width that gets rasterized.
const fontFamily = "Liberation Serif"

// defaultFontSize is the size in px that a font string gets when it has none.
const defaultFontSize = 12.0

// faceKey identifies a cached face. ECharts asks for a handful of sizes per
// chart, so the cache stays small.
type faceKey struct {
	bold bool
	size float64
}

// measurer answers ECharts' text width queries from real glyph advances. The
// faces are cached because building one parses tables and allocates, and
// ECharts measures the same few fonts thousands of times per chart.
type measurer struct {
	regular, bold *opentype.Font
	faces         map[faceKey]font.Face
}

func newMeasurer() (*measurer, error) {
	regular, err := opentype.Parse(serifRegular)
	if err != nil {
		return nil, fmt.Errorf("parse regular font: %w", err)
	}
	bold, err := opentype.Parse(serifBold)
	if err != nil {
		return nil, fmt.Errorf("parse bold font: %w", err)
	}
	return &measurer{regular: regular, bold: bold, faces: make(map[faceKey]font.Face)}, nil
}

// width returns the advance of text in px for a CSS font shorthand string.
// A face that cannot be built measures as zero width, which makes ECharts
// lay the text out as empty instead of failing the whole render.
func (m *measurer) width(text, spec string) float64 {
	key := parseFont(spec)
	face, ok := m.faces[key]
	if !ok {
		f := m.regular
		if key.bold {
			f = m.bold
		}
		var err error
		face, err = opentype.NewFace(f, &opentype.FaceOptions{Size: key.size, DPI: 72, Hinting: font.HintingNone})
		if err != nil {
			return 0
		}
		m.faces[key] = face
	}
	return float64(font.MeasureString(face, text)) / 64
}

// parseFont reads the weight and the size from a CSS font shorthand such as
// `italic 600 13px "Liberation Serif"` or `normal bolder 14px serif`. Style,
// variant and stretch keywords are skipped. Scanning stops at the size token
// so that a family name like "Arial Bold" cannot set the weight.
func parseFont(spec string) faceKey {
	key := faceKey{size: defaultFontSize}
	for _, tok := range strings.Fields(spec) {
		// The size may carry a line height, as in 12px/1.5.
		sizeTok, _, _ := strings.Cut(tok, "/")
		if px, ok := strings.CutSuffix(sizeTok, "px"); ok {
			if v, err := strconv.ParseFloat(px, 64); err == nil && v > 0 {
				key.size = v
				break
			}
		}
		if tok == "bold" || tok == "bolder" {
			key.bold = true
		} else if w, err := strconv.Atoi(tok); err == nil && w >= 600 {
			key.bold = true
		}
	}
	return key
}
