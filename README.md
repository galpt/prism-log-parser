# prism-log-parser

`prism-parser` turns the benchmark logs of [PANDEMONIUM](https://github.com/wllclngn/PANDEMONIUM) into PNG charts.

## Overview

PANDEMONIUM is a `sched_ext` CPU scheduler project. Its frame benchmark, `prism --dev frame` (`tests/prism-frame.py`), writes a text report to `~/.cache/pandemonium/prism-frame-<timestamp>.log`. The report has a preamble, one table per scenario (HEAVY, CONTENDED and PACED by default, and newer versions can print more), some footer notes and, when it was available, a POPULATION section with statistics from [montauk](https://github.com/wllclngn/montauk).

`prism-parser` reads these logs and draws them in the style of the figures of a paper: one image per scenario and one summary image. The parser is strict. A line it does not understand is an error, because a skipped row would put a wrong number in a chart.

## Usage

```
prism-parser [-version] LOG [LOG...]
```

Every run creates one new directory, `charts-<nanosecond timestamp>`, in the current directory, however many logs are given. The directory holds one `prism-scenario-<name>.png` per scenario and `prism-summary.png`:

```
$ prism-parser ~/.cache/pandemonium/prism-frame-20260101-120000.log
charts-1795000000000000000
charts-1795000000000000000/prism-scenario-heavy.png
charts-1795000000000000000/prism-scenario-contended.png
charts-1795000000000000000/prism-scenario-paced.png
charts-1795000000000000000/prism-summary.png
```

The program prints the directory and the files it wrote. The charts of a run share a directory so that the whole set is easy to zip and share. If anything fails, no directory is left behind: all logs are parsed before anything is created, and a failure while drawing removes the directory again.

## Combining logs

When several logs are given, they are drawn side by side in the same images. Each scheduler of each log becomes its own bar column, so the same scheduler measured in two logs stays two columns. A scheduler that appears in more than one log gets the TIMESTAMP of its log added to the label, for example `EEVDF (20260101-120000)`, and a scheduler that only one log has keeps its plain name. Passing the same log twice gives equal labels, and those get ` #1`, ` #2` and so on after them. A dashed line separates the columns of two logs.

Images grow wider with the number of columns. If the logs differ in VERSION, COMMIT, CPUS or ITERATIONS, the captions say so, since those runs are not strictly comparable.

## What the images show

A scenario image has one panel per metric. Bars show the mean, and whiskers show ±1 sample standard deviation over the iterations of the log. The number above a bar is the mean as the log printed it. An arrow after the metric name gives the better direction, and a panel uses a log scale when its values span 20x or more. A metric that a log has no value for is marked `n/a`.

The last panel, "Change vs EEVDF (%)", gives the percent change of each mean against the EEVDF column of the same log. Each log is measured against its own baseline, so bars from different logs do not share one. A caption below the panels names the source logs.

The summary image has three parts. A table lists the runs, one row per log, with its VERSION, COMMIT, TIMESTAMP, CPUS, ITERATIONS, calibration and drift.

A heatmap shows the multiple comparisons with the best (MCB) verdict of montauk for every metric of every scenario, as best, tied or not best. In the other cells it prints Cliff's delta of the winner against that column and the exact two-sided permutation p-value.

The verdicts are computed within each log, so columns of different logs are never compared with each other. A log without a POPULATION section has no verdicts.

Notes at the bottom collect the AUDIO and SKIPPED lines and the montauk messages of the logs.

## How it renders

go-echarts builds ECharts options. ECharts 5.6.0 renders them to SVG in server-side mode inside goja, a JavaScript engine written in Go. resvg, running as WebAssembly on wazero, rasterizes the SVG to PNG with an embedded Liberation Serif font. Text is measured with that same font, so the figures reserve the space their labels need.

The binary is static (`CGO_ENABLED=0`). It does not need a browser, a network connection or installed fonts, and the output is the same on every host. See `internal/render/render.go`.

## Install

Prebuilt archives are attached to the [GitHub releases](https://github.com/galpt/prism-log-parser/releases). Each archive holds the binary and the license files, and `SHA256SUMS` is published next to them. The targets are:

- Linux: amd64, arm64, armv7, armv6, 386
- Windows: amd64, arm64
- macOS: amd64, arm64
- FreeBSD: amd64, arm64

To install with Go, which needs the version given in `go.mod` (1.26.0):

```
go install github.com/galpt/prism-log-parser/cmd/prism-parser@latest
```

To build from a clone:

```
go build ./cmd/prism-parser
```

`prism-parser -version` prints the version of a release build, and `dev` for a build made this way.

## License

`prism-parser` is licensed under GPL-3.0-only (see `LICENSE`). The resvg-go binding it links is GPL-3.0, which sets the terms for the whole program.

It embeds ECharts, which is Apache-2.0 (`internal/render/assets/LICENSE-echarts` and `NOTICE-echarts`), and Liberation Serif, which is under the SIL Open Font License 1.1 (`internal/render/assets/LICENSE-liberation`).
