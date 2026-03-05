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

const yAxisOptions = {
  grid: { color: '#374151' },
  ticks: { color: '#9ca3af' },
  beginAtZero: true
};

// Shared tooltip formatters
function tokenTooltip(ctx) {
  return ctx.parsed.y.toLocaleString() + ' tokens';
}

function costTooltip(ctx) {
  return '$' + ctx.parsed.y.toFixed(4);
}

// Factory for creating line charts with consistent config
function createLineChart(el, label, borderColor, bgColor, yTitle, tooltipFn, xScale) {
  return new Chart(el, {
    type: 'line',
    data: {
      labels: [],
      datasets: [{
        label: label,
        data: [],
        borderColor: borderColor,
        backgroundColor: bgColor,
        fill: true,
        tension: 0.3,
        pointRadius: 2
      }]
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      scales: {
        x: xScale || timeScaleOptions,
        y: {
          ...yAxisOptions,
          title: { display: true, text: yTitle, color: '#9ca3af' }
        }
      },
      plugins: {
        legend: { display: false },
        tooltip: { callbacks: { label: tooltipFn } }
      }
    }
  });
}

// Load chart data from an embedded JSON <script> element
function loadChartFromJSON(chartName, dataId) {
  var el = document.getElementById(dataId);
  if (!el || !window[chartName]) return;
  try {
    var d = JSON.parse(el.textContent);
    if (d.labels && d.labels.length > 0) {
      window[chartName].data.labels = d.labels;
      window[chartName].data.datasets[0].data = d.values;
      window[chartName].update();
    }
  } catch(e) {}
}

// Dashboard charts
const tokensChartEl = document.getElementById('tokensChart');
if (tokensChartEl) {
  window.tokensChart = createLineChart(
    tokensChartEl, 'Tokens/min', '#3b82f6', 'rgba(59, 130, 246, 0.1)',
    'Tokens', tokenTooltip
  );

  fetch('/api/tokens-per-minute')
    .then(r => r.json())
    .then(data => {
      if (!data || !Array.isArray(data)) return;
      var chart = window.tokensChart;
      data.forEach(d => {
        chart.data.labels.push(d.minute);
        chart.data.datasets[0].data.push(d.total_tokens);
      });
      chart.update();
    })
    .catch(() => {});
}

const costChartEl = document.getElementById('costChart');
if (costChartEl) {
  window.costChart = createLineChart(
    costChartEl, 'Cumulative Cost ($)', '#10b981', 'rgba(16, 185, 129, 0.1)',
    'Cost (USD)', costTooltip
  );
  // Override y-axis ticks to show $ prefix
  window.costChart.options.scales.y.ticks = {
    color: '#9ca3af',
    callback: function(value) { return '$' + value.toFixed(2); }
  };

  fetch('/api/hourly-costs')
    .then(r => r.json())
    .then(data => {
      if (!data || !Array.isArray(data)) return;
      var chart = window.costChart;
      var cumCost = 0;
      data.forEach(d => {
        cumCost += d.total_cost;
        chart.data.labels.push(d.hour);
        chart.data.datasets[0].data.push(cumCost);
      });
      chart.update();
    })
    .catch(() => {});
}

// Session detail charts
const sessionTokensEl = document.getElementById('sessionTokensChart');
if (sessionTokensEl) {
  window.sessionTokensChart = createLineChart(
    sessionTokensEl, 'Tokens', '#3b82f6', 'rgba(59, 130, 246, 0.1)',
    'Tokens', tokenTooltip
  );
}

const sessionContextEl = document.getElementById('sessionContextChart');
if (sessionContextEl) {
  window.sessionContextChart = createLineChart(
    sessionContextEl, 'Context Usage', '#f59e0b', 'rgba(245, 158, 11, 0.1)',
    'Tokens in Context', tokenTooltip
  );
}

// Session cost-per-turn bar chart
const sessionCostPerTurnEl = document.getElementById('sessionCostPerTurnChart');
if (sessionCostPerTurnEl) {
  window.sessionCostPerTurnChart = new Chart(sessionCostPerTurnEl, {
    type: 'bar',
    data: {
      labels: [],
      datasets: [{
        label: 'Cost',
        data: [],
        backgroundColor: 'rgba(16, 185, 129, 0.6)',
        borderColor: '#10b981',
        borderWidth: 1
      }]
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      scales: {
        x: categoryScaleOptions,
        y: {
          ...yAxisOptions,
          title: { display: true, text: 'Cost (USD)', color: '#9ca3af' },
          ticks: {
            color: '#9ca3af',
            callback: function(value) { return '$' + value.toFixed(4); }
          }
        }
      },
      plugins: {
        legend: { display: false },
        tooltip: { callbacks: { label: costTooltip } }
      }
    }
  });
}

// Load session chart data from embedded JSON blocks
loadChartFromJSON('sessionTokensChart', 'session-tokens-data');
loadChartFromJSON('sessionContextChart', 'session-context-data');
loadChartFromJSON('sessionCostPerTurnChart', 'session-cost-per-turn-data');
