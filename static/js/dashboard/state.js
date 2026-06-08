// --- JSON dashboard stores ---
var currentActivityFilter = 'all';
var currentCompletedRange = '';
var currentActivityRange = '';
var currentActivityRangePinned = false;
var currentRange = currentCompletedRange;
var currentChartRange = '';
var currentChartMetric = 'total_tokens';
var currentActiveSort = 'recent';
var currentCompletedOffset = 0;
var currentSearchQuery = '';
var currentSearchEventKind = 'session';
var currentSearchSessionID = '';
var currentSearchSort = 'relevance';
var currentSearchLimit = 30;
var dashboardSearchTimer = 0;
var dashboardSearchDebounceDelayMS = 100;
var completedPageSize = 30;
var sortColumn = 'ended';
var sortAsc = false;
var dashboardRequestSeq = {};
var dashboardControllers = {};
var sessionTableHeadHTML = '';
var searchLimitSteps = [30, 60, 120, 240];
var dashboardURLWriteTimer = 0;
var pendingDashboardReturnScroll = null;
var activeSessionPrefsKey = 'beacon-active-session-prefs-v1';
var activeSessionPrefs = {pinned: [], order: []};
var lastActiveSessionsResponse = null;
var lastDashboardChartsPayload = null;
window.dashboardSessionIndex = window.dashboardSessionIndex || {};

var dashboardReturnStateKey = 'beacon-dashboard-return-state-v1';
var dashboardRanges = ['', '1h', '24h', '7d', '30d'];
var dashboardChartMetrics = ['total_tokens', 'input_tokens', 'output_tokens', 'cache_read_tokens', 'tool_calls', 'errors', 'error_rate'];
var dashboardActiveSorts = ['recent', 'longest', 'tokens', 'tools', 'errors'];
var dashboardSearchEventKinds = ['session', 'event', 'message', 'tool_call', 'tool_result', 'reasoning', 'error'];
var dashboardSearchSorts = ['relevance', 'newest', 'oldest'];
var dashboardSortColumns = ['name', 'node', 'runtime', 'model', 'tokens', 'turns', 'tools', 'errors', 'duration', 'project', 'ended', 'id'];
var dashboardActivityFilters = ['all', 'message', 'tool_call', 'error'];
var dashboardScopeFields = {
	node_id: ['node_id', 'node_ids'],
	collector_id: ['collector_id', 'collector_ids'],
	source_id: ['source_id', 'source_ids'],
	source_name: ['source_name', 'source_names'],
	runtime: ['runtime', 'runtimes'],
	project_key: ['project_key', 'project_keys']
};

function dashboardStorageGet(key) {
	try { return window.sessionStorage.getItem(key); } catch (err) { return null; }
}

function dashboardStorageSet(key, value) {
	try { window.sessionStorage.setItem(key, value); } catch (err) {}
}

function dashboardStorageRemove(key) {
	try { window.sessionStorage.removeItem(key); } catch (err) {}
}

function dashboardLocalStorageGet(key) {
	try { return window.localStorage.getItem(key); } catch (err) { return null; }
}

function dashboardLocalStorageSet(key, value) {
	try { window.localStorage.setItem(key, value); } catch (err) {}
}

function normalizeSessionIDList(ids) {
	var seen = {};
	return (Array.isArray(ids) ? ids : []).map(function(id) {
		return String(id || '').trim();
	}).filter(function(id) {
		if (!id || seen[id]) return false;
		seen[id] = true;
		return true;
	});
}

function readActiveSessionPrefs() {
	var raw = dashboardLocalStorageGet(activeSessionPrefsKey);
	if (!raw) return {pinned: [], order: []};
	try {
		var parsed = JSON.parse(raw);
		return {
			pinned: normalizeSessionIDList(parsed && parsed.pinned),
			order: normalizeSessionIDList(parsed && parsed.order)
		};
	} catch (err) {
		return {pinned: [], order: []};
	}
}

function saveActiveSessionPrefs() {
	activeSessionPrefs.pinned = normalizeSessionIDList(activeSessionPrefs.pinned);
	activeSessionPrefs.order = normalizeSessionIDList(activeSessionPrefs.order);
	dashboardLocalStorageSet(activeSessionPrefsKey, JSON.stringify(activeSessionPrefs));
}

