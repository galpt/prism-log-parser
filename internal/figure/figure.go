// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

// Package figure turns a set of compared PRISM-FRAME logs into ECharts options
// laid out like the figures of a paper, each with the pixel size it needs.
//
// Each scenario gets a directory with one image per metric, one bar per
// column, and an image of the changes against EEVDF. A summary directory holds
// the run metadata and notes, and one montauk verdict heatmap per scenario.
// Every image stands on its own with a title, a legend and a caption, so it
// can be read at its own size and shown without the others. The options are
// built with go-echarts types, and the few things go-echarts cannot express
// are added in one place (see missing). Every size is worked out here from the
// content: text is measured with the same font the renderer draws with, so an
// image can reserve the space its labels need instead of leaving ECharts to
// guess and overlap them.
package figure

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/galpt/prism-log-parser/internal/compare"
	"github.com/galpt/prism-log-parser/internal/report"
)

// Measure returns the width in px of text drawn at sizePx in the regular or
// the bold face of the font that the renderer uses.
type Measure func(text string, sizePx float64, bold bool) float64

// Figure is one image to render, Width by Height in layout px. Option is the
// ECharts option as JSON. Path is where the image is meant to be saved,
// relative to the output directory, with forward slashes and exactly one
// directory in front of the file name.
type Figure struct {
	Path          string
	Width, Height int
	Option        []byte
}

// summaryDir holds the images that cover the whole run. Every scenario
// directory starts with "scenario-", so no scenario can take this name.
const summaryDir = "summary"

// Build makes the images of every scenario, in the order of set.Scenarios,
// followed by the summary images. The reports must be the ones set was made
// from, in the same order. The images name each report by its Source.
func Build(reports []*report.Report, set *compare.Set, m Measure) ([]Figure, error) {
	colors := map[string]int{}
	for i, c := range set.Columns() {
		colors[c.Label] = i
	}
	// Two scenario names can sanitize to the same directory, and the images
	// of one would then overwrite those of the other.
	dirs := map[string]string{}
	for _, t := range set.Scenarios() {
		d := scenarioDir(t.Name)
		if other, ok := dirs[d]; ok {
			return nil, fmt.Errorf("scenarios %s and %s would both be written to %s", other, t.Name, d)
		}
		dirs[d] = t.Name
	}

	var figs []Figure
	for _, t := range set.Scenarios() {
		fs, err := scenarioFigures(t, colors, sourceLines(reports, set, t), m)
		if err != nil {
			return nil, fmt.Errorf("scenario %s: %w", t.Name, err)
		}
		figs = append(figs, fs...)
	}
	f, err := runsFigure(reports, set, m)
	if err != nil {
		return nil, fmt.Errorf("summary: %w", err)
	}
	figs = append(figs, f)
	summary := set.Summary()
	for _, t := range set.Scenarios() {
		f, ok, err := verdictFigure(summary, t.Name, m)
		if err != nil {
			return nil, fmt.Errorf("verdicts of %s: %w", t.Name, err)
		}
		if ok {
			figs = append(figs, f)
		}
	}
	return figs, nil
}

// sanitize keeps a name that comes from the log inside the output directory:
// it is lowercased, and anything but letters, digits, '-' and '_' becomes '-'.
func sanitize(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// scenarioDir is the directory of a scenario's images.
func scenarioDir(name string) string { return "scenario-" + sanitize(name) }

// verdictPath is the file of a scenario's verdict heatmap.
func verdictPath(name string) string { return path.Join(summaryDir, "verdicts-"+sanitize(name)+".png") }

// slug turns a metric label into the readable part of a file name. Letters
// and digits are kept and lowercased, '%' becomes "pct", and every other run
// of characters, non-ASCII ones included, becomes a single '-', with none at
// either end. "1% LOW FPS (p99)" becomes "1pct-low-fps-p99" and "JOB WAKE
// p99.9" becomes "job-wake-p99-9". A label with nothing left gets "metric",
// which the number in front of it still keeps apart from any other.
func slug(label string) string {
	var b strings.Builder
	dash := false
	for _, r := range label {
		var part string
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			part = string(r)
		case r >= 'A' && r <= 'Z':
			part = string(r - 'A' + 'a')
		case r == '%':
			part = "pct"
		default:
			dash = true
			continue
		}
		if dash && b.Len() > 0 {
			b.WriteByte('-')
		}
		dash = false
		b.WriteString(part)
	}
	if b.Len() == 0 {
		return "metric"
	}
	return b.String()
}

// sourceLines are the caption lines of a scenario image that name the logs
// its columns come from. When the logs agree on the fields that make runs
// comparable, those are given once. Otherwise each log gets its own, and the
// lines of Set.Differences follow to say which fields differ.
func sourceLines(reports []*report.Report, set *compare.Set, t compare.ScenarioTable) []string {
	var logs []*report.Report
	seen := map[int]bool{}
	for _, c := range t.Columns {
		if !seen[c.Log] {
			seen[c.Log] = true
			logs = append(logs, reports[c.Log])
		}
	}
	diffs := set.Differences()
	if len(diffs) == 0 {
		names := make([]string, len(logs))
		for i, r := range logs {
			names[i] = r.Source
		}
		return []string{"Source: " + strings.Join(names, ", ") + " (" + metaText(logs[0].Meta) + ")."}
	}
	parts := make([]string, len(logs))
	for i, r := range logs {
		parts[i] = r.Source + " (" + metaText(r.Meta) + ")"
	}
	lines := []string{"Sources: " + strings.Join(parts, "; ") + "."}
	for _, d := range diffs {
		lines = append(lines, "Note: "+d+".")
	}
	return lines
}

// metaText gives the preamble fields that make two runs comparable.
func metaText(m report.Meta) string {
	return fmt.Sprintf("VERSION %s, COMMIT %s, CPUS %d, ITERATIONS %d", m.Version, commitText(m), m.CPUs, m.Iterations)
}

func commitText(m report.Meta) string {
	if m.Dirty {
		return m.Commit + " (dirty)"
	}
	return m.Commit
}

// px formats a coordinate for the string-typed position fields of go-echarts.
func px(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }
