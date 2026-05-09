// Chart.js initialization with dark theme for Beacon dashboard.

const darkTheme = {
  color: '#9ca3af',          // gray-400
  borderColor: '#374151',    // gray-700
  backgroundColor: '#1f2937' // gray-800
};

Chart.defaults.color = darkTheme.color;
Chart.defaults.borderColor = darkTheme.borderColor;
Chart.defaults.animation = { duration: 0 };
Chart.defaults.animations = {
  colors: { duration: 0 },
  x: { duration: 0 },
  y: { duration: 0 }
};
Chart.defaults.transitions = {
  active: { animation: { duration: 0 } },
  resize: { animation: { duration: 0 } },
  show: { animations: { x: { duration: 0 }, y: { duration: 0 } } },
  hide: { animations: { x: { duration: 0 }, y: { duration: 0 } } }
};

if (Chart.defaults.global) {
  Chart.defaults.global.responsiveAnimationDuration = 0;
  if (Chart.defaults.global.animation && typeof Chart.defaults.global.animation === 'object') {
    Chart.defaults.global.animation.duration = 0;
  } else {
    Chart.defaults.global.animation = { duration: 0 };
  }
}

const noChartAnimation = {
  animation: { duration: 0 },
  responsiveAnimationDuration: 0,
  animations: {
    colors: { duration: 0 },
    x: { duration: 0 },
    y: { duration: 0 }
  },
  transitions: {
    active: { animation: { duration: 0 } },
    resize: { animation: { duration: 0 } },
    show: { animations: { x: { duration: 0 }, y: { duration: 0 } } },
    hide: { animations: { x: { duration: 0 }, y: { duration: 0 } } }
  }
};

const timeScaleOptions = {
  type: 'time',
  time: { unit: 'minute', displayFormats: { minute: 'h:mm a' } },
  grid: { color: '#374151' },
  ticks: { color: '#9ca3af', maxTicksLimit: 6, autoSkip: true, maxRotation: 0 }
};

const categoryScaleOptions = {
  grid: { color: '#374151' },
  ticks: { color: '#9ca3af' }
};

function formatTokenTick(value) {
  if (value >= 1000000) return (value / 1000000).toFixed(1).replace(/\.0$/, '') + 'M';
  if (value >= 1000) return (value / 1000).toFixed(0) + 'K';
  return value;
}

function formatCompactNumber(value) {
  value = Number(value || 0);
  if (value >= 1000000) return (value / 1000000).toFixed(1).replace(/\.0$/, '') + 'M';
  if (value >= 1000) return (value / 1000).toFixed(1).replace(/\.0$/, '') + 'K';
  return value.toLocaleString();
}

function formatRateTick(value) {
  value = Number(value || 0);
  if (value >= 10) return value.toFixed(0) + '%';
  if (value > 0) return value.toFixed(1) + '%';
  return '0%';
}

function providerDisplayName(provider) {
  if (provider === 'anthropic') return 'Claude Code';
  if (provider === 'openai') return 'Codex';
  if (provider === 'unknown') return 'Unknown';
  return provider || '';
}

const yAxisOptions = {
  grid: { color: '#374151' },
  ticks: { color: '#9ca3af', callback: formatTokenTick },
  beginAtZero: true
};

// Provider identity colors — single source of truth for all chart-level
// provider styling (labels, borders, legend items).
const providerColors = {
  'Claude Code': { border: '#fb923c', bg: 'rgba(251, 146, 60, 0.15)' },  // orange
  'Codex':       { border: '#22d3ee', bg: 'rgba(34, 211, 238, 0.15)' },  // cyan
};

const seriesColors = [
  { border: '#3b82f6', bg: 'rgba(59, 130, 246, 0.15)' },   // blue - input
  { border: '#a78bfa', bg: 'rgba(167, 139, 250, 0.15)' },   // violet - output
  { border: '#10b981', bg: 'rgba(16, 185, 129, 0.15)' },    // green - cache read
  { border: '#f59e0b', bg: 'rgba(245, 158, 11, 0.15)' },    // amber
  { border: '#ef4444', bg: 'rgba(239, 68, 68, 0.15)' },     // red
  { border: '#06b6d4', bg: 'rgba(6, 182, 212, 0.15)' },     // cyan
];

