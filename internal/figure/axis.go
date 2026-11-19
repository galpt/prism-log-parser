// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package figure

import (
	"math"
	"strconv"
	"strings"

	"github.com/galpt/prism-log-parser/internal/report"
)

// logRatio is the spread of means above which a plot switches to a log axis.
// Below it a linear axis still shows the smallest bar at a readable height.
const logRatio = 20

// axisRange is a y axis whose ends are chosen here rather than by ECharts.
// The figure has to know the ends in advance, both to keep room for the value
// labels above the whiskers and to place an "n/a" at the bottom of a log axis.
// For a linear axis step is the tick interval. A log axis has its ticks at the
// powers of ten between min and max and leaves step zero.
type axisRange struct {
	log            bool
	min, max, step float64
}

// useLog reports whether the valid means of a plot call for a log axis. Every
// mean must be positive, since a log axis has no place for zero, and they must
// spread over at least logRatio.
func useLog(cells []report.Cell) bool {
	lo, hi := math.Inf(1), 0.0
	n := 0
	for _, c := range cells {
		if !c.Valid {
			continue
		}
		if c.Mean <= 0 {
			return false
		}
		lo, hi = math.Min(lo, c.Mean), math.Max(hi, c.Mean)
		n++
	}
	return n >= 2 && hi/lo >= logRatio
}

// whisker returns the ends of the ±SD whisker of a valid cell. On a log axis
// the lower end stops at the mean when mean - sd is not positive, because the
// axis cannot show it. On a linear axis a whisker of a non-negative mean
// stops at zero: every metric here is a rate, a time or a count, and an axis
// that runs below zero only to hold a whisker suggests values that cannot
// occur.
func whisker(c report.Cell, log bool) (low, high float64) {
	low, high = c.Mean-c.SD, c.Mean+c.SD
	switch {
	case log && low <= 0:
		low = c.Mean
	case !log && c.Mean >= 0 && low < 0:
		low = 0
	}
	return low, high
}

// metricAxis fits an axis to the whiskers of a metric plot. head is the room in px
// that the value labels need above the highest whisker, out of a plot that is
// plotH px tall.
func metricAxis(cells []report.Cell, plotH, head float64) axisRange {
	log := useLog(cells)
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, c := range cells {
		if c.Valid {
			low, high := whisker(c, log)
			lo, hi = math.Min(lo, low), math.Max(hi, high)
		}
	}
	if math.IsInf(lo, 1) {
		lo, hi = 0, 0
	}
	if log {
		return logAxis(lo, hi, plotH, head)
	}
	return linearAxis(lo, hi, plotH, head, 0)
}

// linearAxis returns an axis that includes zero and [lo, hi], with at least
// head px above hi and foot px below lo when lo is negative. The ends are
// multiples of a 1-2-5 step that gives about five intervals.
func linearAxis(lo, hi, plotH, head, foot float64) axisRange {
	lo, hi = math.Min(lo, 0), math.Max(hi, 0)
	if hi == lo {
		hi = 1
	}
	step := niceStep((hi - lo) / 5)
	a := axisRange{step: step, min: math.Floor(lo/step) * step, max: math.Ceil(hi/step) * step}
	for (a.max-hi)/(a.max-a.min)*plotH < head {
		a.max += step
	}
	for lo < 0 && (lo-a.min)/(a.max-a.min)*plotH < foot {
		a.min -= step
	}
	a.min, a.max = roundTo(a.min, step), roundTo(a.max, step)
	return a
}

// logAxis returns an axis from the power of ten at or below lo to one above
// hi with head px to spare. Both lo and hi must be positive.
func logAxis(lo, hi, plotH, head float64) axisRange {
	a := axisRange{log: true, min: math.Pow(10, math.Floor(math.Log10(lo))), max: math.Pow(10, math.Ceil(math.Log10(hi)))}
	for a.max <= a.min || math.Log10(a.max/hi)/math.Log10(a.max/a.min)*plotH < head {
		a.max *= 10
	}
	return a
}

// niceStep rounds a raw interval up to 1, 2 or 5 times a power of ten.
func niceStep(raw float64) float64 {
	p := math.Pow(10, math.Floor(math.Log10(raw)))
	for _, f := range []float64{1, 2, 5} {
		if f*p >= raw*(1-1e-9) {
			return f * p
		}
	}
	return 10 * p
}

// roundTo removes the binary noise that repeated additions of step leave, so
// that 0.30000000000000004 reaches ECharts as 0.3.
func roundTo(v, step float64) float64 {
	d := decimals(step)
	r, _ := strconv.ParseFloat(strconv.FormatFloat(v, 'f', d, 64), 64)
	return r
}

// decimals is the number of digits after the point that a multiple of step
// needs, which is also how ECharts prints its tick labels.
func decimals(step float64) int {
	return max(0, int(math.Ceil(-math.Log10(step)-1e-9)))
}

// ticks returns the tick labels of an axis as ECharts prints them, with
// thousands separators, so that the space for them can be measured.
func (a axisRange) ticks() []string {
	var out []string
	if a.log {
		for v := a.min; v <= a.max*(1+1e-9); v *= 10 {
			out = append(out, tickText(v, decimals(v)))
		}
		return out
	}
	d := decimals(a.step)
	for i := 0; ; i++ {
		v := a.min + float64(i)*a.step
		if v > a.max+a.step*1e-9 {
			return out
		}
		out = append(out, tickText(v, d))
	}
}

func tickText(v float64, decimals int) string {
	s := strconv.FormatFloat(math.Abs(v), 'f', decimals, 64)
	intPart, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	if v < 0 && strings.Trim(s, "0.") != "" {
		b.WriteByte('-')
	}
	for i := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(intPart[i])
	}
	if frac != "" {
		b.WriteString("." + frac)
	}
	return b.String()
}
