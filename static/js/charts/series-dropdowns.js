function setupSeriesModelFilters(chartName, payload) {
  var chart = window[chartName];
  if (!chart || !payload) return;
  var labels = (payload.datasets || []).map(function(ds) { return ds.label; });

  var signature = JSON.stringify(labels.map(function(label, i) {
    var ds = payload.datasets[i] || {};
    return [label, ds.provider || '', ds.model || ''];
  }));
  var existing = document.getElementById(chartName + '-model-dropdown');
  if (existing && chart._seriesFilterSignature === signature) {
    syncSeriesDropdownState(chartName);
    applyStoredSeriesVisibility(chartName);
    chart.update('none');
    return;
  }
  if (labels.length <= 1) {
    if (chart._seriesFilterCloseHandler) {
      document.removeEventListener('click', chart._seriesFilterCloseHandler);
      chart._seriesFilterCloseHandler = null;
    }
    if (existing) existing.remove();
    chart._seriesFilterSignature = '';
    chart._seriesLabels = labels;
    chart._activeModels = new Set(labels);
    applyStoredSeriesVisibility(chartName);
    chart.update('none');
    return;
  }
  if (existing) {
    if (chart._seriesFilterCloseHandler) {
      document.removeEventListener('click', chart._seriesFilterCloseHandler);
    }
    existing.remove();
  }

  var storageKey = 'beacon-model-hidden-' + chartName;
  var savedHidden = [];
  try { savedHidden = JSON.parse(localStorage.getItem(storageKey)) || []; } catch(e) {}
  var validLabels = new Set(labels);
  var previousLabels = new Set(chart._seriesLabels || []);
  savedHidden = savedHidden.filter(function(label) { return validLabels.has(label); });
  chart._seriesLabels = labels;
  chart._seriesFilterSignature = signature;
  if (chart._activeModels) {
    chart._activeModels = new Set(Array.from(chart._activeModels).filter(function(label) {
      return validLabels.has(label);
    }));
    labels.forEach(function(label) {
      if (!previousLabels.has(label) && savedHidden.indexOf(label) === -1) {
        chart._activeModels.add(label);
      }
    });
  } else {
    chart._activeModels = new Set(labels.filter(function(label) {
      return savedHidden.indexOf(label) === -1;
    }));
  }
  if (chart._activeModels.size === 0 && savedHidden.length === 0) {
    chart._activeModels = new Set(labels);
  }

  var headerDiv = chart.canvas.parentElement.parentElement.querySelector('.chart-card-header, .flex.items-center');
  if (!headerDiv) return;
  var wrapper = document.createElement('div');
  wrapper.className = 'model-dropdown relative ml-auto';
  wrapper.id = chartName + '-model-dropdown';

  var trigger = document.createElement('button');
  trigger.type = 'button';
  trigger.className = 'model-dropdown-trigger';
  trigger.setAttribute('aria-haspopup', 'dialog');
  trigger.setAttribute('aria-expanded', 'false');
  var triggerLabel = document.createElement('span');
  triggerLabel.className = 'model-dropdown-label';
  trigger.appendChild(triggerLabel);
  var chevronSvg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  chevronSvg.setAttribute('class', 'model-dropdown-chevron');
  chevronSvg.setAttribute('width', '10');
  chevronSvg.setAttribute('height', '10');
  chevronSvg.setAttribute('viewBox', '0 0 10 10');
  var chevronPath = document.createElementNS('http://www.w3.org/2000/svg', 'path');
  chevronPath.setAttribute('d', 'M2 4l3 3 3-3');
  chevronPath.setAttribute('stroke', 'currentColor');
  chevronPath.setAttribute('stroke-width', '1.5');
  chevronPath.setAttribute('fill', 'none');
  chevronSvg.appendChild(chevronPath);
  trigger.appendChild(chevronSvg);
  wrapper.appendChild(trigger);

  var panel = document.createElement('div');
  panel.className = 'model-dropdown-panel hidden';
  panel.id = chartName + '-model-dropdown-panel';
  trigger.setAttribute('aria-controls', panel.id);
  var allRow = document.createElement('label');
  allRow.className = 'model-dropdown-item model-dropdown-all';
  var allCb = document.createElement('input');
  allCb.type = 'checkbox';
  allRow.appendChild(allCb);
  var allDot = document.createElement('span');
  allDot.className = 'model-dropdown-dot';
  allDot.style.background = chartCSSVar('--dash-accent', '#60a5fa');
  allRow.appendChild(allDot);
  var allLabel = document.createElement('span');
  allLabel.textContent = 'All Models';
  allRow.appendChild(allLabel);
  panel.appendChild(allRow);
  var divider = document.createElement('div');
  divider.className = 'model-dropdown-divider';
  panel.appendChild(divider);

  var lastProvider = '';
  (payload.datasets || []).forEach(function(ds, i) {
    var provider = ds.provider_label || providerDisplayName(ds.provider);
    if (provider && provider !== lastProvider) {
      var groupHeader = document.createElement('div');
      groupHeader.className = 'model-dropdown-group';
      groupHeader.textContent = provider;
      panel.appendChild(groupHeader);
      lastProvider = provider;
    }
    var row = document.createElement('label');
    row.className = 'model-dropdown-item';
    var cb = document.createElement('input');
    cb.type = 'checkbox';
    cb.dataset.model = ds.label;
    row.appendChild(cb);
    var dot = document.createElement('span');
    dot.className = 'model-dropdown-dot';
    dot.style.background = modelDatasetColor(i).border;
    row.appendChild(dot);
    var nameSpan = document.createElement('span');
    nameSpan.textContent = ds.label;
    row.appendChild(nameSpan);
    panel.appendChild(row);
  });
  wrapper.appendChild(panel);

  var logBtn = headerDiv.querySelector('.log-scale-toggle');
  if (logBtn) {
    headerDiv.insertBefore(wrapper, logBtn);
  } else {
    headerDiv.appendChild(wrapper);
  }

  function closeSeriesDropdown(focusTrigger) {
    panel.classList.add('hidden');
    trigger.classList.remove('open');
    trigger.setAttribute('aria-expanded', 'false');
    if (focusTrigger) trigger.focus({preventScroll: true});
  }

  function openSeriesDropdown() {
    panel.classList.remove('hidden');
    trigger.classList.add('open');
    trigger.setAttribute('aria-expanded', 'true');
    requestAnimationFrame(function() {
      clampDropdownPanel(panel);
    });
  }

  trigger.onclick = function(e) {
    e.stopPropagation();
    if (panel.classList.contains('hidden')) {
      openSeriesDropdown();
    } else {
      closeSeriesDropdown(false);
    }
  };
  wrapper.addEventListener('keydown', function(e) {
    if (e.key === 'Escape' && !panel.classList.contains('hidden')) {
      e.preventDefault();
      closeSeriesDropdown(true);
    }
  });
  wrapper.addEventListener('focusout', function() {
    setTimeout(function() {
      if (!wrapper.contains(document.activeElement)) closeSeriesDropdown(false);
    }, 0);
  });
  chart._seriesFilterCloseHandler = function(e) {
    if (!wrapper.contains(e.target)) {
      closeSeriesDropdown(false);
    }
  };
  document.addEventListener('click', chart._seriesFilterCloseHandler);

  allCb.addEventListener('change', function() {
    chart._activeModels = allCb.checked ? new Set(labels) : new Set();
    syncSeriesDropdownState(chartName);
    applyStoredSeriesVisibility(chartName);
    saveSeriesModelFilterState(chartName);
    chart.update('none');
  });
  panel.querySelectorAll('input[data-model]').forEach(function(cb) {
    cb.addEventListener('change', function() {
      if (cb.checked) chart._activeModels.add(cb.dataset.model);
      else chart._activeModels.delete(cb.dataset.model);
      syncSeriesDropdownState(chartName);
      applyStoredSeriesVisibility(chartName);
      saveSeriesModelFilterState(chartName);
      chart.update('none');
    });
  });

  syncSeriesDropdownState(chartName);
  applyStoredSeriesVisibility(chartName);
  chart.update('none');
}

