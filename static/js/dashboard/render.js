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
var relativeTime = dashboardUtils.relativeTime;
var durationSeconds = dashboardUtils.durationSeconds;
var requestURL = dashboardUtils.requestURL;

function modelChip(model) {
	if (!model) return '';
	return '<span class="px-1.5 py-0.5 rounded bg-gray-700/80 text-gray-300 max-w-full truncate" title="' + escapeAttr(model) + '">' + escapeHTML(shortModel(model)) + '</span>';
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

function isDesktopDashboardLayout() {
	if (!document.getElementById('dashboard-wrap')) return false;
	if (window.matchMedia) return window.matchMedia('(min-width: 1101px)').matches;
	return (window.innerWidth || 0) > 1100;
}

function dashboardScrollOwner() {
	return document.getElementById('dashboard-main');
}

function completedTableRegion() {
	var search = document.getElementById('dashboard-search');
	var region = search && search.parentElement ? search.parentElement : null;
	if (region) region.setAttribute('data-dashboard-stable-region', 'completed');
	return region;
}

function stabilizeCompletedTableRegion() {
	var region = completedTableRegion();
	if (!region) return;
	if (!isDesktopDashboardLayout()) {
		region.style.minHeight = '';
		region.removeAttribute('data-dashboard-min-height');
		return;
	}
	var height = Math.ceil(region.getBoundingClientRect().height);
	var remembered = parseFloat(region.getAttribute('data-dashboard-min-height') || '0') || 0;
	var next = Math.max(remembered, height);
	if (next > 0) {
		region.setAttribute('data-dashboard-min-height', String(next));
		region.style.minHeight = next + 'px';
	}
}

function restoreDashboardScroll(owner, scrollTop, windowX, windowY) {
	if (owner) owner.scrollTop = scrollTop;
	if (typeof window.scrollTo === 'function') window.scrollTo(windowX, windowY);
	if (typeof window.requestAnimationFrame === 'function') {
		window.requestAnimationFrame(function() {
			if (owner) owner.scrollTop = scrollTop;
			if (typeof window.scrollTo === 'function') window.scrollTo(windowX, windowY);
		});
	}
}

function withDashboardScrollStability(mutator, options) {
	var desktop = isDesktopDashboardLayout();
	var owner = desktop ? dashboardScrollOwner() : null;
	var scrollTop = owner ? owner.scrollTop : 0;
	var windowX = window.scrollX || window.pageXOffset || 0;
	var windowY = window.scrollY || window.pageYOffset || 0;
	try {
		if (desktop && options && options.completedRegion) stabilizeCompletedTableRegion();
		return mutator();
	} finally {
		if (desktop && options && options.completedRegion) stabilizeCompletedTableRegion();
		if (desktop && owner) restoreDashboardScroll(owner, scrollTop, windowX, windowY);
	}
}

function markDashboardUpdated() {
	var el = document.getElementById('dashboard-last-updated');
	if (el) el.textContent = 'Updated ' + new Date().toLocaleTimeString([], {hour: 'numeric', minute: '2-digit', second: '2-digit'});
}

function setDashboardConnection(status) {
	var el = document.getElementById('dashboard-connection-label');
	if (!el) return;
	el.textContent = status;
	el.className = status === 'Live' ? 'text-green-400' : (status === 'Disconnected' ? 'text-red-400' : 'text-gray-500');
}

function isSearchMode() {
	return currentSearchQuery !== '' || currentSearchRange !== '' || currentSearchEventKind !== '' || currentSearchSessionID !== '';
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
		markDashboardUpdated();
		return {data: data};
	} catch (err) {
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
	var mobileMeta = formatTokens(totalTokens) + ' tok · ' + turnCount + ' turns · ' + toolCount + ' tools · ' + (session.duration || relativeTime(session.ended_at));
	var toggle = '';
	if (!isSubagent && subagentCount > 0) {
		toggle = '<button type="button" class="json-subagent-toggle text-gray-500 hover:text-gray-300 transition-colors flex-shrink-0" data-session-id="' + escapeAttr(session.id) + '" title="' + subagentCount + ' subagents" aria-label="Toggle subagents" aria-expanded="false"><svg class="w-3.5 h-3.5 transition-transform" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7"></path></svg></button>';
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
		'<td class="py-2 px-3 text-right text-xs text-gray-500 tabular-nums whitespace-nowrap">' + relativeTime(session.ended_at) + '</td>' +
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
			var start = offset + 1;
			var end = offset + (response.items || []).length;
			rows.push('<tr class="border-none" data-pagination-row><td colspan="10" class="py-3"><div class="flex items-center justify-center gap-4">' + prev + '<span class="text-xs text-gray-500 tabular-nums">Showing ' + start + '-' + end + (hasMore ? '+' : '') + '<\/span>' + next + '<\/div><\/td><\/tr>');
		}
		if (rows.length === 0) {
			rows.push('<tr><td colspan="10" class="text-center py-4"><span class="text-sm text-gray-500">' + (currentSearchQuery ? 'No sessions match your search' : 'No completed sessions') + '<\/span><\/td><\/tr>');
		}
		if (status) {
			var count = (response.items || []).length;
			var countLabel = count + (hasMore ? '+' : '');
			status.textContent = currentSearchQuery ? (countLabel + ' search result' + (count === 1 && !hasMore ? '' : 's') + ' in ' + rangeLabel(currentRange)) : (countLabel + ' shown for ' + rangeLabel(currentRange));
		}
		var changed = setHTMLIfChanged(tbody, rows.join(''));
		if (changed) updateCompletedSortIndicators();
	}, {completedRegion: true});
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
	return '<tr class="border-b border-gray-800/50 hover:bg-gray-800/40 transition-colors cursor-pointer" data-search-row data-transcript-link="true" data-href="' + escapeAttr(href) + '" data-event-kind="' + escapeAttr(item.event_kind || '') + '" data-session-id="' + escapeAttr(item.session_id || '') + '">' +
		'<td class="py-2 px-3 min-w-[18rem]"><a href="' + escapeAttr(href) + '" data-transcript-link="true" class="dashboard-search-result-link"><div class="flex items-center gap-2 mb-1"><span class="px-1.5 py-0.5 rounded text-[10px] font-semibold uppercase tracking-wide ' + searchEventBadge(item.event_kind) + '">' + escapeHTML(searchEventLabel(item.event_kind)) + '</span>' + tool + '</div><div class="dashboard-search-snippet">' + escapeHTML(item.snippet || '') + '</div></a></td>' +
		'<td class="py-2 px-3 text-xs whitespace-nowrap">' + providerBadge(item.provider) + '</td>' +
		'<td class="py-2 px-3 text-xs text-gray-400 max-w-[160px] truncate" title="' + escapeAttr(item.model || '') + '">' + escapeHTML(shortModel(item.model || '')) + '</td>' +
		'<td class="py-2 px-3 text-xs text-gray-500 max-w-[220px]"><div class="truncate" title="' + escapeAttr(item.session_id || '') + '">' + escapeHTML(sessionLabel) + ' <span class="font-mono text-gray-600">' + escapeHTML(shortID(item.session_id)) + '</span></div>' + project + '</td>' +
		'<td class="py-2 px-3 text-right text-xs text-gray-500 tabular-nums whitespace-nowrap">' + escapeHTML(item.relative_time || relativeTime(item.timestamp)) + '</td>' +
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
			rows.push('<tr class="border-none" data-search-more-row><td colspan="6" class="py-3"><div class="flex items-center justify-center gap-4"><button type="button" class="dashboard-search-show-more px-3 py-1 text-xs rounded border border-gray-600 text-gray-400 hover:text-gray-200 hover:border-gray-500 transition-colors">Show more</button><span class="text-xs text-gray-500 tabular-nums">Showing ' + response.items.length + '+ results</span></div></td></tr>');
		}
		if (rows.length === 0) {
			var message = response.state === 'unavailable'
				? 'Search is not connected'
				: (response.state === 'idle' ? 'Enter a query or filter to search sessions and events' : 'No matching sessions or events');
			rows.push('<tr><td colspan="6" class="text-center py-4"><span class="text-sm text-gray-500">' + escapeHTML(message) + '</span></td></tr>');
		}
		setHTMLIfChanged(tbody, rows.join(''));
		if (status) {
			if (response.state === 'unavailable') {
				status.textContent = 'Search unavailable';
			} else if (response.state === 'idle') {
				status.textContent = 'Search sessions and events from the dashboard table';
			} else {
				var count = response.items.length;
				status.textContent = count + (hasMore ? '+' : '') + ' search result' + (count === 1 && !hasMore ? '' : 's');
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
		setHTMLIfChanged(tbody, '<tr><td colspan="6" class="text-center py-4"><span class="text-sm text-gray-500">Searching sessions and events...</span></td></tr>');
		if (status) status.textContent = 'Searching sessions and events...';
	}, {completedRegion: true});
}

