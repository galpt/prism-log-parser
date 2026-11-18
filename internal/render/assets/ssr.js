// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

// Platform hooks and the named renderItem registry for server-side rendering.
//
// ECharts expects a browser. This file supplies the few globals it touches
// while it renders to an SVG string inside goja, which has none of them.

// SSR renders synchronously, so a timer callback never has to fire. ECharts
// only schedules them for animation frames and deferred work that SSR skips.
// This runs before ECharts loads because some of its modules read the globals
// at load time.
var timerId = 0;
globalThis.setTimeout = function () { return ++timerId; };
globalThis.setInterval = globalThis.setTimeout;
globalThis.clearTimeout = function () {};
globalThis.clearInterval = function () {};

// A few ECharts code paths call console.warn without checking that it exists.
// Library callers have no use for the output, so it is dropped.
globalThis.console = {
  log: function () {},
  info: function () {},
  warn: function () {},
  error: function () {}
};

// Text is measured by Go with the embedded font, so layout matches what resvg
// draws later. ECharts falls back to a canvas for this, which does not exist
// here.
function installPlatform(measure) {
  echarts.setPlatformAPI({
    measureText: function (text, font) {
      return { width: measure(text, font) };
    }
  });
}

// JSON cannot carry functions, so a custom series names its renderItem by
// string and the name is swapped for a function before setOption. prepare()
// fills in the series fields that a named renderer needs to behave sensibly
// and that the caller has not set.
const renderers = {
  // Each data item is [x, low, high], where x is a category index (or a value
  // on a value axis). It draws a vertical line from low to high at the center
  // of the category band, with a horizontal cap at each end. The caps are at
  // most 8px wide each side and shrink to 15% of the band so that they stay
  // inside a narrow category.
  errorBar: {
    prepare: function (series) {
      // Above bars, which sit at the default z of 2.
      if (series.z === undefined) series.z = 10;
      // Without this the y axis would be fitted to the bars alone and could
      // clip the top of a whisker.
      if (series.encode === undefined) series.encode = { x: 0, y: [1, 2] };
    },
    renderItem: function (params, api) {
      const x = api.value(0);
      const high = api.coord([x, api.value(2)]);
      const low = api.coord([x, api.value(1)]);
      const center = high[0];
      const cap = Math.min(8, api.size([1, 0])[0] * 0.15);

      // itemStyle gives borderColor and borderWidth, which api.style() maps to
      // stroke and lineWidth. Color falls back to the fill so that setting
      // itemStyle.color alone also works, and the width to a visible line
      // because the default border width is 0.
      const base = api.style();
      const style = {
        stroke: base.stroke || base.fill,
        lineWidth: base.lineWidth || 1.5,
        fill: null
      };
      function line(x1, y1, x2, y2) {
        return { type: 'line', shape: { x1: x1, y1: y1, x2: x2, y2: y2 }, style: style };
      }
      return {
        type: 'group',
        children: [
          line(center, high[1], center, low[1]),
          line(center - cap, high[1], center + cap, high[1]),
          line(center - cap, low[1], center + cap, low[1])
        ]
      };
    }
  }
};

function resolveRenderItems(option) {
  const list = option.series === undefined ? [] : [].concat(option.series);
  list.forEach(function (series) {
    if (!series || series.type !== 'custom' || typeof series.renderItem !== 'string') return;
    const name = series.renderItem;
    if (!Object.prototype.hasOwnProperty.call(renderers, name)) {
      throw new Error('unknown renderItem "' + name + '"');
    }
    renderers[name].prepare(series);
    series.renderItem = renderers[name].renderItem;
  });
}

// Each call builds a chart and disposes of it, so nothing but the registry
// survives between renders.
function renderSVG(optionJSON, width, height) {
  const option = JSON.parse(optionJSON);
  if (option === null || typeof option !== 'object' || Array.isArray(option)) {
    throw new Error('option must be a JSON object');
  }
  resolveRenderItems(option);
  const chart = echarts.init(null, null, { renderer: 'svg', ssr: true, width: width, height: height });
  try {
    chart.setOption(option);
    return chart.renderToSVGString();
  } finally {
    chart.dispose();
  }
}
