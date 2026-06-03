var dashboardUtils = window.BeaconDashboard.utils;
var escapeHTML = dashboardUtils.escapeHTML;
var escapeAttr = dashboardUtils.escapeAttr;
var numericValue = dashboardUtils.numericValue;
var nonNegativeInt = dashboardUtils.nonNegativeInt;
var cssEscape = dashboardUtils.cssEscape;
var shortID = dashboardUtils.shortID;
var shortModel = dashboardUtils.shortModel;
var providerShort = dashboardUtils.providerShort;
var formatTokens = dashboardUtils.formatTokens;
var absoluteTime = dashboardUtils.absoluteTime;
var durationSeconds = dashboardUtils.durationSeconds;
var requestURL = dashboardUtils.requestURL;

function modelChip(model) {
	if (!model) return '';
	return '<span class="px-1.5 py-0.5 rounded bg-gray-700/80 text-gray-300 min-w-0 max-w-full truncate" title="' + escapeAttr(model) + '">' + escapeHTML(shortModel(model)) + '</span>';
}

function providerBadgeClasses(provider) {
	if (provider === 'anthropic') return 'bg-orange-500/15 text-orange-300';
	if (provider === 'openai') return 'bg-cyan-500/15 text-cyan-400';
	return 'bg-gray-500/15 text-gray-400';
}

function providerBadge(provider) {
	if (!provider) return '';
	return '<span class="px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide rounded ' + providerBadgeClasses(provider) + '" title="' + escapeAttr(providerShort(provider)) + '">' + escapeHTML(providerShort(provider)) + '</span>';
}

function rememberSessions(items) {
	(items || []).filter(validSession).forEach(function(session) {
		window.dashboardSessionIndex[session.id] = session;
		rememberSessions(session.child_sessions || []);
	});
}

function sessionTitle(session) {
	return session.title || shortID(session.id) || 'Session';
}

function validSession(session) {
	return session && typeof session.id === 'string' && session.id.length > 0;
}

function setHTMLIfChanged(el, html) {
	if (!el) return false;
	if (el.__beaconRenderSignature === html) return false;
	// Central dashboard HTML sink. Callers build static markup and must escape
	// dynamic text/attributes with escapeHTML/escapeAttr or normalize numbers.
	el.innerHTML = html;
	el.__beaconRenderSignature = html;
	return true;
}

function setTextIfChanged(el, text) {
	if (!el) return false;
	text = String(text == null ? '' : text);
	if (el.textContent === text) return false;
	el.textContent = text;
	return true;
}

function isDesktopDashboardLayout() {
	if (!document.getElementById('dashboard-wrap')) return false;
	if (window.matchMedia) return window.matchMedia('(min-width: 1101px)').matches;
	return (window.innerWidth || 0) > 1100;
}

function dashboardScrollOwner() {
	return document.getElementById('dashboard-main');
}

var activeSessionScrollAnchorSelector = '#dashboard-search, #completed-table, .dashboard-analytics-panel, #dashboardTokenCumulativeChart';

function dashboardScrollAnchor(owner, selectorList) {
	if (!owner || !selectorList) return null;
	var ownerRect = owner.getBoundingClientRect();
	var selectors = selectorList.split(',');
	for (var i = 0; i < selectors.length; i++) {
		var selector = selectors[i].trim();
		if (!selector) continue;
		var elements = document.querySelectorAll(selector);
		for (var j = 0; j < elements.length; j++) {
			var rect = elements[j].getBoundingClientRect();
			if (rect.width <= 0 || rect.height <= 0) continue;
			if (rect.bottom <= ownerRect.top || rect.top >= ownerRect.bottom) continue;
			return {element: elements[j], top: rect.top};
		}
	}
	return null;
}

function completedTableRegion() {
	var search = document.getElementById('dashboard-search');
	var region = search && search.closest ? search.closest('.completed-table-surface') : null;
	if (!region && search && search.parentElement) region = search.parentElement;
	if (region) region.setAttribute('data-dashboard-stable-region', 'completed');
	return region;
}

function stabilizeCompletedTableRegion(establishFloor) {
	var region = completedTableRegion();
	if (!region) return;
	if (!isDesktopDashboardLayout()) {
		region.style.minHeight = '';
		region.removeAttribute('data-dashboard-height-floor');
		return;
	}
	region.style.minHeight = '';
	var height = Math.ceil(region.getBoundingClientRect().height);
	var floor = parseFloat(region.getAttribute('data-dashboard-height-floor') || '0') || 0;
	if (establishFloor && height > floor) {
		region.setAttribute('data-dashboard-height-floor', String(height));
		floor = height;
	}
	var next = Math.max(height, floor);
	if (next > 0) region.style.minHeight = next + 'px';
}

function restoreDashboardScroll(owner, scrollTop, windowX, windowY, anchor) {
	function apply() {
		if (owner) {
			if (anchor && anchor.element && anchor.element.isConnected) {
				var delta = anchor.element.getBoundingClientRect().top - anchor.top;
				if (Math.abs(delta) > 0.5) owner.scrollTop += delta;
			} else {
				owner.scrollTop = scrollTop;
			}
		}
		if (typeof window.scrollTo === 'function') window.scrollTo(windowX, windowY);
	}
	apply();
	if (typeof window.requestAnimationFrame === 'function') {
		window.requestAnimationFrame(apply);
	}
}

function withDashboardScrollStability(mutator, options) {
	var desktop = isDesktopDashboardLayout();
	var owner = desktop ? dashboardScrollOwner() : null;
	var scrollTop = owner ? owner.scrollTop : 0;
	var anchor = owner && scrollTop > 0 && options && options.anchorSelector ? dashboardScrollAnchor(owner, options.anchorSelector) : null;
	var windowX = window.scrollX || window.pageXOffset || 0;
	var windowY = window.scrollY || window.pageYOffset || 0;
	try {
		if (desktop && options && options.completedRegion) stabilizeCompletedTableRegion(false);
		return mutator();
	} finally {
		if (desktop && options && options.completedRegion) stabilizeCompletedTableRegion(!!options.establishCompletedHeightFloor);
		if (desktop && owner) restoreDashboardScroll(owner, scrollTop, windowX, windowY, anchor);
	}
}