function renderActive(response) {
	withDashboardScrollStability(function() {
		var wrap = document.getElementById('active-sessions');
		var items = (response.items || []).filter(validSession);
		var dot = items.length ? '<span class="relative flex h-2 w-2"><span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-400 opacity-75"></span><span class="relative inline-flex rounded-full h-2 w-2 bg-green-500"></span></span>' : '<span class="relative flex h-2 w-2"><span class="relative inline-flex rounded-full h-2 w-2 bg-gray-600"></span></span>';
		var count = items.length ? '<span class="text-xs font-normal text-gray-500">(' + items.length + ')</span>' : '';
		var cards = items.map(activeCard).join('');
		if (!cards) {
			cards = '<div class="text-center py-6 col-span-full"><p class="text-sm text-gray-500">No active sessions</p><p class="text-xs text-gray-600 mt-1">Sessions appear here when agents are running</p></div>';
		}
		setHTMLIfChanged(wrap, '<h2 class="text-lg font-semibold text-gray-200 mb-3 flex items-center gap-2">' + dot + 'Active Sessions ' + count + '</h2><div class="grid grid-cols-1 lg:grid-cols-2 gap-3">' + cards + '</div>');
	});
}

function activeCard(session) {
	var live = session.status === 'active';
	var sub = !!session.parent_session_id;
	var border = sub ? (live ? 'border-blue-500/50' : 'border-red-500/40') : (live ? 'border-green-500/50' : 'border-red-500/30 border-dashed');
	var liveColor = sub ? 'blue' : 'green';
	var statusDot = live ? '<span class="relative flex h-2 w-2 flex-shrink-0"><span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-' + liveColor + '-400 opacity-75"></span><span class="relative inline-flex rounded-full h-2 w-2 bg-' + liveColor + '-500"></span></span>' : '<span class="relative flex h-2 w-2 flex-shrink-0"><span class="relative inline-flex rounded-full h-2 w-2 bg-red-500/60"></span></span>';
	var totalTokens = numericValue(session.total_tokens, 0);
	var turnCount = nonNegativeInt(session.turn_count);
	var toolCount = nonNegativeInt(session.tool_call_count);
	if (sub) {
		return '<a href="' + escapeAttr('/sessions/' + encodeURIComponent(session.id)) + '" class="block rounded-lg overflow-hidden bg-gray-800/40 border-l-2 px-4 py-3 hover:bg-gray-700/20 transition-colors ' + border + '">' +
			'<div class="flex items-center justify-between gap-3"><div class="flex items-center gap-2 min-w-0">' + statusDot + '<span class="font-medium text-gray-100 truncate">' + escapeHTML(sessionTitle(session)) + '</span><span class="text-xs text-gray-600 font-mono flex-shrink-0">' + escapeHTML(shortID(session.id)) + '</span></div>' +
			'<span class="px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide rounded flex-shrink-0 ' + (live ? 'bg-blue-500/15 text-blue-400' : 'bg-red-500/15 text-red-400') + '">' + (live ? 'Sub' : 'Idle') + '</span></div>' +
			'<div class="flex items-center gap-2 text-xs text-gray-500 mt-1 ml-4 flex-wrap"><span class="text-blue-400/50">↑ ' + escapeHTML(shortID(session.parent_session_id)) + '</span>' + modelChip(session.last_model || '') + '<span>' + escapeHTML(session.duration || '') + '</span><span class="text-gray-700">·</span><span>' + turnCount + ' turns</span><span class="text-gray-700">·</span><span>' + formatTokens(totalTokens) + ' tok</span><span class="text-gray-700">·</span><span>' + toolCount + ' tools</span></div>' +
			(session.working_dir ? '<p class="text-[11px] text-gray-600 truncate mt-0.5 ml-4" title="' + escapeAttr(session.working_dir) + '">' + escapeHTML(session.working_dir) + '</p>' : '') +
			'</a>';
	}
	var childHTML = '';
	var childSessions = (session.child_sessions || []).filter(validSession);
	if (childSessions.length > 0) {
		childHTML = '<div class="border-t border-gray-700/30 px-4 py-2"><div class="text-[10px] uppercase tracking-wider text-blue-400/50 mb-1">' + (childSessions.length === 1 ? '1 subagent' : childSessions.length + ' subagents') + '</div>' + childSessions.map(function(child) {
			var childLive = child.status === 'active';
			var childDot = childLive ? '<span class="relative flex h-1.5 w-1.5 flex-shrink-0"><span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-blue-400 opacity-75"></span><span class="relative inline-flex rounded-full h-1.5 w-1.5 bg-blue-500"></span></span>' : '<span class="w-1.5 h-1.5 rounded-full bg-red-500/60 flex-shrink-0"></span>';
			var childModel = child.last_model ? '<span class="text-gray-400 truncate min-w-0 flex-1" title="' + escapeAttr(child.last_model) + '">' + escapeHTML(shortModel(child.last_model)) + '</span>' : '';
			return '<a href="' + escapeAttr('/sessions/' + encodeURIComponent(child.id)) + '" class="flex items-center gap-2 text-xs py-1 px-2 -mx-1 rounded hover:bg-gray-700/30 transition-colors min-w-0">' + childDot + '<span class="text-gray-500 font-mono flex-shrink-0">' + escapeHTML(shortID(child.id)) + '</span>' + childModel + '<span class="ml-auto flex items-center text-gray-500 tabular-nums flex-shrink-0"><span class="w-14 text-right">' + escapeHTML(child.duration || '') + '</span><span class="w-12 text-right">' + formatTokens(child.total_tokens) + '</span><span class="w-8 text-right">' + nonNegativeInt(child.tool_call_count) + 't</span></span></a>';
		}).join('') + '</div>';
	}
	return '<div class="rounded-lg overflow-hidden border-l-2 bg-gray-800/60 ' + border + '">' +
		'<a href="' + escapeAttr('/sessions/' + encodeURIComponent(session.id)) + '" class="block px-4 py-3 hover:bg-gray-700/20 transition-colors">' +
		'<div class="flex items-center justify-between"><div class="flex items-center gap-2 min-w-0">' + statusDot + '<span class="font-medium text-gray-100 truncate">' + escapeHTML(sessionTitle(session)) + '</span><span class="text-xs text-gray-600 font-mono flex-shrink-0">' + escapeHTML(shortID(session.id)) + '</span></div><div class="flex items-center gap-1.5 flex-shrink-0 ml-2">' + providerBadge(session.provider) + '<span class="px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide rounded ' + (live ? 'bg-green-500/15 text-green-400' : 'bg-red-500/15 text-red-400') + '">' + (live ? 'Live' : 'Idle') + '</span></div></div>' +
		'<div class="flex items-center gap-2 text-xs text-gray-500 mt-1 ml-4 flex-wrap">' + modelChip(session.last_model || '') + '<span>' + escapeHTML(session.duration || '') + '</span><span class="text-gray-700">·</span><span>' + turnCount + ' turns</span><span class="text-gray-700">·</span><span>' + formatTokens(totalTokens) + ' tok</span><span class="text-gray-700">·</span><span>' + toolCount + ' tools</span></div>' +
		(session.working_dir ? '<p class="text-[11px] text-gray-600 truncate mt-0.5 ml-4" title="' + escapeAttr(session.working_dir) + '">' + escapeHTML(session.working_dir) + '</p>' : '') +
		'</a>' + childHTML + '</div>';
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
		setHTMLIfChanged(feed, '<p class="text-sm text-gray-500 text-center py-4">No recent activity</p>');
		return;
	}
	setHTMLIfChanged(feed, '<div class="relative pl-8"><div class="absolute left-3 top-0 bottom-0 w-px bg-gray-700"></div>' + items.map(function(item) {
		var url = '/sessions/' + encodeURIComponent(item.session_id || '') + '#' + encodeURIComponent(item.id || '');
		var provider = item.provider ? '<span class="px-1.5 py-0.5 rounded text-[10px] flex-shrink-0 ' + providerBadgeClasses(item.provider) + '">' + escapeHTML(providerShort(item.provider)) + '</span>' : '';
		var sid = item.session_id ? '<span class="text-xs text-gray-600 font-mono flex-shrink-0">' + escapeHTML(shortID(item.session_id)) + '</span>' : '';
		return '<a href="' + escapeAttr(url) + '" data-type="' + escapeAttr(item.type) + '" data-transcript-link="true" class="block relative py-2 pl-4 hover:bg-gray-800/50 rounded-lg transition-colors group"><div class="absolute left-[-8px] top-3.5 w-2.5 h-2.5 rounded-full ring-2 ring-gray-900 ' + activityDotColor(item.type) + '"></div><p class="text-sm text-gray-300 group-hover:text-gray-100 transition-colors mb-1">' + escapeHTML(item.summary) + '</p><div class="flex items-center gap-2 flex-wrap"><span class="px-1.5 py-0.5 rounded text-xs flex-shrink-0 ' + activityBadgeStyle(item.type) + '">' + escapeHTML(activityLabel(item.type)) + '</span>' + provider + sid + '<span class="text-xs text-gray-600 flex-shrink-0">' + escapeHTML(item.relative_time || relativeTime(item.timestamp)) + '</span></div></a>';
	}).join('') + '</div>');
}

