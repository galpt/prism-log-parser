// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

// Command prism-parser turns PANDEMONIUM PRISM-FRAME logs into PNG charts.
//
// It writes a new charts-<timestamp> directory under the current one, with a
// directory of images for every scenario, one image per metric, and a
// summary directory. Several logs are drawn side by side in the same images.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
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

// renderWorkers bounds the number of Renderers that draw at once. A run has
// dozens of images and each Renderer is single threaded, so three of them
// in parallel cut the wall time of two logs by more than half. Each one holds
// its own JavaScript runtime and wasm memory, about 110 MB at the image sizes
// here, so a fourth adds more memory than the time it saves is worth.
const renderWorkers = 3

// writeFigures renders every figure into dir and returns the paths written,
// in the order of figs. The directories are created first, in order, so that
// the workers only ever write files. rd is used as one of the workers, and
// the others are created here and closed again before it returns.
func writeFigures(rd *render.Renderer, figs []figure.Figure, dir string) ([]string, error) {
	paths := make([]string, len(figs))
	made := map[string]bool{}
	for i, f := range figs {
		paths[i] = filepath.Join(dir, filepath.FromSlash(f.Path))
		if sub := filepath.Dir(paths[i]); !made[sub] {
			if err := os.Mkdir(sub, 0o755); err != nil {
				return nil, err
			}
			made[sub] = true
		}
	}

	workers, err := extraRenderers(min(renderWorkers, runtime.GOMAXPROCS(0), len(figs)) - 1)
	if err != nil {
		return nil, err
	}
	defer func() {
		for _, w := range workers {
			_ = w.Close()
		}
	}()
	workers = append(workers, rd)

	jobs := make(chan int)
	errs := make([]error, len(figs))
	var failed atomic.Bool
	var wg sync.WaitGroup
	for _, w := range workers {
		wg.Go(func() {
			for i := range jobs {
				if errs[i] = writeFigure(w, figs[i], paths[i]); errs[i] != nil {
					failed.Store(true)
				}
			}
		})
	}
	// After the first failure the run is lost anyway, so no new image is
	// started.
	for i := range figs {
		if failed.Load() {
			break
		}
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return paths, nil
}

// extraRenderers creates n Renderers at the same time. The wasm module was
// compiled by the first Renderer, so what is left for each is loading
// ECharts and instantiating the module, which run well in parallel.
func extraRenderers(n int) ([]*render.Renderer, error) {
	rds := make([]*render.Renderer, max(n, 0))
	errs := make([]error, len(rds))
	var wg sync.WaitGroup
	for i := range rds {
		wg.Go(func() { rds[i], errs[i] = render.New() })
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		for _, rd := range rds {
			if rd != nil {
				_ = rd.Close()
			}
		}
		return nil, err
	}
	return rds, nil
}

func writeFigure(rd *render.Renderer, f figure.Figure, path string) error {
	png, err := rd.Render(f.Option, f.Width, f.Height)
	if err != nil {
		return fmt.Errorf("%s: %w", f.Path, err)
	}
	return os.WriteFile(path, png, 0o644)
}