function activeSessionPinnedIDs(items) {
	var available = null;
	if (Array.isArray(items)) {
		available = {};
		items.forEach(function(session) {
			if (session && session.id) available[session.id] = true;
		});
	}
	return normalizeSessionIDList(activeSessionPrefs.pinned).filter(function(id) {
		return !available || !!available[id];
	});
}

function activeSessionOrderIDs(items) {
	var available = null;
	if (Array.isArray(items)) {
		available = {};
		items.forEach(function(session) {
			if (session && session.id) available[session.id] = true;
		});
	}
	return normalizeSessionIDList(activeSessionPrefs.order).filter(function(id) {
		return !available || !!available[id];
	});
}

function lastActiveSessionItems() {
	if (!lastActiveSessionsResponse || !Array.isArray(lastActiveSessionsResponse.items)) return null;
	return lastActiveSessionsResponse.items;
}

function isActiveSessionPinned(id) {
	return activeSessionPinnedIDs().indexOf(id) >= 0;
}

function toggleActiveSessionPin(id) {
	id = String(id || '').trim();
	if (!id) return;
	var pinned = activeSessionPinnedIDs(lastActiveSessionItems());
	var index = pinned.indexOf(id);
	if (index >= 0) {
		pinned.splice(index, 1);
	} else {
		pinned.unshift(id);
	}
	activeSessionPrefs.pinned = pinned;
	activeSessionPrefs.order = [];
	saveActiveSessionPrefs();
}

function sortedActiveSessionIDsForMove(items) {
	items = Array.isArray(items) ? items : [];
	var sorted = typeof sortActiveSessions === 'function' ? sortActiveSessions(items) : items;
	return sorted.map(function(session) {
		return session && session.id ? String(session.id) : '';
	}).filter(function(id) {
		return id !== '';
	});
}

function moveActiveSession(id, direction) {
	id = String(id || '').trim();
	if (!id) return;
	var delta = direction === 'down' ? 1 : -1;
	var ids = sortedActiveSessionIDsForMove(lastActiveSessionItems());
	var index = ids.indexOf(id);
	var next = index + delta;
	if (index < 0 || next < 0 || next >= ids.length) return;
	var swap = ids[next];
	ids[next] = ids[index];
	ids[index] = swap;
	activeSessionPrefs.pinned = activeSessionPinnedIDs(lastActiveSessionItems());
	activeSessionPrefs.order = ids;
	saveActiveSessionPrefs();
}

function movePinnedActiveSession(id, direction) {
	moveActiveSession(id, direction);
}

function clearActiveSessionManualOrder() {
	if (!activeSessionPrefs.order || activeSessionPrefs.order.length === 0) return;
	activeSessionPrefs.order = [];
	saveActiveSessionPrefs();
}

function dashboardIntParam(params, name, fallback, allowedValues) {
	var raw = params.get(name);
	if (raw === null || raw === '') return fallback;
	var parsed = Number.parseInt(raw, 10);
	if (!Number.isFinite(parsed) || parsed < 0) return fallback;
	if (allowedValues && allowedValues.indexOf(parsed) < 0) return fallback;
	return parsed;
}

function dashboardEnumParam(params, name, fallback, allowedValues, aliases) {
	var raw = params.get(name);
	if (raw === null) return fallback;
	raw = String(raw).trim();
	if (aliases && Object.prototype.hasOwnProperty.call(aliases, raw)) raw = aliases[raw];
	return allowedValues.indexOf(raw) >= 0 ? raw : fallback;
}

function dashboardHasValidEnumParam(params, name, allowedValues, aliases) {
	var raw = params.get(name);
	if (raw === null) return false;
	raw = String(raw).trim();
	if (aliases && Object.prototype.hasOwnProperty.call(aliases, raw)) raw = aliases[raw];
	return allowedValues.indexOf(raw) >= 0;
}