const modelLineColors = [
  { border: '#22d3ee', bg: 'rgba(34, 211, 238, 0.10)' },
  { border: '#fb923c', bg: 'rgba(251, 146, 60, 0.10)' },
  { border: '#34d399', bg: 'rgba(52, 211, 153, 0.10)' },
  { border: '#60a5fa', bg: 'rgba(96, 165, 250, 0.10)' },
  { border: '#f43f5e', bg: 'rgba(244, 63, 94, 0.10)' },
  { border: '#facc15', bg: 'rgba(250, 204, 21, 0.10)' },
  { border: '#a3e635', bg: 'rgba(163, 230, 53, 0.10)' },
  { border: '#c084fc', bg: 'rgba(192, 132, 252, 0.10)' },
  { border: '#2dd4bf', bg: 'rgba(45, 212, 191, 0.10)' },
  { border: '#f87171', bg: 'rgba(248, 113, 113, 0.10)' },
  { border: '#818cf8', bg: 'rgba(129, 140, 248, 0.10)' },
  { border: '#e5e7eb', bg: 'rgba(229, 231, 235, 0.08)' },
];

function tokenTooltip(ctx) {
  return ctx.dataset.label + ': ' + ctx.parsed.y.toLocaleString() + ' tokens';
}

function tokensByModelTooltipFooter(items) {
  if (!items || !items.length) return [];
  return [
    'Input = fresh input only; cache hits are shown under Cache.',
    'Cache = cache read tokens; provider-specific cache creation is excluded.'
  ];
}

// Create a stacked area chart with multiple token series
function createMultiSeriesChart(el, seriesLabels, yTitle, xScale) {
  var datasets = seriesLabels.map(function(label, i) {
    var c = seriesColors[i % seriesColors.length];
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
          title: { display: true, text: yTitle, color: '#9ca3af' }
        }
      },
      plugins: {
        legend: { display: true, position: 'top', labels: { usePointStyle: true, boxWidth: 8 } },
        tooltip: { callbacks: { label: tokenTooltip } }
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
    grid: { color: 'rgba(55, 65, 81, 0.65)' },
    ticks: { color: '#9ca3af', maxTicksLimit: 7, autoSkip: true, maxRotation: 0 }
  };
}

