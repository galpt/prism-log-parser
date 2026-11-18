// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package report

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// deltaPrefix starts the label of every "vs EEVDF" row.
const deltaPrefix = "vs EEVDF "

type metricSpec struct {
	key    string
	metric Metric
	// delta is set for the metrics that get a "vs EEVDF" row.
	delta bool
}

// registry mirrors METRICS in tests/prism-frame.py. It is how a printed label
// turns back into a unit and a direction, and how the decoder finds where a
// label ends. The REGIME TICKS rows are Unknown because a tick count describes
// what the adaptive layer did and says nothing about how well the run went.
var registry = []metricSpec{
	{"fps", Metric{"AVG FPS", "", HigherIsBetter}, true},
	{"low1", Metric{"1% LOW FPS (p99)", "", HigherIsBetter}, true},
	{"low1_avg", Metric{"1% LOW FPS (avg)", "", HigherIsBetter}, true},
	{"low01", Metric{"0.1% LOW FPS (p99.9)", "", HigherIsBetter}, false},
	{"ft_p50", Metric{"FRAME TIME p50", "ms", LowerIsBetter}, false},
	{"ft_p99", Metric{"FRAME TIME p99", "ms", LowerIsBetter}, true},
	{"ft_max", Metric{"FRAME TIME MAX", "ms", LowerIsBetter}, false},
	{"stutter_pk", Metric{"STUTTERS PER 1K FRAMES", "", LowerIsBetter}, false},
	{"late_pct", Metric{"LATE FRAMES", "%", LowerIsBetter}, true},
	{"wake_p50", Metric{"JOB WAKE p50", "us", LowerIsBetter}, false},
	{"wake_p99", Metric{"JOB WAKE p99", "us", LowerIsBetter}, true},
	{"wake_p999", Metric{"JOB WAKE p99.9", "us", LowerIsBetter}, false},
	{"audio_p99", Metric{"AUDIO LATE p99", "us", LowerIsBetter}, false},
	{"audio_max", Metric{"AUDIO LATE MAX", "us", LowerIsBetter}, false},
	{"reg_l", Metric{"REGIME TICKS LIGHT", "", Unknown}, false},
	{"reg_m", Metric{"REGIME TICKS MIXED", "", Unknown}, false},
	{"reg_h", Metric{"REGIME TICKS HEAVY", "", Unknown}, false},
}

// MetricByKey returns the registry entry for a population key such as "fps".
func MetricByKey(key string) (Metric, bool) {
	for _, s := range registry {
		if s.key == key {
			return s.metric, true
		}
	}
	return Metric{}, false
}

// text is the label as the writer prints it in a metric row.
func (m Metric) text() string {
	if m.Unit == "" {
		return m.Label
	}
	return m.Label + " " + m.Unit
}

// knownLabels are the strings the decoder tries as a row label, longest first
// so that "JOB WAKE p99.9 us" wins over any shorter label it starts with.
var knownLabels = buildKnownLabels()

func buildKnownLabels() []string {
	labels := []string{"METRIC"}
	for _, s := range registry {
		labels = append(labels, s.metric.text())
		if s.delta {
			labels = append(labels, deltaPrefix+s.metric.Label)
		}
	}
	sort.SliceStable(labels, func(i, j int) bool {
		return utf8.RuneCountInString(labels[i]) > utf8.RuneCountInString(labels[j])
	})
	return labels
}

// rowMetric resolves the label of a metric row. A label outside the registry
// still parses: a trailing unit is split off and the direction stays Unknown.
func rowMetric(text string) Metric {
	for _, s := range registry {
		if s.metric.text() == text {
			return s.metric
		}
	}
	for _, unit := range []string{"ms", "us", "%"} {
		if label, ok := strings.CutSuffix(text, " "+unit); ok && label != "" {
			return Metric{Label: label, Unit: unit}
		}
	}
	return Metric{Label: text}
}

// deltaMetric resolves the label of a "vs EEVDF" row. The writer never prints
// a unit there, so the unit comes from the registry and an unknown label is
// kept whole.
func deltaMetric(label string) Metric {
	for _, s := range registry {
		if s.metric.Label == label {
			return s.metric
		}
	}
	return Metric{Label: label}
}
