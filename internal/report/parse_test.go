// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package report

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

const longName = "sched_gamma_with_a_rather_long_name"

// fullLog is a synthetic log with made-up numbers. It has three scenarios, a
// dirty commit, no drift, deltas including a "-0.0%" one, a LATE FRAMES row in
// one scenario only, the AUDIO and SKIPPED lines, and a POPULATION section
// with every line shape.
func fullLog() string {
	var b strings.Builder
	w := func(s string) { b.WriteString(s + "\n") }
	names := []string{"EEVDF", "sched_alpha", "sched_beta", longName}
	w("PANDEMONIUM PRISM-FRAME")
	w("VERSION:     9.8.7")
	w("COMMIT:      abc1234 (dirty)")
	w("TIMESTAMP:   20300102-030405")
	w("CPUS:        12")
	w("ITERATIONS:  4")
	w("CALIBRATION: 1000.250 iterations/us")
	w("")
	w("SCENARIO ALPHA")
	w(tableRow("METRIC", names...))
	w(tableRow("AVG FPS", "100.00 ±1.00", "112.80 ±0.50", "99.10 ±2.25", "1234567890123456789.5 ±12345.5"))
	w(tableRow("FRAME TIME p99 ms", "4.20 ±0.10", "-", "3.90 ±0.05", "5.00 ±0.00"))
	w(tableRow("JOB WAKE p99.9 us", "20.50 ±1.00", "18.25 ±0.75", "-", "19.00 ±0.50"))
	w(tableRow("CUSTOM THING ms", "7.00 ±0.70", "6.00 ±0.60", "5.00 ±0.50", "4.00 ±0.40"))
	w(tableRow("UNIT-LESS THING", "1.00 ±0.10", "2.00 ±0.20", "3.00 ±0.30", "4.00 ±0.40"))
	w(tableRow("vs EEVDF AVG FPS", "", "+12.8%", "-0.0%", "+9999999999999999999.9%"))
	w(tableRow("vs EEVDF FRAME TIME p99", "", "", "-97.8%", "+19.0%"))
	w("")
	w("SCENARIO BETA")
	w(tableRow("METRIC", names...))
	w(tableRow("AVG FPS", "90.00 ±1.00", "91.00 ±1.00", "92.00 ±1.00", "93.00 ±1.00"))
	w(tableRow("LATE FRAMES %", "0.36 ±0.38", "0.06 ±0.02", "0.03 ±0.03", "0.02 ±0.03"))
	w(tableRow("REGIME TICKS LIGHT", "10.0 ±1.0", "-", "-", "-"))
	w(tableRow("vs EEVDF LATE FRAMES", "", "-12.5%", "+3.0%", ""))
	w("")
	w("SCENARIO EACH_CONTENDED")
	w(tableRow("METRIC", names...))
	w(tableRow("AVG FPS", "50.00 ±1.00", "51.00 ±1.00", "52.00 ±1.00", "53.00 ±1.00"))
	w("")
	w("vs EEVDF rows are mean deltas; significance is montauk's, below.")
	w("AUDIO: the audio thread did not get SCHED_FIFO on every run")
	w("SKIPPED: sched_delta -- crashed (during ALPHA)")
	w("SKIPPED: sched_eps -- fell through to EEVDF -- twice")
	w("")
	w("POPULATION (montauk, per-iteration .prom files)")
	w("METRIC pandemonium_frame_higher_fps")
	w("  [commit=abc1234 scenario=alpha version=9.8.7]")
	w("    sched_alpha vs eevdf [N=4/4]: mean 112.8 (112.1..113.5) vs 100 (98.5..101.25) | cliff +1.00 (large) | perm p=0.029 | power 2")
	w("    sched_alpha vs sched_beta [N=4/3]: mean 112.8 (112.1..113.5) vs 99.1 (96.9..101.2) | cliff -0.50 (large) | perm p=0.114 | power >20")
	w("    MCB (higher=better): eevdf=not-best sched_alpha=best sched_beta=tied " + longName + "=not-best")
	w("  [commit=abc1234 scenario=beta version=9.8.7]")
	w("    only sched_alpha [N=1]: mean 12.5 (12.5..12.5) -- no comparison")
	w("")
	w("METRIC pandemonium_frame_lower_ft_p99")
	w("  [(no labels)]")
	w("    only (none) -- no comparison")
	w("  [scenario=each_contended]")
	w("    eevdf vs sched_beta [N=4/4]: mean 3.463 (2.813..4.769) vs 6.0156 (5.798..6.137) | cliff -0.12 (negligible) | perm p=1.000 | power 20")
	w("    MCB (lower=better): eevdf=tied sched_beta=tied")
	w("")
	w("METRIC some_other_family")
	w("  [scenario=alpha]")
	w("    MCB (higher=better): eevdf=best")
	return b.String()
}

