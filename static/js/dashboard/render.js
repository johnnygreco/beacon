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
var dashboardAdvancedTopology = false;

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

function runtimeLabel(value, fallback) {
	value = String(value || '').trim();
	if (!value) value = String(fallback || '').trim();
	if (!value) return 'unknown';
	if (value === 'claude-code') return 'Claude Code';
	if (value === 'codex') return 'Codex';
	if (value === 'hermes-agent') return 'Hermes';
	return value.replace(/[-_]+/g, ' ');
}

function nodeLabel(value) {
	value = String(value || '').trim();
	if (!value || value === 'local') return 'Local';
	return value;
}

function nodeBadge(value, interactive) {
	var raw = String(value || '').trim();
	var label = nodeLabel(raw);
	if (interactive === false || !raw) return '<span class="dashboard-machine-only dashboard-scope-inline-chip dashboard-scope-inline-chip-static">' + escapeHTML(label) + '</span>';
	return '<button type="button" class="dashboard-machine-only dashboard-scope-inline-chip" data-dashboard-scope-field="node_id" data-dashboard-scope-value="' + escapeAttr(raw) + '" aria-label="Filter dashboard to node ' + escapeAttr(label) + '" title="Filter dashboard to node ' + escapeAttr(label) + '">' + escapeHTML(label) + '</button>';
}

function runtimeBadge(value, fallback, interactive) {
	var raw = String(value || '').trim();
	var label = runtimeLabel(raw, fallback);
	if (interactive === false || !raw) return '<span class="dashboard-scope-inline-chip dashboard-scope-inline-chip-static dashboard-scope-inline-chip-runtime">' + escapeHTML(label) + '</span>';
	return '<button type="button" class="dashboard-scope-inline-chip dashboard-scope-inline-chip-runtime" data-dashboard-scope-field="runtime" data-dashboard-scope-value="' + escapeAttr(raw) + '" aria-label="Filter dashboard to runtime ' + escapeAttr(label) + '" title="Filter dashboard to runtime ' + escapeAttr(label) + '">' + escapeHTML(label) + '</button>';
}

function attentionStateLabel(state) {
	if (state === 'error') return 'Needs attention';
	if (state === 'stale') return 'Stale';
	if (state === 'expensive') return 'Expensive';
	if (state === 'blocked') return 'Blocked';
	if (state === 'running') return 'Running';
	if (state === 'idle') return 'Idle';
	if (state === 'completed') return 'Completed';
	if (state === 'archived') return 'Archived';
	return 'Unknown';
}

function attentionBadge(session) {
	var state = String(session.attention_state || '').trim();
	var reasons = Array.isArray(session.attention_reasons) ? session.attention_reasons : [];
	if (!state || state === 'running' || state === 'completed' || state === 'unknown') return '';
	var tone = state === 'error' || state === 'blocked' ? 'danger' : (state === 'stale' || state === 'expensive' ? 'warn' : 'neutral');
	var title = reasons.length ? reasons.join(', ') : attentionStateLabel(state);
	return '<span class="dashboard-attention-badge dashboard-attention-badge-' + escapeAttr(tone) + '" title="' + escapeAttr(title) + '">' + escapeHTML(attentionStateLabel(state)) + '</span>';
}