function rangeLabel(value) {
	if (value === '1h') return 'Last hour';
	if (value === '24h') return 'Last 24 hours';
	if (value === '7d') return 'Last 7 days';
	if (value === '30d') return 'Last 30 days';
	return 'All time';
}

function updateRangeCaption() {
	var caption = document.getElementById('dashboard-range-caption');
	if (caption) caption.textContent = rangeLabel(currentRange);
	var title = document.querySelector('#timeline-sidebar h2 span');
	if (title) title.textContent = '(' + (currentRange || 'all') + ')';
}

function summaryTile(label, value, sublabel) {
	return '<div class="rounded-lg border border-gray-700 bg-gray-800/70 px-3 py-2 min-w-0">' +
		'<p class="text-[11px] uppercase tracking-wide text-gray-500">' + escapeHTML(label) + '</p>' +
		'<p class="text-lg font-semibold text-gray-100 tabular-nums truncate">' + escapeHTML(value) + '</p>' +
		'<p class="text-xs text-gray-500 truncate">' + escapeHTML(sublabel || rangeLabel(currentRange)) + '</p>' +
		'</div>';
}

function formatPercent(n) {
	n = numericValue(n, 0);
	if (n >= 10) return n.toFixed(1) + '%';
	if (n > 0) return n.toFixed(2) + '%';
	return '0%';
}

