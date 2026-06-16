function toggleLogScale(chartId) {
  var chart = window[chartId];
  if (!chart) return;
  var btn = document.getElementById(chartId + '-log-toggle');
  if (!btn) return;
  var isLog = btn.getAttribute('data-log-active') === 'true';
  var newType = isLog ? 'linear' : 'logarithmic';
  chart.options.scales.y.type = newType;
  if (newType === 'logarithmic') {
    chart.options.scales.y.min = 1;
  } else {
    delete chart.options.scales.y.min;
  }
  chart.update('none');
  btn.setAttribute('data-log-active', String(!isLog));
  btn.setAttribute('aria-pressed', String(!isLog));
  if (!isLog) {
    btn.classList.add('bg-blue-500/20', 'text-blue-400', 'border-blue-500/40');
    btn.classList.remove('text-gray-400', 'border-gray-600');
  } else {
    btn.classList.remove('bg-blue-500/20', 'text-blue-400', 'border-blue-500/40');
    btn.classList.add('text-gray-400', 'border-gray-600');
  }
}

window.toggleLogScale = toggleLogScale;

document.addEventListener('click', function(evt) {
  var btn = evt.target.closest && evt.target.closest('.log-scale-toggle[data-chart-id]');
  if (!btn) return;
  evt.preventDefault();
  toggleLogScale(btn.getAttribute('data-chart-id') || '');
});

var dashboardCumulativeEl = document.getElementById('dashboardTokenCumulativeChart');
if (dashboardCumulativeEl) {
  var cumulativePayload = readJSONScript('dashboard-token-cumulative-data') || {};
  window.dashboardTokenCumulativeChart = createDashboardModelChart(dashboardCumulativeEl, cumulativePayload, 'total_tokens');
  applyDefaultLog(window.dashboardTokenCumulativeChart, dashboardCumulativeEl);
  window.dashboardTokenCumulativeChart.update('none');
  setupSeriesModelFilters('dashboardTokenCumulativeChart', cumulativePayload);
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