func mustParse(t *testing.T, text string) *Report {
	t.Helper()
	r, err := Parse("full.log", strings.NewReader(text))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return r
}

func TestParseFullLog(t *testing.T) {
	r := mustParse(t, fullLog())
	check(t, "Source", r.Source, "full.log")
	want := Meta{Version: "9.8.7", Commit: "abc1234", Dirty: true, Timestamp: "20300102-030405",
		CPUs: 12, Iterations: 4, Calibration: 1000.25}
	if r.Meta != want {
		t.Errorf("Meta = %+v, want %+v", r.Meta, want)
	}
	check(t, "scenario count", len(r.Scenarios), 3)
	for i, name := range []string{"ALPHA", "BETA", "EACH_CONTENDED"} {
		check(t, "scenario name", r.Scenarios[i].Name, name)
	}
	wantNames := []string{"EEVDF", "sched_alpha", "sched_beta", longName}
	for _, sc := range r.Scenarios {
		if !reflect.DeepEqual(sc.Schedulers, wantNames) {
			t.Errorf("%s schedulers = %q", sc.Name, sc.Schedulers)
		}
	}

	alpha := r.Scenarios[0]
	check(t, "alpha rows", len(alpha.Rows), 5)
	fps := alpha.Rows[0]
	if fps.Metric != (Metric{"AVG FPS", "", HigherIsBetter}) {
		t.Errorf("AVG FPS metric = %+v", fps.Metric)
	}
	check(t, "fps[0]", fps.Cells[0], Cell{Mean: 100, SD: 1, Text: "100.00 ±1.00", Valid: true})
	check(t, "fps[2]", fps.Cells[2], Cell{Mean: 99.1, SD: 2.25, Text: "99.10 ±2.25", Valid: true})
	check(t, "fps[3]", fps.Cells[3], Cell{Mean: 1234567890123456789.5, SD: 12345.5, Text: "1234567890123456789.5 ±12345.5", Valid: true})
	ft := alpha.Rows[1]
	if ft.Metric != (Metric{"FRAME TIME p99", "ms", LowerIsBetter}) {
		t.Errorf("FRAME TIME metric = %+v", ft.Metric)
	}
	check(t, "dash cell", ft.Cells[1], Cell{Text: "-"})
	check(t, "wake metric", alpha.Rows[2].Metric, Metric{"JOB WAKE p99.9", "us", LowerIsBetter})
	check(t, "wake dash", alpha.Rows[2].Cells[2], Cell{Text: "-"})
	check(t, "unknown metric with a unit", alpha.Rows[3].Metric, Metric{Label: "CUSTOM THING", Unit: "ms"})
	check(t, "unknown metric", alpha.Rows[4].Metric, Metric{Label: "UNIT-LESS THING"})

	check(t, "alpha deltas", len(alpha.Deltas), 2)
	d := alpha.Deltas[0]
	check(t, "delta metric", d.Metric, Metric{"AVG FPS", "", HigherIsBetter})
	check(t, "baseline delta", d.Cells[0], DeltaCell{})
	check(t, "delta +12.8", d.Cells[1], DeltaCell{Percent: 12.8, Text: "+12.8%", Valid: true})
	if c := d.Cells[2]; c.Percent != 0 || !math.Signbit(c.Percent) || c.Text != "-0.0%" || !c.Valid {
		t.Errorf("-0.0%% delta = %+v", c)
	}
	check(t, "wide delta text", d.Cells[3].Text, "+9999999999999999999.9%")
	d = alpha.Deltas[1]
	check(t, "delta ft metric", d.Metric, Metric{"FRAME TIME p99", "ms", LowerIsBetter})
	check(t, "blank mid delta", d.Cells[1], DeltaCell{})
	check(t, "delta -97.8", d.Cells[2], DeltaCell{Percent: -97.8, Text: "-97.8%", Valid: true})

	beta := r.Scenarios[1]
	check(t, "beta rows", len(beta.Rows), 3)
	check(t, "late metric", beta.Rows[1].Metric, Metric{"LATE FRAMES", "%", LowerIsBetter})
	check(t, "regime metric", beta.Rows[2].Metric, Metric{"REGIME TICKS LIGHT", "", Unknown})
	check(t, "late delta", beta.Deltas[0].Cells[1].Percent, -12.5)
	check(t, "trailing blank delta", beta.Deltas[0].Cells[3], DeltaCell{})
	for _, row := range r.Scenarios[2].Rows {
		if row.Metric.Label == "LATE FRAMES" {
			t.Errorf("LATE FRAMES leaked into %s", r.Scenarios[2].Name)
		}
	}
	check(t, "each_contended deltas", len(r.Scenarios[2].Deltas), 0)

	if !reflect.DeepEqual(r.Notes, []string{"the audio thread did not get SCHED_FIFO on every run"}) {
		t.Errorf("Notes = %q", r.Notes)
	}
	wantSkips := []Skip{{"sched_delta", "crashed (during ALPHA)"}, {"sched_eps", "fell through to EEVDF -- twice"}}
	if !reflect.DeepEqual(r.Skipped, wantSkips) {
		t.Errorf("Skipped = %+v", r.Skipped)
	}

	check(t, "family count", len(r.Population), 3)
	f := r.Population[0]
	check(t, "family name", f.Name, "pandemonium_frame_higher_fps")
	check(t, "family key", f.Key, "fps")
	check(t, "family direction", f.Direction, HigherIsBetter)
	check(t, "group count", len(f.Groups), 2)
	g := f.Groups[0]
	wantLabels := map[string]string{"commit": "abc1234", "scenario": "alpha", "version": "9.8.7"}
	if !reflect.DeepEqual(g.Labels, wantLabels) {
		t.Errorf("Labels = %v", g.Labels)
	}
	check(t, "pair count", len(g.Pairs), 2)
	wantPair := Pair{A: "sched_alpha", B: "eevdf", NA: 4, NB: 4, MeanA: 112.8, MinA: 112.1, MaxA: 113.5,
		MeanB: 100, MinB: 98.5, MaxB: 101.25, Cliff: 1, Magnitude: "large", P: 0.029, Power: 2}
	check(t, "pair 0", g.Pairs[0], wantPair)
	wantPair = Pair{A: "sched_alpha", B: "sched_beta", NA: 4, NB: 3, MeanA: 112.8, MinA: 112.1, MaxA: 113.5,
		MeanB: 99.1, MinB: 96.9, MaxB: 101.2, Cliff: -0.5, Magnitude: "large", P: 0.114, PowerAbove: true}
	check(t, "pair 1 with power >20", g.Pairs[1], wantPair)
	wantStandings := []Standing{{"eevdf", NotBest}, {"sched_alpha", Best}, {"sched_beta", Tied}, {longName, NotBest}}
	if !reflect.DeepEqual(g.Standings, wantStandings) {
		t.Errorf("Standings = %+v", g.Standings)
	}
	check(t, "only", f.Groups[1].Only[0], Only{Scheduler: "sched_alpha", N: 1, Mean: 12.5, Min: 12.5, Max: 12.5})

	lower := r.Population[1]
	check(t, "lower direction", lower.Direction, LowerIsBetter)
	check(t, "lower key", lower.Key, "ft_p99")
	check(t, "no labels", len(lower.Groups[0].Labels), 0)
	if lower.Groups[0].Labels == nil {
		t.Error("Labels of (no labels) is nil, want an empty map")
	}
	check(t, "only (none)", lower.Groups[0].Only[0], Only{})
	check(t, "power 20", lower.Groups[1].Pairs[0].Power, 20)
	check(t, "negligible", lower.Groups[1].Pairs[0].Magnitude, "negligible")

	other := r.Population[2]
	check(t, "foreign family direction from MCB", other.Direction, HigherIsBetter)
	check(t, "foreign family key", other.Key, "")
}