function renderAnalyticsSummary(summary) {
	withDashboardScrollStability(function() {
		var wrap = document.getElementById('dashboard-analytics-summary');
		if (!wrap) return;
		summary = summary || {};
		setHTMLIfChanged(wrap, [
			summaryTile('Tokens', formatTokens(summary.total_tokens), 'Cumulative across shown models'),
			summaryTile('Models', nonNegativeInt(summary.model_count), 'Selectable series'),
			summaryTile('Tool Calls', formatTokens(summary.tool_call_count), rangeLabel(currentRange)),
			summaryTile('Error Rate', formatPercent(summary.error_rate), nonNegativeInt(summary.error_count) + ' errors')
		].join(''));
	});
}

function updateDashboardCharts(payload) {
	if (!payload) return;
	payload.token_cumulative = payload.token_cumulative || {labels: [], datasets: [], summary: {}};
	payload.model_activity = payload.model_activity || {labels: [], summary: {}, metrics: {}};
	var tokenDataEl = document.getElementById('dashboard-token-cumulative-data');
	var activityDataEl = document.getElementById('dashboard-model-activity-data');
	if (tokenDataEl && payload.token_cumulative) tokenDataEl.textContent = JSON.stringify(payload.token_cumulative);
	if (activityDataEl && payload.model_activity) activityDataEl.textContent = JSON.stringify(payload.model_activity);
	if (typeof updateDashboardModelChart === 'function') {
		updateDashboardModelChart('dashboardTokenCumulativeChart', payload.token_cumulative, 'tokens');
	}
	if (typeof updateDashboardModelActivityChart === 'function') {
		updateDashboardModelActivityChart(payload.model_activity, currentDashboardMetric);
	}
	var summary = payload.token_cumulative && payload.token_cumulative.summary ? payload.token_cumulative.summary : null;
	if (!summary && payload.model_activity) summary = payload.model_activity.summary;
	renderAnalyticsSummary(summary);
}

