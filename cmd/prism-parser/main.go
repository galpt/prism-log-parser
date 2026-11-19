// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

// Command prism-parser turns PANDEMONIUM PRISM-FRAME logs into PNG charts.
//
// It writes one image per scenario and one summary image into a new
// charts-<timestamp> directory under the current one. Several logs are drawn
// side by side in the same images.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/galpt/prism-log-parser/internal/compare"
	"github.com/galpt/prism-log-parser/internal/figure"
	"github.com/galpt/prism-log-parser/internal/render"
	"github.com/galpt/prism-log-parser/internal/report"
)

// version is set by the release build with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the whole command. It returns the exit status: 0 on success, 1 when
// a log or an image fails and 2 for bad usage.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("prism-parser", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print the version and exit")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: prism-parser [-version] LOG [LOG...]")
		fmt.Fprintln(stderr, "Writes the charts into a new charts-<timestamp> directory under the current one.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, "prism-parser", version)
		return 0
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(stderr, "prism-parser: no log given")
		fs.Usage()
		return 2
	}
	if err := generate(fs.Args(), stdout); err != nil {
		fmt.Fprintln(stderr, "prism-parser:", err)
		return 1
	}
	return 0
}

// generate parses and compares every log before it creates anything, so a
// bad log leaves no directory behind. Once the directory exists, any failure
// removes it again, and a run leaves either a complete set of charts or none.
func generate(paths []string, stdout io.Writer) error {
	reports := make([]*report.Report, len(paths))
	for i, p := range paths {
		r, err := report.ParseFile(p)
		if err != nil {
			return err
		}
		// The figures and the difference lines name each log by its source,
		// and the directory it was read from says nothing to a reader.
		r.Source = filepath.Base(p)
		reports[i] = r
	}
	set, err := compare.New(reports)
	if err != nil {
		return err
	}
	rd, err := render.New()
	if err != nil {
		return err
	}
	defer rd.Close()
	figs, err := figure.Build(reports, set, rd.TextWidth)
	if err != nil {
		return err
	}

	// A nanosecond timestamp keeps two runs from sharing a directory, and
	// Mkdir rather than MkdirAll makes a collision an error instead of a
	// silent mix of two runs.
	dir := fmt.Sprintf("charts-%d", time.Now().UnixNano())
	if err := os.Mkdir(dir, 0o755); err != nil {
		return err
	}
	written, err := writeFigures(rd, figs, dir)
	if err != nil {
		return errors.Join(err, os.RemoveAll(dir))
	}
	fmt.Fprintln(stdout, dir)
	for _, p := range written {
		fmt.Fprintln(stdout, p)
	}
	return nil
}

// writeFigures renders every figure into dir and returns the paths written.
func writeFigures(rd *render.Renderer, figs []figure.Figure, dir string) ([]string, error) {
	var written []string
	for _, f := range figs {
		png, err := rd.Render(f.Option, f.Width, f.Height)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
		path := filepath.Join(dir, f.Name)
		if err := os.WriteFile(path, png, 0o644); err != nil {
			return nil, err
		}
		written = append(written, path)
	}
	return written, nil
}