function setDashboardConnection(status) {
	var el = document.getElementById('dashboard-connection-label');
	var indicator = document.getElementById('dashboard-connection-indicator');
	var normalized = ['Live', 'Connecting', 'Static', 'Disconnected'].indexOf(status) >= 0 ? status : 'Connecting';
	var key = normalized.toLowerCase();
	if (el) {
		el.textContent = normalized;
		el.className = normalized === 'Live' ? 'text-green-400' : (normalized === 'Disconnected' ? 'text-red-400' : 'text-gray-500');
	}
	if (indicator) {
		indicator.setAttribute('data-status', key);
		indicator.setAttribute('aria-label', 'Dashboard connection: ' + normalized);
	}
}

function isSearchMode() {
	return currentSearchQuery !== '' ||
		currentSearchEventKind !== 'session' ||
		currentSearchSessionID !== '' ||
		currentSearchSort !== 'relevance' ||
		currentSearchLimit !== 30;
}

function dashboardSearchRequestEventKind() {
	return currentSearchEventKind || 'event';
}

function setCompletedTableMode(mode) {
	var table = document.getElementById('completed-table');
	if (!table) return;
	table.setAttribute('data-table-mode', mode);
	var head = table.querySelector('thead');
	if (!head) return;
	if (mode === 'search') {
		setHTMLIfChanged(head, '<tr class="text-xs text-gray-500 uppercase tracking-wider border-b border-gray-700">' +
			'<th scope="col" class="text-left py-2 px-3 font-medium">Match</th>' +
			'<th scope="col" class="text-left py-2 px-3 font-medium">Provider</th>' +
			'<th scope="col" class="text-left py-2 px-3 font-medium">Model</th>' +
			'<th scope="col" class="text-left py-2 px-3 font-medium">Session</th>' +
			'<th scope="col" class="text-right py-2 px-3 font-medium">Time</th>' +
			'<th scope="col" class="text-right py-2 px-3 font-medium">Score</th>' +
			'</tr>');
		return;
	}
	if (sessionTableHeadHTML) {
		setHTMLIfChanged(head, sessionTableHeadHTML);
	}
}

async function fetchDashboardJSON(key, url) {
	dashboardRequestSeq[key] = (dashboardRequestSeq[key] || 0) + 1;
	var seq = dashboardRequestSeq[key];
	if (dashboardControllers[key]) {
		dashboardControllers[key].abort();
	}
	var controller = window.AbortController ? new AbortController() : null;
	dashboardControllers[key] = controller;
	try {
		var opts = {headers: {'Accept': 'application/json'}};
		if (controller) opts.signal = controller.signal;
		var res = await fetch(url, opts);
		if (dashboardRequestSeq[key] !== seq) return {stale: true};
		if (!res.ok) return {error: true, status: res.status};
		var data = await res.json();
		if (dashboardRequestSeq[key] !== seq) return {stale: true};
		return {data: data};
	} catch (err) {
		if (dashboardRequestSeq[key] !== seq) return {stale: true};
		if (err && err.name === 'AbortError') return {stale: true};
		return {error: true};
	} finally {
		if (dashboardControllers[key] === controller) {
			dashboardControllers[key] = null;
		}
	}
}

function completedRow(session, isSubagent, parentID) {
	var subagentCount = nonNegativeInt(session.subagent_count);
	var totalTokens = numericValue(session.total_tokens, 0);
	var turnCount = nonNegativeInt(session.turn_count);
	var toolCount = nonNegativeInt(session.tool_call_count);
	var endedTime = new Date(session.ended_at || 0).getTime();
	var endedSort = Number.isFinite(endedTime) ? Math.floor(endedTime / 1000) : 0;
	var rowClass = isSubagent ? 'border-b border-gray-800/50 cursor-pointer transition-colors bg-gray-800/20' : 'border-b border-gray-800/50 cursor-pointer transition-colors';
	var nameCellClass = isSubagent ? 'py-1.5 px-3 text-sm text-gray-400 whitespace-nowrap pl-10' : 'py-2 px-3 text-sm text-gray-300 whitespace-nowrap';
	var endedLabel = absoluteTime(session.ended_at);
	var mobileMeta = formatTokens(totalTokens) + ' tok · ' + turnCount + ' turns · ' + toolCount + ' tools · ' + (session.duration || endedLabel);
	var toggle = '';
	if (!isSubagent && subagentCount > 0) {
		toggle = '<button type="button" class="json-subagent-toggle text-gray-500 hover:text-gray-300 transition-colors flex-shrink-0" data-session-id="' + escapeAttr(session.id) + '" title="' + subagentCount + ' subagents" aria-label="Toggle ' + subagentCount + ' subagents for ' + escapeAttr(sessionTitle(session)) + '" aria-expanded="false"><svg class="w-3.5 h-3.5 transition-transform" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7"></path></svg></button>';
	}
	var subPrefix = isSubagent ? '<span class="w-1.5 h-1.5 rounded-full bg-blue-400/50 flex-shrink-0"></span><span class="text-blue-400/70 text-xs">sub</span>' : '';
	var subCount = !isSubagent && subagentCount > 0 ? '<span class="text-[10px] text-blue-400/60 font-normal">+' + subagentCount + ' sub</span>' : '';
	var sessionURL = '/sessions/' + encodeURIComponent(session.id);
	var escapedSessionURL = escapeAttr(sessionURL);
	var titleButton = '<button type="button" class="session-row-open text-left transition-colors hover:text-blue-300 focus-visible:text-blue-300" data-session-link="' + escapedSessionURL + '" aria-label="Open session ' + escapeAttr(sessionTitle(session)) + '">' + escapeHTML(sessionTitle(session)) + '</button>';
	var rowActionAttrs = ' data-session-link="' + escapedSessionURL + '"';
	var attrs = isSubagent ? ' data-parent="' + escapeAttr(parentID) + '"' : ' id="session-row-' + escapeAttr(session.id) + '"' +
		' data-sort-name="' + escapeAttr(sessionTitle(session)) + '"' +
		' data-sort-provider="' + escapeAttr(providerShort(session.provider)) + '"' +
		' data-sort-model="' + escapeAttr(session.last_model || '') + '"' +
		' data-sort-tokens="' + totalTokens + '"' +
		' data-sort-turns="' + turnCount + '"' +
		' data-sort-tools="' + toolCount + '"' +
		' data-sort-duration="' + durationSeconds(session) + '"' +
		' data-sort-project="' + escapeAttr(session.working_dir || '') + '"' +
		' data-sort-ended="' + endedSort + '"' +
		' data-sort-id="' + escapeAttr(session.id) + '"';
	return '<tr' + attrs + rowActionAttrs + ' class="' + rowClass + '">' +
		'<td class="' + nameCellClass + '"><span class="inline-flex items-center gap-1.5">' + toggle + subPrefix + titleButton + subCount + '</span><span class="mobile-session-meta hidden">' + escapeHTML(mobileMeta) + '</span></td>' +
		'<td class="py-2 px-3 text-xs whitespace-nowrap">' + (isSubagent ? '' : providerBadge(session.provider)) + '</td>' +
		'<td class="py-2 px-3 text-xs text-gray-400 max-w-[160px] truncate" title="' + escapeAttr(session.last_model || '') + '">' + escapeHTML(shortModel(session.last_model || '')) + '</td>' +
		'<td class="py-2 px-3 text-right text-xs text-gray-400 tabular-nums">' + formatTokens(totalTokens) + '</td>' +
		'<td class="py-2 px-3 text-right text-xs text-gray-400 tabular-nums">' + turnCount + '</td>' +
		'<td class="py-2 px-3 text-right text-xs text-gray-400 tabular-nums">' + toolCount + '</td>' +
		'<td class="py-2 px-3 text-right text-xs text-gray-400 tabular-nums whitespace-nowrap">' + escapeHTML(session.duration || '') + '</td>' +
		'<td class="py-2 px-3 text-xs text-gray-500 max-w-[180px] truncate" title="' + escapeAttr(session.working_dir || '') + '">' + escapeHTML(session.working_dir || '') + '</td>' +
		'<td class="py-2 px-3 text-right text-xs text-gray-500 tabular-nums whitespace-nowrap">' + escapeHTML(endedLabel) + '</td>' +
		'<td class="py-2 px-3 text-xs text-gray-600 font-mono whitespace-nowrap">' + escapeHTML(shortID(session.id)) + '</td>' +
		'</tr>';
}

