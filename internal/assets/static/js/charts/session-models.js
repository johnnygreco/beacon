// ---- Model filter system for tokens-by-model charts ----

function getModelProvider(fullData, index) {
  if (!fullData.providerGroups) return '';
  for (var i = 0; i < fullData.providerGroups.length; i++) {
    var g = fullData.providerGroups[i];
    if (index >= g.start && index <= g.end) return g.provider;
  }
  return '';
}

function modelFilterSignature(fullData) {
  return JSON.stringify({
    labels: fullData.labels || [],
    groups: fullData.providerGroups || [],
    datasets: (fullData.datasets || []).map(function(ds) { return ds.label; })
  });
}

function modelProviderMap(fullData) {
  var providerMap = {};
  (fullData.labels || []).forEach(function(label, i) {
    providerMap[label] = getModelProvider(fullData, i);
  });
  return providerMap;
}

function setupModelFilters(chartName, dataEl) {
  var chart = window[chartName];
  if (!chart || !dataEl) return;

  var fullData;
  try { fullData = JSON.parse(dataEl.textContent); } catch(e) { return; }
  if (!fullData.labels || fullData.labels.length <= 1) return;

  var signature = modelFilterSignature(fullData);
  var existing = document.getElementById(chartName + '-model-dropdown');
  if (existing && chart._modelFilterSignature === signature) {
    chart._fullModelData = fullData;
    chart._hadMultipleProviders = fullData.providerGroups && fullData.providerGroups.length > 1;
    chart._modelProviderMap = modelProviderMap(fullData);
    if (chart._activeModels) {
      var currentLabels = new Set(fullData.labels);
      chart._activeModels = new Set(Array.from(chart._activeModels).filter(function(label) {
        return currentLabels.has(label);
      }));
    }
    syncDropdownState(chartName);
    if (chart._activeModels && chart._activeModels.size < fullData.labels.length) {
      rebuildModelChart(chartName);
    }
    return;
  }

  // Clean up previous dropdown if re-initialized
  if (existing) {
    if (chart._modelFilterCloseHandler) {
      document.removeEventListener('click', chart._modelFilterCloseHandler);
    }
    existing.remove();
  }

  // Store full data and build provider map
  chart._fullModelData = fullData;
  chart._modelFilterSignature = signature;
  chart._hadMultipleProviders = fullData.providerGroups && fullData.providerGroups.length > 1;
  var providerMap = modelProviderMap(fullData);
  chart._modelProviderMap = providerMap;

  // Load saved hidden models from localStorage
  var storageKey = 'beacon-model-hidden-' + chartName;
  var savedHidden = [];
  try { savedHidden = JSON.parse(localStorage.getItem(storageKey)) || []; } catch(e) {}
  var validLabels = new Set(fullData.labels);
  savedHidden = savedHidden.filter(function(l) { return validLabels.has(l); });

  chart._activeModels = new Set(fullData.labels.filter(function(l) {
    return savedHidden.indexOf(l) === -1;
  }));

  // Build dropdown UI
  var canvasWrapper = chart.canvas.parentElement;
  var containerCard = canvasWrapper.parentElement;
  var headerDiv = containerCard.querySelector('.chart-card-header, .flex.items-center');
  if (!headerDiv) return;

  // Insert dropdown trigger into the header bar (between title and log toggle)
  var wrapper = document.createElement('div');
  wrapper.className = 'model-dropdown relative ml-auto';
  wrapper.id = chartName + '-model-dropdown';

  var trigger = document.createElement('button');
  trigger.type = 'button';
  trigger.className = 'model-dropdown-trigger';
  trigger.setAttribute('aria-haspopup', 'true');
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

  // Dropdown panel
  var panel = document.createElement('div');
  panel.className = 'model-dropdown-panel hidden';

  // "All" checkbox row
  var allRow = document.createElement('label');
  allRow.className = 'model-dropdown-item model-dropdown-all';
  var allCb = document.createElement('input');
  allCb.type = 'checkbox';
  allCb.checked = true;
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

  // Group models by provider
  var lastProv = '';
  fullData.labels.forEach(function(label) {
    var prov = providerMap[label] || '';
    var pc = providerColors[prov] || { border: chartTheme().color };
    if (prov !== lastProv && prov) {
      var groupHeader = document.createElement('div');
      groupHeader.className = 'model-dropdown-group';
      groupHeader.style.color = pc.border;
      groupHeader.textContent = prov;
      panel.appendChild(groupHeader);
      lastProv = prov;
    }
    var row = document.createElement('label');
    row.className = 'model-dropdown-item';
    var cb = document.createElement('input');
    cb.type = 'checkbox';
    cb.dataset.model = label;
    cb.checked = true;
    row.appendChild(cb);
    var dot = document.createElement('span');
    dot.className = 'model-dropdown-dot';
    dot.style.background = pc.border;
    row.appendChild(dot);
    var nameSpan = document.createElement('span');
    nameSpan.textContent = label;
    row.appendChild(nameSpan);
    panel.appendChild(row);
  });

  wrapper.appendChild(panel);

  // Insert into header — before the log toggle button if present, else at end
  var logBtn = headerDiv.querySelector('.log-scale-toggle');
  if (logBtn) {
    headerDiv.insertBefore(wrapper, logBtn);
  } else {
    headerDiv.appendChild(wrapper);
  }

  // Event: toggle dropdown
  trigger.onclick = function(e) {
    e.stopPropagation();
    panel.classList.toggle('hidden');
    trigger.classList.toggle('open', !panel.classList.contains('hidden'));
    trigger.setAttribute('aria-expanded', panel.classList.contains('hidden') ? 'false' : 'true');
  };

  // Event: close on outside click (stored for cleanup on re-init)
  chart._modelFilterCloseHandler = function(e) {
    if (!wrapper.contains(e.target)) {
      panel.classList.add('hidden');
      trigger.classList.remove('open');
      trigger.setAttribute('aria-expanded', 'false');
    }
  };
  document.addEventListener('click', chart._modelFilterCloseHandler);

  // Event: "All" checkbox
  var allCheckbox = allRow.querySelector('input');
  allCheckbox.addEventListener('change', function() {
    if (allCheckbox.checked) {
      chart._activeModels = new Set(fullData.labels);
    } else {
      chart._activeModels.clear();
    }
    syncDropdownState(chartName);
    rebuildModelChart(chartName);
    saveModelFilterState(chartName);
  });

  // Event: individual model checkboxes
  panel.querySelectorAll('input[data-model]').forEach(function(cb) {
    cb.addEventListener('change', function() {
      var model = cb.dataset.model;
      if (cb.checked) {
        chart._activeModels.add(model);
      } else {
        chart._activeModels.delete(model);
      }
      syncDropdownState(chartName);
      rebuildModelChart(chartName);
      saveModelFilterState(chartName);
    });
  });

  // Initial sync
  syncDropdownState(chartName);

  // If some models were previously hidden, rebuild chart
  if (savedHidden.length > 0 && chart._activeModels.size < fullData.labels.length) {
    rebuildModelChart(chartName);
  }
}