function modelDatasetColor(index) {
  return modelLineColors[index % modelLineColors.length];
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
    pointHoverRadius: 3,
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
      interaction: { mode: 'nearest', axis: 'x', intersect: false },
      scales: {
        x: dashboardTimeScale(payload),
        y: {
          ...yAxisOptions,
          title: { display: true, text: metricKind === 'tokens' ? 'Cumulative Tokens' : '', color: '#9ca3af' }
        }
      },
      plugins: {
        legend: { display: false },
        tooltip: {
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

function setupSeriesModelFilters(chartName, payload) {
  var chart = window[chartName];
  if (!chart || !payload) return;
  var labels = (payload.datasets || []).map(function(ds) { return ds.label; });

  var signature = JSON.stringify(labels.map(function(label, i) {
    var ds = payload.datasets[i] || {};
    return [label, ds.provider || '', ds.model || ''];
  }));
  var existing = document.getElementById(chartName + '-model-dropdown');
  if (existing && chart._seriesFilterSignature === signature) {
    syncSeriesDropdownState(chartName);
    applyStoredSeriesVisibility(chartName);
    chart.update('none');
    return;
  }
  if (labels.length <= 1) {
    if (existing) existing.remove();
    chart._seriesLabels = labels;
    chart._activeModels = new Set(labels);
    applyStoredSeriesVisibility(chartName);
    chart.update('none');
    return;
  }
  if (existing) {
    if (chart._seriesFilterCloseHandler) {
      document.removeEventListener('click', chart._seriesFilterCloseHandler);
    }
    existing.remove();
  }

  var storageKey = 'beacon-model-hidden-' + chartName;
  var savedHidden = [];
  try { savedHidden = JSON.parse(localStorage.getItem(storageKey)) || []; } catch(e) {}
  var validLabels = new Set(labels);
  var previousLabels = new Set(chart._seriesLabels || []);
  savedHidden = savedHidden.filter(function(label) { return validLabels.has(label); });
  chart._seriesLabels = labels;
  chart._seriesFilterSignature = signature;
  if (chart._activeModels) {
    chart._activeModels = new Set(Array.from(chart._activeModels).filter(function(label) {
      return validLabels.has(label);
    }));
    labels.forEach(function(label) {
      if (!previousLabels.has(label) && savedHidden.indexOf(label) === -1) {
        chart._activeModels.add(label);
      }
    });
  } else {
    chart._activeModels = new Set(labels.filter(function(label) {
      return savedHidden.indexOf(label) === -1;
    }));
  }
  if (chart._activeModels.size === 0 && savedHidden.length === 0) {
    chart._activeModels = new Set(labels);
  }

  var headerDiv = chart.canvas.parentElement.parentElement.querySelector('.flex.items-center');
  if (!headerDiv) return;
  var wrapper = document.createElement('div');
  wrapper.className = 'model-dropdown relative ml-auto';
  wrapper.id = chartName + '-model-dropdown';

  var trigger = document.createElement('button');
  trigger.type = 'button';
  trigger.className = 'model-dropdown-trigger';
  trigger.setAttribute('aria-haspopup', 'true');
  trigger.setAttribute('aria-expanded', 'false');
  var triggerLabel = document.createElement('span');
  triggerLabel.className = 'model-dropdown-label';
  trigger.appendChild(triggerLabel);
  var chevronSvg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  chevronSvg.setAttribute('class', 'model-dropdown-chevron');
  chevronSvg.setAttribute('width', '10');
  chevronSvg.setAttribute('height', '10');
  chevronSvg.setAttribute('viewBox', '0 0 10 10');
  var chevronPath = document.createElementNS('http://www.w3.org/2000/svg', 'path');
  chevronPath.setAttribute('d', 'M2 4l3 3 3-3');
  chevronPath.setAttribute('stroke', 'currentColor');
  chevronPath.setAttribute('stroke-width', '1.5');
  chevronPath.setAttribute('fill', 'none');
  chevronSvg.appendChild(chevronPath);
  trigger.appendChild(chevronSvg);
  wrapper.appendChild(trigger);

  var panel = document.createElement('div');
  panel.className = 'model-dropdown-panel hidden';
  var allRow = document.createElement('label');
  allRow.className = 'model-dropdown-item model-dropdown-all';
  var allCb = document.createElement('input');
  allCb.type = 'checkbox';
  allRow.appendChild(allCb);
  var allDot = document.createElement('span');
  allDot.className = 'model-dropdown-dot';
  allDot.style.background = '#60a5fa';
  allRow.appendChild(allDot);
  var allLabel = document.createElement('span');
  allLabel.textContent = 'All Models';
  allRow.appendChild(allLabel);
  panel.appendChild(allRow);
  var divider = document.createElement('div');
  divider.className = 'model-dropdown-divider';
  panel.appendChild(divider);

  var lastProvider = '';
  (payload.datasets || []).forEach(function(ds, i) {
    var provider = ds.provider_label || providerDisplayName(ds.provider);
    if (provider && provider !== lastProvider) {
      var groupHeader = document.createElement('div');
      groupHeader.className = 'model-dropdown-group';
      groupHeader.textContent = provider;
      panel.appendChild(groupHeader);
      lastProvider = provider;
    }
    var row = document.createElement('label');
    row.className = 'model-dropdown-item';
    var cb = document.createElement('input');
    cb.type = 'checkbox';
    cb.dataset.model = ds.label;
    row.appendChild(cb);
    var dot = document.createElement('span');
    dot.className = 'model-dropdown-dot';
    dot.style.background = modelDatasetColor(i).border;
    row.appendChild(dot);
    var nameSpan = document.createElement('span');
    nameSpan.textContent = ds.label;
    row.appendChild(nameSpan);
    panel.appendChild(row);
  });
  wrapper.appendChild(panel);

  var logBtn = headerDiv.querySelector('.log-scale-toggle');
  if (logBtn) {
    headerDiv.insertBefore(wrapper, logBtn);
  } else {
    headerDiv.appendChild(wrapper);
  }

  trigger.onclick = function(e) {
    e.stopPropagation();
    panel.classList.toggle('hidden');
    trigger.classList.toggle('open', !panel.classList.contains('hidden'));
    trigger.setAttribute('aria-expanded', panel.classList.contains('hidden') ? 'false' : 'true');
  };
  chart._seriesFilterCloseHandler = function(e) {
    if (!wrapper.contains(e.target)) {
      panel.classList.add('hidden');
      trigger.classList.remove('open');
      trigger.setAttribute('aria-expanded', 'false');
    }
  };
  document.addEventListener('click', chart._seriesFilterCloseHandler);

  allCb.addEventListener('change', function() {
    chart._activeModels = allCb.checked ? new Set(labels) : new Set();
    syncSeriesDropdownState(chartName);
    applyStoredSeriesVisibility(chartName);
    saveSeriesModelFilterState(chartName);
    chart.update('none');
  });
  panel.querySelectorAll('input[data-model]').forEach(function(cb) {
    cb.addEventListener('change', function() {
      if (cb.checked) chart._activeModels.add(cb.dataset.model);
      else chart._activeModels.delete(cb.dataset.model);
      syncSeriesDropdownState(chartName);
      applyStoredSeriesVisibility(chartName);
      saveSeriesModelFilterState(chartName);
      chart.update('none');
    });
  });

  syncSeriesDropdownState(chartName);
  applyStoredSeriesVisibility(chartName);
  chart.update('none');
}

function syncSeriesDropdownState(chartName) {
  var chart = window[chartName];
  var wrapper = document.getElementById(chartName + '-model-dropdown');
  if (!chart || !wrapper || !chart._seriesLabels) return;
  var total = chart._seriesLabels.length;
  var active = chart._activeModels ? chart._activeModels.size : total;
  var label = wrapper.querySelector('.model-dropdown-label');
  if (label) label.textContent = active === total ? 'All Models' : 'Models (' + active + '/' + total + ')';
  var allCb = wrapper.querySelector('.model-dropdown-all input');
  if (allCb) {
    allCb.checked = active === total;
    allCb.indeterminate = active > 0 && active < total;
  }
  wrapper.querySelectorAll('input[data-model]').forEach(function(cb) {
    cb.checked = chart._activeModels && chart._activeModels.has(cb.dataset.model);
  });
}

function applyStoredSeriesVisibility(chartName) {
  var chart = window[chartName];
  if (!chart) return;
  if (!chart._activeModels) {
    chart._activeModels = new Set((chart.data.datasets || []).map(function(ds) { return ds.label; }));
  }
  (chart.data.datasets || []).forEach(function(ds) {
    ds.hidden = !chart._activeModels.has(ds.label);
  });
}

function saveSeriesModelFilterState(chartName) {
  var chart = window[chartName];
  if (!chart || !chart._seriesLabels || !chart._activeModels) return;
  var hidden = chart._seriesLabels.filter(function(label) {
    return !chart._activeModels.has(label);
  });
  var storageKey = 'beacon-model-hidden-' + chartName;
  if (hidden.length === 0) {
    localStorage.removeItem(storageKey);
  } else {
    localStorage.setItem(storageKey, JSON.stringify(hidden));
  }
}

// Apply default log scale if the canvas has data-default-log="true".
// Does NOT call chart.update() — caller is responsible for triggering a render.
function applyDefaultLog(chart, el) {
  if (el.getAttribute('data-default-log') === 'true') {
    chart.options.scales.y.type = 'logarithmic';
    chart.options.scales.y.min = 1;
  }
}

// Plugin: draw provider group labels and dashed vertical dividers
var providerGroupPlugin = {
  id: 'providerGroupHeaders',
  afterDraw: function(chart) {
    var groups = chart.options.plugins.providerGroupHeaders &&
                 chart.options.plugins.providerGroupHeaders.groups;
    if (!groups || !groups.length) return;
    var ctx = chart.ctx;
    var xAxis = chart.scales.x;
    var chartArea = chart.chartArea;

    ctx.save();

    // Draw dashed vertical dividers between groups
    if (groups.length >= 2) {
      ctx.strokeStyle = 'rgba(156,163,175,0.4)';
      ctx.lineWidth = 1;
      ctx.setLineDash([4, 4]);
      for (var i = 0; i < groups.length - 1; i++) {
        var endTick = groups[i].end;
        var startTick = groups[i + 1].start;
        var x = (xAxis.getPixelForTick(endTick) + xAxis.getPixelForTick(startTick)) / 2;
        ctx.beginPath();
        ctx.moveTo(x, chartArea.top);
        ctx.lineTo(x, chartArea.bottom);
        ctx.stroke();
      }
      ctx.setLineDash([]);
    }

    // Draw provider labels below x-axis
    ctx.font = 'bold 11px -apple-system, BlinkMacSystemFont, sans-serif';
    ctx.textAlign = 'center';
    ctx.textBaseline = 'top';
    var y = chartArea.bottom + 38;
    groups.forEach(function(g) {
      var x0 = xAxis.getPixelForTick(g.start);
      var x1 = xAxis.getPixelForTick(g.end);
      var pc = providerColors[g.provider] || { border: '#9ca3af' };
      ctx.fillStyle = pc.border;
      ctx.fillText(g.provider, (x0 + x1) / 2, y);
    });

    ctx.restore();
  }
};

var noDataOverlayPlugin = {
  id: 'noDataOverlay',
  afterDraw: function(chart, args, opts) {
    opts = opts || {};
    if (opts.enabled === false) return;
    var hasVisibleData = (chart.data.datasets || []).some(function(ds) {
      return !ds.hidden && ds.data && ds.data.length > 0;
    });
    if (hasVisibleData) return;
    var area = chart.chartArea;
    if (!area) return;
    var ctx = chart.ctx;
    ctx.save();
    ctx.fillStyle = '#6b7280';
    ctx.font = '12px -apple-system, BlinkMacSystemFont, sans-serif';
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillText(opts.message || 'No data in selected range', (area.left + area.right) / 2, (area.top + area.bottom) / 2);
    ctx.restore();
  }
};

Chart.register(providerGroupPlugin, noDataOverlayPlugin);

// ---- Model filter system for tokens-by-model charts ----

function getModelProvider(fullData, index) {
  if (!fullData.providerGroups) return '';
  for (var i = 0; i < fullData.providerGroups.length; i++) {
    var g = fullData.providerGroups[i];
    if (index >= g.start && index <= g.end) return g.provider;
  }
  return '';
}

function modelFilterSignature(fullData) {
  return JSON.stringify({
    labels: fullData.labels || [],
    groups: fullData.providerGroups || [],
    datasets: (fullData.datasets || []).map(function(ds) { return ds.label; })
  });
}

function modelProviderMap(fullData) {
  var providerMap = {};
  (fullData.labels || []).forEach(function(label, i) {
    providerMap[label] = getModelProvider(fullData, i);
  });
  return providerMap;
}

function setupModelFilters(chartName, dataEl) {
  var chart = window[chartName];
  if (!chart || !dataEl) return;

  var fullData;
  try { fullData = JSON.parse(dataEl.textContent); } catch(e) { return; }
  if (!fullData.labels || fullData.labels.length <= 1) return;

  var signature = modelFilterSignature(fullData);
  var existing = document.getElementById(chartName + '-model-dropdown');
  if (existing && chart._modelFilterSignature === signature) {
    chart._fullModelData = fullData;
    chart._hadMultipleProviders = fullData.providerGroups && fullData.providerGroups.length > 1;
    chart._modelProviderMap = modelProviderMap(fullData);
    if (chart._activeModels) {
      var validLabels = new Set(fullData.labels);
      chart._activeModels = new Set(Array.from(chart._activeModels).filter(function(label) {
        return validLabels.has(label);
      }));
    }
    syncDropdownState(chartName);
    if (chart._activeModels && chart._activeModels.size < fullData.labels.length) {
      rebuildModelChart(chartName);
    }
    return;
  }

  // Clean up previous dropdown if re-initialized
  if (existing) {
    if (chart._modelFilterCloseHandler) {
      document.removeEventListener('click', chart._modelFilterCloseHandler);
    }
    existing.remove();
  }

  // Store full data and build provider map
  chart._fullModelData = fullData;
  chart._modelFilterSignature = signature;
  chart._hadMultipleProviders = fullData.providerGroups && fullData.providerGroups.length > 1;
  var providerMap = modelProviderMap(fullData);
  chart._modelProviderMap = providerMap;

  // Load saved hidden models from localStorage
  var storageKey = 'beacon-model-hidden-' + chartName;
  var savedHidden = [];
  try { savedHidden = JSON.parse(localStorage.getItem(storageKey)) || []; } catch(e) {}
  var validLabels = new Set(fullData.labels);
  savedHidden = savedHidden.filter(function(l) { return validLabels.has(l); });

  chart._activeModels = new Set(fullData.labels.filter(function(l) {
    return savedHidden.indexOf(l) === -1;
  }));

  // Build dropdown UI
  var canvasWrapper = chart.canvas.parentElement;
  var containerCard = canvasWrapper.parentElement;
  var headerDiv = containerCard.querySelector('.flex.items-center');

  // Insert dropdown trigger into the header bar (between title and log toggle)
  var wrapper = document.createElement('div');
  wrapper.className = 'model-dropdown relative ml-auto mr-2';
  wrapper.id = chartName + '-model-dropdown';

  var trigger = document.createElement('button');
  trigger.type = 'button';
  trigger.className = 'model-dropdown-trigger';
  var triggerLabel = document.createElement('span');
  triggerLabel.className = 'model-dropdown-label';
  trigger.appendChild(triggerLabel);
  var chevronSvg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  chevronSvg.setAttribute('class', 'model-dropdown-chevron');
  chevronSvg.setAttribute('width', '10');
  chevronSvg.setAttribute('height', '10');
  chevronSvg.setAttribute('viewBox', '0 0 10 10');
  var chevronPath = document.createElementNS('http://www.w3.org/2000/svg', 'path');
  chevronPath.setAttribute('d', 'M2 4l3 3 3-3');
  chevronPath.setAttribute('stroke', 'currentColor');
  chevronPath.setAttribute('stroke-width', '1.5');
  chevronPath.setAttribute('fill', 'none');
  chevronSvg.appendChild(chevronPath);
  trigger.appendChild(chevronSvg);
  wrapper.appendChild(trigger);

  // Dropdown panel
  var panel = document.createElement('div');
  panel.className = 'model-dropdown-panel hidden';

  // "All" checkbox row
  var allRow = document.createElement('label');
  allRow.className = 'model-dropdown-item model-dropdown-all';
  var allCb = document.createElement('input');
  allCb.type = 'checkbox';
  allCb.checked = true;
  allRow.appendChild(allCb);
  var allDot = document.createElement('span');
  allDot.className = 'model-dropdown-dot';
  allDot.style.background = '#60a5fa';
  allRow.appendChild(allDot);
  var allLabel = document.createElement('span');
  allLabel.textContent = 'All Models';
  allRow.appendChild(allLabel);
  panel.appendChild(allRow);

  var divider = document.createElement('div');
  divider.className = 'model-dropdown-divider';
  panel.appendChild(divider);

  // Group models by provider
  var lastProv = '';
  fullData.labels.forEach(function(label) {
    var prov = providerMap[label] || '';
    var pc = providerColors[prov] || { border: '#9ca3af' };
    if (prov !== lastProv && prov) {
      var groupHeader = document.createElement('div');
      groupHeader.className = 'model-dropdown-group';
      groupHeader.style.color = pc.border;
      groupHeader.textContent = prov;
      panel.appendChild(groupHeader);
      lastProv = prov;
    }
    var row = document.createElement('label');
    row.className = 'model-dropdown-item';
    var cb = document.createElement('input');
    cb.type = 'checkbox';
    cb.dataset.model = label;
    cb.checked = true;
    row.appendChild(cb);
    var dot = document.createElement('span');
    dot.className = 'model-dropdown-dot';
    dot.style.background = pc.border;
    row.appendChild(dot);
    var nameSpan = document.createElement('span');
    nameSpan.textContent = label;
    row.appendChild(nameSpan);
    panel.appendChild(row);
  });

  wrapper.appendChild(panel);

  // Insert into header — before the log toggle button if present, else at end
  var logBtn = headerDiv.querySelector('.log-scale-toggle');
  if (logBtn) {
    headerDiv.insertBefore(wrapper, logBtn);
  } else {
    headerDiv.appendChild(wrapper);
  }

  // Event: toggle dropdown
  trigger.onclick = function(e) {
    e.stopPropagation();
    panel.classList.toggle('hidden');
    trigger.classList.toggle('open', !panel.classList.contains('hidden'));
  };

  // Event: close on outside click (stored for cleanup on re-init)
  chart._modelFilterCloseHandler = function(e) {
    if (!wrapper.contains(e.target)) {
      panel.classList.add('hidden');
      trigger.classList.remove('open');
    }
  };
  document.addEventListener('click', chart._modelFilterCloseHandler);

  // Event: "All" checkbox
  var allCheckbox = allRow.querySelector('input');
  allCheckbox.addEventListener('change', function() {
    if (allCheckbox.checked) {
      chart._activeModels = new Set(fullData.labels);
    } else {
      chart._activeModels.clear();
    }
    syncDropdownState(chartName);
    rebuildModelChart(chartName);
    saveModelFilterState(chartName);
  });

  // Event: individual model checkboxes
  panel.querySelectorAll('input[data-model]').forEach(function(cb) {
    cb.addEventListener('change', function() {
      var model = cb.dataset.model;
      if (cb.checked) {
        chart._activeModels.add(model);
      } else {
        chart._activeModels.delete(model);
      }
      syncDropdownState(chartName);
      rebuildModelChart(chartName);
      saveModelFilterState(chartName);
    });
  });

  // Initial sync
  syncDropdownState(chartName);

  // If some models were previously hidden, rebuild chart
  if (savedHidden.length > 0 && chart._activeModels.size < fullData.labels.length) {
    rebuildModelChart(chartName);
  }
}

function syncDropdownState(chartName) {
  var chart = window[chartName];
  if (!chart || !chart._fullModelData) return;

  var wrapper = document.getElementById(chartName + '-model-dropdown');
  if (!wrapper) return;

  var total = chart._fullModelData.labels.length;
  var active = chart._activeModels.size;
  var allActive = active === total;

  // Update trigger label
  var label = wrapper.querySelector('.model-dropdown-label');
  if (label) {
    label.textContent = allActive ? 'All Models' : 'Models (' + active + '/' + total + ')';
  }

  // Update "All" checkbox
  var allCb = wrapper.querySelector('.model-dropdown-all input');
  if (allCb) {
    allCb.checked = allActive;
    allCb.indeterminate = !allActive && active > 0;
  }

  // Update individual checkboxes
  wrapper.querySelectorAll('input[data-model]').forEach(function(cb) {
    cb.checked = chart._activeModels.has(cb.dataset.model);
  });
}

function rebuildModelChart(chartName) {
  var chart = window[chartName];
  if (!chart || !chart._fullModelData) return;

  var fullData = chart._fullModelData;
  var active = chart._activeModels;

  var newLabels = [];
  var newDatasets = fullData.datasets.map(function(ds) {
    return { label: ds.label, data: [] };
  });
  var newGroups = [];
  var lastProvider = '';
  var groupStart = 0;

  fullData.labels.forEach(function(label, i) {
    if (!active.has(label)) return;

    var provider = chart._modelProviderMap[label] || '';

    if (lastProvider && provider !== lastProvider) {
      newGroups.push({
        provider: lastProvider,
        start: groupStart,
        end: newLabels.length - 1
      });
      groupStart = newLabels.length;
    }
    lastProvider = provider;

    newLabels.push(label);
    fullData.datasets.forEach(function(ds, j) {
      newDatasets[j].data.push(ds.data[i]);
    });
  });

  // Close last group
  if (lastProvider && newLabels.length > 0) {
    newGroups.push({
      provider: lastProvider,
      start: groupStart,
      end: newLabels.length - 1
    });
  }

  // Update chart data
  chart.data.labels = newLabels;
  chart.data.datasets.forEach(function(ds, i) {
    ds.data = newDatasets[i].data;
  });

  // Update provider group headers — keep single-provider group visible
  // if the original data had multiple providers (so user sees context)
  var pgh = chart.options.plugins.providerGroupHeaders;
  if (pgh) {
    pgh.groups = (newGroups.length > 1 || (newGroups.length === 1 && chart._hadMultipleProviders)) ? newGroups : [];
  }

  chart.update('none');
}

function saveModelFilterState(chartName) {
  var chart = window[chartName];
  if (!chart || !chart._fullModelData) return;

  var storageKey = 'beacon-model-hidden-' + chartName;
  var hidden = chart._fullModelData.labels.filter(function(l) {
    return !chart._activeModels.has(l);
  });

  if (hidden.length === 0) {
    localStorage.removeItem(storageKey);
  } else {
    localStorage.setItem(storageKey, JSON.stringify(hidden));
  }
}

// Create a grouped bar chart for tokens by model
function createTokensByModelChart(el, dataEl) {
  var modelData = JSON.parse(dataEl.textContent);
  if (!modelData.labels || !modelData.datasets) return null;
  var datasets = modelData.datasets.map(function(ds, i) {
    var c = seriesColors[i % seriesColors.length];
    return {
      label: ds.label,
      data: ds.data,
      backgroundColor: c.bg,
      borderColor: c.border,
      borderWidth: 1
    };
  });
  var hasGroups = modelData.providerGroups && modelData.providerGroups.length > 0;
  var xOpts = Object.assign({}, categoryScaleOptions);
  if (modelData.labels.length > 4) {
    xOpts.ticks = Object.assign({}, xOpts.ticks || {}, {
      maxRotation: 45, minRotation: 0, font: { size: 10 }
    });
  }
  return new Chart(el, {
    type: 'bar',
    data: { labels: modelData.labels, datasets: datasets },
    options: {
      ...noChartAnimation,
      responsive: true,
      maintainAspectRatio: false,
      layout: hasGroups ? { padding: { bottom: 25 } } : {},
      scales: {
        x: xOpts,
        y: {
          ...yAxisOptions,
          title: { display: true, text: 'Tokens', color: '#9ca3af' }
        }
      },
      plugins: {
        legend: { display: true, position: 'top', labels: { usePointStyle: true, boxWidth: 8 } },
        tooltip: {
          callbacks: {
            label: tokenTooltip,
            footer: tokensByModelTooltipFooter
          }
        },
        providerGroupHeaders: { groups: modelData.providerGroups || [] }
      }
    }
  });
}

// Dashboard: cumulative tokens by selected model.
var dashboardCumulativeEl = document.getElementById('dashboardTokenCumulativeChart');
if (dashboardCumulativeEl) {
  var cumulativePayload = readJSONScript('dashboard-token-cumulative-data') || {};
  window.dashboardTokenCumulativeChart = createDashboardModelChart(dashboardCumulativeEl, cumulativePayload, 'tokens');
  applyDefaultLog(window.dashboardTokenCumulativeChart, dashboardCumulativeEl);
  window.dashboardTokenCumulativeChart.update('none');
  setupSeriesModelFilters('dashboardTokenCumulativeChart', cumulativePayload);
}

// Dashboard: selectable model activity/health metric.
var dashboardActivityEl = document.getElementById('dashboardModelActivityChart');
if (dashboardActivityEl) {
  var activityPayload = readJSONScript('dashboard-model-activity-data') || {};
  updateDashboardModelActivityChart(activityPayload, 'error_rate');
}

// Session detail: Total Tokens Over Time (single-curve, matching dashboard)
var sessionTokensEl = document.getElementById('sessionTokensChart');
if (sessionTokensEl) {
  window.sessionTokensChart = createMultiSeriesChart(
    sessionTokensEl, ['Total Tokens'], 'Tokens'
  );
  window.sessionTokensChart.options.plugins.legend.display = false;
  applyDefaultLog(window.sessionTokensChart, sessionTokensEl);
  loadMultiSeriesFromJSON('sessionTokensChart', 'session-tokens-data');
}

// Session detail: tokens by model (grouped bar chart)
var sessionTokensByModelEl = document.getElementById('sessionTokensByModelChart');
if (sessionTokensByModelEl) {
  var sessionModelDataEl = document.getElementById('session-tokens-by-model-data');
  if (sessionModelDataEl) {
    try {
      window.sessionTokensByModelChart = createTokensByModelChart(sessionTokensByModelEl, sessionModelDataEl);
      if (window.sessionTokensByModelChart) {
        applyDefaultLog(window.sessionTokensByModelChart, sessionTokensByModelEl);
        window.sessionTokensByModelChart.update('none');
      }
    } catch(e) {}
    setupModelFilters('sessionTokensByModelChart', sessionModelDataEl);
  }
}