function clampDropdownPanel(panel) {
  panel.style.left = '';
  panel.style.right = '0';
  panel.style.transform = '';
  var rect = panel.getBoundingClientRect();
  var pad = 8;
  if (rect.left < pad) {
    panel.style.left = (pad - rect.left) + 'px';
    panel.style.right = 'auto';
  } else if (rect.right > window.innerWidth - pad) {
    panel.style.right = (rect.right - window.innerWidth + pad) + 'px';
  }
}

function syncSeriesDropdownState(chartName) {
  var chart = window[chartName];
  var wrapper = document.getElementById(chartName + '-model-dropdown');
  if (!chart || !wrapper || !chart._seriesLabels) return;
  var total = chart._seriesLabels.length;
  var active = chart._activeModels ? chart._activeModels.size : total;
  var label = wrapper.querySelector('.model-dropdown-label');
  if (label) label.textContent = active === total ? 'All Models' : 'Models (' + active + '/' + total + ')';
  var allCb = wrapper.querySelector('.model-dropdown-all input');
  if (allCb) {
    allCb.checked = active === total;
    allCb.indeterminate = active > 0 && active < total;
  }
  wrapper.querySelectorAll('input[data-model]').forEach(function(cb) {
    cb.checked = chart._activeModels && chart._activeModels.has(cb.dataset.model);
  });
}