function syncDropdownState(chartName) {
  var chart = window[chartName];
  if (!chart || !chart._fullModelData) return;

  var wrapper = document.getElementById(chartName + '-model-dropdown');
  if (!wrapper) return;

  var total = chart._fullModelData.labels.length;
  var active = chart._activeModels.size;
  var allActive = active === total;

  // Update trigger label
  var label = wrapper.querySelector('.model-dropdown-label');
  if (label) {
    label.textContent = allActive ? 'All Models' : 'Models (' + active + '/' + total + ')';
  }

  // Update "All" checkbox
  var allCb = wrapper.querySelector('.model-dropdown-all input');
  if (allCb) {
    allCb.checked = allActive;
    allCb.indeterminate = !allActive && active > 0;
  }

  // Update individual checkboxes
  wrapper.querySelectorAll('input[data-model]').forEach(function(cb) {
    cb.checked = chart._activeModels.has(cb.dataset.model);
  });
}

function rebuildModelChart(chartName) {
  var chart = window[chartName];
  if (!chart || !chart._fullModelData) return;

  var fullData = chart._fullModelData;
  var active = chart._activeModels;

  var newLabels = [];
  var newDatasets = fullData.datasets.map(function(ds) {
    return { label: ds.label, data: [] };
  });
  var newGroups = [];
  var lastProvider = '';
  var groupStart = 0;

  fullData.labels.forEach(function(label, i) {
    if (!active.has(label)) return;

    var provider = chart._modelProviderMap[label] || '';

    if (lastProvider && provider !== lastProvider) {
      newGroups.push({
        provider: lastProvider,
        start: groupStart,
        end: newLabels.length - 1
      });
      groupStart = newLabels.length;
    }
    lastProvider = provider;

    newLabels.push(label);
    fullData.datasets.forEach(function(ds, j) {
      newDatasets[j].data.push(ds.data[i]);
    });
  });

  // Close last group
  if (lastProvider && newLabels.length > 0) {
    newGroups.push({
      provider: lastProvider,
      start: groupStart,
      end: newLabels.length - 1
    });
  }

  // Update chart data
  chart.data.labels = newLabels;
  chart.data.datasets.forEach(function(ds, i) {
    ds.data = newDatasets[i].data;
  });

  // Update provider group headers — keep single-provider group visible
  // if the original data had multiple providers (so user sees context)
  var pgh = chart.options.plugins.providerGroupHeaders;
  if (pgh) {
    pgh.groups = (newGroups.length > 1 || (newGroups.length === 1 && chart._hadMultipleProviders)) ? newGroups : [];
  }

  chart.update('none');
}

