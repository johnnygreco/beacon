// SSE-to-Chart.js bridge for real-time chart updates.
// Uses native EventSource API to receive SSE events directly,
// bypassing HTMX's sse-swap mechanism which only handles HTML swaps.

(function() {
  // Find the SSE connect URL from the HTMX container on the page.
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

  es.onopen = function() {
    setConnected(true);
  };

  es.onerror = function() {
    setConnected(false);
  };

  // Handle chart data events.
  // Full-series payload: { labels: [...], values: [...] } → replace all data.
  // Single-point payload: { timestamp, value } → append.
  function handleChartEvent(chart, payload) {
    if (!chart) return;
    if (payload.labels && Array.isArray(payload.labels)) {
      // Full series — replace
      chart.data.labels = payload.labels;
      chart.data.datasets[0].data = payload.values;
    } else if (payload.timestamp !== undefined) {
      // Single point — append
      chart.data.labels.push(payload.timestamp);
      chart.data.datasets[0].data.push(payload.value);
      if (chart.data.labels.length > 60) {
        chart.data.labels.shift();
        chart.data.datasets[0].data.shift();
      }
    }
    chart.update('none');
  }

  es.addEventListener('token-data', function(evt) {
    try {
      handleChartEvent(window.tokensChart, JSON.parse(evt.data));
    } catch(e) {}
  });

  es.addEventListener('cost-data', function(evt) {
    try {
      handleChartEvent(window.costChart, JSON.parse(evt.data));
    } catch(e) {}
  });

  es.addEventListener('context-data', function(evt) {
    try {
      handleChartEvent(window.sessionContextChart, JSON.parse(evt.data));
    } catch(e) {}
  });

  // Clean up EventSource on page unload to prevent connection leaks
  window.addEventListener('beforeunload', function() { es.close(); });
})();
