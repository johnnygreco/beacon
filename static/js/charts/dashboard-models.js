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
          chart.data.datasets[i].data = ds.Values || ds.values || ds.data || [];
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

function modelDatasetFromPayload(ds, index, metricKind) {
  var c = modelDatasetColor(index);
  return {
    label: ds.label,
    data: ds.values || ds.Values || [],
    borderColor: c.border,
    backgroundColor: c.bg,
    borderWidth: metricKind === 'tokens' ? 2.5 : 2,
    tension: 0.32,
    pointRadius: 0,
    pointHoverRadius: 5,
    hitRadius: 10,
    fill: metricKind === 'tokens',
    provider: ds.provider || '',
    providerLabel: ds.provider_label || providerDisplayName(ds.provider),
    model: ds.model || ds.label,
    totalTokens: ds.total_tokens || 0,
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
  if (unit === 'tokens') {
    return ctx.dataset.label + ': ' + formatCompactNumber(value) + ' tokens';
  }
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
  var datasets = (payload.datasets || []).map(function(ds, i) {
    return modelDatasetFromPayload(ds, i, metricKind || 'tokens');
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
          title: { display: true, text: metricKind === 'tokens' ? 'Cumulative Tokens' : '', color: chartTheme().title }
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
  chart.$dashboardMetricUnit = metricKind === 'tokens' ? 'tokens' : '';
  chart.$dashboardMetricKind = metricKind || 'tokens';
  return chart;
}

function updateDashboardModelChart(chartName, payload, metricKind) {
  var canvas = document.getElementById(chartName);
  if (!canvas || !payload) return;
  if (!window[chartName] || !window[chartName].data) {
    window[chartName] = createDashboardModelChart(canvas, payload, metricKind);
    applyDefaultLog(window[chartName], canvas);
  } else {
    var chart = window[chartName];
    chart.data.labels = payload.labels || [];
    chart.data.datasets = (payload.datasets || []).map(function(ds, i) {
      return modelDatasetFromPayload(ds, i, metricKind || chart.$dashboardMetricKind || 'tokens');
    });
    chart.options.scales.x = dashboardTimeScale(payload);
    chart.options.scales.y.ticks.callback = metricKind === 'error_rate' ? formatRateTick : formatTokenTick;
    chart.options.scales.y.title.text = metricKind === 'tokens' ? 'Cumulative Tokens' : '';
    chart.$dashboardMetricUnit = metricKind === 'tokens' ? 'tokens' : '';
    chart.$dashboardMetricKind = metricKind || 'tokens';
    applyStoredSeriesVisibility(chartName);
    chart.update('none');
  }
  setupSeriesModelFilters(chartName, payload);
}

function dashboardActivityMetricPayload(payload, metricKey) {
  payload = payload || {};
  var metrics = payload.metrics || {};
  var metric = metrics[metricKey] || metrics.error_rate || { label: '', unit: '', datasets: [] };
  return {
    labels: payload.labels || [],
    datasets: metric.datasets || [],
    metric: metric,
    summary: payload.summary || {},
    time_unit: payload.time_unit,
    bucket_minutes: payload.bucket_minutes
  };
}

function updateDashboardModelActivityChart(payload, metricKey) {
  var selected = dashboardActivityMetricPayload(payload, metricKey || 'error_rate');
  var canvas = document.getElementById('dashboardModelActivityChart');
  if (!canvas) return;
  if (!window.dashboardModelActivityChart || !window.dashboardModelActivityChart.data) {
    window.dashboardModelActivityChart = createDashboardModelChart(canvas, selected, metricKey || 'error_rate');
  }
  var chart = window.dashboardModelActivityChart;
  chart.data.labels = selected.labels || [];
  chart.data.datasets = (selected.datasets || []).map(function(ds, i) {
    return modelDatasetFromPayload(ds, i, metricKey || 'error_rate');
  });
  chart.options.scales.x = dashboardTimeScale(selected);
  chart.options.scales.y.type = 'linear';
  delete chart.options.scales.y.min;
  chart.options.scales.y.title.text = selected.metric.label || '';
  chart.options.scales.y.ticks.callback = selected.metric.unit === '%' ? formatRateTick : formatTokenTick;
  chart.$dashboardMetricUnit = selected.metric.unit || '';
  chart.$dashboardMetricKind = metricKey || 'error_rate';
  applyStoredSeriesVisibility('dashboardModelActivityChart');
  chart.update('none');
  setupSeriesModelFilters('dashboardModelActivityChart', selected);
}