function costLabel(session) {
	var cost = numericValue(session.total_cost_usd, 0);
	var provenance = String(session.cost_provenance || '').trim();
	if (cost <= 0 && (!provenance || provenance === 'none')) return '';
	var label = cost > 0 ? ('$' + cost.toFixed(cost >= 10 ? 2 : 4)) : 'cost n/a';
	var detail = provenance && provenance !== 'none' ? provenance.replace(/_/g, ' ') : 'no cost events';
	return '<span class="dashboard-cost-chip" title="' + escapeAttr(detail) + '">' + escapeHTML(label) + '</span>';
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
	var floor = parseFloat(region.getAttribute('data-dashboard-height-floor') || '0') || 0;
	if (!establishFloor && floor > 0) {
		region.style.minHeight = floor + 'px';
		return;
	}
	region.style.minHeight = '';
	var height = Math.ceil(region.getBoundingClientRect().height);
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

function hasDashboardSearchFilter() {
	return currentSearchQuery !== '' ||
		currentSearchEventKind !== 'session' ||
		currentSearchSessionID !== '';
}

function isSearchMode() {
	return (currentSearchEventKind || 'session') !== 'session';
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
	var errorCount = nonNegativeInt(session.error_count);
	var endedTime = new Date(session.ended_at || 0).getTime();
	var endedSort = Number.isFinite(endedTime) ? Math.floor(endedTime / 1000) : 0;
	var node = nodeLabel(session.node_id);
	var runtime = runtimeLabel(session.runtime, session.source || session.provider);
	var rowClass = isSubagent ? 'border-b border-gray-800/50 cursor-pointer transition-colors bg-gray-800/20' : 'border-b border-gray-800/50 cursor-pointer transition-colors';
	var nameCellClass = isSubagent ? 'py-1.5 px-3 text-sm text-gray-400 whitespace-nowrap pl-10' : 'py-2 px-3 text-sm text-gray-300 whitespace-nowrap';
	var endedLabel = absoluteTime(session.ended_at);
	var mobileNodeMeta = '<span class="dashboard-machine-only">' + escapeHTML(node) + ' · </span>';
	var mobileMeta = mobileNodeMeta + escapeHTML(runtime + ' · ' + formatTokens(totalTokens) + ' tok · ' + toolCount + ' tools · ' + errorCount + ' err · ' + (session.duration || endedLabel));
	var toggle = '';
	if (!isSubagent && subagentCount > 0) {
		toggle = '<button type="button" class="json-subagent-toggle text-gray-500 hover:text-gray-300 transition-colors flex-shrink-0" data-session-id="' + escapeAttr(session.id) + '" title="' + subagentCount + ' subagents" aria-label="Toggle ' + subagentCount + ' subagents for ' + escapeAttr(sessionTitle(session)) + '" aria-expanded="false"><svg class="w-3.5 h-3.5 transition-transform" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7"></path></svg></button>';
	}
	var subPrefix = isSubagent ? '<span class="w-1.5 h-1.5 rounded-full bg-blue-400/50 flex-shrink-0"></span><span class="text-blue-400/70 text-xs">sub</span>' : '';
	var subCount = !isSubagent && subagentCount > 0 ? '<span class="text-[10px] text-blue-400/60 font-normal">+' + subagentCount + ' sub</span>' : '';
	var sessionURL = requestURL('/sessions/' + encodeURIComponent(session.id), {});
	var escapedSessionURL = escapeAttr(sessionURL);
	var titleButton = '<button type="button" class="session-row-open text-left transition-colors hover:text-blue-300 focus-visible:text-blue-300" data-session-link="' + escapedSessionURL + '" aria-label="Open session ' + escapeAttr(sessionTitle(session)) + '">' + escapeHTML(sessionTitle(session)) + '</button>';
	var rowActionAttrs = ' data-session-link="' + escapedSessionURL + '"';
	var attrs = isSubagent ? ' data-parent="' + escapeAttr(parentID) + '"' : ' id="session-row-' + escapeAttr(session.id) + '"' +
		' data-sort-name="' + escapeAttr(sessionTitle(session)) + '"' +
		' data-sort-node="' + escapeAttr(node) + '"' +
		' data-sort-runtime="' + escapeAttr(runtime) + '"' +
		' data-sort-model="' + escapeAttr(session.last_model || '') + '"' +
		' data-sort-tokens="' + totalTokens + '"' +
		' data-sort-turns="' + turnCount + '"' +
		' data-sort-tools="' + toolCount + '"' +
		' data-sort-errors="' + errorCount + '"' +
		' data-sort-duration="' + durationSeconds(session) + '"' +
		' data-sort-project="' + escapeAttr(session.working_dir || '') + '"' +
		' data-sort-ended="' + endedSort + '"' +
		' data-sort-id="' + escapeAttr(session.id) + '"';
	return '<tr' + attrs + rowActionAttrs + ' class="' + rowClass + '">' +
		'<td class="' + nameCellClass + '"><span class="inline-flex items-center gap-1.5">' + toggle + subPrefix + titleButton + subCount + '</span><span class="mobile-session-meta hidden">' + mobileMeta + '</span></td>' +
		'<td class="py-2 px-3 text-xs whitespace-nowrap dashboard-machine-only">' + (isSubagent ? '' : nodeBadge(session.node_id)) + '</td>' +
		'<td class="py-2 px-3 text-xs whitespace-nowrap">' + (isSubagent ? '' : runtimeBadge(session.runtime, session.source || session.provider)) + '</td>' +
		'<td class="py-2 px-3 text-xs text-gray-400 max-w-[160px] truncate" title="' + escapeAttr(session.last_model || '') + '">' + escapeHTML(shortModel(session.last_model || '')) + '</td>' +
		'<td class="py-2 px-3 text-right text-xs text-gray-400 tabular-nums">' + formatTokens(totalTokens) + '</td>' +
		'<td class="py-2 px-3 text-right text-xs text-gray-400 tabular-nums">' + turnCount + '</td>' +
		'<td class="py-2 px-3 text-right text-xs text-gray-400 tabular-nums">' + toolCount + '</td>' +
		'<td class="py-2 px-3 text-right text-xs text-gray-400 tabular-nums">' + errorCount + '</td>' +
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
				rows.push('<tr class="border-none" data-pagination-row><td colspan="12" class="py-3"><div class="flex items-center justify-center gap-4">' + prev + next + '<\/div><\/td><\/tr>');
			}
		}
		if (rows.length === 0) {
			rows.push('<tr><td colspan="12" class="text-center py-4"><span class="text-sm text-gray-500">' + (currentSearchQuery ? 'No sessions match your search' : 'No completed sessions') + '<\/span><\/td><\/tr>');
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
	if (item.event_uid) return requestURL(base + '#' + encodeURIComponent(item.event_uid), {});
	return requestURL(base, {});
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
		var cards = sorted.map(function(session, index) {
			return activeCard(session, {
				pinnedIndex: pinned.indexOf(session.id),
				orderIndex: index,
				orderCount: sorted.length
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
	return '<label class="active-session-sort"><span class="sr-only">Active session sort order</span><select id="active-session-sort" class="active-session-sort-select" aria-label="Active session sort order">' + options + '</select></label>';
}

function currentActiveSessionPinnedIDs(items) {
	if (typeof activeSessionPinnedIDs === 'function') return activeSessionPinnedIDs(items);
	return [];
}

function currentActiveSessionOrderIDs(items) {
	if (typeof activeSessionOrderIDs === 'function') return activeSessionOrderIDs(items);
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
	var sorted = pinned.length === 0 ? unpinned : pinned.concat(unpinned);
	var orderIDs = currentActiveSessionOrderIDs(items);
	if (orderIDs.length === 0) return sorted;
	var orderedLookup = {};
	var ordered = orderIDs.map(function(id) {
		orderedLookup[id] = true;
		return byID[id];
	}).filter(validSession);
	return ordered.concat(sorted.filter(function(session) {
		return !orderedLookup[session.id];
	}));
}

function setActiveSessionSort(value) {
	var sorts = activeSortValues();
	currentActiveSort = sorts.indexOf(value) >= 0 ? value : 'recent';
	if (typeof clearActiveSessionManualOrder === 'function') clearActiveSessionManualOrder();
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
	return '<div class="active-session-tracker" role="group" aria-label="Active session live stats">' + cells.join('') + '</div>';
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
	var orderIndex = typeof context.orderIndex === 'number' ? context.orderIndex : -1;
	var orderCount = typeof context.orderCount === 'number' ? context.orderCount : 0;
	var pinned = pinnedIndex >= 0;
	var title = sessionTitle(session);
	var moveButtons = orderCount > 1
		? activeSessionActionButton('move-up', session, 'Move session up: ' + title, 'Move session up', orderIndex <= 0, false, 'up') +
			activeSessionActionButton('move-down', session, 'Move session down: ' + title, 'Move session down', orderIndex < 0 || orderIndex >= orderCount - 1, false, 'down')
		: '';
	return '<div class="active-session-actions" role="group" aria-label="Active session row controls">' +
		activeSessionActionButton('toggle-pin', session, (pinned ? 'Unpin ' : 'Pin ') + title, pinned ? 'Unpin session' : 'Pin to top', false, pinned, 'pin') +
		moveButtons +
		'</div>';
}

function activeSessionScopeControls(session) {
	var controls = nodeBadge(session.node_id, true) + runtimeBadge(session.runtime, session.source || session.provider, true);
	if (!controls) return '';
	return '<div class="active-session-scope-controls" role="group" aria-label="Active session scope filters">' + controls + '</div>';
}

function activeSessionLinkContent(session, statusDot, live, sub, turnCount, toolCount, errorCount) {
	var statusClass = sub ? 'active-session-status-sub' : (live ? 'active-session-status-live' : 'active-session-status-idle');
	var statusLabel = live ? 'Live' : 'Idle';
	var kicker = sub
		? '<span>Subagent</span><span class="font-mono">' + escapeHTML(shortID(session.id)) + '</span><span>parent ' + escapeHTML(shortID(session.parent_session_id)) + '</span>'
		: '<span class="font-mono">' + escapeHTML(shortID(session.id)) + '</span><span>' + escapeHTML(session.status || '') + '</span>';
	var path = session.working_dir ? '<span class="active-session-path" title="' + escapeAttr(session.working_dir) + '">' + escapeHTML(session.working_dir) + '</span>' : '';
	var project = session.project_key ? '<span class="active-session-project" title="' + escapeAttr(session.project_path || session.working_dir || session.project_key) + '">' + escapeHTML(session.project_key) + '</span>' : '';
	var meta = nodeBadge(session.node_id, false) + runtimeBadge(session.runtime, session.source || session.provider, false) + modelChip(session.last_model || '') + providerBadge(session.provider) + attentionBadge(session) + costLabel(session) + '<span class="active-session-status ' + statusClass + '">' + statusLabel + '</span><span class="active-session-kicker">' + kicker + '</span>' + project + path;
	return '<div class="active-session-card-header">' +
			'<div class="active-session-title-row">' + statusDot + '<div class="active-session-title">' + escapeHTML(sessionTitle(session)) + '</div></div>' +
			activeSessionTracker(session, turnCount, toolCount, errorCount) +
		'</div>' +
		'<div class="active-session-inline-meta">' + meta + '</div>';
}

function activeCard(session, context) {
	var live = session.status === 'active';
	var sub = !!session.parent_session_id;
	var attentionState = String(session.attention_state || '').trim();
	var border = attentionState === 'error' || attentionState === 'blocked'
		? 'active-session-card-attention'
		: (sub ? (live ? 'active-session-card-sub' : 'active-session-card-idle') : (live ? 'active-session-card-live' : 'active-session-card-idle'));
	var statusDot = activeStatusDot(live, sub);
	var turnCount = nonNegativeInt(session.turn_count);
	var toolCount = nonNegativeInt(session.tool_call_count);
	var errorCount = nonNegativeInt(session.error_count);
	var pinned = context && typeof context.pinnedIndex === 'number' && context.pinnedIndex >= 0;
	var attrs = ' data-active-session-id="' + escapeAttr(session.id) + '" data-active-pinned="' + (pinned ? 'true' : 'false') + '"';
	var link = '<a href="' + escapeAttr(requestURL('/sessions/' + encodeURIComponent(session.id), {})) + '" class="active-session-link">' + activeSessionLinkContent(session, statusDot, live, sub, turnCount, toolCount, errorCount) + '</a>';
	var controls = '<div class="active-session-controls-column">' + activeSessionScopeControls(session) + activeSessionActions(session, context) + '</div>';
	var shell = '<div class="active-session-row-shell">' + link + controls + '</div>';
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
			return '<a href="' + escapeAttr(requestURL('/sessions/' + encodeURIComponent(child.id), {})) + '" class="active-child-row"><div class="active-child-main">' + childDot + '<span class="active-child-id">' + escapeHTML(shortID(child.id)) + '</span>' + childModel + '<span class="active-child-stat">' + escapeHTML(child.duration || '') + '</span><span class="active-child-stat">' + nonNegativeInt(child.tool_call_count) + 't</span></div></a>';
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
		var url = requestURL('/sessions/' + encodeURIComponent(item.session_id || '') + '#' + encodeURIComponent(item.id || ''), {});
		var provider = item.provider ? '<span class="px-1.5 py-0.5 rounded text-[10px] flex-shrink-0 ' + providerBadgeClasses(item.provider) + '">' + escapeHTML(providerShort(item.provider)) + '</span>' : '';
		var node = item.node_id ? '<span class="dashboard-machine-only px-1.5 py-0.5 rounded text-[10px] flex-shrink-0 bg-gray-700/70 text-gray-300">' + escapeHTML(nodeLabel(item.node_id)) + '</span>' : '';
		var runtime = item.runtime ? '<span class="px-1.5 py-0.5 rounded text-[10px] flex-shrink-0 bg-blue-500/10 text-blue-300">' + escapeHTML(runtimeLabel(item.runtime)) + '</span>' : '';
		var sid = item.session_id ? '<span class="text-xs text-gray-600 font-mono flex-shrink-0">' + escapeHTML(shortID(item.session_id)) + '</span>' : '';
		return '<a href="' + escapeAttr(url) + '" data-type="' + escapeAttr(item.type) + '" data-transcript-link="true" class="activity-bar-item group">' +
			'<div class="activity-bar-dot ' + activityDotColor(item.type) + '"></div>' +
			'<p class="activity-bar-summary">' + escapeHTML(item.summary) + '</p>' +
			'<div class="activity-bar-meta"><span class="px-1.5 py-0.5 rounded text-xs flex-shrink-0 ' + activityBadgeStyle(item.type) + '">' + escapeHTML(activityLabel(item.type)) + '</span>' + node + runtime + provider + sid + '</div>' +
			'</a>';
	}).join('') + '</div>');
}

function formatBytes(value) {
	value = numericValue(value, 0);
	if (value >= 1024 * 1024 * 1024) return (value / (1024 * 1024 * 1024)).toFixed(1) + ' GiB';
	if (value >= 1024 * 1024) return (value / (1024 * 1024)).toFixed(1) + ' MiB';
	if (value >= 1024) return (value / 1024).toFixed(1) + ' KiB';
	return String(Math.round(value)) + ' B';
}

function fleetStatusLabel(status) {
	if (status === 'online') return 'Online';
	if (status === 'active') return 'Active';
	if (status === 'stale') return 'Stale';
	return 'Offline';
}

function fleetStatusClass(status) {
	if (status === 'online') return 'dashboard-fleet-status-online';
	if (status === 'active') return 'dashboard-fleet-status-active';
	if (status === 'stale') return 'dashboard-fleet-status-stale';
	return 'dashboard-fleet-status-offline';
}

function headerMetric(label, value, tone) {
	return '<span class="dashboard-header-fleet-pill' + (tone ? ' dashboard-header-fleet-pill-' + escapeAttr(tone) : '') + '"><strong>' + escapeHTML(value) + '</strong><span>' + escapeHTML(label) + '</span></span>';
}

function dashboardHasMachineAttention(response) {
	response = response || {};
	var totals = response.totals || {};
	if (nonNegativeInt(totals.stale_collectors) > 0) return true;
	if (nonNegativeInt(totals.offline_collectors) > 0) return true;
	if (nonNegativeInt(totals.missing_heartbeat_collectors) > 0) return true;
	if (nonNegativeInt(totals.queue_depth) > 0) return true;
	if (nonNegativeInt(totals.spool_bytes) > 0) return true;
	if (nonNegativeInt(totals.heartbeat_error_count) > 0) return true;
	return (Array.isArray(response.nodes) ? response.nodes : []).some(function(node) {
		if (!node) return false;
		var status = String(node.status || node.heartbeat_status || '').trim();
		if (status && status !== 'online' && status !== 'active') return true;
		if (nonNegativeInt(node.missing_heartbeat_collectors) > 0) return true;
		if (nonNegativeInt(node.queue_depth) > 0) return true;
		if (nonNegativeInt(node.spool_bytes) > 0) return true;
		if (nonNegativeInt(node.heartbeat_error_count) > 0) return true;
		return (Array.isArray(node.sources_detail) ? node.sources_detail : []).some(function(source) {
			if (!source) return false;
			var sourceStatus = String(source.status || '').trim();
			return (sourceStatus && sourceStatus !== 'online' && sourceStatus !== 'active') ||
				nonNegativeInt(source.queue_depth) > 0 ||
				nonNegativeInt(source.spool_bytes) > 0 ||
				nonNegativeInt(source.error_count) > 0;
		});
	});
}

function dashboardShouldShowMachinePanel(response) {
	response = response || {};
	var totals = response.totals || {};
	var nodes = Array.isArray(response.nodes) ? response.nodes : [];
	if (typeof dashboardHasScopeFilters === 'function' && dashboardHasScopeFilters()) return true;
	if (nodes.length > 1) return true;
	if (nonNegativeInt(totals.node_count) > 1) return true;
	if (nonNegativeInt(totals.collector_count) > 1) return true;
	if (nodes.some(function(node) { return nonNegativeInt(node && node.collector_count) > 1; })) return true;
	return dashboardHasMachineAttention(response);
}

function setDashboardTopologyMode(advanced) {
	advanced = !!advanced;
	var changed = dashboardAdvancedTopology !== advanced;
	dashboardAdvancedTopology = advanced;
	var wrap = document.getElementById('dashboard-wrap');
	if (wrap) {
		wrap.classList.toggle('dashboard-advanced-topology', advanced);
		wrap.classList.toggle('dashboard-local-topology', !advanced);
	}
	var panel = document.getElementById('dashboard-fleet');
	if (panel) {
		panel.classList.toggle('hidden', !advanced);
		panel.setAttribute('aria-hidden', advanced ? 'false' : 'true');
	}
	var header = document.getElementById('dashboard-header-fleet-metrics');
	if (header) {
		header.classList.toggle('hidden', !advanced);
		header.setAttribute('aria-hidden', advanced ? 'false' : 'true');
		if (!advanced) setHTMLIfChanged(header, '');
	}
	return changed;
}

function renderFleetHeader(totals) {
	var wrap = document.getElementById('dashboard-header-fleet-metrics');
	if (!wrap) return;
	totals = totals || {};
	var offline = nonNegativeInt(totals.offline_collectors) + nonNegativeInt(totals.stale_collectors);
	var metrics = [
		headerMetric('active', String(nonNegativeInt(totals.active_sessions)), ''),
		headerMetric('online', String(nonNegativeInt(totals.online_collectors)), 'ok'),
		headerMetric('stale/offline', String(offline), offline > 0 ? 'warn' : ''),
		headerMetric('tokens', formatTokens(totals.total_tokens), ''),
		headerMetric('attention', String(nonNegativeInt(totals.attention_sessions)), nonNegativeInt(totals.attention_sessions) > 0 ? 'danger' : '')
	];
	if (nonNegativeInt(totals.missing_heartbeat_collectors) > 0) {
		metrics.splice(3, 0, headerMetric('missing heartbeat', String(nonNegativeInt(totals.missing_heartbeat_collectors)), 'warn'));
	}
	setHTMLIfChanged(wrap, metrics.join(''));
}

function fleetScopeChip(field, value, label, display, kind, accessibleDisplay) {
	value = String(value || '').trim();
	if (!value) return '';
	display = String(display || value).trim() || value;
	accessibleDisplay = String(accessibleDisplay || display).trim() || display;
	kind = String(kind || field || '').trim();
	return '<button type="button" class="dashboard-fleet-chip dashboard-fleet-chip-' + escapeAttr(kind) + (kind === 'runtime' ? ' dashboard-fleet-runtime' : '') + '" data-dashboard-scope-field="' + escapeAttr(field) + '" data-dashboard-scope-value="' + escapeAttr(value) + '" aria-label="Filter dashboard to ' + escapeAttr(label) + ' ' + escapeAttr(accessibleDisplay) + '" title="Filter dashboard to ' + escapeAttr(label) + ' ' + escapeAttr(accessibleDisplay) + '">' + escapeHTML(display) + '</button>';
}

function fleetScopeChips(field, values, label, kind, formatter, limit) {
	var seen = {};
	return (Array.isArray(values) ? values : []).map(function(value) {
		value = String(value || '').trim();
		if (!value || seen[value]) return '';
		seen[value] = true;
		return fleetScopeChip(field, value, label, formatter ? formatter(value) : value, kind);
	}).filter(function(html) { return !!html; }).slice(0, limit || 5).join('');
}

function fleetMetaRow(chips, emptyText) {
	if (chips) return '<div class="dashboard-fleet-filter-row">' + chips + '</div>';
	return '<div class="dashboard-fleet-filter-row"><span class="dashboard-fleet-muted">' + escapeHTML(emptyText) + '</span></div>';
}

var dashboardSourceScopeLabels = {};

function rememberFleetSourceScopeLabels(nodes) {
	(Array.isArray(nodes) ? nodes : []).forEach(function(node) {
		(Array.isArray(node && node.sources_detail) ? node.sources_detail : []).forEach(function(source) {
			var sourceID = String(source && source.source_id || '').trim();
			var sourceName = String(source && source.source_name || '').trim();
			if (sourceID && sourceName) dashboardSourceScopeLabels[sourceID] = sourceName;
		});
	});
}

function fleetSourceChips(node) {
	var seen = {};
	var details = Array.isArray(node.sources_detail) ? node.sources_detail : [];
	var chips = details.map(function(source) {
		source = source || {};
		var sourceID = String(source.source_id || '').trim();
		var sourceName = String(source.source_name || '').trim();
		var value = sourceID || sourceName;
		var field = sourceID ? 'source_id' : 'source_name';
		var key = field + '\x00' + value;
		if (!value || seen[key]) return '';
		seen[key] = true;
		if (sourceID && sourceName) dashboardSourceScopeLabels[sourceID] = sourceName;
		return fleetScopeChip(field, value, 'source', sourceName || sourceID, 'source', sourceID && sourceName && sourceID !== sourceName ? sourceName + ' (' + sourceID + ')' : '');
	}).filter(function(html) { return !!html; });
	if (chips.length > 0) return chips.slice(0, 4).join('');
	return fleetScopeChips('source_name', node.sources || [], 'source', 'source', null, 4);
}

function activeScopeChip(field, value, label) {
	var display = field === 'source_id' && dashboardSourceScopeLabels[value] ? dashboardSourceScopeLabels[value] : value;
	var visible = display === value ? display : display + ' (' + value + ')';
	var title = display === value ? 'Clear ' + label + ' filter ' + value : 'Clear ' + label + ' filter ' + display + ' (' + value + ')';
	return '<button type="button" class="dashboard-scope-chip" data-dashboard-scope-clear="' + escapeAttr(field) + '" data-dashboard-scope-value="' + escapeAttr(value) + '" aria-label="' + escapeAttr(title) + '" title="' + escapeAttr(title) + '"><span>' + escapeHTML(label) + '</span><strong>' + escapeHTML(visible) + '</strong></button>';
}

function syncDashboardScopeControls() {
	var wrap = document.getElementById('dashboard-scope-chips');
	if (!wrap) return;
	var chips = [];
	[
		['node_id', 'Node'],
		['collector_id', 'Collector'],
		['source_id', 'Source'],
		['source_name', 'Source'],
		['runtime', 'Runtime'],
		['project_key', 'Project']
	].forEach(function(entry) {
		var field = entry[0];
		var label = entry[1];
		var values = typeof dashboardScopeValues === 'function' ? dashboardScopeValues(field) : [];
		values.forEach(function(value) {
			chips.push(activeScopeChip(field, value, label));
		});
	});
	if (chips.length > 0) {
		chips.push('<button type="button" class="dashboard-scope-clear-all" data-dashboard-scope-clear="all" aria-label="Clear all dashboard filters">Clear all</button>');
		setHTMLIfChanged(wrap, chips.join(''));
	} else {
		setHTMLIfChanged(wrap, '<span class="dashboard-scope-empty">All machines</span>');
	}
}

function fleetNodeCard(node) {
	node = node || {};
	var nodeID = node.node_id || 'local';
	var status = node.status || 'offline';
	var runtimes = (node.runtimes || []).slice(0, 5);
	var collectors = (node.collectors || []).slice(0, 4);
	var projects = (node.projects || []).slice(0, 3);
	var collectorChips = fleetScopeChips('collector_id', collectors, 'collector', 'collector', null, 4);
	var runtimeChips = fleetScopeChips('runtime', runtimes, 'runtime', 'runtime', runtimeLabel, 5);
	var sourceChips = fleetSourceChips(node);
	var projectChips = fleetScopeChips('project_key', projects, 'project', 'project', null, 3);
	var missing = nonNegativeInt(node.missing_heartbeat_collectors);
	var missingHealth = missing > 0 ? '<span><strong>' + missing + '</strong> missing heartbeat</span>' : '';
	var nodeName = node.label || nodeLabel(nodeID);
	var healthLabel = [
		'Filter dashboard to node ' + nodeName,
		fleetStatusLabel(status),
		nonNegativeInt(node.active_sessions) + ' active sessions',
		nonNegativeInt(node.attention_sessions) + ' attention sessions',
		formatTokens(node.total_tokens) + ' tokens',
		nonNegativeInt(node.collector_count) + ' collectors',
		formatBytes(node.spool_bytes) + ' spool',
		nonNegativeInt(node.queue_depth) + ' queued',
		missing > 0 ? missing + ' missing heartbeat collectors' : '',
		node.last_seen_label || 'not seen'
	].filter(function(part) { return !!part; }).join('; ');
	return '<article class="dashboard-fleet-node ' + fleetStatusClass(status) + '">' +
		'<button type="button" class="dashboard-fleet-node-main" data-dashboard-scope-field="node_id" data-dashboard-scope-value="' + escapeAttr(nodeID) + '" aria-label="' + escapeAttr(healthLabel) + '">' +
			'<span class="dashboard-fleet-node-top"><strong>' + escapeHTML(nodeName) + '</strong><span>' + escapeHTML(fleetStatusLabel(status)) + '</span></span>' +
			'<span class="dashboard-fleet-node-stats">' +
				'<span><strong>' + nonNegativeInt(node.active_sessions) + '</strong> active</span>' +
				'<span><strong>' + nonNegativeInt(node.attention_sessions) + '</strong> attention</span>' +
				'<span><strong>' + formatTokens(node.total_tokens) + '</strong> tokens</span>' +
				'</span>' +
				'<span class="dashboard-fleet-node-health">' +
					'<span>' + nonNegativeInt(node.collector_count) + ' collectors</span>' +
					'<span>' + formatBytes(node.spool_bytes) + ' spool</span>' +
					'<span>' + nonNegativeInt(node.queue_depth) + ' queued</span>' +
					missingHealth +
					'<span>' + escapeHTML(node.last_seen_label || 'not seen') + '</span>' +
				'</span>' +
			'</button>' +
			'<div class="dashboard-fleet-node-meta">' +
				fleetMetaRow(collectorChips, 'No collectors yet') +
				fleetMetaRow(runtimeChips, 'No runtimes yet') +
				fleetMetaRow(sourceChips, 'No source heartbeat yet') +
				fleetMetaRow(projectChips, 'All projects') +
			'</div>' +
		'</article>';
}

function renderFleet(response) {
	response = response || {};
	var nodes = Array.isArray(response.nodes) ? response.nodes : [];
	var strip = document.getElementById('dashboard-fleet-strip');
	var subtitle = document.getElementById('dashboard-fleet-subtitle');
	var advanced = dashboardShouldShowMachinePanel(response);
	setDashboardTopologyMode(advanced);
	rememberFleetSourceScopeLabels(nodes);
	syncDashboardScopeControls();
	if (!advanced) return;
	renderFleetHeader(response.totals || {});
	if (subtitle) {
		var totals = response.totals || {};
		subtitle.textContent = nonNegativeInt(totals.node_count) + ' nodes · ' + nonNegativeInt(totals.collector_count) + ' collectors · ' + nonNegativeInt(totals.active_sessions) + ' active sessions';
	}
	if (!strip) return;
	if (nodes.length === 0) {
		setHTMLIfChanged(strip, '<div class="dashboard-fleet-empty">No machine activity yet</div>');
		return;
	}
	setHTMLIfChanged(strip, nodes.map(fleetNodeCard).join(''));
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

var dashboardChartMetricLabels = {
	total_tokens: 'Total Tokens',
	input_tokens: 'Input Tokens',
	output_tokens: 'Output Tokens',
	cache_read_tokens: 'Cache Read Tokens',
	tool_calls: 'Tool Calls',
	errors: 'Errors',
	error_rate: 'Error Rate'
};

var dashboardChartMetricUnits = {
	total_tokens: 'tokens',
	input_tokens: 'tokens',
	output_tokens: 'tokens',
	cache_read_tokens: 'tokens',
	tool_calls: 'tool calls',
	errors: 'errors',
	error_rate: '%'
};

function dashboardChartMetricValues() {
	return typeof dashboardChartMetrics !== 'undefined' ? dashboardChartMetrics : Object.keys(dashboardChartMetricLabels);
}

function chartMetricValue() {
	var values = dashboardChartMetricValues();
	var current = typeof currentChartMetric !== 'undefined' ? currentChartMetric : 'total_tokens';
	if (current === 'tokens') current = 'total_tokens';
	return values.indexOf(current) >= 0 ? current : 'total_tokens';
}

function chartMetricLabel(metric) {
	metric = metric || chartMetricValue();
	return dashboardChartMetricLabels[metric] || dashboardChartMetricLabels.total_tokens;
}

function chartMetricUnit(metric) {
	metric = metric || chartMetricValue();
	return dashboardChartMetricUnits[metric] || '';
}

function emptyChartMetricLabel(metric) {
	metric = metric || chartMetricValue();
	if (metric === 'input_tokens') return 'input token';
	if (metric === 'output_tokens') return 'output token';
	if (metric === 'cache_read_tokens') return 'cache read token';
	if (metric === 'tool_calls') return 'tool call';
	if (metric === 'error_rate') return 'error rate';
	if (metric === 'errors') return 'error';
	return 'token';
}

function updateChartRangeCaption(state) {
	var caption = document.getElementById('dashboard-chart-range-caption');
	if (!caption) return;
	var label = rangeLabel(chartRangeValue());
	caption.textContent = label;
}

function setAnalyticsBusy(busy) {
	var panel = document.querySelector('.dashboard-analytics-panel');
	if (!panel) return;
	panel.setAttribute('data-loading', busy ? 'true' : 'false');
	panel.removeAttribute('aria-busy');
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

function dashboardMetricPayloadTotal(metricPayload) {
	var total = 0;
	(metricPayload && metricPayload.datasets ? metricPayload.datasets : []).forEach(function(dataset) {
		var points = dataset.data || dataset.values || [];
		points.forEach(function(value) {
			total += chartPointValue(value);
		});
	});
	return total;
}

function formatDashboardMetricValue(metric, value) {
	if (metric === 'error_rate') return formatPercent(value);
	return formatTokens(value);
}

function dashboardMetricSummaryValue(metric, summary, metricPayload) {
	summary = summary || {};
	if (metric === 'total_tokens') return numericValue(summary.total_tokens, dashboardMetricPayloadTotal(metricPayload));
	if (metric === 'tool_calls') return numericValue(summary.tool_call_count, dashboardMetricPayloadTotal(metricPayload));
	if (metric === 'errors') return numericValue(summary.error_count, dashboardMetricPayloadTotal(metricPayload));
	if (metric === 'error_rate') return numericValue(summary.error_rate, 0);
	return dashboardMetricPayloadTotal(metricPayload);
}

function pluralizeCount(value, singular, plural) {
	var count = nonNegativeInt(value);
	return count + ' ' + (count === 1 ? singular : plural);
}

function emptyDashboardMetricPayload(payload, metric) {
	payload = payload || {};
	var tokenChart = payload.token_cumulative || {};
	var modelActivity = payload.model_activity || {};
	var labels = tokenChart.labels && tokenChart.labels.length ? tokenChart.labels : (modelActivity.labels || []);
	return {
		labels: labels,
		datasets: [],
		summary: tokenChart.summary || modelActivity.summary || {},
		time_unit: tokenChart.time_unit || modelActivity.time_unit || 'hour',
		bucket_minutes: tokenChart.bucket_minutes || modelActivity.bucket_minutes || 60,
		metric: metric,
		label: chartMetricLabel(metric),
		unit: chartMetricUnit(metric)
	};
}

function dashboardMetricPayload(payload) {
	payload = payload || {};
	var metric = chartMetricValue();
	var selected;
	if (metric === 'total_tokens') {
		selected = payload.token_cumulative || {};
		return Object.assign({}, selected, {
			metric: metric,
			label: chartMetricLabel(metric),
			unit: chartMetricUnit(metric)
		});
	}
	var modelActivity = payload.model_activity || {};
	var series = modelActivity.metrics && modelActivity.metrics[metric];
	if (!series) return emptyDashboardMetricPayload(payload, metric);
	return {
		labels: modelActivity.labels || [],
		datasets: series.datasets || [],
		summary: modelActivity.summary || (payload.token_cumulative && payload.token_cumulative.summary) || {},
		time_unit: modelActivity.time_unit || (payload.token_cumulative && payload.token_cumulative.time_unit) || 'hour',
		bucket_minutes: modelActivity.bucket_minutes || (payload.token_cumulative && payload.token_cumulative.bucket_minutes) || 60,
		metric: metric,
		label: series.label || chartMetricLabel(metric),
		unit: series.unit || chartMetricUnit(metric)
	};
}

function dashboardMetricHasSeriesData(metricPayload) {
	var datasets = metricPayload && metricPayload.datasets ? metricPayload.datasets : [];
	var hasPositiveValue = datasets.some(function(dataset) {
		var points = dataset.data || dataset.values || [];
		return points.some(function(value) { return chartPointValue(value) > 0; });
	});
	if (hasPositiveValue) return true;
	var metric = metricPayload && metricPayload.metric ? metricPayload.metric : chartMetricValue();
	if (metric !== 'error_rate' && !(metricPayload && metricPayload.unit === '%')) return false;
	var summary = metricPayload && metricPayload.summary ? metricPayload.summary : {};
	var summaryAttempts = numericValue(summary.call_count, 0) + numericValue(summary.error_count, 0);
	if (summaryAttempts > 0) return true;
	return datasets.some(function(dataset) {
		var points = dataset.data || dataset.values || [];
		var attempts = numericValue(dataset.call_count, dataset.callCount || 0) + numericValue(dataset.error_count, dataset.errorCount || 0);
		return points.length > 0 && attempts > 0;
	});
}

function renderAnalyticsSummary(summary, metricPayload) {
	withDashboardScrollStability(function() {
		var wrap = document.getElementById('dashboard-analytics-summary');
		if (!wrap) return;
		summary = summary || {};
		var metric = chartMetricValue();
		var chartRangeLabel = rangeLabel(chartRangeValue());
		var metricValue = dashboardMetricSummaryValue(metric, summary, metricPayload);
		var metricSublabel = metric === 'error_rate' ? pluralizeCount(summary.error_count, 'error', 'errors') : chartRangeLabel;
		var thirdTile = metric === 'total_tokens'
			? summaryTile('Tool Calls', formatTokens(summary.tool_call_count), chartRangeLabel)
			: summaryTile('Total Tokens', formatTokens(summary.total_tokens), chartRangeLabel);
		var fourthTile = (metric === 'errors' || metric === 'error_rate')
			? summaryTile('Tool Calls', formatTokens(summary.tool_call_count), chartRangeLabel)
			: summaryTile('Error Rate', formatPercent(summary.error_rate), pluralizeCount(summary.error_count, 'error', 'errors'));
		setHTMLIfChanged(wrap, [
			summaryTile(chartMetricLabel(metric), formatDashboardMetricValue(metric, metricValue), metricSublabel),
			summaryTile('Models', nonNegativeInt(summary.model_count), 'Series'),
			thirdTile,
			fourthTile
		].join(''));
	});
}

function updateDashboardCharts(payload) {
	if (!payload) return;
	lastDashboardChartsPayload = payload;
	payload.token_cumulative = payload.token_cumulative || {labels: [], datasets: [], summary: {}};
	payload.model_activity = payload.model_activity || {labels: [], metrics: {}, summary: payload.token_cumulative.summary || {}};
	setAnalyticsBusy(false);
	var tokenDataEl = document.getElementById('dashboard-token-cumulative-data');
	if (tokenDataEl && payload.token_cumulative) tokenDataEl.textContent = JSON.stringify(payload.token_cumulative);
	var metricPayload = dashboardMetricPayload(payload);
	if (typeof updateDashboardModelChart === 'function') {
		updateDashboardModelChart('dashboardTokenCumulativeChart', metricPayload, chartMetricValue());
	}
	var summary = metricPayload && metricPayload.summary ? metricPayload.summary : null;
	renderAnalyticsSummary(summary, metricPayload);
	updateChartRangeCaption(dashboardMetricHasSeriesData(metricPayload) ? '' : 'empty');
}

async function loadDashboardCharts() {
	setAnalyticsBusy(true);
	updateChartRangeCaption();
	var result = await fetchDashboardJSON('charts', requestURL('/api/dashboard/charts', {chart_range: chartRangeValue()}));
	if (!result || result.stale) return;
	if (result.error) {
		updateDashboardCharts({token_cumulative: {labels: [], datasets: [], summary: {}}});
		updateChartRangeCaption();
		return;
	}
	updateDashboardCharts(result.data);
}

async function loadDashboardFleet() {
	var result = await fetchDashboardJSON('fleet', requestURL('/api/dashboard/fleet', {}));
	if (!result || result.stale) return;
	if (result.error) {
		renderFleet({totals: {}, nodes: []});
		var strip = document.getElementById('dashboard-fleet-strip');
		if (strip && dashboardAdvancedTopology) setHTMLIfChanged(strip, '<div class="dashboard-fleet-empty dashboard-fleet-error">Unable to load machine status. <button type="button" data-dashboard-retry="fleet">Retry</button></div>');
		return;
	}
	renderFleet(result.data);
}

async function loadActiveSessions() {
	var result = await fetchDashboardJSON('active', requestURL('/api/dashboard/sessions', {state: 'active'}));
	if (!result || result.stale) return;
	if (result.error) {
		withDashboardScrollStability(function() {
			var wrap = document.getElementById('active-sessions');
			if (!wrap) return;
			renderActiveShell(wrap, '<h2 id="active-sessions-title" class="text-lg font-semibold text-gray-200 flex items-center gap-2">Active Sessions</h2>' + activeSortControl(), '<div class="rounded border border-red-500/30 bg-red-500/10 p-4 text-sm text-red-300">Unable to load active sessions. <button type="button" class="underline" data-dashboard-retry="active">Retry</button></div>');
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
			setHTMLIfChanged(tbody, '<tr><td colspan="6" class="text-center py-4"><span class="text-sm text-red-400">Unable to search table sessions and events. <button type="button" class="underline" data-dashboard-retry="search">Retry</button></span></td></tr>');
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
	if (status && !options.silent) setTextIfChanged(status, rangeLabel(completedRangeValue()));
	var result = await fetchDashboardJSON('completed', requestURL('/api/dashboard/sessions', {
		state: 'completed',
		limit: completedPageSize,
		offset: currentCompletedOffset,
		completed_range: completedRangeValue(),
		q: currentSearchQuery,
		session_id: currentSearchSessionID,
		sort: sortColumn,
		direction: sortAsc ? 'asc' : 'desc'
	}));
	if (!result || result.stale) return;
	if (result.error) {
		if (options.silent) return;
		withDashboardScrollStability(function() {
			var tbody = document.getElementById('completed-sessions');
			setHTMLIfChanged(tbody, '<tr><td colspan="12" class="text-center py-4"><span class="text-sm text-red-400">Unable to load completed sessions. <button type="button" class="underline" data-dashboard-retry="completed">Retry</button></span></td></tr>');
			setTextIfChanged(status, 'Unable to load completed sessions');
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
		setHTMLIfChanged(document.getElementById('activity-feed'), '<p class="text-sm text-red-400 text-center py-4">Unable to load activity. <button type="button" class="underline" data-dashboard-retry="activity">Retry</button></p>');
		return;
	}
	renderActivity(result.data);
}