func TestParseOptionalParts(t *testing.T) {
	text := strings.Replace(fullLog(), "CALIBRATION: 1000.250 iterations/us",
		"CALIBRATION: 1000.125 iterations/us  (end-of-run drift -0.4%)", 1)
	text = strings.Replace(text, " (dirty)", "", 1)
	m := mustParse(t, text).Meta
	check(t, "Calibration", m.Calibration, 1000.125)
	check(t, "Drift", m.Drift, -0.4)
	check(t, "HasDrift", m.HasDrift, true)
	check(t, "Dirty", m.Dirty, false)
	check(t, "Commit", m.Commit, "abc1234")

	// A log without a population section ends at the footer, with or without
	// a trailing blank line.
	cut := fullLog()
	cut = cut[:strings.Index(cut, "\nPOPULATION")]
	for _, tail := range []string{"", "\n"} {
		r := mustParse(t, cut+tail)
		check(t, "population families", len(r.Population), 0)
		check(t, "skips", len(r.Skipped), 2)
	}
}

// populationLog is fullLog cut at its POPULATION header, followed by the header
// and body.
func populationLog(body string) string {
	full := fullLog()
	return full[:strings.Index(full, "POPULATION")] + populationLine + "\n" + body
}

// TestParsePopulationStderr covers a POPULATION section that holds only the
// text montauk printed on stderr. Blank lines separate paragraphs and are not
// kept, and each line keeps its indentation but not its trailing blanks.
func TestParsePopulationStderr(t *testing.T) {
	text := populationLog("population: 1 file(s) found\n\nN=1 per group -- inference underpowered\n    (need N>=3 per side)   \n")
	r := mustParse(t, text)
	want := []string{"population: 1 file(s) found", "N=1 per group -- inference underpowered", "    (need N>=3 per side)"}
	if !reflect.DeepEqual(r.PopulationText, want) {
		t.Errorf("PopulationText = %q, want %q", r.PopulationText, want)
	}
	check(t, "population families", len(r.Population), 0)
}