async function loadDashboardCharts() {
	var result = await fetchDashboardJSON('charts', requestURL('/api/dashboard/charts', {range: currentRange}));
	if (!result || result.stale) return;
	if (result.error) {
		updateDashboardCharts({token_cumulative: {labels: [], datasets: [], summary: {}}, model_activity: {labels: [], summary: {}, metrics: {}}});
		return;
	}
	updateDashboardCharts(result.data);
}

async function loadActiveSessions() {
	var result = await fetchDashboardJSON('active', requestURL('/api/dashboard/sessions', {state: 'active'}));
	if (!result || result.stale) return;
	if (result.error) {
		var wrap = document.getElementById('active-sessions');
		setHTMLIfChanged(wrap, '<h2 class="text-lg font-semibold text-gray-200 mb-3">Active Sessions</h2><div class="rounded border border-red-500/30 bg-red-500/10 p-4 text-sm text-red-300">Unable to load active sessions. <button type="button" class="underline" onclick="loadActiveSessions()">Retry</button></div>');
		return;
	}
	var data = result.data;
	rememberSessions(data.items);
	renderActive(data);
}

async function loadDashboardSearch() {
	currentCompletedOffset = 0;
	renderDashboardSearchLoading();
	var result = await fetchDashboardJSON('completed', requestURL('/api/dashboard/search', {
		q: currentSearchQuery,
		range: currentSearchRange,
		event_kind: currentSearchEventKind,
		session_id: currentSearchSessionID,
		sort: currentSearchSort,
		limit: currentSearchLimit
	}));
	if (!result || result.stale) return;
	if (result.error) {
		withDashboardScrollStability(function() {
			var tbody = document.getElementById('completed-sessions');
			var status = document.getElementById('completed-session-status');
			var title = document.getElementById('completed-table-title');
			if (title) title.textContent = 'Search Results';
			setCompletedTableMode('search');
			setHTMLIfChanged(tbody, '<tr><td colspan="6" class="text-center py-4"><span class="text-sm text-red-400">Unable to search sessions and events. <button type="button" class="underline" onclick="loadDashboardSearch()">Retry</button></span></td></tr>');
			if (status) status.textContent = 'Search failed';
		}, {completedRegion: true});
		return;
	}
	renderDashboardSearch(result.data);
}

