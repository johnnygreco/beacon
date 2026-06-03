function createMultiSeriesChart(el, seriesLabels, yTitle, xScale) {
  var datasets = seriesLabels.map(function(label, i) {
    var c = dashboardSeriesColor(i, seriesColors[i % seriesColors.length]);
    return {
      label: label,
      data: [],
      borderColor: c.border,
      backgroundColor: c.bg,
      fill: true,
      tension: 0.3,
      pointRadius: 2
    };
  });

  return new Chart(el, {
    type: 'line',
    data: { labels: [], datasets: datasets },
    options: {
      ...noChartAnimation,
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: 'index', intersect: false },
      scales: {
        x: xScale || timeScaleOptions,
        y: {
          ...yAxisOptions,
          stacked: false,
          title: { display: true, text: yTitle, color: chartTheme().title }
        }
      },
      plugins: {
        legend: { display: true, position: 'top', labels: { usePointStyle: true, boxWidth: 8 } },
        tooltip: {
          ...stableChartTooltipOptions(),
          callbacks: { label: tokenTooltip }
        }
      }
    }
  });
}

// Load multi-series chart data from embedded JSON
function loadMultiSeriesFromJSON(chartName, dataId) {
  var el = document.getElementById(dataId);
  if (!el || !window[chartName]) return;
  try {
    var d = JSON.parse(el.textContent);
    if (d.labels && d.labels.length > 0 && d.datasets) {
      var chart = window[chartName];
      chart.data.labels = d.labels;
      d.datasets.forEach(function(ds, i) {
        if (chart.data.datasets[i]) {
          chart.data.datasets[i].data = ds.values || [];
        }
      });
      chart.update('none');
    }
  } catch(e) {}
}

function readJSONScript(id) {
  var el = document.getElementById(id);
  if (!el) return null;
  try {
    return JSON.parse(el.textContent || '{}');
  } catch (e) {
    return null;
  }
}

function dashboardTimeScale(payload) {
  var unit = payload && payload.time_unit ? payload.time_unit : 'hour';
  var theme = chartTheme();
  return {
    type: 'time',
    time: {
      unit: unit,
      displayFormats: {
        minute: 'h:mm a',
        hour: 'MMM d h a',
        day: 'MMM d'
      }
    },
    grid: { color: theme.borderColor },
    ticks: { color: theme.color, maxTicksLimit: 7, autoSkip: true, maxRotation: 0 }
  };
}

function modelDatasetColor(index) {
  return dashboardSeriesColor(index, modelLineColors[index % modelLineColors.length]);
}

var dashboardModelMetricConfigs = {
  tokens: { label: 'Total Tokens', unit: 'tokens', title: 'Tokens', fill: true, borderWidth: 2.5 },
  total_tokens: { label: 'Total Tokens', unit: 'tokens', title: 'Tokens', fill: true, borderWidth: 2.5 },
  input_tokens: { label: 'Input Tokens', unit: 'tokens', title: 'Input Tokens', fill: false, borderWidth: 2 },
  output_tokens: { label: 'Output Tokens', unit: 'tokens', title: 'Output Tokens', fill: false, borderWidth: 2 },
  cache_read_tokens: { label: 'Cache Read Tokens', unit: 'tokens', title: 'Cache Read Tokens', fill: false, borderWidth: 2 },
  tool_calls: { label: 'Tool Calls', unit: 'tool calls', title: 'Tool Calls', fill: false, borderWidth: 2 },
  errors: { label: 'Errors', unit: 'errors', title: 'Errors', fill: false, borderWidth: 2 },
  error_rate: { label: 'Error Rate', unit: '%', title: 'Error Rate', fill: false, borderWidth: 2 }
};

function dashboardModelMetricConfig(metricKind, payload) {
  var key = metricKind === 'tokens' ? 'total_tokens' : (metricKind || 'total_tokens');
  if (!dashboardModelMetricConfigs[key]) key = 'total_tokens';
  var base = dashboardModelMetricConfigs[key] || dashboardModelMetricConfigs.total_tokens;
  return {
    key: key,
    label: payload && payload.label ? payload.label : base.label,
    unit: payload && payload.unit ? payload.unit : base.unit,
    title: payload && payload.label && key !== 'total_tokens' ? payload.label : base.title,
    fill: !!base.fill,
    borderWidth: base.borderWidth || 2
  };
}

function dashboardModelMetricUnitLabel(unit, value) {
  if (!unit || unit === '%') return '';
  var numeric = Number(value || 0);
  if (Math.abs(numeric) === 1 && unit.charAt(unit.length - 1) === 's') {
    return unit.slice(0, -1);
  }
  return unit;
}

