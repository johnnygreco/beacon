// --- JSON dashboard stores ---
var currentActivityFilter = 'all';
var currentCompletedRange = '24h';
var currentActivityRange = '24h';
var currentActivityRangePinned = false;
var currentRange = currentCompletedRange;
var currentChartRange = '24h';
var currentActiveSort = 'recent';
var currentCompletedOffset = 0;
var currentSearchQuery = '';
var currentSearchEventKind = '';
var currentSearchSessionID = '';
var currentSearchSort = 'relevance';
var currentSearchLimit = 30;
var dashboardSearchTimer = 0;
var completedPageSize = 30;
var sortColumn = 'ended';
var sortAsc = false;
var dashboardRequestSeq = {};
var dashboardControllers = {};
var sessionTableHeadHTML = '';
var searchLimitSteps = [30, 60, 120, 240];
var dashboardURLWriteTimer = 0;
var pendingDashboardReturnScroll = null;
window.dashboardSessionIndex = window.dashboardSessionIndex || {};

var dashboardReturnStateKey = 'beacon-dashboard-return-state-v1';
var dashboardRanges = ['', '1h', '24h', '7d', '30d'];
var dashboardActiveSorts = ['recent', 'longest', 'tokens', 'tools', 'errors'];
var dashboardSearchEventKinds = ['', 'message', 'tool_call', 'tool_result', 'reasoning', 'error'];
var dashboardSearchSorts = ['relevance', 'newest', 'oldest'];
var dashboardSortColumns = ['name', 'provider', 'model', 'tokens', 'turns', 'tools', 'duration', 'project', 'ended', 'id'];
var dashboardActivityFilters = ['all', 'message', 'tool_call', 'error'];

function dashboardStorageGet(key) {
	try { return window.sessionStorage.getItem(key); } catch (err) { return null; }
}

function dashboardStorageSet(key, value) {
	try { window.sessionStorage.setItem(key, value); } catch (err) {}
}

function dashboardStorageRemove(key) {
	try { window.sessionStorage.removeItem(key); } catch (err) {}
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
	currentActivityRangePinned = dashboardHasValidEnumParam(params, 'activity_range', dashboardRanges, {all: ''});
	currentActivityRange = currentActivityRangePinned
		? dashboardEnumParam(params, 'activity_range', currentActivityRange, dashboardRanges, {all: ''})
		: currentCompletedRange;
	currentActiveSort = dashboardEnumParam(params, 'active_sort', currentActiveSort, dashboardActiveSorts);
	currentSearchQuery = (params.get('q') || '').trim();
	currentSearchEventKind = dashboardEnumParam(params, 'event_kind', currentSearchEventKind, dashboardSearchEventKinds);
	currentSearchSessionID = (params.get('session_id') || '').trim();
	currentSearchSort = dashboardEnumParam(params, 'search_sort', currentSearchSort, dashboardSearchSorts);
	currentSearchLimit = dashboardIntParam(params, 'search_limit', currentSearchLimit, searchLimitSteps);
	currentCompletedOffset = dashboardIntParam(params, 'offset', currentCompletedOffset);
	sortColumn = dashboardEnumParam(params, 'sort', sortColumn, dashboardSortColumns);
	var dir = dashboardEnumParam(params, 'dir', sortAsc ? 'asc' : 'desc', ['asc', 'desc']);
	sortAsc = dir === 'asc';
	currentActivityFilter = dashboardEnumParam(params, 'activity', currentActivityFilter, dashboardActivityFilters);
}

function dashboardStatePath() {
	var url = new URL('/', window.location.origin);
	var params = url.searchParams;
	if (currentCompletedRange !== '24h') params.set('range', currentCompletedRange === '' ? 'all' : currentCompletedRange);
	if (currentChartRange !== '24h') params.set('chart_range', currentChartRange === '' ? 'all' : currentChartRange);
	if (currentActivityRangePinned || currentActivityRange !== currentCompletedRange) params.set('activity_range', currentActivityRange === '' ? 'all' : currentActivityRange);
	if (currentActiveSort !== 'recent') params.set('active_sort', currentActiveSort);
	if (currentSearchQuery) params.set('q', currentSearchQuery);
	if (currentSearchEventKind) params.set('event_kind', currentSearchEventKind);
	if (currentSearchSessionID) params.set('session_id', currentSearchSessionID);
	if (currentSearchSort !== 'relevance') params.set('search_sort', currentSearchSort);
	if (currentSearchLimit !== 30) params.set('search_limit', String(currentSearchLimit));
	if (currentCompletedOffset > 0) params.set('offset', String(currentCompletedOffset));
	if (sortColumn !== 'ended' || sortAsc) {
		params.set('sort', sortColumn);
		params.set('dir', sortAsc ? 'asc' : 'desc');
	}
	if (currentActivityFilter !== 'all') params.set('activity', currentActivityFilter);
	return url.pathname + url.search;
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

readDashboardStateFromURL();
readDashboardReturnScroll();
