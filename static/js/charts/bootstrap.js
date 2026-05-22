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