// TestParsePopulationMixed covers a METRIC block followed by a stderr paragraph
// for the lower family, which is how a half-failed run reads.
func TestParsePopulationMixed(t *testing.T) {
	full := fullLog()
	text := full[:strings.Index(full, "METRIC pandemonium_frame_lower")] +
		"lower family: N=1 per group -- inference underpowered\n"
	r := mustParse(t, text)
	check(t, "family count", len(r.Population), 1)
	check(t, "family name", r.Population[0].Name, "pandemonium_frame_higher_fps")
	check(t, "group count", len(r.Population[0].Groups), 2)
	want := []string{"lower family: N=1 per group -- inference underpowered"}
	if !reflect.DeepEqual(r.PopulationText, want) {
		t.Errorf("PopulationText = %q, want %q", r.PopulationText, want)
	}
}

// TestParsePopulationBlockStrict checks that a line the METRIC grammar does not
// know stays an error inside an open block, where it could hide a wrong number.
func TestParsePopulationBlockStrict(t *testing.T) {
	text, line := corrupt(t, fullLog(), "METRIC pandemonium_frame_higher_fps",
		"  [commit=abc1234 scenario=alpha version=9.8.7]", "garbage")
	wantErr(t, text, "bad.log:"+strconv.Itoa(line)+": ", "unknown line in the POPULATION section")
}

