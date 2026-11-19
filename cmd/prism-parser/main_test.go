// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package main

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/galpt/prism-log-parser/internal/compare"
	"github.com/galpt/prism-log-parser/internal/figure"
	"github.com/galpt/prism-log-parser/internal/render"
	"github.com/galpt/prism-log-parser/internal/report"
)

// tableRow pads a line the way prism-frame does: the label to 26 characters
// and each cell right-aligned in 20.
func tableRow(label string, cells ...string) string {
	line := fmt.Sprintf("%-26s", label)
	for _, c := range cells {
		line += fmt.Sprintf(" %20s", c)
	}
	return line + "\n"
}

// writeLog writes a made-up log with two scenarios, deltas and a POPULATION
// section, and returns its path.
func writeLog(t *testing.T, dir, name, stamp string, schedulers ...string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("PANDEMONIUM PRISM-FRAME\nVERSION:     1.2.3\nCOMMIT:      abc1234\n")
	b.WriteString("TIMESTAMP:   " + stamp + "\nCPUS:        8\nITERATIONS:  5\n")
	b.WriteString("CALIBRATION: 1000.250 iterations/us  (end-of-run drift +0.1%)\n")
	for s, scenario := range []string{"HEAVY", "PACED"} {
		b.WriteString("\nSCENARIO " + scenario + "\n")
		b.WriteString(tableRow("METRIC", schedulers...))
		var fps, wake, delta []string
		for i := range schedulers {
			fps = append(fps, fmt.Sprintf("%.1f ±%.1f", 300+10*float64(i+s), 1.5))
			wake = append(wake, fmt.Sprintf("%.2f ±%.2f", 900/float64(1+i*20), 0.5))
			if i == 0 {
				delta = append(delta, "")
			} else {
				delta = append(delta, fmt.Sprintf("+%.1f%%", 3.3*float64(i)))
			}
		}
		b.WriteString(tableRow("AVG FPS", fps...))
		b.WriteString(tableRow("JOB WAKE p99 us", wake...))
		b.WriteString(tableRow("vs EEVDF AVG FPS", delta...))
	}
	b.WriteString("\nvs EEVDF rows are mean deltas; significance is montauk's, below.\n")
	b.WriteString("\nPOPULATION (montauk, per-iteration .prom files)\nMETRIC pandemonium_frame_higher_fps\n")
	b.WriteString("  [commit=abc1234 scenario=heavy version=1.2.3]\n")
	last := strings.ToLower(schedulers[len(schedulers)-1])
	var standings []string
	for i, s := range schedulers[:len(schedulers)-1] {
		b.WriteString(fmt.Sprintf("    %s vs %s [N=5/5]: mean 330 (329..331) vs %d (299..301) | cliff +1.00 (large) | perm p=0.008 | power 2\n",
			last, strings.ToLower(s), 300+10*i))
		standings = append(standings, strings.ToLower(s)+"=not-best")
	}
	b.WriteString("    MCB (higher=better): " + strings.Join(standings, " ") + " " + last + "=best\n")
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func runCmd(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// chartDirs lists the charts-<digits> directories in the working directory.
func chartDirs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`^charts-[0-9]+$`)
	var dirs []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "charts-") {
			if !e.IsDir() || !pattern.MatchString(e.Name()) {
				t.Errorf("unexpected entry %s", e.Name())
			}
			dirs = append(dirs, e.Name())
		}
	}
	return dirs
}

// predicted builds the figures the command should have written, with the
// same measurement, to learn their sizes.
func predicted(t *testing.T, logs []string) map[string][2]int {
	t.Helper()
	var reports []*report.Report
	for _, p := range logs {
		r, err := report.ParseFile(p)
		if err != nil {
			t.Fatal(err)
		}
		r.Source = filepath.Base(p)
		reports = append(reports, r)
	}
	set, err := compare.New(reports)
	if err != nil {
		t.Fatal(err)
	}
	rd, err := render.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rd.Close()
	figs, err := figure.Build(reports, set, rd.TextWidth)
	if err != nil {
		t.Fatal(err)
	}
	sizes := map[string][2]int{}
	for _, f := range figs {
		sizes[f.Name] = [2]int{f.Width, f.Height}
	}
	return sizes
}

