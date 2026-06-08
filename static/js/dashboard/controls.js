function filterActivity(btn, type) {
	currentActivityFilter = ['all', 'message', 'tool_call', 'error'].indexOf(type) >= 0 ? type : 'all';
	syncActivityControls();
	if (typeof scheduleDashboardStateURLWrite === 'function') scheduleDashboardStateURLWrite();
	loadActivity();
}

function activateSessionsPagination(offset) {
	loadCompletedSessions(offset);
}

function activityButtonType(button) {
	return button.getAttribute('data-activity-filter') || 'all';
}

function syncActivityControls() {
	document.querySelectorAll('.activity-filter-btn').forEach(function(button) {
		var active = activityButtonType(button) === currentActivityFilter;
		button.className = active
			? 'activity-filter-btn px-2 py-1 text-xs rounded border border-blue-500/40 bg-blue-500/20 text-blue-400 transition-colors'
			: 'activity-filter-btn px-2 py-1 text-xs rounded border border-gray-600 text-gray-400 hover:text-gray-200 hover:border-gray-500 transition-colors';
		button.setAttribute('aria-pressed', active ? 'true' : 'false');
	});
}

function rangeButtonValue(button) {
	return button.getAttribute('data-dashboard-range') || '';
}

function syncDashboardRangeControls() {
	document.querySelectorAll('#dashboard-range-control .dash-range-btn').forEach(function(button) {
		var active = rangeButtonValue(button) === completedRangeValue();
		button.className = active
			? 'dash-range-btn px-2 py-1 text-xs rounded border border-blue-500/40 bg-blue-500/20 text-blue-400 transition-colors'
			: 'dash-range-btn px-2 py-1 text-xs rounded border border-gray-600 text-gray-400 hover:text-gray-200 hover:border-gray-500 transition-colors';
		button.setAttribute('aria-pressed', active ? 'true' : 'false');
	});
}

function chartRangeButtonValue(button) {
	return button.getAttribute('data-dashboard-chart-range') || '';
}

function syncDashboardChartRangeControls() {
	document.querySelectorAll('#dashboard-chart-range-control .dash-range-btn').forEach(function(button) {
		var active = chartRangeButtonValue(button) === currentChartRange;
		button.className = active
			? 'dash-range-btn px-2 py-1 text-xs rounded border border-blue-500/40 bg-blue-500/20 text-blue-400 transition-colors'
			: 'dash-range-btn px-2 py-1 text-xs rounded border border-gray-600 text-gray-400 hover:text-gray-200 hover:border-gray-500 transition-colors';
		button.setAttribute('aria-pressed', active ? 'true' : 'false');
	});
}

function syncDashboardChartMetricControl() {
	var select = document.getElementById('dashboard-chart-metric');
	if (!select) return;
	select.value = chartMetricValue();
}

function setDashboardRange(btn, value) {
	currentCompletedRange = value || '';
	currentRange = currentCompletedRange;
	if (!currentActivityRangePinned) currentActivityRange = currentCompletedRange;
	currentCompletedOffset = 0;
	syncDashboardRangeControls();
	updateRangeCaption();
	if (typeof scheduleDashboardStateURLWrite === 'function') scheduleDashboardStateURLWrite();
	loadCompletedSessions(0);
	if (!currentActivityRangePinned) loadActivity();
}

function setDashboardChartRange(btn, value) {
	currentChartRange = value || '';
	syncDashboardChartRangeControls();
	updateChartRangeCaption();
	if (typeof scheduleDashboardStateURLWrite === 'function') scheduleDashboardStateURLWrite();
	loadDashboardCharts();
}

function setDashboardChartMetric(value) {
	currentChartMetric = dashboardChartMetricValues().indexOf(value) >= 0 ? value : 'total_tokens';
	syncDashboardChartMetricControl();
	if (typeof scheduleDashboardStateURLWrite === 'function') scheduleDashboardStateURLWrite();
	if (lastDashboardChartsPayload) {
		updateDashboardCharts(lastDashboardChartsPayload);
	} else {
		loadDashboardCharts();
	}
}

function refreshDashboardCharts() {
	loadDashboardCharts();
}