func TestParseNonFinite(t *testing.T) {
	text := strings.Replace(fullLog(), tableRow("AVG FPS", "50.00 ±1.00", "51.00 ±1.00", "52.00 ±1.00", "53.00 ±1.00"),
		tableRow("AVG FPS", "nan ±nan", "inf ±1.00", "-inf ±1.00", "5.00 ±nan"), 1)
	text = strings.Replace(text, tableRow("vs EEVDF AVG FPS", "", "+12.8%", "-0.0%", "+9999999999999999999.9%"),
		tableRow("vs EEVDF AVG FPS", "", "+nan%", "-0.0%", "+9999999999999999999.9%"), 1)
	text = strings.Replace(text, tableRow("vs EEVDF FRAME TIME p99", "", "", "-97.8%", "+19.0%"),
		tableRow("vs EEVDF FRAME TIME p99", "", "", "+inf%", "+19.0%"), 1)
	r := mustParse(t, text)
	cells := r.Scenarios[2].Rows[0].Cells
	for i, c := range cells {
		if c.Valid {
			t.Errorf("cell %d is Valid: %+v", i, c)
		}
	}
	check(t, "nan text", cells[0].Text, "nan ±nan")
	if !math.IsNaN(cells[0].Mean) || !math.IsInf(cells[1].Mean, 1) || !math.IsInf(cells[2].Mean, -1) || !math.IsNaN(cells[3].SD) {
		t.Errorf("non-finite values were not kept: %+v", cells)
	}
	check(t, "finite mean beside nan sd", cells[3].Mean, 5.0)
	delta := r.Scenarios[0].Deltas
	check(t, "nan delta valid", delta[0].Cells[1].Valid, false)
	check(t, "nan delta text", delta[0].Cells[1].Text, "+nan%")
	check(t, "inf delta valid", delta[1].Cells[2].Valid, false)
	check(t, "finite delta beside nan", delta[0].Cells[3].Valid, true)
}

func TestParseCRLF(t *testing.T) {
	plain := mustParse(t, fullLog())
	crlf := strings.ReplaceAll(fullLog(), "\n", "\r\n")
	if got := mustParse(t, crlf); !reflect.DeepEqual(got, plain) {
		t.Error("CRLF input parsed differently from LF input")
	}
	// Editors and pipes also strip or add trailing blanks, including the blank
	// last column of a delta row.
	var lines []string
	for i, l := range strings.Split(fullLog(), "\n") {
		if i%2 == 0 {
			l = strings.TrimRight(l, " ")
		} else {
			l += "  \t"
		}
		lines = append(lines, l)
	}
	if got := mustParse(t, strings.Join(lines, "\r\n")); !reflect.DeepEqual(got, plain) {
		t.Error("input with trailing whitespace parsed differently")
	}
}

// corrupt replaces old by repl on the first line at or after the line that
// contains after, and returns the new text with the 1-based number of the line
// it changed. An empty after starts at the top.
func corrupt(t *testing.T, text, after, old, repl string) (string, int) {
	t.Helper()
	lines := strings.Split(text, "\n")
	start := 0
	if after != "" {
		for start < len(lines) && !strings.Contains(lines[start], after) {
			start++
		}
	}
	for i := start; i < len(lines); i++ {
		if strings.Contains(lines[i], old) {
			lines[i] = strings.Replace(lines[i], old, repl, 1)
			return strings.Join(lines, "\n"), i + 1
		}
	}
	t.Fatalf("%q not found after %q", old, after)
	return "", 0
}

func wantErr(t *testing.T, text, wantPrefix, wantText string) {
	t.Helper()
	_, err := Parse("bad.log", strings.NewReader(text))
	if err == nil {
		t.Fatal("no error")
	}
	if !strings.HasPrefix(err.Error(), wantPrefix) || !strings.Contains(err.Error(), wantText) {
		t.Errorf("error %q, want prefix %q and text %q", err, wantPrefix, wantText)
	}
}