function applyStoredSeriesVisibility(chartName) {
  var chart = window[chartName];
  if (!chart) return;
  if (!chart._activeModels) {
    chart._activeModels = new Set((chart.data.datasets || []).map(function(ds) { return ds.label; }));
  }
  (chart.data.datasets || []).forEach(function(ds) {
    ds.hidden = !chart._activeModels.has(ds.label);
  });
}

function saveSeriesModelFilterState(chartName) {
  var chart = window[chartName];
  if (!chart || !chart._seriesLabels || !chart._activeModels) return;
  var hidden = chart._seriesLabels.filter(function(label) {
    return !chart._activeModels.has(label);
  });
  var storageKey = 'beacon-model-hidden-' + chartName;
  if (hidden.length === 0) {
    localStorage.removeItem(storageKey);
  } else {
    localStorage.setItem(storageKey, JSON.stringify(hidden));
  }
}

function rethemeChart(chart) {
  if (!chart || !chart.options) return;
  var theme = chartTheme();
  Object.keys(chart.options.scales || {}).forEach(function(key) {
    var scale = chart.options.scales[key];
    if (!scale) return;
    if (scale.grid) scale.grid.color = theme.borderColor;
    if (scale.ticks) scale.ticks.color = theme.color;
    if (scale.title) scale.title.color = theme.title;
  });
  if (chart.options.plugins && chart.options.plugins.legend && chart.options.plugins.legend.labels) {
    chart.options.plugins.legend.labels.color = theme.color;
  }
  (chart.data.datasets || []).forEach(function(ds, i) {
    var c = modelDatasetColor(i);
    ds.borderColor = c.border;
    ds.backgroundColor = c.bg;
  });
  chart.update('none');
}

function rethemeModelDropdown(chartName) {
  var wrapper = document.getElementById(chartName + '-model-dropdown');
  if (!wrapper) return;
  var allDot = wrapper.querySelector('.model-dropdown-all .model-dropdown-dot');
  if (allDot) allDot.style.background = chartCSSVar('--dash-accent', '#60a5fa');
  wrapper.querySelectorAll('input[data-model]').forEach(function(input, index) {
    var dot = input.parentElement && input.parentElement.querySelector('.model-dropdown-dot');
    if (dot) dot.style.background = modelDatasetColor(index).border;
  });
}

window.addEventListener('beacon:dashboard-theme-change', function() {
  applyChartDefaults();
  [
    'dashboardTokenCumulativeChart',
    'dashboardModelActivityChart',
    'sessionTokensChart',
    'sessionTokensByModelChart'
  ].forEach(function(chartName) {
    rethemeChart(window[chartName]);
    rethemeModelDropdown(chartName);
  });
});