function renderCompleted(response) {
	var offset = nonNegativeInt(response.offset);
	var limit = Math.max(1, nonNegativeInt(response.limit, completedPageSize));
	var hasMore = !!response.has_more;
	response.items = (response.items || []).filter(validSession);
	if ((response.items || []).length === 0 && offset > 0) {
		loadCompletedSessions(Math.max(0, offset - limit));
		return;
	}
	withDashboardScrollStability(function() {
		var tbody = document.getElementById('completed-sessions');
		var title = document.getElementById('completed-table-title');
		if (title) title.textContent = 'Completed Sessions';
		setCompletedTableMode('sessions');
		var rows = (response.items || []).map(function(session) { return completedRow(session, false, ''); });
		var status = document.getElementById('completed-session-status');
		if ((response.items || []).length > 0 || offset > 0) {
			var prev = offset > 0 ? '<button type="button" class="json-page-btn px-3 py-1 text-xs rounded border border-gray-600 text-gray-400 hover:text-gray-200 hover:border-gray-500 transition-colors" data-offset="' + Math.max(0, offset - limit) + '">Previous</button>' : '';
			var next = hasMore ? '<button type="button" class="json-page-btn px-3 py-1 text-xs rounded border border-gray-600 text-gray-400 hover:text-gray-200 hover:border-gray-500 transition-colors" data-offset="' + (offset + limit) + '">Next</button>' : '';
			if (prev || next) {
				rows.push('<tr class="border-none" data-pagination-row><td colspan="10" class="py-3"><div class="flex items-center justify-center gap-4">' + prev + next + '<\/div><\/td><\/tr>');
			}
		}
		if (rows.length === 0) {
			rows.push('<tr><td colspan="10" class="text-center py-4"><span class="text-sm text-gray-500">' + (currentSearchQuery ? 'No sessions match your search' : 'No completed sessions') + '<\/span><\/td><\/tr>');
		}
		setTextIfChanged(status, rangeLabel(completedRangeValue()));
		var changed = setHTMLIfChanged(tbody, rows.join(''));
		if (changed) updateCompletedSortIndicators();
	}, {completedRegion: true, establishCompletedHeightFloor: true});
}

function searchEventLabel(kind) {
	if (kind === 'session') return 'Session';
	if (kind === 'tool_call') return 'Tool call';
	if (kind === 'tool_result') return 'Tool result';
	if (kind === 'tool_error') return 'Tool error';
	if (kind === 'reasoning') return 'Reasoning';
	if (kind === 'error') return 'Error';
	if (kind === 'message') return 'Message';
	return kind || 'Event';
}

function searchEventBadge(kind) {
	if (kind === 'session') return 'bg-gray-600/40 text-gray-300';
	if (kind === 'tool_call') return 'bg-yellow-500/15 text-yellow-400';
	if (kind === 'tool_result') return 'bg-emerald-500/15 text-emerald-400';
	if (kind === 'tool_error') return 'bg-orange-500/15 text-orange-400';
	if (kind === 'reasoning') return 'bg-violet-500/15 text-violet-400';
	if (kind === 'error') return 'bg-red-500/15 text-red-400';
	if (kind === 'message') return 'bg-blue-500/15 text-blue-400';
	return 'bg-gray-600/40 text-gray-400';
}

function searchResultHref(item) {
	var base = '/sessions/' + encodeURIComponent(item.session_id || '');
	if (item.event_uid) return base + '#' + encodeURIComponent(item.event_uid);
	return base;
}