func TestParseErrors(t *testing.T) {
	names := []string{"EEVDF", "sched_alpha", "sched_beta", longName}
	ftRow := tableRow("FRAME TIME p99 ms", "4.20 ±0.10", "-", "3.90 ±0.05", "5.00 ±0.00")
	wakeRow := tableRow("JOB WAKE p99.9 us", "20.50 ±1.00", "18.25 ±0.75", "-", "19.00 ±0.50")
	fpsRow := tableRow("AVG FPS", "100.00 ±1.00", "112.80 ±0.50", "99.10 ±2.25", "1234567890123456789.5 ±12345.5")
	delta := tableRow("vs EEVDF AVG FPS", "", "+12.8%", "-0.0%", "+9999999999999999999.9%")
	lastDelta := tableRow("vs EEVDF FRAME TIME p99", "", "", "-97.8%", "+19.0%")
	mcbLower := "    MCB (lower=better): eevdf=tied sched_beta=tied"
	cases := []struct {
		name, after, old, repl, want string
		// shift is how many lines past the changed one the parser notices.
		shift int
	}{
		{"first line", "", "PANDEMONIUM PRISM-FRAME", "PRISM v1.0", "not a PRISM-FRAME log", 0},
		{"missing preamble field", "", "ITERATIONS:  4", "ITERATIONS:", "expected ITERATIONS:", 0},
		{"preamble field out of order", "", "ITERATIONS:  4", "VERSION:     1", "expected ITERATIONS:", 0},
		{"bad cpus", "", "CPUS:        12", "CPUS:        twelve", "bad CPUS", 0},
		{"zero iterations", "", "ITERATIONS:  4", "ITERATIONS:  0", "bad ITERATIONS", 0},
		{"bad timestamp", "", "20300102-030405", "2030-01-02", "bad TIMESTAMP", 0},
		{"bad calibration", "", "1000.250 iterations/us", "1000.250 iters", "bad CALIBRATION", 0},
		{"bad drift", "", "1000.250 iterations/us", "1000.250 iterations/us  (end-of-run drift fast%)", "bad CALIBRATION", 0},
		{"no blank after the preamble", "", "CALIBRATION: 1000.250 iterations/us", "CALIBRATION: 1000.250 iterations/us\nSCENARIO X", "expected a blank line", 1},
		{"scenario name with a space", "", "SCENARIO BETA", "SCENARIO BE TA", "expected \"SCENARIO", 0},
		{"duplicate scenario", "", "SCENARIO BETA", "SCENARIO ALPHA", "appears twice", 0},
		{"scenario without a header", "SCENARIO BETA", tableRow("METRIC", names...), "", "no METRIC header", 0},
		{"wrong header label", "SCENARIO BETA", tableRow("METRIC", names...), tableRow("NOT A HEADER", names...), "METRIC header", 0},
		{"header with a blank column", "SCENARIO BETA", tableRow("METRIC", names...), tableRow("METRIC", "EEVDF", "", "sched_beta"), "cannot split the header", 0},
		{"duplicate scheduler", "SCENARIO BETA", tableRow("METRIC", names...), tableRow("METRIC", "EEVDF", "SCHED_ALPHA", "sched_alpha", longName), "appears twice", 0},
		{"header without schedulers", "SCENARIO BETA", tableRow("METRIC", names...), "METRIC", "no scheduler columns", 0},
		{"row with a missing cell", "", ftRow, tableRow("FRAME TIME p99 ms", "4.20 ±0.10", "-", "3.90 ±0.05"), "empty cell", 0},
		{"row with an extra cell", "", ftRow, tableRow("FRAME TIME p99 ms", "4.20 ±0.10", "-", "3.90 ±0.05", "5.00 ±0.00", "6.00 ±0.00"), "text after the last column", 0},
		{"blank cell in a metric row", "", wakeRow, tableRow("JOB WAKE p99.9 us", "20.50 ±1.00", "", "-", "19.00 ±0.50"), "empty cell", 0},
		{"bad number", "", wakeRow, tableRow("JOB WAKE p99.9 us", "9x.10 ±2.25", "18.25 ±0.75", "-", "19.00 ±0.50"), "bad number", 0},
		{"missing sd", "", wakeRow, tableRow("JOB WAKE p99.9 us", "20.50", "18.25 ±0.75", "-", "19.00 ±0.50"), "not \"-\" or \"mean ±sd\"", 0},
		{"hex number", "", wakeRow, tableRow("JOB WAKE p99.9 us", "0x1p3 ±0.50", "18.25 ±0.75", "-", "19.00 ±0.50"), "bad number", 0},
		{"misaligned cell", "", wakeRow, strings.Replace(wakeRow, "  20.50 ±1.00", " 20.50 ±1.00 ", 1), "not aligned", 0},
		{"delta without a sign", "", delta, tableRow("vs EEVDF AVG FPS", "", "12.8%", "-0.0%", "+1.0%"), "not a signed percent", 0},
		{"delta without a percent", "", delta, tableRow("vs EEVDF AVG FPS", "", "+12.8", "-0.0%", "+1.0%"), "not a signed percent", 0},
		{"delta with a bad number", "", delta, tableRow("vs EEVDF AVG FPS", "", "+1x.8%", "-0.0%", "+1.0%"), "bad number", 0},
		{"EEVDF cell filled in a delta row", "", delta, tableRow("vs EEVDF AVG FPS", "+1.0%", "+12.8%", "-0.0%", "+1.0%"), "must be blank", 0},
		{"metric row after deltas", "", lastDelta, lastDelta + "\n" + tableRow("OTHER THING ms", "1.00 ±0.10", "-", "-", "-"), "metric row after", 1},
		{"duplicate row", "", ftRow, ftRow + "\n" + ftRow, "appears twice", 1},
		{"unknown footer line", "", "AUDIO: the audio thread did not get SCHED_FIFO on every run", "WARNING: something", "unknown line in the footer", 0},
		{"bad skipped line", "", "SKIPPED: sched_delta -- crashed (during ALPHA)", "SKIPPED: sched_delta crashed", "SKIPPED:", 0},
		{"table row in the footer", "", "AUDIO: the audio thread did not get SCHED_FIFO on every run", fpsRow, "unknown line in the footer", 0},
		{"wrong population header", "", "POPULATION (montauk, per-iteration .prom files)", "POPULATION (something else)", "expected \"POPULATION", 0},
		{"within-run line", "", mcbLower, mcbLower + "\n      within-run q50 perm p=0.5 (a vs b)", "unknown line in the POPULATION", 1},
		{"bad cell header", "", "  [(no labels)]", "  (no labels)", "expected a \"[labels]\" cell header", 0},
		{"bad cell label", "", "[scenario=each_contended]", "[scenario]", "bad label", 0},
		{"duplicate cell label", "", "[scenario=each_contended]", "[scenario=a scenario=b]", "bad label", 0},
		{"pair with a bad number", "", "mean 112.8 (112.1..113.5)", "mean 11x.8 (112.1..113.5)", "bad number", 0},
		{"pair with a bad range", "", "(112.1..113.5)", "(112.1,113.5)", "bad number", 0},
		{"unknown MCB verdict", "", "sched_alpha=best", "sched_alpha=winner", "unreadable MCB entry", 0},
		{"MCB contradicts the family", "", "MCB (higher=better): eevdf=not-best", "MCB (lower=better): eevdf=not-best", "family name says otherwise", 0},
		{"second MCB line", "", mcbLower, "    MCB (lower=better): eevdf=tied\n    MCB (lower=better): sched_beta=tied", "second MCB", 1},
		{"pair after MCB", "", mcbLower, mcbLower + "\n    eevdf vs sched_beta [N=4/4]: mean 1 (1..1) vs 2 (2..2) | cliff +0.00 (negligible) | perm p=1.000 | power 3", "pair line after", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text, line := corrupt(t, fullLog(), c.after, c.old, c.repl)
			wantErr(t, text, "bad.log:"+strconv.Itoa(line+c.shift)+": ", c.want)
		})
	}
}

