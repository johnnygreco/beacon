// SSE-to-Chart.js bridge for real-time chart updates.
// Listens for SSE events and pushes data into Chart.js instances.

document.body.addEventListener('sse:token-data', function(evt) {
  evt.preventDefault();
  if (!window.tokensChart) return;
  const payload = JSON.parse(evt.detail.data);
  const chart = window.tokensChart;
  chart.data.labels.push(payload.timestamp);
  chart.data.datasets[0].data.push(payload.value);
  if (chart.data.labels.length > 60) {
    chart.data.labels.shift();
    chart.data.datasets[0].data.shift();
  }
  chart.update('none');
});

document.body.addEventListener('sse:cost-data', function(evt) {
  evt.preventDefault();
  if (!window.costChart) return;
  const payload = JSON.parse(evt.detail.data);
  const chart = window.costChart;
  chart.data.labels.push(payload.timestamp);
  chart.data.datasets[0].data.push(payload.value);
  if (chart.data.labels.length > 60) {
    chart.data.labels.shift();
    chart.data.datasets[0].data.shift();
  }
  chart.update('none');
});

document.body.addEventListener('sse:context-data', function(evt) {
  evt.preventDefault();
  if (!window.sessionContextChart) return;
  const payload = JSON.parse(evt.detail.data);
  const chart = window.sessionContextChart;
  chart.data.labels.push(payload.timestamp);
  chart.data.datasets[0].data.push(payload.value);
  if (chart.data.labels.length > 60) {
    chart.data.labels.shift();
    chart.data.datasets[0].data.shift();
  }
  chart.update('none');
});
