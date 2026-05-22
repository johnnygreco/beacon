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
      ctx.strokeStyle = chartTheme().borderColor;
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
      var pc = providerColors[g.provider] || { border: chartTheme().color };
      ctx.fillStyle = pc.border;
      ctx.fillText(g.provider, (x0 + x1) / 2, y);
    });

    ctx.restore();
  }
};

var noDataOverlayPlugin = {
  id: 'noDataOverlay',
  afterDraw: function(chart, args, opts) {
    opts = opts || {};
    if (opts.enabled === false) return;
    var hasVisibleData = (chart.data.datasets || []).some(function(ds) {
      return !ds.hidden && ds.data && ds.data.length > 0;
    });
    if (hasVisibleData) return;
    var area = chart.chartArea;
    if (!area) return;
    var ctx = chart.ctx;
    ctx.save();
    ctx.fillStyle = chartTheme().noData;
    ctx.font = '12px -apple-system, BlinkMacSystemFont, sans-serif';
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillText(opts.message || 'No data in selected range', (area.left + area.right) / 2, (area.top + area.bottom) / 2);
    ctx.restore();
  }
};

Chart.register(providerGroupPlugin, noDataOverlayPlugin);