function refreshCompletedTable() {
	loadCompletedSessions(currentCompletedOffset);
}

function focusActiveSessionAction(sessionID, action) {
	if (!sessionID || !action) return;
	function focusButton(preferredAction) {
		var selector = '[data-active-session-id="' + cssEscape(sessionID) + '"] [data-active-session-action="' + cssEscape(preferredAction) + '"]';
		var button = document.querySelector(selector);
		if (!button || button.hasAttribute('disabled')) return false;
		try {
			button.focus({preventScroll: true});
		} catch (err) {
			button.focus();
		}
		return document.activeElement === button;
	}
	window.requestAnimationFrame(function() {
		if (!focusButton(action)) focusButton('toggle-pin');
	});
}

async function toggleJSONSubagents(button) {
	var sessionID = button.getAttribute('data-session-id');
	var parentRow = document.getElementById('session-row-' + sessionID);
	if (!parentRow) return;
	var existing = parentRow.parentNode.querySelectorAll('tr[data-parent="' + cssEscape(sessionID) + '"]');
	if (existing.length > 0) {
		var hidden = existing[0].classList.contains('hidden');
		existing.forEach(function(row) { row.classList.toggle('hidden'); });
		button.querySelector('svg').style.transform = hidden ? 'rotate(90deg)' : '';
		button.setAttribute('aria-expanded', hidden ? 'true' : 'false');
		return;
	}
	button.querySelector('svg').style.transform = 'rotate(90deg)';
	button.setAttribute('aria-expanded', 'true');
	var res = await fetch(requestURL('/api/sessions/' + encodeURIComponent(sessionID) + '/subagents', {}), {headers: {'Accept': 'application/json'}});
	if (!res.ok) {
		button.querySelector('svg').style.transform = '';
		button.setAttribute('aria-expanded', 'false');
		return;
	}
	var items = await res.json();
	rememberSessions(items);
	// completedRow() escapes all dynamic fields before this insertion.
	parentRow.insertAdjacentHTML('afterend', items.map(function(session) { return completedRow(session, true, sessionID); }).join(''));
}

