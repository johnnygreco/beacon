// SSE-to-Chart.js bridge for real-time chart updates.
// Hooks into HTMX SSE extension's events to avoid a duplicate EventSource.

(function() {
  var sseContainer = document.querySelector('[sse-connect]');
  if (!sseContainer) return;

  // Show the SSE status bar only on pages that use SSE
  var statusBar = document.getElementById('sse-status-bar');
  if (statusBar) statusBar.classList.remove('hidden');

  var indicator = document.getElementById('sse-indicator');
  var statusEl = document.getElementById('sse-status');

  function setConnected(connected) {
    if (indicator) {
      indicator.className = connected
        ? 'w-2 h-2 rounded-full bg-green-500 sse-glow'
        : 'w-2 h-2 rounded-full bg-red-500';
    }
    if (statusEl) {
      statusEl.textContent = connected ? 'Connected' : 'Disconnected';
    }
  }

  function handleMultiSeriesEvent(chart, payload) {
    if (!chart) return;
    if (payload.labels && Array.isArray(payload.labels) && payload.datasets) {
      chart.data.labels = payload.labels;
      payload.datasets.forEach(function(ds, i) {
        if (chart.data.datasets[i]) {
          chart.data.datasets[i].data = ds.Values || ds.values || [];
        }
      });
      chart.update('none');
    }
  }

  // htmx-ext-sse v2.x fires htmx:sseBeforeMessage for every incoming SSE event
  // on the element with sse-connect. We intercept non-HTML events here.
  sseContainer.addEventListener('htmx:sseBeforeMessage', function(evt) {
    var type = evt.detail && evt.detail.type;
    var data = evt.detail && evt.detail.message && evt.detail.message.data;

    if (type === 'token-data' && data) {
      try {
        handleMultiSeriesEvent(window.tokensChart, JSON.parse(data));
      } catch(e) {}
      evt.preventDefault();
      return;
    }

    if (type === 'connected') {
      setConnected(true);
      evt.preventDefault();
      return;
    }
  });

  // Track SSE connection open/error via HTMX lifecycle events
  document.body.addEventListener('htmx:sseOpen', function() {
    setConnected(true);
  });
  document.body.addEventListener('htmx:sseError', function() {
    setConnected(false);
  });
  document.body.addEventListener('htmx:sseClose', function() {
    setConnected(false);
  });
})();