function searchRow(item) {
	var href = searchResultHref(item);
	var sessionLabel = item.session_title || shortID(item.session_id) || 'Session';
	var project = item.working_dir ? '<div class="text-[11px] text-gray-600 truncate" title="' + escapeAttr(item.working_dir) + '">' + escapeHTML(item.working_dir) + '</div>' : '';
	var tool = item.tool_name ? '<span class="text-[11px] text-gray-500 truncate" title="' + escapeAttr(item.tool_name) + '">' + escapeHTML(item.tool_name) + '</span>' : '';
	var scoreValue = numericValue(item.score, 0);
	var score = currentSearchSort === 'relevance' && scoreValue > 0 ? scoreValue.toFixed(2) : '';
	var timeLabel = absoluteTime(item.timestamp);
	return '<tr class="border-b border-gray-800/50 hover:bg-gray-800/40 transition-colors cursor-pointer" data-search-row data-transcript-link="true" data-href="' + escapeAttr(href) + '" data-event-kind="' + escapeAttr(item.event_kind || '') + '" data-session-id="' + escapeAttr(item.session_id || '') + '">' +
		'<td class="py-2 px-3 min-w-[18rem]"><a href="' + escapeAttr(href) + '" data-transcript-link="true" class="dashboard-search-result-link"><div class="flex items-center gap-2 mb-1"><span class="px-1.5 py-0.5 rounded text-[10px] font-semibold uppercase tracking-wide ' + searchEventBadge(item.event_kind) + '">' + escapeHTML(searchEventLabel(item.event_kind)) + '</span>' + tool + '</div><div class="dashboard-search-snippet">' + escapeHTML(item.snippet || '') + '</div></a></td>' +
		'<td class="py-2 px-3 text-xs whitespace-nowrap">' + providerBadge(item.provider) + '</td>' +
		'<td class="py-2 px-3 text-xs text-gray-400 max-w-[160px] truncate" title="' + escapeAttr(item.model || '') + '">' + escapeHTML(shortModel(item.model || '')) + '</td>' +
		'<td class="py-2 px-3 text-xs text-gray-500 max-w-[220px]"><div class="truncate" title="' + escapeAttr(item.session_id || '') + '">' + escapeHTML(sessionLabel) + ' <span class="font-mono text-gray-600">' + escapeHTML(shortID(item.session_id)) + '</span></div>' + project + '</td>' +
		'<td class="py-2 px-3 text-right text-xs text-gray-500 tabular-nums whitespace-nowrap">' + escapeHTML(timeLabel) + '</td>' +
		'<td class="py-2 px-3 text-right text-xs text-gray-500 tabular-nums">' + escapeHTML(score) + '</td>' +
		'</tr>';
}

function renderDashboardSearch(response) {
	response.items = response.items || [];
	withDashboardScrollStability(function() {
		var tbody = document.getElementById('completed-sessions');
		var status = document.getElementById('completed-session-status');
		var title = document.getElementById('completed-table-title');
		if (title) title.textContent = 'Search Results';
		setCompletedTableMode('search');
		var hasMore = !!response.has_more;
		var rows = response.items.map(searchRow);
		if (hasMore) {
			rows.push('<tr class="border-none" data-search-more-row><td colspan="6" class="py-3"><div class="flex items-center justify-center gap-4"><button type="button" class="dashboard-search-show-more px-3 py-1 text-xs rounded border border-gray-600 text-gray-400 hover:text-gray-200 hover:border-gray-500 transition-colors">Show more</button></div></td></tr>');
		}
		if (rows.length === 0) {
			var message = response.state === 'unavailable'
				? 'Search is not connected'
				: (response.state === 'idle' ? 'Enter a query or filter to search table sessions and events' : 'No matching table sessions or events');
			rows.push('<tr><td colspan="6" class="text-center py-4"><span class="text-sm text-gray-500">' + escapeHTML(message) + '</span></td></tr>');
		}
		setHTMLIfChanged(tbody, rows.join(''));
		if (status) {
			if (response.state === 'unavailable') {
				setTextIfChanged(status, 'Search unavailable');
			} else if (response.state === 'idle') {
				setTextIfChanged(status, 'Search table sessions and events');
			} else {
				setTextIfChanged(status, rangeLabel(completedRangeValue()));
			}
		}
	}, {completedRegion: true});
}

function renderDashboardSearchLoading() {
	withDashboardScrollStability(function() {
		var tbody = document.getElementById('completed-sessions');
		var status = document.getElementById('completed-session-status');
		var title = document.getElementById('completed-table-title');
		if (title) title.textContent = 'Search Results';
		setCompletedTableMode('search');
		setHTMLIfChanged(tbody, '<tr><td colspan="6" class="text-center py-4"><span class="text-sm text-gray-500">Searching table sessions and events...</span></td></tr>');
		setTextIfChanged(status, 'Searching table sessions and events...');
	}, {completedRegion: true});
}

function renderActive(response) {
	withDashboardScrollStability(function() {
		var wrap = document.getElementById('active-sessions');
		if (!wrap) return;
		var items = (response.items || []).filter(validSession);
		lastActiveSessionsResponse = response;
		var dot = items.length ? '<span class="relative flex h-2 w-2"><span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-400 opacity-75"></span><span class="relative inline-flex rounded-full h-2 w-2 bg-green-500"></span></span>' : '<span class="relative flex h-2 w-2"><span class="relative inline-flex rounded-full h-2 w-2 bg-gray-600"></span></span>';
		var count = items.length ? '<span class="text-xs font-normal text-gray-500">(' + items.length + ')</span>' : '';
		var sorted = sortActiveSessions(items);
		var pinned = currentActiveSessionPinnedIDs(sorted);
		var cards = sorted.map(function(session) {
			return activeCard(session, {
				pinnedIndex: pinned.indexOf(session.id),
				pinnedCount: pinned.length
			});
		}).join('');
		if (!cards) {
			cards = '<div class="active-session-empty"><p class="text-sm text-gray-500">No active sessions</p><p class="text-xs text-gray-600 mt-1">Sessions appear here when agents are running</p></div>';
		}
		renderActiveShell(wrap, '<h2 id="active-sessions-title" class="text-lg font-semibold text-gray-200 flex items-center gap-2">' + dot + 'Active Sessions ' + count + '</h2>' + activeSortControl(), '<div class="active-session-grid">' + cards + '</div>');
	}, {anchorSelector: activeSessionScrollAnchorSelector});
}

