// Chart.js initialization with dark theme for Technodrome dashboard.

const darkTheme = {
  color: '#9ca3af',          // gray-400
  borderColor: '#374151',    // gray-700
  backgroundColor: '#1f2937' // gray-800
};

Chart.defaults.color = darkTheme.color;
Chart.defaults.borderColor = darkTheme.borderColor;

const timeScaleOptions = {
  type: 'time',
  time: { unit: 'minute', displayFormats: { minute: 'HH:mm' } },
  grid: { color: '#374151' },
  ticks: { color: '#9ca3af' }
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

// Dashboard token throughput chart (multi-series: input, output, cache read)
var tokensChartEl = document.getElementById('tokensChart');
if (tokensChartEl) {
  window.tokensChart = createMultiSeriesChart(
    tokensChartEl, ['Input', 'Output', 'Cache Read'], 'Tokens'
  );

  fetch('/api/tokens-per-minute')
    .then(function(r) { return r.json(); })
    .then(function(data) {
      if (!data || !Array.isArray(data)) return;
      var chart = window.tokensChart;
      data.forEach(function(d) {
        chart.data.labels.push(d.minute);
        chart.data.datasets[0].data.push(d.input_tokens);
        chart.data.datasets[1].data.push(d.output_tokens);
        chart.data.datasets[2].data.push(d.cache_read_tokens);
      });
      chart.update();
    })
    .catch(function() {});
}

// Session detail: tokens per event (multi-series)
var sessionTokensEl = document.getElementById('sessionTokensChart');
if (sessionTokensEl) {
  window.sessionTokensChart = createMultiSeriesChart(
    sessionTokensEl, ['Input', 'Output', 'Cache Read'], 'Tokens'
  );
  loadMultiSeriesFromJSON('sessionTokensChart', 'session-tokens-data');
}

// Session detail: tokens by model (grouped bar chart)
var sessionTokensByModelEl = document.getElementById('sessionTokensByModelChart');
if (sessionTokensByModelEl) {
  var modelDataEl = document.getElementById('session-tokens-by-model-data');
  if (modelDataEl) {
    try {
      var modelData = JSON.parse(modelDataEl.textContent);
      if (modelData.labels && modelData.datasets) {
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
        window.sessionTokensByModelChart = new Chart(sessionTokensByModelEl, {
          type: 'bar',
          data: { labels: modelData.labels, datasets: datasets },
          options: {
            responsive: true,
            maintainAspectRatio: false,
            scales: {
              x: categoryScaleOptions,
              y: {
                ...yAxisOptions,
                title: { display: true, text: 'Tokens', color: '#9ca3af' }
              }
            },
            plugins: {
              legend: { display: true, position: 'top', labels: { usePointStyle: true, boxWidth: 8 } },
              tooltip: { callbacks: { label: tokenTooltip } }
            }
          }
        });
      }
    } catch(e) {}
  }
}