function readDashboardStateFromURL() {
	var params = new URLSearchParams(window.location.search || '');
	currentCompletedRange = dashboardEnumParam(params, 'range', currentCompletedRange, dashboardRanges, {all: ''});
	if (!params.has('range') && params.has('search_range')) {
		currentCompletedRange = dashboardEnumParam(params, 'search_range', currentCompletedRange, dashboardRanges, {all: ''});
	}
	currentRange = currentCompletedRange;
	currentChartRange = dashboardEnumParam(params, 'chart_range', currentChartRange, dashboardRanges, {all: ''});
	currentChartMetric = dashboardEnumParam(params, 'chart_metric', currentChartMetric, dashboardChartMetrics, {tokens: 'total_tokens'});
	currentActivityRangePinned = dashboardHasValidEnumParam(params, 'activity_range', dashboardRanges, {all: ''});
	currentActivityRange = currentActivityRangePinned
		? dashboardEnumParam(params, 'activity_range', currentActivityRange, dashboardRanges, {all: ''})
		: currentCompletedRange;
	currentActiveSort = dashboardEnumParam(params, 'active_sort', currentActiveSort, dashboardActiveSorts);
	currentSearchQuery = (params.get('q') || '').trim();
	currentSearchEventKind = dashboardEnumParam(params, 'event_kind', currentSearchEventKind, dashboardSearchEventKinds, {'': 'session', all: 'event'});
	currentSearchSessionID = (params.get('session_id') || '').trim();
	currentSearchSort = dashboardEnumParam(params, 'search_sort', currentSearchSort, dashboardSearchSorts);
	currentSearchLimit = dashboardIntParam(params, 'search_limit', currentSearchLimit, searchLimitSteps);
	if (currentSearchQuery === '' && currentSearchEventKind === 'session' && currentSearchSessionID === '') {
		currentSearchSort = 'relevance';
		currentSearchLimit = 30;
	}
	currentCompletedOffset = dashboardIntParam(params, 'offset', currentCompletedOffset);
	sortColumn = dashboardEnumParam(params, 'sort', sortColumn, dashboardSortColumns);
	var dir = dashboardEnumParam(params, 'dir', sortAsc ? 'asc' : 'desc', ['asc', 'desc']);
	sortAsc = dir === 'asc';
	currentActivityFilter = dashboardEnumParam(params, 'activity', currentActivityFilter, dashboardActivityFilters);
}

function dashboardStatePath() {
	var url = new URL('/', window.location.origin);
	var params = url.searchParams;
	if (window.BeaconDashboard && window.BeaconDashboard.utils && typeof window.BeaconDashboard.utils.copyDashboardScopeParams === 'function') {
		window.BeaconDashboard.utils.copyDashboardScopeParams(params, new URLSearchParams(window.location.search));
	}
	if (currentCompletedRange !== '') params.set('range', currentCompletedRange);
	if (currentChartRange !== '') params.set('chart_range', currentChartRange);
	if (currentChartMetric !== 'total_tokens') params.set('chart_metric', currentChartMetric);
	if (currentActivityRangePinned || currentActivityRange !== currentCompletedRange) params.set('activity_range', currentActivityRange === '' ? 'all' : currentActivityRange);
	if (currentActiveSort !== 'recent') params.set('active_sort', currentActiveSort);
	var searchActive = typeof hasDashboardSearchFilter === 'function'
		? hasDashboardSearchFilter()
		: (currentSearchQuery || currentSearchEventKind !== 'session' || currentSearchSessionID);
	if (searchActive) {
		if (currentSearchQuery) params.set('q', currentSearchQuery);
		if (currentSearchEventKind !== 'session') params.set('event_kind', currentSearchEventKind);
		if (currentSearchSessionID) params.set('session_id', currentSearchSessionID);
		if (currentSearchSort !== 'relevance') params.set('search_sort', currentSearchSort);
		if (currentSearchLimit !== 30) params.set('search_limit', String(currentSearchLimit));
	}
	if (currentCompletedOffset > 0) params.set('offset', String(currentCompletedOffset));
	if (sortColumn !== 'ended' || sortAsc) {
		params.set('sort', sortColumn);
		params.set('dir', sortAsc ? 'asc' : 'desc');
	}
	if (currentActivityFilter !== 'all') params.set('activity', currentActivityFilter);
	return url.pathname + url.search;
}

