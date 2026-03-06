// SSE-to-Chart.js bridge for real-time chart updates.

(function() {
  var sseContainer = document.querySelector('[sse-connect]');
  if (!sseContainer) return;

  var url = sseContainer.getAttribute('sse-connect');
  if (!url) return;

  var indicator = document.getElementById('sse-indicator');
  var statusEl = document.getElementById('sse-status');

  function setConnected(connected) {
    if (indicator) {
      indicator.className = connected
        ? 'w-2 h-2 rounded-full bg-green-500'
        : 'w-2 h-2 rounded-full bg-red-500';
    }
    if (statusEl) {
      statusEl.textContent = connected ? 'Connected' : 'Disconnected';
    }
  }

  var es = new EventSource(url);

  es.onopen = function() { setConnected(true); };
  es.onerror = function() { setConnected(false); };

  // Handle multi-series chart data.
  // Payload: { labels: [...], datasets: [{ Label, Values }, ...] }
  function handleMultiSeriesEvent(chart, payload) {
    if (!chart) return;
    if (payload.labels && Array.isArray(payload.labels) && payload.datasets) {
      chart.data.labels = payload.labels;
      payload.datasets.forEach(function(ds, i) {
        if (chart.data.datasets[i]) {
          chart.data.datasets[i].data = ds.Values || ds.values || [];
        }
      });
    }
    chart.update('none');
  }

  es.addEventListener('token-data', function(evt) {
    try {
      handleMultiSeriesEvent(window.tokensChart, JSON.parse(evt.data));
    } catch(e) {}
  });

  window.addEventListener('beforeunload', function() { es.close(); });
})();