function renderActiveShell(wrap, headingHTML, bodyHTML) {
	var heading = wrap.querySelector('.active-session-heading');
	var body = wrap.querySelector('.active-session-board-scroll');
	if (!heading || !body) {
		setHTMLIfChanged(wrap, '<div class="active-session-heading">' + headingHTML + '</div><div class="active-session-board-scroll">' + bodyHTML + '</div>');
		return;
	}
	setHTMLIfChanged(heading, headingHTML);
	setHTMLIfChanged(body, bodyHTML);
}

var activeSortLabels = {
	recent: 'Recently updated',
	longest: 'Longest running',
	tokens: 'Most tokens',
	tools: 'Most tool calls',
	errors: 'Errors first'
};

function activeSortValues() {
	return typeof dashboardActiveSorts !== 'undefined' ? dashboardActiveSorts : ['recent', 'longest', 'tokens', 'tools', 'errors'];
}

function activeSortValue() {
	var sorts = activeSortValues();
	var current = typeof currentActiveSort !== 'undefined' ? currentActiveSort : 'recent';
	return sorts.indexOf(current) >= 0 ? current : 'recent';
}

function activeSortControl() {
	var current = activeSortValue();
	var options = activeSortValues().map(function(value) {
		return '<option value="' + escapeAttr(value) + '"' + (value === current ? ' selected' : '') + '>' + escapeHTML(activeSortLabels[value] || value) + '</option>';
	}).join('');
	return '<label class="active-session-sort"><span class="sr-only">Active session sort order</span><select id="active-session-sort" class="active-session-sort-select" aria-label="Active session sort order" onchange="setActiveSessionSort(this.value)">' + options + '</select></label>';
}

function currentActiveSessionPinnedIDs(items) {
	if (typeof activeSessionPinnedIDs === 'function') return activeSessionPinnedIDs(items);
	return [];
}

function activeDurationFromLabel(session) {
	var total = 0;
	String(session && session.duration || '').replace(/(\d+)\s*([hms])/g, function(_, amount, unit) {
		var value = Number(amount) || 0;
		if (unit === 'h') total += value * 3600;
		else if (unit === 'm') total += value * 60;
		else total += value;
		return '';
	});
	return total;
}

function activeMetric(session, sort) {
	if (sort === 'longest') return activeDurationFromLabel(session) || durationSeconds(session);
	if (sort === 'tokens') return nonNegativeInt(session.total_tokens);
	if (sort === 'tools') return nonNegativeInt(session.tool_call_count);
	if (sort === 'errors') return nonNegativeInt(session.error_count);
	var updated = session.ended_at || session.started_at || '';
	var time = updated ? new Date(updated).getTime() : 0;
	return Number.isFinite(time) ? time : 0;
}

function compareActiveSessionRecords(a, b) {
	var sort = activeSortValue();
	var diff = activeMetric(b.session, sort) - activeMetric(a.session, sort);
	if (diff !== 0) return diff;
	return a.index - b.index;
}

function sortActiveSessions(items) {
	var byID = {};
	items.forEach(function(session) {
		byID[session.id] = session;
	});
	var pinnedIDs = currentActiveSessionPinnedIDs(items);
	var pinnedLookup = {};
	pinnedIDs.forEach(function(id) {
		pinnedLookup[id] = true;
	});
	var unpinned = items.filter(function(session) {
		return !pinnedLookup[session.id];
	})
		.map(function(session, index) { return {session: session, index: index}; })
		.sort(compareActiveSessionRecords)
		.map(function(record) { return record.session; });
	var pinned = pinnedIDs.map(function(id) {
		return byID[id];
	}).filter(validSession);
	if (pinned.length === 0) return unpinned;
	return pinned.concat(unpinned);
}

function setActiveSessionSort(value) {
	var sorts = activeSortValues();
	currentActiveSort = sorts.indexOf(value) >= 0 ? value : 'recent';
	if (typeof scheduleDashboardStateURLWrite === 'function') scheduleDashboardStateURLWrite();
	loadActiveSessions();
}

function activeStatusDot(live, sub) {
	if (!live) return '<span class="active-session-dot active-session-dot-idle" aria-hidden="true"></span>';
	if (sub) return '<span class="relative flex h-2.5 w-2.5 flex-shrink-0" aria-hidden="true"><span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-blue-400 opacity-75"></span><span class="relative inline-flex rounded-full h-2.5 w-2.5 bg-blue-500"></span></span>';
	return '<span class="relative flex h-2.5 w-2.5 flex-shrink-0" aria-hidden="true"><span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-400 opacity-75"></span><span class="relative inline-flex rounded-full h-2.5 w-2.5 bg-green-500"></span></span>';
}

function activeTrackerCell(label, value, tone) {
	var toneClass = tone ? ' active-tracker-cell-' + escapeAttr(tone) : '';
	return '<div class="active-tracker-cell' + toneClass + '"><span class="active-tracker-label">' + escapeHTML(label) + '</span><strong class="active-tracker-value">' + escapeHTML(value) + '</strong></div>';
}

function compactActiveDuration(duration) {
	var text = String(duration || '').trim();
	var hours = text.match(/^(\d+)\s*h(?:\s+(\d+)\s*m)?/);
	if (hours) return hours[1] + 'h' + (hours[2] ? ' ' + hours[2] + 'm' : '');
	var minutes = text.match(/^(\d+)\s*m/);
	if (minutes) return minutes[1] + 'm';
	var seconds = text.match(/^(\d+)\s*s/);
	if (seconds) return seconds[1] + 's';
	return text;
}

