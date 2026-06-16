// Chart.js initialization with dashboard theme support.

var chartUtils = window.BeaconCharts.utils;
var colorWithAlpha = chartUtils.colorWithAlpha;
var formatCompactNumber = chartUtils.formatCompactNumber;
var formatRateTick = chartUtils.formatRateTick;
var providerDisplayName = chartUtils.providerDisplayName;

function chartCSSVar(name, fallback) {
  var value = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return value || fallback;
}

function chartTheme() {
  return {
    color: chartCSSVar('--dash-text-muted', '#9ca3af'),
    title: chartCSSVar('--dash-text-muted', '#9ca3af'),
    borderColor: chartCSSVar('--dash-chart-grid', '#374151'),
    noData: chartCSSVar('--dash-text-faint', '#6b7280')
  };
}

function dashboardSeriesColor(index, fallback) {
  var slot = (index % 6) + 1;
  var border = chartCSSVar('--dash-chart-' + slot, fallback && fallback.border ? fallback.border : '#60a5fa');
  return {
    border: border,
    bg: colorWithAlpha(border, 0.14)
  };
}

function applyChartDefaults() {
  var theme = chartTheme();
  Chart.defaults.color = theme.color;
  Chart.defaults.borderColor = theme.borderColor;
}

applyChartDefaults();
Chart.defaults.animation = { duration: 0 };
Chart.defaults.animations = {
  colors: { duration: 0 },
  x: { duration: 0 },
  y: { duration: 0 }
};
Chart.defaults.transitions = {
  active: { animation: { duration: 0 } },
  resize: { animation: { duration: 0 } },
  show: { animations: { x: { duration: 0 }, y: { duration: 0 } } },
  hide: { animations: { x: { duration: 0 }, y: { duration: 0 } } }
};

if (Chart.defaults.global) {
  Chart.defaults.global.responsiveAnimationDuration = 0;
  if (Chart.defaults.global.animation && typeof Chart.defaults.global.animation === 'object') {
    Chart.defaults.global.animation.duration = 0;
  } else {
    Chart.defaults.global.animation = { duration: 0 };
  }
}

const noChartAnimation = {
  animation: { duration: 0 },
  responsiveAnimationDuration: 0,
  animations: {
    colors: { duration: 0 },
    x: { duration: 0 },
    y: { duration: 0 }
  },
  transitions: {
    active: { animation: { duration: 0 } },
    resize: { animation: { duration: 0 } },
    show: { animations: { x: { duration: 0 }, y: { duration: 0 } } },
    hide: { animations: { x: { duration: 0 }, y: { duration: 0 } } }
  }
};

if (Chart.Tooltip && Chart.Tooltip.positioners) {
  Chart.Tooltip.positioners.dashboardStable = function(elements, eventPosition) {
    if (!elements || !elements.length) return false;
    var area = this.chart && this.chart.chartArea;
    if (!area) return eventPosition || false;
    return {
      x: area.right - 10,
      y: area.top + 10,
      xAlign: 'right',
      yAlign: 'top'
    };
  };
}

function stableChartTooltipOptions() {
  return {
    position: 'dashboardStable',
    xAlign: 'right',
    yAlign: 'top',
    caretSize: 0,
    caretPadding: 0,
    animation: { duration: 0 },
    animations: {
      numbers: {
        type: 'number',
        properties: ['x', 'y', 'width', 'height', 'caretX', 'caretY'],
        duration: 0
      },
      opacity: { duration: 0 }
    }
  };
}

const timeScaleOptions = {
  type: 'time',
  time: { unit: 'minute', displayFormats: { minute: 'h:mm a' } },
  grid: { color: chartTheme().borderColor },
  ticks: { color: chartTheme().color, maxTicksLimit: 6, autoSkip: true, maxRotation: 0 }
};

const categoryScaleOptions = {
  grid: { color: chartTheme().borderColor },
  ticks: { color: chartTheme().color }
};

function formatTokenTick(value) {
  if (value >= 1000000) return (value / 1000000).toFixed(1).replace(/\.0$/, '') + 'M';
  if (value >= 1000) return (value / 1000).toFixed(0) + 'K';
  return value;
}

const yAxisOptions = {
  grid: { color: chartTheme().borderColor },
  ticks: { color: chartTheme().color, callback: formatTokenTick },
  beginAtZero: true
};

// Provider identity colors — single source of truth for all chart-level
// provider styling (labels, borders, legend items).
const providerColors = {
  'Claude Code': { border: '#fb923c', bg: 'rgba(251, 146, 60, 0.15)' },  // orange
  'Codex':       { border: '#22d3ee', bg: 'rgba(34, 211, 238, 0.15)' },  // cyan
};

const seriesColors = [
  { border: '#3b82f6', bg: 'rgba(59, 130, 246, 0.15)' },   // blue - input
  { border: '#a78bfa', bg: 'rgba(167, 139, 250, 0.15)' },   // violet - output
  { border: '#10b981', bg: 'rgba(16, 185, 129, 0.15)' },    // green - cache read
  { border: '#f59e0b', bg: 'rgba(245, 158, 11, 0.15)' },    // amber
  { border: '#ef4444', bg: 'rgba(239, 68, 68, 0.15)' },     // red
  { border: '#06b6d4', bg: 'rgba(6, 182, 212, 0.15)' },     // cyan
];

const modelLineColors = [
  { border: '#22d3ee', bg: 'rgba(34, 211, 238, 0.10)' },
  { border: '#fb923c', bg: 'rgba(251, 146, 60, 0.10)' },
  { border: '#34d399', bg: 'rgba(52, 211, 153, 0.10)' },
  { border: '#60a5fa', bg: 'rgba(96, 165, 250, 0.10)' },
  { border: '#f43f5e', bg: 'rgba(244, 63, 94, 0.10)' },
  { border: '#facc15', bg: 'rgba(250, 204, 21, 0.10)' },
  { border: '#a3e635', bg: 'rgba(163, 230, 53, 0.10)' },
  { border: '#c084fc', bg: 'rgba(192, 132, 252, 0.10)' },
  { border: '#2dd4bf', bg: 'rgba(45, 212, 191, 0.10)' },
  { border: '#f87171', bg: 'rgba(248, 113, 113, 0.10)' },
  { border: '#818cf8', bg: 'rgba(129, 140, 248, 0.10)' },
  { border: '#e5e7eb', bg: 'rgba(229, 231, 235, 0.08)' },
];

function tokenTooltip(ctx) {
  return ctx.dataset.label + ': ' + ctx.parsed.y.toLocaleString() + ' tokens';
}

function tokensByModelTooltipFooter(items) {
  if (!items || !items.length) return [];
  return [
    'Input = fresh input only; cache hits are shown under Cache.',
    'Cache = cache read tokens; provider-specific cache creation is excluded.'
  ];
}
