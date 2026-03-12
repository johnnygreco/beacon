// Chart.js initialization with dark theme for Beacon dashboard.

const darkTheme = {
  color: '#9ca3af',          // gray-400
  borderColor: '#374151',    // gray-700
  backgroundColor: '#1f2937' // gray-800
};

Chart.defaults.color = darkTheme.color;
Chart.defaults.borderColor = darkTheme.borderColor;

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

const yAxisOptions = {
  grid: { color: '#374151' },
  ticks: { color: '#9ca3af', callback: formatTokenTick },
  beginAtZero: true
};

const seriesColors = [
  { border: '#3b82f6', bg: 'rgba(59, 130, 246, 0.15)' },   // blue - input
  { border: '#f59e0b', bg: 'rgba(245, 158, 11, 0.15)' },   // amber - output
  { border: '#10b981', bg: 'rgba(16, 185, 129, 0.15)' },    // green - cache read
  { border: '#8b5cf6', bg: 'rgba(139, 92, 246, 0.15)' },    // purple
  { border: '#ef4444', bg: 'rgba(239, 68, 68, 0.15)' },     // red
  { border: '#06b6d4', bg: 'rgba(6, 182, 212, 0.15)' },     // cyan
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
      chart.update();
    }
  } catch(e) {}
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
      ctx.fillStyle = g.provider === 'Codex' ? '#34d399' : '#fb923c';
      ctx.fillText(g.provider, (x0 + x1) / 2, y);
    });

    ctx.restore();
  }
};

Chart.register(providerGroupPlugin);

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
  return new Chart(el, {
    type: 'bar',
    data: { labels: modelData.labels, datasets: datasets },
    options: {
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

// Dashboard: Total Tokens Over Time (single-curve line chart)
var dashboardTotalEl = document.getElementById('dashboardTotalTokensChart');
if (dashboardTotalEl) {
  window.dashboardTotalTokensChart = createMultiSeriesChart(
    dashboardTotalEl, ['Total Tokens'], 'Tokens'
  );
  // Hide legend — title already describes the single series
  window.dashboardTotalTokensChart.options.plugins.legend.display = false;
  applyDefaultLog(window.dashboardTotalTokensChart, dashboardTotalEl);
  loadMultiSeriesFromJSON('dashboardTotalTokensChart', 'dashboard-total-tokens-data');
}

// Dashboard: Tokens by Model (grouped bar chart)
var dashboardByModelEl = document.getElementById('dashboardTokensByModelChart');
if (dashboardByModelEl) {
  var dashboardModelDataEl = document.getElementById('dashboard-tokens-by-model-data');
  if (dashboardModelDataEl) {
    try {
      window.dashboardTokensByModelChart = createTokensByModelChart(dashboardByModelEl, dashboardModelDataEl);
      if (window.dashboardTokensByModelChart) {
        applyDefaultLog(window.dashboardTokensByModelChart, dashboardByModelEl);
        window.dashboardTokensByModelChart.update();
      }
    } catch(e) {}
  }
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
        window.sessionTokensByModelChart.update();
      }
    } catch(e) {}
  }
}