document.addEventListener('click', function(evt) {
	var completedRangeBtn = evt.target.closest && evt.target.closest('[data-dashboard-range]');
	if (completedRangeBtn) {
		evt.preventDefault();
		setDashboardRange(completedRangeBtn, completedRangeBtn.getAttribute('data-dashboard-range') || '');
		return;
	}
	var chartRangeBtn = evt.target.closest && evt.target.closest('[data-dashboard-chart-range]');
	if (chartRangeBtn) {
		evt.preventDefault();
		setDashboardChartRange(chartRangeBtn, chartRangeBtn.getAttribute('data-dashboard-chart-range') || '');
		return;
	}
	var actionBtn = evt.target.closest && evt.target.closest('[data-dashboard-action]');
	if (actionBtn) {
		evt.preventDefault();
		var dashboardAction = actionBtn.getAttribute('data-dashboard-action') || '';
		if (dashboardAction === 'refresh-charts') refreshDashboardCharts();
		else if (dashboardAction === 'refresh-completed') refreshCompletedTable();
		return;
	}
	var sortBtn = evt.target.closest && evt.target.closest('[data-dashboard-sort]');
	if (sortBtn) {
		evt.preventDefault();
		sortCompletedTable(sortBtn, sortBtn.getAttribute('data-dashboard-sort') || '');
		return;
	}
	var retryBtn = evt.target.closest && evt.target.closest('[data-dashboard-retry]');
	if (retryBtn) {
		evt.preventDefault();
		var retry = retryBtn.getAttribute('data-dashboard-retry') || '';
		if (retry === 'fleet') loadDashboardFleet();
		else if (retry === 'active') loadActiveSessions();
		else if (retry === 'search') loadDashboardSearch();
		else if (retry === 'completed') loadCompletedSessions(currentCompletedOffset);
		else if (retry === 'activity') loadActivity();
		return;
	}
	var activityFilterBtn = evt.target.closest && evt.target.closest('[data-activity-filter]');
	if (activityFilterBtn) {
		evt.preventDefault();
		filterActivity(activityFilterBtn, activityFilterBtn.getAttribute('data-activity-filter') || 'all');
		return;
	}
	var scopeButton = evt.target.closest && evt.target.closest('[data-dashboard-scope-field]');
	if (scopeButton) {
		evt.preventDefault();
		evt.stopPropagation();
		if (typeof setDashboardScope === 'function') {
			setDashboardScope(scopeButton.getAttribute('data-dashboard-scope-field') || '', scopeButton.getAttribute('data-dashboard-scope-value') || '');
		}
		return;
	}
	var scopeClear = evt.target.closest && evt.target.closest('[data-dashboard-scope-clear]');
	if (scopeClear) {
		evt.preventDefault();
		evt.stopPropagation();
		if (typeof clearDashboardScope === 'function') {
			var field = scopeClear.getAttribute('data-dashboard-scope-clear') || '';
			var value = scopeClear.getAttribute('data-dashboard-scope-value') || '';
			clearDashboardScope(field === 'all' ? '' : field, value);
		}
		return;
	}
	var moreBtn = evt.target.closest && evt.target.closest('.dashboard-search-show-more');
	if (moreBtn) {
		evt.preventDefault();
		var idx = searchLimitSteps.indexOf(currentSearchLimit);
		currentSearchLimit = searchLimitSteps[Math.min(searchLimitSteps.length - 1, idx < 0 ? 1 : idx + 1)];
		if (typeof scheduleDashboardStateURLWrite === 'function') scheduleDashboardStateURLWrite();
		loadDashboardSearch();
		return;
	}
	var searchRowEl = evt.target.closest && evt.target.closest('tr[data-search-row]');
	if (searchRowEl && !evt.target.closest('button') && !evt.target.closest('a')) {
		evt.preventDefault();
		var searchHref = searchRowEl.getAttribute('data-href');
		if (typeof saveDashboardReturnState === 'function') saveDashboardReturnState(searchHref);
		window.location.href = searchHref;
		return;
	}
	var pageBtn = evt.target.closest && evt.target.closest('.json-page-btn');
	if (pageBtn) {
		evt.preventDefault();
		loadCompletedSessions(parseInt(pageBtn.getAttribute('data-offset'), 10) || 0);
		return;
	}
	var subBtn = evt.target.closest && evt.target.closest('.json-subagent-toggle');
	if (subBtn) {
		evt.preventDefault();
		evt.stopPropagation();
		toggleJSONSubagents(subBtn);
		return;
	}
	var activeSessionAction = evt.target.closest && evt.target.closest('[data-active-session-action]');
	if (activeSessionAction) {
		evt.preventDefault();
		evt.stopPropagation();
		if (activeSessionAction.hasAttribute('disabled')) return;
		var action = activeSessionAction.getAttribute('data-active-session-action') || '';
		var sessionID = activeSessionAction.getAttribute('data-active-session-id') || '';
		if (action === 'toggle-pin' && typeof toggleActiveSessionPin === 'function') {
			toggleActiveSessionPin(sessionID);
		} else if ((action === 'move-up' || action === 'move-down') && typeof moveActiveSession === 'function') {
			moveActiveSession(sessionID, action === 'move-down' ? 'down' : 'up');
		}
		if (lastActiveSessionsResponse && typeof renderActive === 'function') {
			renderActive(lastActiveSessionsResponse);
			focusActiveSessionAction(sessionID, action);
		}
		return;
	}
	var openBtn = evt.target.closest && evt.target.closest('.session-row-open');
	if (openBtn) {
		evt.preventDefault();
		evt.stopPropagation();
		window.goToSession(openBtn.getAttribute('data-session-link'), openBtn);
		return;
	}
	var row = evt.target.closest && evt.target.closest('tr[data-session-link]');
	if (row && !evt.target.closest('button')) {
		evt.preventDefault();
		window.goToSession(row.getAttribute('data-session-link'), row);
	}
});

document.addEventListener('change', function(evt) {
	var activeSort = evt.target && evt.target.closest && evt.target.closest('#active-session-sort');
	if (activeSort) {
		setActiveSessionSort(activeSort.value);
		return;
	}
	var chartMetric = evt.target && evt.target.closest && evt.target.closest('#dashboard-chart-metric');
	if (chartMetric) {
		setDashboardChartMetric(chartMetric.value);
	}
});