func TestRunWritesOneDirectory(t *testing.T) {
	logs := t.TempDir()
	one := writeLog(t, logs, "one.log", "20300101-000001", "EEVDF", "sched_a", "sched_b", "sched_c")
	two := writeLog(t, logs, "two.log", "20300102-000002", "EEVDF", "sched_d", "sched_e", "sched_c")
	widths := map[int]int{}
	for _, args := range [][]string{{one}, {one, two}} {
		t.Run(fmt.Sprint(len(args), " logs"), func(t *testing.T) {
			t.Chdir(t.TempDir())
			code, stdout, stderr := runCmd(args...)
			if code != 0 {
				t.Fatalf("exit %d, stderr %q", code, stderr)
			}
			dirs := chartDirs(t)
			if len(dirs) != 1 {
				t.Fatalf("%d charts directories, want exactly one: %v", len(dirs), dirs)
			}
			dir := dirs[0]
			names := []string{"prism-scenario-heavy.png", "prism-scenario-paced.png", "prism-summary.png"}
			want := []string{dir}
			for _, n := range names {
				want = append(want, filepath.Join(dir, n))
			}
			if got := strings.Fields(stdout); !slices.Equal(got, want) {
				t.Errorf("stdout = %q, want %q", got, want)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			var files []string
			for _, e := range entries {
				files = append(files, e.Name())
			}
			if !slices.Equal(files, names) {
				t.Errorf("files = %q, want %q", files, names)
			}
			sizes := predicted(t, args)
			for _, n := range names {
				data, err := os.ReadFile(filepath.Join(dir, n))
				if err != nil {
					t.Fatal(err)
				}
				img, err := png.Decode(bytes.NewReader(data))
				if err != nil {
					t.Fatalf("%s: %v", n, err)
				}
				got := [2]int{img.Bounds().Dx(), img.Bounds().Dy()}
				if got != sizes[n] {
					t.Errorf("%s is %v, the layout predicts %v", n, got, sizes[n])
				}
				if n == "prism-scenario-heavy.png" {
					widths[len(args)] = got[0]
				}
			}
		})
	}
	if widths[2] <= widths[1] {
		t.Errorf("8 columns give width %d, not more than %d for 4", widths[2], widths[1])
	}
}

func TestRunErrors(t *testing.T) {
	logs := t.TempDir()
	good := writeLog(t, logs, "good.log", "20300101-000001", "EEVDF", "sched_a")
	bad := filepath.Join(logs, "bad.log")
	if err := os.WriteFile(bad, []byte("not a prism log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		args   []string
		code   int
		stderr string
	}{
		{"no log", nil, 2, "prism-parser: no log given"},
		{"unknown flag", []string{"-x", good}, 2, "flag provided but not defined"},
		{"removed output flag", []string{"-o", "out", good}, 2, "flag provided but not defined"},
		{"bad log", []string{good, bad}, 1, "prism-parser: "},
		{"missing log", []string{filepath.Join(logs, "nope.log")}, 1, "prism-parser: "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			code, stdout, stderr := runCmd(c.args...)
			if code != c.code || !strings.Contains(stderr, c.stderr) {
				t.Errorf("exit %d, stderr %q; want %d and %q", code, stderr, c.code, c.stderr)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing", stdout)
			}
			if dirs := chartDirs(t); len(dirs) != 0 {
				t.Errorf("a failed run left %v", dirs)
			}
		})
	}
}

func TestRunVersionAndHelp(t *testing.T) {
	t.Chdir(t.TempDir())
	if code, stdout, _ := runCmd("-version"); code != 0 || stdout != "prism-parser dev\n" {
		t.Errorf("-version: exit %d, stdout %q", code, stdout)
	}
	if code, _, stderr := runCmd("-h"); code != 0 || !strings.Contains(stderr, "usage: prism-parser [-version] LOG [LOG...]") {
		t.Errorf("-h: exit %d, stderr %q", code, stderr)
	}
	if dirs := chartDirs(t); len(dirs) != 0 {
		t.Errorf("-version and -h left %v", dirs)
	}
}
