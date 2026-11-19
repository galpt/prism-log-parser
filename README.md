# prism-log-parser

`prism-parser` turns the benchmark logs of [PANDEMONIUM](https://github.com/wllclngn/PANDEMONIUM) into PNG charts.

PANDEMONIUM is a `sched_ext` CPU scheduler project. Its frame benchmark, `prism --dev frame` (`tests/prism-frame.py`), writes a text report to `~/.cache/pandemonium/prism-frame-<timestamp>.log`. The report has a preamble, one table per scenario (HEAVY, CONTENDED and PACED by default, and newer versions can print more), some footer notes and, when it was available, a POPULATION section with statistics from [montauk](https://github.com/wllclngn/montauk).

`prism-parser` reads these logs and draws them in the style of the figures of a paper: one image per metric of every scenario, and a few summary images. The parser is strict. A line it does not understand is an error, because a skipped row would put a wrong number in a chart.

## Install

### Release binaries

Prebuilt archives are attached to the [GitHub releases](https://github.com/galpt/prism-log-parser/releases). Each archive holds the binary and the license files, and `SHA256SUMS` is published next to them. The targets are:

- Linux: amd64, arm64, armv7, armv6, 386
- Windows: amd64, arm64
- macOS: amd64, arm64
- FreeBSD: amd64, arm64

### Go

This needs the Go version given in `go.mod` (1.26.0):

```sh
go install github.com/galpt/prism-log-parser/cmd/prism-parser@latest
```

### From source

To build from a clone:

```sh
go build ./cmd/prism-parser
```

`prism-parser -version` prints the version of a release build, and `dev` for a build made this way.

## Usage

```sh
prism-parser [-version] LOG [LOG...]
```

### Output

Every run creates one new directory, `charts-<nanosecond timestamp>`, in the current directory, however many logs are given. It holds one directory per scenario and a `summary` directory:

```
charts-1795000000000000000/
  scenario-heavy/
    01-avg-fps.png
    02-1pct-low-fps-p99.png
    ...
    14-change-vs-eevdf.png
  scenario-contended/
  scenario-paced/
  summary/
    runs.png
    verdicts-heavy.png
    verdicts-contended.png
    verdicts-paced.png
```

A scenario directory has one image per metric, in the order of the table in the log, and the image of the changes against EEVDF last, when the scenario has them. The number in front of each file name keeps that order when the files are sorted, and it is zero padded to the width of the image count. The rest of the name is the metric label in lowercase: `%` becomes `pct`, and any other run of characters that are not ASCII letters or digits becomes a single `-`, so `0.1% LOW FPS (p99.9)` becomes `0-1pct-low-fps-p99-9`. The scenario name is lowercased in the directory name, and characters other than letters, digits, `-` and `_` become `-`.

The program prints the directory and then every file it wrote:

```
$ prism-parser ~/.cache/pandemonium/prism-frame-20260101-120000.log
charts-1795000000000000000
charts-1795000000000000000/scenario-heavy/01-avg-fps.png
charts-1795000000000000000/scenario-heavy/02-1pct-low-fps-p99.png
...
charts-1795000000000000000/summary/runs.png
charts-1795000000000000000/summary/verdicts-heavy.png
...
```

The charts of a run share a directory so that the whole set is easy to zip and share. If anything fails, no directory is left behind: all logs are parsed before anything is created, and a failure while drawing removes the directory again.

### Combining logs

When several logs are given, they are drawn side by side in the same images. Each scheduler of each log becomes its own bar column, so the same scheduler measured in two logs stays two columns. A scheduler that appears in more than one log gets the TIMESTAMP of its log added to the label, for example `EEVDF (20260101-120000)`, and a scheduler that only one log has keeps its plain name. Passing the same log twice gives equal labels, and those get ` #1`, ` #2` and so on after them. Scheduler names may contain spaces, so a name can look like another column's label, for example `EEVDF (20260101-120000)`. A label that is still taken after that gets ` #k` with the position of its log, and then `-2`, `-3` and so on after the number. A dashed line separates the columns of two logs.

Images grow wider with the number of columns. A column has the same color and pattern in every image. If the logs differ in VERSION, COMMIT, CPUS or ITERATIONS, the captions say so, since those runs are not strictly comparable.

## What the images show

Each image stands on its own, with a title, a legend of the columns, one plot and a short caption, and is meant to be read at its own size. The PNG files have twice the pixels of the layout on each side, so text stays sharp when zoomed in and on high density screens.

### Metric images

A metric image has one bar per column. Bars show the mean, and whiskers show ±1 sample standard deviation over the iterations of the log. The number above a bar is the mean as the log printed it. The title names the scenario and the metric, with an arrow for the better direction, and says "log scale" when the values span 20x or more and the plot uses one. A metric that a log has no value for is marked `n/a`. The caption names the source logs, and says so when a whisker was cut off at zero.

### Change vs EEVDF

The last image of a scenario, "Change vs EEVDF (%)", gives the percent change of each mean against the EEVDF column of the same log, one group of bars per metric. Each log is measured against its own baseline, so bars from different logs do not share one.

### Run table

`summary/runs.png` has a table of the runs, one row per log, with its VERSION, COMMIT, TIMESTAMP, CPUS, ITERATIONS, calibration and drift. Notes below it collect the AUDIO and SKIPPED lines and the montauk messages of the logs.

### Verdict heatmaps

`summary/verdicts-<scenario>.png` is a heatmap of the multiple comparisons with the best (MCB) verdicts of montauk for every metric of that scenario, as best, tied or not best. In the other cells it prints Cliff's delta of the winner against that column and the exact two-sided permutation p-value. The verdicts are computed within each log, so columns of different logs are never compared with each other. A log without a POPULATION section has no verdicts, and a scenario without any has no heatmap.

## How it renders

go-echarts builds ECharts options. ECharts 5.6.0 renders them to SVG in server-side mode inside goja, a JavaScript engine written in Go. resvg, running as WebAssembly on wazero, rasterizes the SVG to PNG at twice its size with an embedded Liberation Serif font. Text is measured with that same font, so the figures reserve the space their labels need.

The binary is static (`CGO_ENABLED=0`). It does not need a browser, a network connection or installed fonts, and the output is the same on every host. See `internal/render/render.go`.

## Project layout

```
cmd/prism-parser   the command
internal/report    parser of one log
internal/compare   columns, labels and verdicts of several logs
internal/figure    ECharts options and layout of every image
internal/render    ECharts in goja and resvg in wazero
```

## License

`prism-parser` is licensed under GPL-3.0-only (see `LICENSE`). The resvg-go binding it links is GPL-3.0, which sets the terms for the whole program.

It embeds ECharts, which is Apache-2.0 (`internal/render/assets/LICENSE-echarts` and `NOTICE-echarts`), and Liberation Serif, which is under the SIL Open Font License 1.1 (`internal/render/assets/LICENSE-liberation`).