function activeSessionTracker(session, turnCount, toolCount, errorCount) {
	var cells = [
		activeTrackerCell('Run', compactActiveDuration(session.duration), ''),
		activeTrackerCell('Turns', String(turnCount), ''),
		activeTrackerCell('Tools', String(toolCount), '')
	];
	if (errorCount > 0) {
		cells.push(activeTrackerCell('Errors', String(errorCount), 'error'));
	}
	return '<div class="active-session-tracker" aria-label="Active session live stats">' + cells.join('') + '</div>';
}

function activeSessionActionIcon(name) {
	if (name === 'pin') {
		return '<svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 17v5"></path><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 17h14"></path><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 3h8l-1 7 3 4H6l3-4-1-7z"></path></svg>';
	}
	if (name === 'up') {
		return '<svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 19V5"></path><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 12l7-7 7 7"></path></svg>';
	}
	return '<svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 5v14"></path><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 12l-7 7-7-7"></path></svg>';
}

function activeSessionActionButton(action, session, label, title, disabled, active, icon) {
	return '<button type="button" class="active-session-action-btn' + (active ? ' active-session-action-btn-active' : '') + '" data-active-session-action="' + escapeAttr(action) + '" data-active-session-id="' + escapeAttr(session.id) + '" aria-label="' + escapeAttr(label) + '" title="' + escapeAttr(title) + '"' + (disabled ? ' disabled aria-disabled="true"' : '') + '>' + activeSessionActionIcon(icon) + '</button>';
}

function activeSessionActions(session, context) {
	context = context || {};
	var pinnedIndex = typeof context.pinnedIndex === 'number' ? context.pinnedIndex : -1;
	var pinnedCount = typeof context.pinnedCount === 'number' ? context.pinnedCount : 0;
	var pinned = pinnedIndex >= 0;
	var title = sessionTitle(session);
	return '<div class="active-session-actions" aria-label="Active session row controls">' +
		activeSessionActionButton('toggle-pin', session, (pinned ? 'Unpin ' : 'Pin ') + title, pinned ? 'Unpin session' : 'Pin to top', false, pinned, 'pin') +
		activeSessionActionButton('move-up', session, 'Move pinned session up: ' + title, 'Move pinned session up', !pinned || pinnedIndex === 0, false, 'up') +
		activeSessionActionButton('move-down', session, 'Move pinned session down: ' + title, 'Move pinned session down', !pinned || pinnedIndex === pinnedCount - 1, false, 'down') +
		'</div>';
}

function activeSessionLinkContent(session, statusDot, live, sub, turnCount, toolCount, errorCount) {
	var statusClass = sub ? 'active-session-status-sub' : (live ? 'active-session-status-live' : 'active-session-status-idle');
	var statusLabel = live ? 'Live' : 'Idle';
	var kicker = sub
		? '<span>Subagent</span><span class="font-mono">' + escapeHTML(shortID(session.id)) + '</span><span>parent ' + escapeHTML(shortID(session.parent_session_id)) + '</span>'
		: '<span class="font-mono">' + escapeHTML(shortID(session.id)) + '</span><span>' + escapeHTML(session.status || '') + '</span>';
	var path = session.working_dir ? '<span class="active-session-path" title="' + escapeAttr(session.working_dir) + '">' + escapeHTML(session.working_dir) + '</span>' : '';
	var meta = modelChip(session.last_model || '') + providerBadge(session.provider) + '<span class="active-session-status ' + statusClass + '">' + statusLabel + '</span><span class="active-session-kicker">' + kicker + '</span>' + path;
	return '<div class="active-session-card-header">' +
			'<div class="active-session-title-row">' + statusDot + '<div class="active-session-title">' + escapeHTML(sessionTitle(session)) + '</div></div>' +
			activeSessionTracker(session, turnCount, toolCount, errorCount) +
		'</div>' +
		'<div class="active-session-inline-meta">' + meta + '</div>';
}

function activeCard(session, context) {
	var live = session.status === 'active';
	var sub = !!session.parent_session_id;
	var border = sub ? (live ? 'active-session-card-sub' : 'active-session-card-idle') : (live ? 'active-session-card-live' : 'active-session-card-idle');
	var statusDot = activeStatusDot(live, sub);
	var turnCount = nonNegativeInt(session.turn_count);
	var toolCount = nonNegativeInt(session.tool_call_count);
	var errorCount = nonNegativeInt(session.error_count);
	var pinned = context && typeof context.pinnedIndex === 'number' && context.pinnedIndex >= 0;
	var attrs = ' data-active-session-id="' + escapeAttr(session.id) + '" data-active-pinned="' + (pinned ? 'true' : 'false') + '"';
	var link = '<a href="' + escapeAttr('/sessions/' + encodeURIComponent(session.id)) + '" class="active-session-link">' + activeSessionLinkContent(session, statusDot, live, sub, turnCount, toolCount, errorCount) + '</a>';
	var shell = '<div class="active-session-row-shell">' + link + activeSessionActions(session, context) + '</div>';
	if (sub) {
		return '<div class="active-session-card ' + border + '"' + attrs + '>' + shell + '</div>';
	}
	var childHTML = '';
	var childSessions = (session.child_sessions || []).filter(validSession);
	if (childSessions.length > 0) {
		childHTML = '<div class="active-child-list"><div class="active-child-heading">' + (childSessions.length === 1 ? '1 subagent' : childSessions.length + ' subagents') + '</div>' + childSessions.map(function(child) {
			var childLive = child.status === 'active';
			var childDot = activeStatusDot(childLive, true);
			var childModel = child.last_model ? '<span class="text-gray-400 truncate min-w-0 flex-1" title="' + escapeAttr(child.last_model) + '">' + escapeHTML(shortModel(child.last_model)) + '</span>' : '';
			return '<a href="' + escapeAttr('/sessions/' + encodeURIComponent(child.id)) + '" class="active-child-row"><div class="active-child-main">' + childDot + '<span class="active-child-id">' + escapeHTML(shortID(child.id)) + '</span>' + childModel + '<span class="active-child-stat">' + escapeHTML(child.duration || '') + '</span><span class="active-child-stat">' + nonNegativeInt(child.tool_call_count) + 't</span></div></a>';
		}).join('') + '</div>';
	}
	return '<div class="active-session-card ' + border + '"' + attrs + '>' + shell + childHTML + '</div>';
}