function dashboardCurrentURL() {
	return new URL(window.location.pathname + window.location.search, window.location.origin);
}

function dashboardScopeValues(field) {
	var names = dashboardScopeFields[field] || [field];
	var params = new URLSearchParams(window.location.search || '');
	var values = [];
	var seen = {};
	names.forEach(function(name) {
		params.getAll(name).forEach(function(raw) {
			String(raw || '').split(',').forEach(function(part) {
				var value = part.trim();
				if (!value || seen[value]) return;
				seen[value] = true;
				values.push(value);
			});
		});
	});
	return values;
}

function dashboardHasScopeFilters() {
	return Object.keys(dashboardScopeFields).some(function(field) {
		return dashboardScopeValues(field).length > 0;
	});
}

function dashboardApplyURL(url) {
	if (!window.history || typeof window.history.replaceState !== 'function') return;
	window.history.replaceState(window.history.state || {}, '', url.pathname + url.search);
}

function setDashboardScope(field, value) {
	if (!dashboardScopeFields[field]) return;
	var url = dashboardCurrentURL();
	var names = dashboardScopeFields[field];
	names.forEach(function(name) { url.searchParams.delete(name); });
	value = String(value || '').trim();
	if (value) url.searchParams.set(names[0], value);
	url.searchParams.delete('offset');
	currentCompletedOffset = 0;
	dashboardApplyURL(url);
	refreshDashboardForScopeChange();
}

function clearDashboardScope(field) {
	var url = dashboardCurrentURL();
	var fields = field && dashboardScopeFields[field] ? [field] : Object.keys(dashboardScopeFields);
	var value = String(arguments.length > 1 ? arguments[1] : '').trim();
	if (field && dashboardScopeFields[field] && value) {
		removeDashboardScopeValue(url, field, value);
	} else {
		fields.forEach(function(name) {
			dashboardScopeFields[name].forEach(function(param) { url.searchParams.delete(param); });
		});
	}
	url.searchParams.delete('offset');
	currentCompletedOffset = 0;
	dashboardApplyURL(url);
	refreshDashboardForScopeChange();
}

function removeDashboardScopeValue(url, field, value) {
	var names = dashboardScopeFields[field] || [];
	names.forEach(function(param) {
		var entries = url.searchParams.getAll(param);
		url.searchParams.delete(param);
		entries.forEach(function(raw) {
			var rawText = String(raw || '');
			var parts = rawText.split(',').map(function(part) {
				return part.trim();
			}).filter(function(part) {
				return part && part !== value;
			});
			if (parts.length === 0) return;
			if (rawText.indexOf(',') >= 0) {
				url.searchParams.append(param, parts.join(','));
			} else {
				parts.forEach(function(part) { url.searchParams.append(param, part); });
			}
		});
	});
}

function refreshDashboardForScopeChange() {
	if (typeof syncDashboardScopeControls === 'function') syncDashboardScopeControls();
	if (typeof loadDashboardFleet === 'function') loadDashboardFleet();
	if (typeof loadActiveSessions === 'function') loadActiveSessions();
	if (typeof loadCompletedSessions === 'function') loadCompletedSessions(0);
	if (typeof loadActivity === 'function') loadActivity();
	if (typeof loadDashboardCharts === 'function') loadDashboardCharts();
}

function writeDashboardStateToURL() {
	if (!window.history || typeof window.history.replaceState !== 'function') return;
	var next = dashboardStatePath();
	var current = window.location.pathname + window.location.search;
	if (next !== current) window.history.replaceState(window.history.state || {}, '', next);
}

function scheduleDashboardStateURLWrite() {
	clearTimeout(dashboardURLWriteTimer);
	dashboardURLWriteTimer = setTimeout(writeDashboardStateToURL, 80);
}