function setDashboardModelChartTitle(canvas, metricKind, payload) {
  var metric = dashboardModelMetricConfig(metricKind, payload);
  var title = metric.label + ' by Model Over Time';
  var shell = canvas && canvas.closest ? canvas.closest('.dashboard-compact-chart') : null;
  var heading = shell ? shell.querySelector('.dashboard-compact-chart-title') : null;
  if (heading) heading.textContent = title;
  if (canvas) canvas.setAttribute('aria-label', title + ' chart');
}

function modelDatasetFromPayload(ds, index, metricKind) {
  var metric = dashboardModelMetricConfig(metricKind);
  var c = modelDatasetColor(index);
  return {
    label: ds.label,
    data: ds.values || ds.data || [],
    borderColor: c.border,
    backgroundColor: c.bg,
    borderWidth: metric.borderWidth,
    tension: 0.32,
    pointRadius: 0,
    pointHoverRadius: 5,
    hitRadius: 10,
    fill: metric.fill,
    provider: ds.provider || '',
    providerLabel: ds.provider_label || providerDisplayName(ds.provider),
    model: ds.model || ds.label,
    totalTokens: ds.total_tokens || 0,
    inputTokens: ds.input_tokens || 0,
    outputTokens: ds.output_tokens || 0,
    cacheReadTokens: ds.cache_read_tokens || 0,
    toolCallCount: ds.tool_call_count || 0,
    errorCount: ds.error_count || 0,
    callCount: ds.call_count || 0
  };
}

function dashboardModelTooltipLabel(ctx) {
  var value = ctx.parsed.y || 0;
  var unit = ctx.chart.$dashboardMetricUnit || '';
  if (unit === '%') {
    return ctx.dataset.label + ': ' + value.toFixed(value >= 10 ? 1 : 2) + '%';
  }
  unit = dashboardModelMetricUnitLabel(unit, value);
  return ctx.dataset.label + ': ' + formatCompactNumber(value) + (unit ? ' ' + unit : '');
}

function dashboardModelTooltipFooter(items) {
  if (!items || !items.length) return [];
  var ds = items[0].dataset || {};
  var footer = [];
  if (ds.providerLabel) footer.push(ds.providerLabel);
  if (ds.errorCount) footer.push(formatCompactNumber(ds.errorCount) + ' errors in range');
  if (ds.toolCallCount) footer.push(formatCompactNumber(ds.toolCallCount) + ' tool calls in range');
  return footer;
}

function createDashboardModelChart(el, payload, metricKind) {
  payload = payload || {};
  var metric = dashboardModelMetricConfig(metricKind, payload);
  setDashboardModelChartTitle(el, metric.key, payload);
  var datasets = (payload.datasets || []).map(function(ds, i) {
    return modelDatasetFromPayload(ds, i, metric.key);
  });
  var chart = new Chart(el, {
    type: 'line',
    data: { labels: payload.labels || [], datasets: datasets },
    options: {
      ...noChartAnimation,
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: 'nearest', axis: 'xy', intersect: false },
      scales: {
        x: dashboardTimeScale(payload),
        y: {
          ...yAxisOptions,
          ticks: { ...yAxisOptions.ticks, callback: metric.unit === '%' ? formatRateTick : formatTokenTick },
          title: { display: true, text: metric.title, color: chartTheme().title }
        }
      },
      plugins: {
        legend: { display: false },
        tooltip: {
          ...stableChartTooltipOptions(),
          callbacks: {
            label: dashboardModelTooltipLabel,
            footer: dashboardModelTooltipFooter
          }
        }
      }
    }
  });
  chart.$dashboardMetricUnit = metric.unit;
  chart.$dashboardMetricKind = metric.key;
  return chart;
}

function updateDashboardModelChart(chartName, payload, metricKind) {
  var canvas = document.getElementById(chartName);
  if (!canvas || !payload) return;
  var metric = dashboardModelMetricConfig(metricKind, payload);
  if (!window[chartName] || !window[chartName].data) {
    window[chartName] = createDashboardModelChart(canvas, payload, metric.key);
    applyDefaultLog(window[chartName], canvas);
  } else {
    var chart = window[chartName];
    chart.data.labels = payload.labels || [];
    chart.data.datasets = (payload.datasets || []).map(function(ds, i) {
      return modelDatasetFromPayload(ds, i, metric.key || chart.$dashboardMetricKind || 'total_tokens');
    });
    chart.options.scales.x = dashboardTimeScale(payload);
    chart.options.scales.y.ticks.callback = metric.unit === '%' ? formatRateTick : formatTokenTick;
    chart.options.scales.y.title.text = metric.title;
    chart.$dashboardMetricUnit = metric.unit;
    chart.$dashboardMetricKind = metric.key;
    setDashboardModelChartTitle(canvas, metric.key, payload);
    applyStoredSeriesVisibility(chartName);
    chart.update('none');
  }
  setupSeriesModelFilters(chartName, payload);
}