function activityDotColor(type) {
	if (type === 'message') return 'bg-blue-400';
	if (type === 'tool_call') return 'bg-yellow-400';
	if (type === 'error') return 'bg-red-400';
	if (type === 'tool_error') return 'bg-orange-400';
	if (type === 'session_meta') return 'bg-teal-400';
	return 'bg-gray-400';
}

function activityBadgeStyle(type) {
	if (type === 'message') return 'bg-blue-500/15 text-blue-400';
	if (type === 'tool_call') return 'bg-yellow-500/15 text-yellow-400';
	if (type === 'error') return 'bg-red-500/15 text-red-400';
	if (type === 'tool_error') return 'bg-orange-500/15 text-orange-400';
	if (type === 'session_meta') return 'bg-teal-500/15 text-teal-400';
	return 'bg-gray-600/40 text-gray-400';
}

function activityLabel(type) {
	if (type === 'tool_call') return 'tool';
	if (type === 'error') return 'api error';
	if (type === 'tool_error') return 'tool error';
	if (type === 'session_meta') return 'session';
	return type || 'event';
}

function renderActivity(items) {
	var feed = document.getElementById('activity-feed');
	if (!items || items.length === 0) {
		setHTMLIfChanged(feed, '<p class="activity-bar-state">No recent activity</p>');
		return;
	}
	setHTMLIfChanged(feed, '<div class="activity-bar-list"><div class="activity-bar-rail" aria-hidden="true"></div>' + items.map(function(item) {
		var url = '/sessions/' + encodeURIComponent(item.session_id || '') + '#' + encodeURIComponent(item.id || '');
		var provider = item.provider ? '<span class="px-1.5 py-0.5 rounded text-[10px] flex-shrink-0 ' + providerBadgeClasses(item.provider) + '">' + escapeHTML(providerShort(item.provider)) + '</span>' : '';
		var sid = item.session_id ? '<span class="text-xs text-gray-600 font-mono flex-shrink-0">' + escapeHTML(shortID(item.session_id)) + '</span>' : '';
		return '<a href="' + escapeAttr(url) + '" data-type="' + escapeAttr(item.type) + '" data-transcript-link="true" class="activity-bar-item group">' +
			'<div class="activity-bar-dot ' + activityDotColor(item.type) + '"></div>' +
			'<p class="activity-bar-summary">' + escapeHTML(item.summary) + '</p>' +
			'<div class="activity-bar-meta"><span class="px-1.5 py-0.5 rounded text-xs flex-shrink-0 ' + activityBadgeStyle(item.type) + '">' + escapeHTML(activityLabel(item.type)) + '</span>' + provider + sid + '</div>' +
			'</a>';
	}).join('') + '</div>');
}

function rangeLabel(value) {
	if (value === '1h') return 'Last hour';
	if (value === '24h') return 'Last 24 hours';
	if (value === '7d') return 'Last 7 days';
	if (value === '30d') return 'Last 30 days';
	return 'All time';
}

function completedRangeValue() {
	if (typeof currentCompletedRange !== 'undefined') return currentCompletedRange;
	if (typeof currentRange !== 'undefined') return currentRange;
	return '24h';
}

function activityRangeValue() {
	if (typeof currentActivityRange !== 'undefined') return currentActivityRange;
	return completedRangeValue();
}

function updateRangeCaption() {
	var caption = document.getElementById('dashboard-range-caption');
	if (caption) caption.textContent = rangeLabel(completedRangeValue());
	var title = document.querySelector('#timeline-sidebar .activity-bar-range');
	if (title) title.textContent = '(' + (activityRangeValue() || 'all') + ')';
}

function chartRangeValue() {
	return typeof currentChartRange !== 'undefined' ? currentChartRange : completedRangeValue();
}

function updateChartRangeCaption(state) {
	var caption = document.getElementById('dashboard-chart-range-caption');
	if (!caption) return;
	var label = rangeLabel(chartRangeValue());
	if (state === 'loading') caption.textContent = 'Loading ' + label;
	else if (state === 'error') caption.textContent = 'Unable to load ' + label;
	else if (state === 'empty') caption.textContent = label + ' · no token data';
	else caption.textContent = label;
}

function setAnalyticsBusy(busy) {
	var panel = document.querySelector('.dashboard-analytics-panel');
	if (panel) panel.setAttribute('aria-busy', busy ? 'true' : 'false');
}

function summaryTile(label, value, sublabel) {
	return '<div class="dashboard-summary-tile">' +
		'<p class="dashboard-summary-label">' + escapeHTML(label) + '</p>' +
		'<p class="dashboard-summary-value">' + escapeHTML(value) + '</p>' +
		'<p class="dashboard-summary-subvalue">' + escapeHTML(sublabel || rangeLabel(chartRangeValue())) + '</p>' +
		'</div>';
}

function formatPercent(n) {
	n = numericValue(n, 0);
	if (n >= 10) return n.toFixed(1) + '%';
	if (n > 0) return n.toFixed(2) + '%';
	return '0%';
}

function chartPointValue(point) {
	if (point && typeof point === 'object') return numericValue(point.y, 0);
	return numericValue(point, 0);
}

function renderAnalyticsSummary(summary) {
	withDashboardScrollStability(function() {
		var wrap = document.getElementById('dashboard-analytics-summary');
		if (!wrap) return;
		summary = summary || {};
		var chartRangeLabel = rangeLabel(chartRangeValue());
		setHTMLIfChanged(wrap, [
			summaryTile('Tokens', formatTokens(summary.total_tokens), chartRangeLabel),
			summaryTile('Models', nonNegativeInt(summary.model_count), 'Series'),
			summaryTile('Tool Calls', formatTokens(summary.tool_call_count), chartRangeLabel),
			summaryTile('Error Rate', formatPercent(summary.error_rate), nonNegativeInt(summary.error_count) + ' errors')
		].join(''));
	});
}

function updateDashboardCharts(payload) {
	if (!payload) return;
	payload.token_cumulative = payload.token_cumulative || {labels: [], datasets: [], summary: {}};
	setAnalyticsBusy(false);
	var tokenDataEl = document.getElementById('dashboard-token-cumulative-data');
	if (tokenDataEl && payload.token_cumulative) tokenDataEl.textContent = JSON.stringify(payload.token_cumulative);
	if (typeof updateDashboardModelChart === 'function') {
		updateDashboardModelChart('dashboardTokenCumulativeChart', payload.token_cumulative, 'tokens');
	}
	var summary = payload.token_cumulative && payload.token_cumulative.summary ? payload.token_cumulative.summary : null;
	renderAnalyticsSummary(summary);
	var datasets = payload.token_cumulative && payload.token_cumulative.datasets ? payload.token_cumulative.datasets : [];
	var hasSeriesData = datasets.some(function(dataset) {
		var points = dataset.data || dataset.values || [];
		return points.some(function(value) { return chartPointValue(value) > 0; });
	});
	updateChartRangeCaption(hasSeriesData ? '' : 'empty');
}

async function loadDashboardCharts() {
	setAnalyticsBusy(true);
	updateChartRangeCaption('loading');
	var result = await fetchDashboardJSON('charts', requestURL('/api/dashboard/charts', {chart_range: chartRangeValue()}));
	if (!result || result.stale) return;
	if (result.error) {
		updateDashboardCharts({token_cumulative: {labels: [], datasets: [], summary: {}}});
		updateChartRangeCaption('error');
		return;
	}
	updateDashboardCharts(result.data);
}

async function loadActiveSessions() {
	var result = await fetchDashboardJSON('active', requestURL('/api/dashboard/sessions', {state: 'active'}));
	if (!result || result.stale) return;
	if (result.error) {
		withDashboardScrollStability(function() {
			var wrap = document.getElementById('active-sessions');
			if (!wrap) return;
			renderActiveShell(wrap, '<h2 id="active-sessions-title" class="text-lg font-semibold text-gray-200 flex items-center gap-2">Active Sessions</h2>' + activeSortControl(), '<div class="rounded border border-red-500/30 bg-red-500/10 p-4 text-sm text-red-300">Unable to load active sessions. <button type="button" class="underline" onclick="loadActiveSessions()">Retry</button></div>');
		}, {anchorSelector: activeSessionScrollAnchorSelector});
		return;
	}
	var data = result.data;
	rememberSessions(data.items);
	renderActive(data);
}

async function loadDashboardSearch(options) {
	options = options || {};
	var silent = !!options.silent;
	if (silent && (dashboardControllers.completed || dashboardSearchTimer)) return;
	currentCompletedOffset = 0;
	if (typeof scheduleDashboardStateURLWrite === 'function') scheduleDashboardStateURLWrite();
	if (!silent) renderDashboardSearchLoading();
	var result = await fetchDashboardJSON('completed', requestURL('/api/dashboard/search', {
		q: currentSearchQuery,
		completed_range: completedRangeValue(),
		event_kind: dashboardSearchRequestEventKind(),
		session_id: currentSearchSessionID,
		sort: currentSearchSort,
		limit: currentSearchLimit
	}));
	if (!result || result.stale) return;
	if (result.error) {
		if (silent) return;
		withDashboardScrollStability(function() {
			var tbody = document.getElementById('completed-sessions');
			var status = document.getElementById('completed-session-status');
			var title = document.getElementById('completed-table-title');
			if (title) title.textContent = 'Search Results';
			setCompletedTableMode('search');
			setHTMLIfChanged(tbody, '<tr><td colspan="6" class="text-center py-4"><span class="text-sm text-red-400">Unable to search table sessions and events. <button type="button" class="underline" onclick="loadDashboardSearch()">Retry</button></span></td></tr>');
			setTextIfChanged(status, 'Search failed');
		}, {completedRegion: true});
		return;
	}
	renderDashboardSearch(result.data);
}

async function loadCompletedSessions(offset, options) {
	options = options || {};
	if (isSearchMode()) {
		return loadDashboardSearch(options);
	}
	if (options.silent && (dashboardControllers.completed || dashboardSearchTimer)) return;
	currentCompletedOffset = Math.max(0, offset || 0);
	if (typeof scheduleDashboardStateURLWrite === 'function') scheduleDashboardStateURLWrite();
	var status = document.getElementById('completed-session-status');
	if (status && !options.silent) setTextIfChanged(status, currentSearchQuery ? 'Searching completed sessions...' : 'Loading sessions...');
	var result = await fetchDashboardJSON('completed', requestURL('/api/dashboard/sessions', {
		state: 'completed',
		limit: completedPageSize,
		offset: currentCompletedOffset,
		completed_range: completedRangeValue(),
		sort: sortColumn,
		direction: sortAsc ? 'asc' : 'desc'
	}));
	if (!result || result.stale) return;
	if (result.error) {
		if (options.silent) return;
		withDashboardScrollStability(function() {
			var tbody = document.getElementById('completed-sessions');
			setHTMLIfChanged(tbody, '<tr><td colspan="10" class="text-center py-4"><span class="text-sm text-red-400">Unable to load completed sessions. <button type="button" class="underline" onclick="loadCompletedSessions(currentCompletedOffset)">Retry</button></span></td></tr>');
			setTextIfChanged(status, 'Unable to load sessions');
		}, {completedRegion: true});
		return;
	}
	var data = result.data;
	rememberSessions(data.items);
	renderCompleted(data);
}

async function loadActivity() {
	var result = await fetchDashboardJSON('activity', requestURL('/api/dashboard/activity', {
		activity_range: activityRangeValue(),
		event_kind: currentActivityFilter === 'all' ? '' : (currentActivityFilter === 'error' ? 'error,tool_error' : currentActivityFilter)
	}));
	if (!result || result.stale) return;
	if (result.error) {
		setHTMLIfChanged(document.getElementById('activity-feed'), '<p class="text-sm text-red-400 text-center py-4">Unable to load activity. <button type="button" class="underline" onclick="loadActivity()">Retry</button></p>');
		return;
	}
	renderActivity(result.data);
}