function saveDashboardReturnState(transcriptURL) {
	writeDashboardStateToURL();
	var owner = typeof dashboardScrollOwner === 'function' ? dashboardScrollOwner() : null;
	var transcriptPath = dashboardTranscriptURL(transcriptURL);
	if (!transcriptPath) return '';
	var state = {
		url: window.location.pathname + window.location.search,
		transcriptPath: transcriptPath,
		scrollTop: owner ? Math.round(owner.scrollTop) : 0,
		windowY: Math.round(window.scrollY || window.pageYOffset || 0),
		savedAt: Date.now()
	};
	dashboardStorageSet(dashboardReturnStateKey, JSON.stringify(state));
	return state.url;
}

function readDashboardReturnScroll() {
	var raw = dashboardStorageGet(dashboardReturnStateKey);
	if (!raw) return;
	try {
		var state = JSON.parse(raw);
		var current = window.location.pathname + window.location.search;
		var age = Date.now() - Number(state.savedAt || 0);
		if (state.url !== current || age < 0 || age > 24 * 60 * 60 * 1000) return;
		var scrollTop = Number(state.scrollTop);
		if (!Number.isFinite(scrollTop) || scrollTop < 0) return;
		pendingDashboardReturnScroll = {
			scrollTop: scrollTop,
			windowY: Math.max(0, Number(state.windowY || 0))
		};
	} catch (err) {
		dashboardStorageRemove(dashboardReturnStateKey);
	}
}

function restoreDashboardReturnScrollIfNeeded() {
	if (!pendingDashboardReturnScroll) return;
	var owner = typeof dashboardScrollOwner === 'function' ? dashboardScrollOwner() : null;
	if (!owner) return;
	var target = pendingDashboardReturnScroll;
	pendingDashboardReturnScroll = null;
	dashboardStorageRemove(dashboardReturnStateKey);
	window.requestAnimationFrame(function() {
		var maxTop = Math.max(0, owner.scrollHeight - owner.clientHeight);
		owner.scrollTop = Math.min(target.scrollTop, maxTop);
		if (typeof window.scrollTo === 'function') window.scrollTo(0, target.windowY || 0);
		window.requestAnimationFrame(function() {
			var nextMaxTop = Math.max(0, owner.scrollHeight - owner.clientHeight);
			owner.scrollTop = Math.min(target.scrollTop, nextMaxTop);
			if (typeof window.scrollTo === 'function') window.scrollTo(0, target.windowY || 0);
		});
	});
}

function dashboardTranscriptURL(raw) {
	try {
		var parsed = new URL(String(raw || ''), window.location.origin);
		if (parsed.origin !== window.location.origin) return '';
		if (!/^\/sessions\/[^/]+$/.test(parsed.pathname)) return '';
		return parsed.pathname + parsed.search + parsed.hash;
	} catch (err) {
		return '';
	}
}

function saveDashboardReturnForTranscriptLink(link) {
	if (!link) return;
	var href = link.getAttribute('href') || link.getAttribute('data-href') || '';
	if (!dashboardTranscriptURL(href)) return;
	saveDashboardReturnState(href);
}

document.addEventListener('click', function(evt) {
	var target = evt.target;
	var link = target && target.closest ? target.closest('a[href]') : null;
	if (!link) return;
	if (
		link.id === 'inspector-full-link' ||
		link.closest('#activity-feed') ||
		link.closest('[data-transcript-link]')
	) {
		saveDashboardReturnForTranscriptLink(link);
	}
}, true);

window.writeDashboardStateToURL = writeDashboardStateToURL;
window.scheduleDashboardStateURLWrite = scheduleDashboardStateURLWrite;
window.saveDashboardReturnState = saveDashboardReturnState;
window.restoreDashboardReturnScrollIfNeeded = restoreDashboardReturnScrollIfNeeded;
window.toggleActiveSessionPin = toggleActiveSessionPin;
window.movePinnedActiveSession = movePinnedActiveSession;
window.dashboardScopeValues = dashboardScopeValues;
window.dashboardHasScopeFilters = dashboardHasScopeFilters;
window.setDashboardScope = setDashboardScope;
window.clearDashboardScope = clearDashboardScope;
window.refreshDashboardForScopeChange = refreshDashboardForScopeChange;

activeSessionPrefs = readActiveSessionPrefs();
readDashboardStateFromURL();
readDashboardReturnScroll();
