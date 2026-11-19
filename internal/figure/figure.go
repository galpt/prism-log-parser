// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

// Package figure turns a set of compared PRISM-FRAME logs into ECharts options
// laid out like the figures of a paper, each with the pixel size it needs.
//
// There is one figure per scenario, a grid of small panels with one bar per
// column, and one summary figure with the run metadata, the montauk verdicts
// and the notes of every log. The options are built with go-echarts types, and
// the few things go-echarts cannot express are added in one place (see
// missing). Every size is worked out here from the content: text is measured
// with the same font the renderer draws with, so the figure can reserve the
// space its labels need instead of leaving ECharts to guess and overlap them.
package figure

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/galpt/prism-log-parser/internal/compare"
	"github.com/galpt/prism-log-parser/internal/report"
)

// Measure returns the width in px of text drawn at sizePx in the regular or
// the bold face of the font that the renderer uses.
type Measure func(text string, sizePx float64, bold bool) float64

// Figure is one image to render. Option is the ECharts option as JSON and
// Name the file name the image is meant to be saved under.
type Figure struct {
	Name          string
	Width, Height int
	Option        []byte
}

// Build makes the scenario figures, in the order of set.Scenarios, followed by
// the summary figure. The reports must be the ones set was made from, in the
// same order. The figures name each report by its Source.
func Build(reports []*report.Report, set *compare.Set, m Measure) ([]Figure, error) {
	colors := map[string]int{}
	for i, c := range set.Columns() {
		colors[c.Label] = i
	}
	var figs []Figure
	names := map[string]bool{}
	add := func(f Figure) error {
		if names[f.Name] {
			return fmt.Errorf("two figures are both named %s", f.Name)
		}
		names[f.Name] = true
		figs = append(figs, f)
		return nil
	}
	for _, t := range set.Scenarios() {
		f, err := scenarioFigure(t, colors, scenarioCaption(reports, set, t), m)
		if err != nil {
			return nil, fmt.Errorf("scenario %s: %w", t.Name, err)
		}
		if err := add(f); err != nil {
			return nil, err
		}
	}
	f, err := summaryFigure(reports, set, m)
	if err != nil {
		return nil, fmt.Errorf("summary: %w", err)
	}
	if err := add(f); err != nil {
		return nil, err
	}
	return figs, nil
}

// fileName turns a scenario name into "prism-scenario-<name>.png". The name
// comes from the log, so anything but letters, digits, '-' and '_' is replaced
// to keep the file inside the output directory. The "scenario-" part keeps a
// scenario from ever taking the summary's name, prism-summary.png.
func fileName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return "prism-scenario-" + b.String() + ".png"
}

// scenarioCaption explains the encoding of a scenario figure and names the
// logs its columns come from.
func scenarioCaption(reports []*report.Report, set *compare.Set, t compare.ScenarioTable) []string {
	caption := []string{
		"Bars show the mean and whiskers ±1 sample standard deviation over the ITERATIONS runs of each log. " +
			"The number above a bar is the mean as the log printed it, and n/a marks a metric that a log has no value for. " +
			"A whisker that would reach below zero stops at zero, or at the mean on a log scale.",
	}
	if len(t.Deltas) > 0 {
		caption = append(caption, "Change vs EEVDF is the percent change of each mean against the EEVDF column of the same log, "+
			"so each log is measured against its own baseline and bars from different logs do not share one.")
	}
	var sources []string
	seen := map[int]bool{}
	for _, c := range t.Columns {
		if seen[c.Log] {
			continue
		}
		seen[c.Log] = true
		r := reports[c.Log]
		sources = append(sources, fmt.Sprintf("%s (VERSION %s, COMMIT %s, CPUS %d, ITERATIONS %d)",
			r.Source, r.Meta.Version, commitText(r.Meta), r.Meta.CPUs, r.Meta.Iterations))
	}
	caption = append(caption, "Source: "+strings.Join(sources, "; ")+".")
	for _, d := range set.Differences() {
		caption = append(caption, "Note: "+d+".")
	}
	return caption
}

func commitText(m report.Meta) string {
	if m.Dirty {
		return m.Commit + " (dirty)"
	}
	return m.Commit
}

// px formats a coordinate for the string-typed position fields of go-echarts.
func px(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }
