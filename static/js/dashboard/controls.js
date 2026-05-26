function filterActivity(btn, type) {
	currentActivityFilter = type;
	document.querySelectorAll('.activity-filter-btn').forEach(function(b) {
		b.className = 'activity-filter-btn px-2 py-1 text-xs rounded border border-gray-600 text-gray-400 hover:text-gray-200 hover:border-gray-500 transition-colors';
		b.setAttribute('aria-pressed', 'false');
	});
	btn.className = 'activity-filter-btn px-2 py-1 text-xs rounded border border-blue-500/40 bg-blue-500/20 text-blue-400 transition-colors';
	btn.setAttribute('aria-pressed', 'true');
	loadActivity();
}

function activateSessionsPagination(offset) {
	loadCompletedSessions(offset);
}

function setDashboardRange(btn, value) {
	currentRange = value || '';
	currentCompletedOffset = 0;
	document.querySelectorAll('#dashboard-range-control .dash-range-btn').forEach(function(b) {
		b.className = 'dash-range-btn px-2 py-1 text-xs rounded border border-gray-600 text-gray-400 hover:text-gray-200 hover:border-gray-500 transition-colors';
		b.setAttribute('aria-pressed', 'false');
	});
	btn.className = 'dash-range-btn px-2 py-1 text-xs rounded border border-blue-500/40 bg-blue-500/20 text-blue-400 transition-colors';
	btn.setAttribute('aria-pressed', 'true');
	updateRangeCaption();
	loadDashboardCharts();
	if (!isSearchMode()) loadCompletedSessions(0);
	loadActivity();
}

function refreshDashboard() {
	loadActiveSessions();
	loadDashboardCharts();
	loadCompletedSessions(currentCompletedOffset);
	loadActivity();
}