function saveModelFilterState(chartName) {
  var chart = window[chartName];
  if (!chart || !chart._fullModelData) return;

  var storageKey = 'beacon-model-hidden-' + chartName;
  var hidden = chart._fullModelData.labels.filter(function(l) {
    return !chart._activeModels.has(l);
  });

  if (hidden.length === 0) {
    localStorage.removeItem(storageKey);
  } else {
    localStorage.setItem(storageKey, JSON.stringify(hidden));
  }
}

// Create a grouped bar chart for tokens by model
function createTokensByModelChart(el, dataEl) {
  var modelData = JSON.parse(dataEl.textContent);
  if (!modelData.labels || !modelData.datasets) return null;
  var datasets = modelData.datasets.map(function(ds, i) {
    var c = dashboardSeriesColor(i, seriesColors[i % seriesColors.length]);
    return {
      label: ds.label,
      data: ds.data,
      backgroundColor: c.bg,
      borderColor: c.border,
      borderWidth: 1
    };
  });
  var hasGroups = modelData.providerGroups && modelData.providerGroups.length > 0;
  var xOpts = Object.assign({}, categoryScaleOptions);
  if (modelData.labels.length > 4) {
    xOpts.ticks = Object.assign({}, xOpts.ticks || {}, {
      maxRotation: 45, minRotation: 0, font: { size: 10 }
    });
  }
  return new Chart(el, {
    type: 'bar',
    data: { labels: modelData.labels, datasets: datasets },
    options: {
      ...noChartAnimation,
      responsive: true,
      maintainAspectRatio: false,
      layout: hasGroups ? { padding: { bottom: 25 } } : {},
      scales: {
        x: xOpts,
        y: {
          ...yAxisOptions,
          title: { display: true, text: 'Tokens', color: chartTheme().title }
        }
      },
      plugins: {
        legend: { display: true, position: 'top', labels: { usePointStyle: true, boxWidth: 8 } },
        tooltip: {
          ...stableChartTooltipOptions(),
          callbacks: {
            label: tokenTooltip,
            footer: tokensByModelTooltipFooter
          }
        },
        providerGroupHeaders: { groups: modelData.providerGroups || [] }
      }
    }
  });
}