func TestParseDeltaWithoutBaseline(t *testing.T) {
	avg := tableRow("AVG FPS", "50.00 ±1.00", "51.00 ±1.00", "52.00 ±1.00", "53.00 ±1.00")
	text, _ := corrupt(t, fullLog(), "SCENARIO EACH_CONTENDED", tableRow("METRIC", "EEVDF", "sched_alpha", "sched_beta", longName),
		tableRow("METRIC", "x_eevdf", "sched_alpha", "sched_beta", longName))
	text, line := corrupt(t, text, "SCENARIO EACH_CONTENDED", avg, avg+"\n"+tableRow("vs EEVDF AVG FPS", "", "+1.0%", "+2.0%", "+3.0%"))
	wantErr(t, text, "bad.log:"+strconv.Itoa(line+1)+": ", "no EEVDF column")
}

// TestParseEndOfFile cuts a good log at several points. The reported line is
// the one that would have come next.
func TestParseEndOfFile(t *testing.T) {
	lines := strings.Split(fullLog(), "\n")
	at := func(prefix string) int {
		for i, l := range lines {
			if strings.HasPrefix(l, prefix) {
				return i
			}
		}
		t.Fatalf("no line starting with %q", prefix)
		return 0
	}
	cut := func(n int) string { return strings.Join(lines[:n], "\n") + "\n" }
	cases := []struct {
		name string
		text string
		line int
		want string
	}{
		{"empty file", "", 1, "empty file"},
		{"only blank lines", "\n\n", 1, "not a PRISM-FRAME log"},
		{"title only", cut(1), 2, "missing VERSION:"},
		{"preamble cut short", cut(at("ITERATIONS:")), at("ITERATIONS:") + 1, "missing ITERATIONS:"},
		{"no scenario", cut(at("SCENARIO ALPHA")), at("SCENARIO ALPHA") + 1, "no SCENARIO block"},
		{"cut inside a table", cut(at("SCENARIO BETA") - 1), at("SCENARIO BETA"), "missing the footer"},
		{"no footer", cut(at("vs EEVDF rows")), at("vs EEVDF rows") + 1, "missing the footer"},
		{"empty population", cut(at("METRIC pandemonium_frame_higher_fps")), at("METRIC pandemonium_frame_higher_fps") + 1, "the POPULATION section is empty"},
		{"population header only", cut(at(populationLine) + 1), at(populationLine) + 2, "the POPULATION section is empty"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wantErr(t, c.text, "bad.log:"+strconv.Itoa(c.line)+": ", c.want)
		})
	}
	t.Run("footer before any scenario", func(t *testing.T) {
		text := strings.Join(lines[:at("SCENARIO ALPHA")], "\n") + "\n" + lines[at("vs EEVDF rows")] + "\n"
		wantErr(t, text, "bad.log:"+strconv.Itoa(at("SCENARIO ALPHA")+1)+": ", "before any SCENARIO")
	})
}

func TestParseFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic.log")
	if err := os.WriteFile(path, []byte(fullLog()), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	check(t, "Source", r.Source, path)
	check(t, "scenarios", len(r.Scenarios), 3)
	if _, err := ParseFile(path + ".missing"); err == nil {
		t.Error("no error for a missing file")
	}
}

func TestParseLongLine(t *testing.T) {
	text := fullLog() + strings.Repeat("x", maxLine+1)
	if _, err := Parse("long.log", strings.NewReader(text)); err == nil {
		t.Error("no error for a line longer than the limit")
	}
}

func TestMetricByKey(t *testing.T) {
	m, ok := MetricByKey("ft_p99")
	if !ok || m != (Metric{"FRAME TIME p99", "ms", LowerIsBetter}) {
		t.Errorf("MetricByKey(ft_p99) = %+v, %v", m, ok)
	}
	if _, ok := MetricByKey("nope"); ok {
		t.Error("found a metric for an unknown key")
	}
	keys := 0
	for _, s := range registry {
		if _, ok := MetricByKey(s.key); !ok {
			t.Errorf("registry key %q is not found", s.key)
		}
		keys++
	}
	check(t, "registry size", keys, 17)
}

func TestParseUnknownDeltaLabel(t *testing.T) {
	line := tableRow("vs EEVDF CUSTOM THING", "", "+1.0%", "-2.0%", "+3.0%")
	text, _ := corrupt(t, fullLog(), "", tableRow("vs EEVDF FRAME TIME p99", "", "", "-97.8%", "+19.0%"), line)
	d := mustParse(t, text).Scenarios[0].Deltas[1]
	check(t, "unknown delta metric", d.Metric, Metric{Label: "CUSTOM THING"})
	check(t, "unknown delta cell", d.Cells[2].Percent, -2.0)
}

func TestParseRejectsMoreBadValues(t *testing.T) {
	cases := []struct{ name, old, repl, want string }{
		{"commit with only the dirty tag", "abc1234 (dirty)", "(dirty)", "bad COMMIT"},
		{"family name with a space", "METRIC some_other_family", "METRIC some other family", "bad family name"},
		{"run count out of range", "[N=1]", "[N=99999999999999999999]", "bad number"},
		{"only line with a bad range", "(12.5..12.5)", "(12.5)", "bad number"},
		{"non-finite calibration", "1000.250 iterations/us", "nan iterations/us", "bad CALIBRATION"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text, line := corrupt(t, fullLog(), "", c.old, c.repl)
			wantErr(t, text, "bad.log:"+strconv.Itoa(line)+": ", c.want)
		})
	}
}