async function loadCompletedSessions(offset) {
	if (isSearchMode()) {
		return loadDashboardSearch();
	}
	currentCompletedOffset = Math.max(0, offset || 0);
	var status = document.getElementById('completed-session-status');
	if (status) status.textContent = currentSearchQuery ? 'Searching sessions...' : 'Loading sessions...';
	var result = await fetchDashboardJSON('completed', requestURL('/api/dashboard/sessions', {
		state: 'completed',
		limit: completedPageSize,
		offset: currentCompletedOffset,
		range: currentRange,
		sort: sortColumn,
		direction: sortAsc ? 'asc' : 'desc'
	}));
	if (!result || result.stale) return;
	if (result.error) {
		withDashboardScrollStability(function() {
			var tbody = document.getElementById('completed-sessions');
			setHTMLIfChanged(tbody, '<tr><td colspan="10" class="text-center py-4"><span class="text-sm text-red-400">Unable to load completed sessions. <button type="button" class="underline" onclick="loadCompletedSessions(currentCompletedOffset)">Retry</button></span></td></tr>');
			if (status) status.textContent = 'Unable to load sessions';
		}, {completedRegion: true});
		return;
	}
	var data = result.data;
	rememberSessions(data.items);
	renderCompleted(data);
}

async function loadActivity() {
	var result = await fetchDashboardJSON('activity', requestURL('/api/dashboard/activity', {
		range: currentRange,
		event_kind: currentActivityFilter === 'all' ? '' : (currentActivityFilter === 'error' ? 'error,tool_error' : currentActivityFilter)
	}));
	if (!result || result.stale) return;
	if (result.error) {
		setHTMLIfChanged(document.getElementById('activity-feed'), '<p class="text-sm text-red-400 text-center py-4">Unable to load activity. <button type="button" class="underline" onclick="loadActivity()">Retry</button></p>');
		return;
	}
	renderActivity(result.data);
}