(function() {
	var tableHead = document.querySelector('#completed-table thead');
	// Trusted server-rendered header reused when toggling back from search mode.
	if (tableHead) sessionTableHeadHTML = tableHead.innerHTML;
	var searchInput = document.getElementById('dashboard-session-search');
	var searchSession = document.getElementById('dashboard-search-session');
	var searchKind = document.getElementById('dashboard-search-kind');
	var searchSort = document.getElementById('dashboard-search-sort');
	var searchReset = document.getElementById('dashboard-search-reset');
	function focusWithoutScroll(el) {
		if (!el) return;
		try {
			el.focus({preventScroll: true});
		} catch (err) {
			el.focus();
		}
	}
	function isElementVisibleInOwner(el) {
		if (!el) return false;
		var rect = el.getBoundingClientRect();
		var owner = typeof dashboardScrollOwner === 'function' ? dashboardScrollOwner() : null;
		var viewport = owner && typeof isDesktopDashboardLayout === 'function' && isDesktopDashboardLayout()
			? owner.getBoundingClientRect()
			: {top: 0, bottom: window.innerHeight || document.documentElement.clientHeight};
		return rect.top >= viewport.top && rect.bottom <= viewport.bottom;
	}
	function revealSearchAndFocus() {
		if (!searchInput) return;
		if (!isElementVisibleInOwner(searchInput)) {
			var target = document.getElementById('dashboard-search') || searchInput;
			var owner = typeof dashboardScrollOwner === 'function' ? dashboardScrollOwner() : null;
			if (owner && typeof isDesktopDashboardLayout === 'function' && isDesktopDashboardLayout()) {
				var ownerRect = owner.getBoundingClientRect();
				var targetRect = target.getBoundingClientRect();
				owner.scrollTop += targetRect.top - ownerRect.top - 24;
			} else if (target.scrollIntoView) {
				target.scrollIntoView({block: 'nearest'});
			}
		}
		focusWithoutScroll(searchInput);
	}
	function scheduleDashboardSearch() {
		clearTimeout(dashboardSearchTimer);
		if (typeof scheduleDashboardStateURLWrite === 'function') scheduleDashboardStateURLWrite();
		dashboardSearchTimer = setTimeout(function() {
			dashboardSearchTimer = 0;
			loadCompletedSessions(0);
		}, dashboardSearchDebounceDelayMS);
	}
	function syncSearchControls() {
		if (searchInput && searchInput.value.trim() !== currentSearchQuery) searchInput.value = currentSearchQuery;
		if (searchSession && searchSession.value.trim() !== currentSearchSessionID) searchSession.value = currentSearchSessionID;
		if (searchKind) searchKind.value = currentSearchEventKind || 'session';
		if (searchSort) searchSort.value = currentSearchSort;
	}
	function resetSearchFilters() {
		currentSearchQuery = '';
		currentSearchEventKind = 'session';
		currentSearchSessionID = '';
		currentSearchSort = 'relevance';
		currentSearchLimit = 30;
		currentCompletedOffset = 0;
		if (searchInput) searchInput.value = '';
		if (searchSession) searchSession.value = '';
		if (searchKind) searchKind.value = 'session';
		if (searchSort) searchSort.value = 'relevance';
		clearTimeout(dashboardSearchTimer);
		dashboardSearchTimer = 0;
		syncSearchControls();
		if (typeof writeDashboardStateToURL === 'function') writeDashboardStateToURL();
		loadCompletedSessions(0);
	}
	if (searchInput) {
		searchInput.addEventListener('input', function() {
			currentSearchQuery = searchInput.value.trim();
			currentCompletedOffset = 0;
			currentSearchLimit = 30;
			syncSearchControls();
			scheduleDashboardSearch();
		});
		searchInput.addEventListener('keydown', function(evt) {
			if (evt.key === 'Enter') {
				evt.preventDefault();
				clearTimeout(dashboardSearchTimer);
				dashboardSearchTimer = 0;
				currentSearchQuery = searchInput.value.trim();
				currentCompletedOffset = 0;
				currentSearchLimit = 30;
				if (typeof writeDashboardStateToURL === 'function') writeDashboardStateToURL();
				loadCompletedSessions(0);
			} else if (evt.key === 'Escape' && isSearchMode()) {
				evt.preventDefault();
				resetSearchFilters();
			}
		});
	}
	if (searchKind) {
		searchKind.addEventListener('change', function() {
			currentSearchEventKind = searchKind.value || 'session';
			currentCompletedOffset = 0;
			currentSearchLimit = 30;
			syncSearchControls();
			if (typeof scheduleDashboardStateURLWrite === 'function') scheduleDashboardStateURLWrite();
			loadCompletedSessions(0);
		});
	}
	if (searchSession) {
		searchSession.addEventListener('input', function() {
			currentSearchSessionID = searchSession.value.trim();
			currentCompletedOffset = 0;
			currentSearchLimit = 30;
			scheduleDashboardSearch();
		});
	}
	if (searchSort) {
		searchSort.addEventListener('change', function() {
			currentSearchSort = searchSort.value || 'relevance';
			currentCompletedOffset = 0;
			currentSearchLimit = 30;
			if (typeof scheduleDashboardStateURLWrite === 'function') scheduleDashboardStateURLWrite();
			loadCompletedSessions(0);
		});
	}
	if (searchReset) {
		searchReset.addEventListener('click', function() {
			resetSearchFilters();
			focusWithoutScroll(searchInput);
		});
	}
	document.addEventListener('keydown', function(evt) {
		var active = document.activeElement;
		var tagName = active ? active.tagName : '';
		var isEditing = active && (
			active.isContentEditable ||
			(active.closest && active.closest('[contenteditable=""], [contenteditable="true"]'))
		);
		if (evt.key === '/' && !evt.ctrlKey && !evt.metaKey && !isEditing && ['INPUT', 'TEXTAREA', 'SELECT'].indexOf(tagName) === -1) {
			evt.preventDefault();
			revealSearchAndFocus();
		}
	});
	if (window.EventSource) {
		setDashboardConnection('Connecting');
		var dashboardEvents = new EventSource('/sse/dashboard');
		dashboardEvents.onopen = function() {
			setDashboardConnection('Live');
		};
		dashboardEvents.onerror = function() {
			var closedState = typeof EventSource.CLOSED === 'number' ? EventSource.CLOSED : 2;
			setDashboardConnection(dashboardEvents.readyState === closedState ? 'Disconnected' : 'Connecting');
		};
		dashboardEvents.addEventListener('active-sessions-update', function() {
			loadActiveSessions();
			loadDashboardFleet();
		});
		dashboardEvents.addEventListener('completed-sessions-update', function() {
			loadCompletedSessions(currentCompletedOffset, {silent: true});
			loadDashboardFleet();
		});
		dashboardEvents.addEventListener('activity-update', function() {
			loadActivity();
			loadDashboardFleet();
		});
		dashboardEvents.addEventListener('dashboard-charts-update', function() {
			loadDashboardCharts();
		});
		window.addEventListener('beforeunload', function() {
			dashboardEvents.close();
		}, {once: true});
	} else {
		setDashboardConnection('Static');
	}
	syncDashboardRangeControls();
	syncDashboardChartRangeControls();
	syncDashboardChartMetricControl();
	syncActivityControls();
	syncDashboardScopeControls();
	syncSearchControls();
	updateRangeCaption();
	updateChartRangeCaption();
	window.__beaconDashboardInitialLoadSettled = false;
	var initialLoads = [
		loadDashboardFleet(),
		loadActiveSessions(),
		loadCompletedSessions(currentCompletedOffset),
		loadActivity(),
		loadDashboardCharts()
	];
	Promise.allSettled(initialLoads).then(function() {
		window.__beaconDashboardInitialLoadSettled = true;
		try {
			window.dispatchEvent(new CustomEvent('beacon:dashboard-initial-load-settled'));
		} catch (err) {}
		if (typeof restoreDashboardReturnScrollIfNeeded === 'function') restoreDashboardReturnScrollIfNeeded();
	});
})();
