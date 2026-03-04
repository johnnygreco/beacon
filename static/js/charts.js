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

// Dashboard charts
const tokensChartEl = document.getElementById('tokensChart');
if (tokensChartEl) {
  window.tokensChart = new Chart(tokensChartEl, {
    type: 'line',
    data: {
      labels: [],
      datasets: [{
        label: 'Tokens/min',
        data: [],
        borderColor: '#3b82f6',
        backgroundColor: 'rgba(59, 130, 246, 0.1)',
        fill: true,
        tension: 0.3,
        pointRadius: 2
      }]
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      scales: { x: categoryScaleOptions, y: yAxisOptions },
      plugins: { legend: { display: false } }
    }
  });

  // Fetch initial tokens-per-minute data
  fetch('/api/tokens-per-minute')
    .then(r => r.json())
    .then(data => {
      if (!data || !Array.isArray(data)) return;
      const chart = window.tokensChart;
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
  window.costChart = new Chart(costChartEl, {
    type: 'line',
    data: {
      labels: [],
      datasets: [{
        label: 'Cumulative Cost ($)',
        data: [],
        borderColor: '#10b981',
        backgroundColor: 'rgba(16, 185, 129, 0.1)',
        fill: true,
        tension: 0.3,
        pointRadius: 2
      }]
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      scales: { x: categoryScaleOptions, y: yAxisOptions },
      plugins: { legend: { display: false } }
    }
  });

  // Fetch initial hourly cost data
  fetch('/api/hourly-costs')
    .then(r => r.json())
    .then(data => {
      if (!data || !Array.isArray(data)) return;
      const chart = window.costChart;
      let cumCost = 0;
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
  window.sessionTokensChart = new Chart(sessionTokensEl, {
    type: 'line',
    data: {
      labels: [],
      datasets: [{
        label: 'Tokens',
        data: [],
        borderColor: '#3b82f6',
        backgroundColor: 'rgba(59, 130, 246, 0.1)',
        fill: true,
        tension: 0.3,
        pointRadius: 2
      }]
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      scales: { x: categoryScaleOptions, y: yAxisOptions },
      plugins: { legend: { display: false } }
    }
  });
}

const sessionContextEl = document.getElementById('sessionContextChart');
if (sessionContextEl) {
  window.sessionContextChart = new Chart(sessionContextEl, {
    type: 'line',
    data: {
      labels: [],
      datasets: [{
        label: 'Context Usage',
        data: [],
        borderColor: '#f59e0b',
        backgroundColor: 'rgba(245, 158, 11, 0.1)',
        fill: true,
        tension: 0.3,
        pointRadius: 2
      }]
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      scales: { x: categoryScaleOptions, y: yAxisOptions },
      plugins: { legend: { display: false } }
    }
  });
}