function setDashboardMetric(btn, metric) {
	currentDashboardMetric = metric || 'error_rate';
	document.querySelectorAll('.dashboard-metric-btn').forEach(function(b) {
		b.className = 'dashboard-metric-btn px-2 py-1 text-xs text-gray-400 hover:text-gray-200';
		if (b.nextElementSibling) b.classList.add('border-r', 'border-gray-700');
		b.setAttribute('aria-pressed', 'false');
	});
	btn.className = 'dashboard-metric-btn px-2 py-1 text-xs bg-blue-500/20 text-blue-400';
	if (btn.nextElementSibling) btn.classList.add('border-r', 'border-gray-700');
	btn.setAttribute('aria-pressed', 'true');
	if (typeof updateDashboardModelActivityChart === 'function') {
		var dataEl = document.getElementById('dashboard-model-activity-data');
		if (dataEl) {
			try {
				updateDashboardModelActivityChart(JSON.parse(dataEl.textContent), currentDashboardMetric);
			} catch (err) {
			}
		}
	}
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
	var res = await fetch('/api/sessions/' + encodeURIComponent(sessionID) + '/subagents', {headers: {'Accept': 'application/json'}});
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
	var moreBtn = evt.target.closest && evt.target.closest('.dashboard-search-show-more');
	if (moreBtn) {
		evt.preventDefault();
		var idx = searchLimitSteps.indexOf(currentSearchLimit);
		currentSearchLimit = searchLimitSteps[Math.min(searchLimitSteps.length - 1, idx < 0 ? 1 : idx + 1)];
		loadDashboardSearch();
		return;
	}
	var searchRowEl = evt.target.closest && evt.target.closest('tr[data-search-row]');
	if (searchRowEl && !evt.target.closest('button') && !evt.target.closest('a')) {
		evt.preventDefault();
		window.location.href = searchRowEl.getAttribute('data-href');
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

(function() {
	var tableHead = document.querySelector('#completed-table thead');
	// Trusted server-rendered header reused when toggling back from search mode.
	if (tableHead) sessionTableHeadHTML = tableHead.innerHTML;
	var searchInput = document.getElementById('dashboard-session-search');
	var searchClear = document.getElementById('dashboard-search-clear');
	var searchFocus = document.getElementById('dashboard-search-focus');
	var searchSession = document.getElementById('dashboard-search-session');
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
		dashboardSearchTimer = setTimeout(function() {
			loadCompletedSessions(0);
		}, 250);
	}
	function setSearchButtonGroup(selector, attr, value) {
		document.querySelectorAll(selector).forEach(function(button) {
			var active = (button.getAttribute(attr) || '') === value;
			button.classList.toggle('is-active', active);
			button.setAttribute('aria-pressed', active ? 'true' : 'false');
		});
	}
	function syncSearchControls() {
		if (searchClear) searchClear.classList.toggle('hidden', currentSearchQuery === '');
		if (searchInput && searchInput.value.trim() !== currentSearchQuery) searchInput.value = currentSearchQuery;
		if (searchSession && searchSession.value.trim() !== currentSearchSessionID) searchSession.value = currentSearchSessionID;
		if (searchSort) searchSort.value = currentSearchSort;
		setSearchButtonGroup('[data-search-range]', 'data-search-range', currentSearchRange);
		setSearchButtonGroup('[data-search-event-kind]', 'data-search-event-kind', currentSearchEventKind);
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
				currentSearchQuery = searchInput.value.trim();
				currentSearchLimit = 30;
				loadCompletedSessions(0);
			} else if (evt.key === 'Escape' && currentSearchQuery !== '') {
				evt.preventDefault();
				currentSearchQuery = '';
				currentSearchLimit = 30;
				searchInput.value = '';
				syncSearchControls();
				loadCompletedSessions(0);
			}
		});
	}
	if (searchClear) {
		searchClear.addEventListener('click', function() {
			if (searchInput) searchInput.value = '';
			currentSearchQuery = '';
			currentCompletedOffset = 0;
			currentSearchLimit = 30;
			syncSearchControls();
			loadCompletedSessions(0);
			focusWithoutScroll(searchInput);
		});
	}
	if (searchFocus) {
		searchFocus.addEventListener('click', function() {
			revealSearchAndFocus();
		});
	}
	document.querySelectorAll('[data-search-range]').forEach(function(button) {
		button.addEventListener('click', function() {
			currentSearchRange = button.getAttribute('data-search-range') || '';
			currentSearchLimit = 30;
			syncSearchControls();
			loadCompletedSessions(0);
		});
	});
	document.querySelectorAll('[data-search-event-kind]').forEach(function(button) {
		button.addEventListener('click', function() {
			currentSearchEventKind = button.getAttribute('data-search-event-kind') || '';
			currentSearchLimit = 30;
			syncSearchControls();
			loadCompletedSessions(0);
		});
	});
	if (searchSession) {
		searchSession.addEventListener('input', function() {
			currentSearchSessionID = searchSession.value.trim();
			currentSearchLimit = 30;
			scheduleDashboardSearch();
		});
	}
	if (searchSort) {
		searchSort.addEventListener('change', function() {
			currentSearchSort = searchSort.value || 'relevance';
			currentSearchLimit = 30;
			loadCompletedSessions(0);
		});
	}
	if (searchReset) {
		searchReset.addEventListener('click', function() {
			currentSearchQuery = '';
			currentSearchRange = '';
			currentSearchEventKind = '';
			currentSearchSessionID = '';
			currentSearchSort = 'relevance';
			currentSearchLimit = 30;
			if (searchInput) searchInput.value = '';
			if (searchSession) searchSession.value = '';
			syncSearchControls();
			loadCompletedSessions(0);
			focusWithoutScroll(searchInput);
		});
	}
	document.addEventListener('keydown', function(evt) {
		var tagName = document.activeElement ? document.activeElement.tagName : '';
		if (evt.key === '/' && !evt.ctrlKey && !evt.metaKey && ['INPUT', 'TEXTAREA', 'SELECT'].indexOf(tagName) === -1) {
			evt.preventDefault();
			revealSearchAndFocus();
		}
	});
	if (window.EventSource) {
		var dashboardEvents = new EventSource('/sse/dashboard');
		dashboardEvents.onopen = function() {
			setDashboardConnection('Live');
		};
		dashboardEvents.onerror = function() {
			setDashboardConnection('Disconnected');
		};
		dashboardEvents.addEventListener('active-sessions-update', function() {
			loadActiveSessions();
		});
		dashboardEvents.addEventListener('completed-sessions-update', function() {
			loadCompletedSessions(currentCompletedOffset);
		});
		dashboardEvents.addEventListener('activity-update', function() {
			loadActivity();
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
	syncSearchControls();
	updateRangeCaption();
	loadActiveSessions();
	loadCompletedSessions(0);
	loadActivity();
	loadDashboardCharts();
})();
