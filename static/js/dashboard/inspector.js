// --- JSON-first session inspector ---
(function() {
	var sessionsStore = [];
	var sessionsLoaded = false;
	var selectedSessionId = '';
	var inspectorSeq = 0;
	var inspectorController = null;
	var payloadControllers = [];
	var inspector = document.getElementById('session-inspector');
	var title = document.getElementById('inspector-title');
	var subtitle = document.getElementById('inspector-subtitle');
	var summary = document.getElementById('inspector-summary');
	var events = document.getElementById('inspector-events');
	var fullLink = document.getElementById('inspector-full-link');
	var closeButton = inspector && inspector.querySelector('[aria-label="Close"]');
	var inspectorLauncher = null;
	var inspectorLauncherSession = '';
	var utils = window.BeaconDashboard.utils;
	var escapeHTML = utils.escapeHTML;
	var escapeAttr = utils.escapeAttr;
	var cssEscape = utils.cssEscape;
	var shortID = utils.shortID;

	function validInspectorRestoreTarget(el) {
		if (!el || !el.isConnected || typeof el.focus !== 'function') return null;
		if (inspector && inspector.contains(el)) return null;
		if (el.closest('[hidden], .hidden, [inert], [aria-hidden="true"]')) return null;
		return el;
	}

	function setInspectorBackgroundInert(disabled) {
		['dashboard-main', 'sidebar-divider', 'timeline-sidebar'].forEach(function(id) {
			var el = document.getElementById(id);
			if (!el) return;
			var timelineStillCollapsed = !disabled && id === 'timeline-sidebar' && (
				el.classList.contains('collapsed') ||
				document.documentElement.getAttribute('data-beacon-timeline-collapsed') === 'true'
			);
			if (timelineStillCollapsed) {
				el.setAttribute('inert', '');
				el.setAttribute('aria-hidden', 'true');
				return;
			}
			el.toggleAttribute('inert', disabled);
			if (disabled) el.setAttribute('aria-hidden', 'true');
			else el.removeAttribute('aria-hidden');
		});
	}

	function metric(label, value) {
		return '<div><p class="text-xs text-gray-500">' + escapeHTML(label) + '</p><p class="text-gray-200 font-medium">' + escapeHTML(value) + '</p></div>';
	}

	function renderSummary(session) {
		title.textContent = session ? shortID(session.id) : 'Session';
		subtitle.textContent = session ? session.id : selectedSessionId;
		fullLink.href = '/sessions/' + encodeURIComponent(selectedSessionId);
		if (!session) {
			// Summary markup is static; values go through metric(), which escapes text.
			summary.innerHTML = metric('Status', 'Loading');
			return;
		}
		// Summary markup is static; values go through metric(), which escapes text.
		summary.innerHTML = [
			metric('Duration', session.duration || ''),
			metric('Tokens', session.total_tokens || 0),
			metric('Turns', session.turn_count || 0),
			metric('Tools', session.tool_call_count || 0),
			metric('Source', session.source || ''),
			metric('Model', session.last_model || ''),
			metric('Input', session.input_tokens || 0),
			metric('Output', session.output_tokens || 0)
		].join('');
	}

	function normalizeSessionDetail(data) {
		var session = data && (data.session || data.Session || data);
		if (!session) return null;
		return {
			id: session.id || session.ID || '',
			duration: session.duration || session.Duration || '',
			total_tokens: session.total_tokens || session.TotalTokens || 0,
			turn_count: session.turn_count || session.TurnCount || 0,
			tool_call_count: session.tool_call_count || session.ToolCallCount || 0,
			source: session.source || session.Actor || '',
			last_model: session.last_model || session.ActiveModel || '',
			input_tokens: session.input_tokens || session.InputTokens || 0,
			output_tokens: session.output_tokens || session.OutputTokens || 0
		};
	}

	function payloadElementID(eventUID) {
		return 'payload-' + String(eventUID || 'event').replace(/[^A-Za-z0-9_-]/g, '-');
	}

	function eventRow(event) {
		event = event || {};
		var meta = [event.event_kind, event.actor_role, event.tool_name, event.model].filter(Boolean).join(' · ');
		var eventUID = String(event.event_uid || '');
		var payloadID = payloadElementID(eventUID);
		var payloadButton = event.tool_name ? '<button type="button" class="payload-btn text-xs text-blue-400 hover:text-blue-300" data-event-id="' + escapeAttr(event.event_uid) + '" aria-expanded="false" aria-controls="' + payloadID + '">Payload</button>' : '';
		return '<div class="rounded border border-gray-800 bg-gray-900/60 p-3" data-event="' + escapeAttr(eventUID) + '">' +
			'<div class="flex items-center justify-between gap-3 mb-1">' +
			'<p class="text-xs text-gray-500 truncate">' + escapeHTML(meta) + '</p>' + payloadButton +
			'</div>' +
			'<p class="text-sm text-gray-300 whitespace-pre-wrap break-words">' + escapeHTML(event.text_preview || event.input_preview || event.output_preview || '') + '</p>' +
			'<pre id="' + payloadID + '" class="payload-target hidden mt-3 p-3 rounded bg-black/40 text-xs text-gray-300 overflow-x-auto"></pre>' +
			'</div>';
	}

	function abortPayloadFetches() {
		payloadControllers.forEach(function(controller) {
			controller.abort();
		});
		payloadControllers = [];
	}

	async function loadSessions() {
		if (sessionsLoaded) return;
		if (window.dashboardSessionIndex && Object.keys(window.dashboardSessionIndex).length > 0) {
			sessionsStore = Object.keys(window.dashboardSessionIndex).map(function(id) { return window.dashboardSessionIndex[id]; });
			sessionsLoaded = true;
			return;
		}
		var res = await fetch('/api/sessions?limit=500', {headers: {'Accept': 'application/json'}});
		if (!res.ok) throw new Error('sessions failed');
		sessionsStore = await res.json();
		sessionsLoaded = true;
	}

	async function loadSessionSummary(id, fetchOpts) {
		try {
			await loadSessions();
		} catch (err) {
		}
		var cached = sessionsStore.find(function(s) { return s.id === id; });
		if (cached) return cached;
		var res = await fetch('/api/sessions/' + encodeURIComponent(id), fetchOpts || {headers: {'Accept': 'application/json'}});
		if (!res.ok) throw new Error('session failed');
		var detail = await res.json();
		var normalized = normalizeSessionDetail(detail);
		if (normalized && normalized.id) {
			sessionsStore.push(normalized);
			window.dashboardSessionIndex[normalized.id] = normalized;
		}
		return normalized;
	}

	async function openSessionInspector(id, launcher) {
		var seq = ++inspectorSeq;
		if (inspectorController) {
			inspectorController.abort();
		}
		abortPayloadFetches();
		inspectorController = window.AbortController ? new AbortController() : null;
		selectedSessionId = id;
		inspectorLauncher = validInspectorRestoreTarget(launcher) || validInspectorRestoreTarget(document.activeElement);
		inspectorLauncherSession = id;
		inspector.classList.remove('hidden');
		setInspectorBackgroundInert(true);
		// Inspector event rows are static shells built by eventRow(); all event
		// fields are escaped before insertion and payload bodies use textContent.
		events.innerHTML = '<div class="text-sm text-gray-500">Loading events...</div>';
		renderSummary(null);
		try {
			var fetchOpts = {headers: {'Accept': 'application/json'}};
			if (inspectorController) fetchOpts.signal = inspectorController.signal;
			var session = await loadSessionSummary(id, fetchOpts);
			if (seq !== inspectorSeq) return;
			renderSummary(session);
			if (closeButton) closeButton.focus({preventScroll: true});
			var res = await fetch('/api/sessions/' + encodeURIComponent(id) + '/events?limit=200', fetchOpts);
			if (!res.ok) throw new Error('events failed');
			var items = await res.json();
			if (seq !== inspectorSeq) return;
			// eventRow() escapes dynamic values; empty/error states are static.
			events.innerHTML = items.length ? items.map(eventRow).join('') : '<div class="text-sm text-gray-500">No events</div>';
		} catch (err) {
			if (err && err.name === 'AbortError') return;
			if (seq !== inspectorSeq) return;
			// Static error state.
			events.innerHTML = '<div class="text-sm text-red-400">Unable to load session</div>';
		}
	}

	window.closeSessionInspector = function() {
		inspectorSeq++;
		if (inspectorController) {
			inspectorController.abort();
			inspectorController = null;
		}
		abortPayloadFetches();
		setInspectorBackgroundInert(false);
		var restoreTarget = validInspectorRestoreTarget(inspectorLauncher);
		if (!restoreTarget && inspectorLauncherSession) {
			var sessionURL = '/sessions/' + encodeURIComponent(inspectorLauncherSession);
			var matches = Array.from(document.querySelectorAll('.session-row-open[data-session-link="' + cssEscape(sessionURL) + '"], a[href="' + cssEscape(sessionURL) + '"]'));
			restoreTarget = matches.map(validInspectorRestoreTarget).find(Boolean) || null;
		}
		if (!restoreTarget) {
			restoreTarget = document.getElementById('dashboard-session-search') || document.getElementById('dashboard-refresh-btn');
		}
		inspector.classList.add('hidden');
		selectedSessionId = '';
		if (restoreTarget && typeof restoreTarget.focus === 'function') {
			restoreTarget.focus({preventScroll: true});
		}
		inspectorLauncher = null;
		inspectorLauncherSession = '';
	};

	window.goToSession = function(url, launcher) {
		try {
			var parsed = new URL(String(url || ''), window.location.origin);
			var segments = parsed.pathname.split('/');
			var idx = segments.indexOf('sessions');
			var id = idx >= 0 && idx + 1 < segments.length ? segments[idx + 1] : '';
			if (id) {
				openSessionInspector(decodeURIComponent(id), launcher);
			}
		} catch (err) {
		}
	};

	document.addEventListener('click', function(evt) {
		if (inspector && !inspector.classList.contains('hidden') && !inspector.contains(evt.target)) {
			evt.preventDefault();
			evt.stopPropagation();
			return;
		}
		var link = evt.target.closest && evt.target.closest('a[href^="/sessions/"]');
		if (link && link.id !== 'inspector-full-link' && !link.closest('#activity-feed') && !link.closest('[data-transcript-link]')) {
			evt.preventDefault();
			window.goToSession(link.getAttribute('href'), link);
			return;
		}
		var btn = evt.target.closest && evt.target.closest('.payload-btn');
		if (!btn) return;
		var card = btn.closest('[data-event]');
		var target = card && card.querySelector('.payload-target');
		if (!target) return;
		if (!target.classList.contains('hidden')) {
			target.classList.add('hidden');
			btn.setAttribute('aria-expanded', 'false');
			return;
		}
		target.textContent = 'Loading payload...';
		target.classList.remove('hidden');
		btn.setAttribute('aria-expanded', 'true');
		var payloadSeq = inspectorSeq;
		var payloadController = window.AbortController ? new AbortController() : null;
		if (payloadController) payloadControllers.push(payloadController);
		var payloadOpts = {headers: {'Accept': 'application/json'}};
		if (payloadController) payloadOpts.signal = payloadController.signal;
		fetch('/api/tool-payloads/' + encodeURIComponent(btn.getAttribute('data-event-id')), payloadOpts)
			.then(function(res) { return res.ok ? res.json() : Promise.reject(new Error('payload failed')); })
			.then(function(payload) {
				if (payloadSeq !== inspectorSeq || !target.isConnected) return;
				target.textContent = JSON.stringify({input: parsePayloadJSON(payload.input_json), output: parsePayloadJSON(payload.output_json)}, null, 2);
			})
			.catch(function(err) {
				if (err && err.name === 'AbortError') return;
				if (payloadSeq !== inspectorSeq || !target.isConnected) return;
				target.textContent = 'Payload unavailable';
			});
	}, true);

	function parsePayloadJSON(value) {
		if (typeof value !== 'string') return value;
		try {
			return JSON.parse(value);
		} catch (err) {
			return value;
		}
	}

	document.addEventListener('keydown', function(evt) {
		if (evt.key === 'Escape' && !inspector.classList.contains('hidden')) {
			evt.preventDefault();
			window.closeSessionInspector();
			return;
		}
		if (evt.key === 'Tab' && !inspector.classList.contains('hidden')) {
			var focusables = Array.from(inspector.querySelectorAll('a[href], button:not([disabled]), [tabindex]:not([tabindex="-1"])')).filter(function(el) {
				return el.offsetParent !== null;
			});
			if (!focusables.length) return;
			var first = focusables[0];
			var last = focusables[focusables.length - 1];
			if (evt.shiftKey && document.activeElement === first) {
				evt.preventDefault();
				last.focus();
			} else if (!evt.shiftKey && document.activeElement === last) {
				evt.preventDefault();
				first.focus();
			}
		}
	});
})();
