# prism-log-parser

`prism-parser` turns the benchmark logs of [PANDEMONIUM](https://github.com/wllclngn/PANDEMONIUM) into PNG charts.

PANDEMONIUM is a `sched_ext` CPU scheduler project. Its frame benchmark, `prism --dev frame`, writes a text report to `~/.cache/pandemonium/prism-frame-<timestamp>.log`. The report has a preamble, one table per scenario, some footer notes and, when [montauk](https://github.com/wllclngn/montauk) was available, a POPULATION section with its statistics.

The parser draws one image per metric of every scenario and a few summary images, in the style of the figures of a paper. It is strict. A line it does not understand is an error, since a skipped row would put a wrong number in a chart.

## Install

### Release binaries

Prebuilt archives are attached to the [GitHub releases](https://github.com/galpt/prism-log-parser/releases). Each archive holds the binary, the README and the license files, and `SHA256SUMS` is published next to them.

| System | Architectures | Archive |
|---|---|---|
| Linux | amd64, arm64, armv7, armv6, 386 | `.tar.gz` |
| Windows | amd64, arm64 | `.zip` |
| macOS | amd64, arm64 | `.tar.gz` |
| FreeBSD | amd64, arm64 | `.tar.gz` |

### Go

This needs Go 1.26.0, the version in `go.mod`.

```sh
go install github.com/galpt/prism-log-parser/cmd/prism-parser@latest
```

### From source

```sh
go build ./cmd/prism-parser
```

`prism-parser -version` prints the version of a release build, and `dev` for a build made this way.

## Usage

```sh
prism-parser [-version] LOG [LOG...]
```

Every run creates one new directory, `charts-<nanosecond timestamp>`, in the current directory, however many logs are given. The program prints the directory and then every file it wrote.

### Output

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

| Path | Shows |
|---|---|
| `scenario-<name>/NN-<metric>.png` | One metric, in the order of the log's table |
| `scenario-<name>/NN-change-vs-eevdf.png` | Changes against EEVDF, last, if the scenario has them |
| `summary/runs.png` | The runs of all logs, with notes |
| `summary/verdicts-<name>.png` | The montauk verdicts of one scenario |

The number in front of each file name keeps the table order when the files are sorted, and it is zero padded to the width of the image count. The rest of the name is the metric label in lowercase: `%` becomes `pct`, and any other run of characters that are not ASCII letters or digits becomes a single `-`, so `0.1% LOW FPS (p99.9)` becomes `0-1pct-low-fps-p99-9`. In a directory name the scenario is lowercased, and characters other than letters, digits, `-` and `_` become `-`.

### Errors

All logs are parsed before anything is created, so a failure leaves no directory behind.

| Situation | What happens |
|---|---|
| No log is given, or a flag is unknown | The usage text is printed, with exit status 2 |
| A log line is not understood, or a verdict names an unknown scheduler | The error names the file, with exit status 1 |
| Two scenario names give the same directory name | Nothing is written, with exit status 1 |
| An image cannot be drawn | The directory is removed again, with exit status 1 |

### Combining logs

When several logs are given, they are drawn side by side in the same images, in the order given. Each scheduler of each log becomes its own bar column, so the same scheduler measured in two logs stays two columns. A dashed line separates the columns of two logs, and a column has the same color and pattern in every image.

The labels of the columns are made by these rules, which apply in this order:

| Case | Added to the name |
|---|---|
| The scheduler is in one log only | Nothing |
| The scheduler is in several logs | ` (TIMESTAMP)` of its log, as in `EEVDF (20260101-120000)` |
| Two columns still share a label, as when one log is given twice | ` #k`, with k the position of the log |
| A name looks like another column's label | ` #k`, then `-2`, `-3` and so on until free |

If the logs differ in VERSION, COMMIT, CPUS or ITERATIONS, the captions say so, since those runs are not strictly comparable.

## What the images show

Each image stands on its own, with a title, a legend of the columns, one plot and a short caption. The PNG files have twice the pixels of the layout on each side, so text stays sharp when zoomed in and on high density screens.

### Metric images

A metric image has one bar per column. Bars show the mean, and whiskers show ±1 sample standard deviation over the iterations of the log. The number above a bar is the mean as the log printed it. The title names the scenario and the metric, with an arrow for the better direction, and says "log scale" when all means are positive and span 20x or more. A metric that a log has no value for is marked `n/a`. The caption names the source logs, and says so when a whisker was cut off.

### Change vs EEVDF

The last image of a scenario, "Change vs EEVDF (%)", has one group of bars per metric that has a `vs EEVDF` row. Each bar is the percent change of a mean against the EEVDF column of the same log, as the log printed it. Each log is measured against its own baseline, so bars from different logs do not share one.

### Run table

`summary/runs.png` has one row per log, with its VERSION, COMMIT, TIMESTAMP, CPUS, ITERATIONS, calibration and drift. Notes below it collect the AUDIO and SKIPPED lines, the montauk messages and the fields that differ between logs.

### Verdict heatmaps

`summary/verdicts-<scenario>.png` has one row per metric and one column per column of the combined logs. The color is the verdict of montauk's multiple comparisons with the best (MCB), computed within each log over its iterations, so columns of different logs are never compared with each other.

| Cell | Color | Meaning |
|---|---|---|
| `best` | Dark blue | The winner of its group, called best by montauk |
| `top mean` | By its verdict | The winner, when montauk does not call it best |
| `d=` and `p=` | Medium blue if tied, pale blue if not best | Cliff's delta and exact two-sided permutation p-value of the winner against this column |
| A dash | Gray | The log has no verdict for this cell |

A log without a POPULATION section has no verdicts, and a scenario without any has no heatmap. prism-frame writes the section when ITERATIONS is at least 2 and montauk is installed.

## How it renders

go-echarts builds ECharts options. ECharts 5.6.0 renders them to SVG inside goja, a JavaScript engine written in Go, and resvg, running as WebAssembly on wazero, rasterizes the SVG to PNG at twice its size. Text is measured with the embedded Liberation Serif font that draws it, so the figures reserve the space their labels need.

The binary is static (`CGO_ENABLED=0`). It needs no browser, network connection or installed fonts, so the output is the same on every host.

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
