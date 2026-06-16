// --- Transcript quick-view session inspector ---
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
	var closeButton = inspector && inspector.querySelector('[data-inspector-close]');
	var inspectorLauncher = null;
	var inspectorLauncherSession = '';
	var utils = window.BeaconDashboard.utils;
	var escapeHTML = utils.escapeHTML;
	var cssEscape = utils.cssEscape;
	var formatTokens = utils.formatTokens;
	var shortID = utils.shortID;
	var requestURL = utils.requestURL;

	function validInspectorRestoreTarget(el) {
		if (!el || !el.isConnected || typeof el.focus !== 'function') return null;
		if (inspector && inspector.contains(el)) return null;
		if (el.closest('[hidden], .hidden, [inert], [aria-hidden="true"]')) return null;
		return el;
	}

	function metric(label, value) {
		var display = value === undefined || value === null || value === '' ? '-' : value;
		return '<div class="inspector-stat"><p>' + escapeHTML(label) + '</p><strong>' + escapeHTML(display) + '</strong></div>';
	}

	function renderSummary(session) {
		if (session) {
			var displayTitle = session.title || session.id || shortID(session.id);
			title.textContent = displayTitle || 'Session quick view';
			subtitle.textContent = session.title ? session.id : '';
		} else {
			title.textContent = 'Session quick view';
			subtitle.textContent = selectedSessionId;
		}
		fullLink.href = requestURL('/sessions/' + encodeURIComponent(selectedSessionId), {});
		if (!session) {
			// Summary markup is static; values go through metric(), which escapes text.
			summary.innerHTML = metric('Status', 'Loading') + metric('Events', 'Loading');
			return;
		}
		// Summary markup is static; values go through metric(), which escapes text.
		summary.innerHTML = [
			metric('Duration', session.duration || ''),
			metric('Tokens', formatTokens(session.total_tokens || 0)),
			metric('Turns', session.turn_count || 0),
			metric('Tools', session.tool_call_count || 0),
			metric('Source', session.source || ''),
			metric('Model', session.last_model || ''),
			metric('Input', formatTokens(session.input_tokens || 0)),
			metric('Output', formatTokens(session.output_tokens || 0))
		].join('');
	}

	function normalizeSessionDetail(data) {
		var session = data && (data.session || data);
		if (!session) return null;
		return {
			id: session.id || '',
			title: session.title || '',
			duration: session.duration || '',
			total_tokens: session.total_tokens || 0,
			turn_count: session.turn_count || 0,
			tool_call_count: session.tool_call_count || 0,
			source: session.source || '',
			last_model: session.last_model || '',
			input_tokens: session.input_tokens || 0,
			output_tokens: session.output_tokens || 0
		};
	}

	function abortPayloadFetches() {
		payloadControllers.forEach(function(controller) {
			controller.abort();
		});
		payloadControllers = [];
	}

	function ensureTranscriptHelpers() {
		if (typeof window.toggleTruncation !== 'function') {
			window.toggleTruncation = function(el) {
				if (!el) return;
				var toggle = el.querySelector('.truncate-toggle');
				if (el.classList.contains('truncated')) {
					el.classList.remove('truncated');
					el.classList.add('expanded');
					if (toggle) toggle.textContent = 'Show less';
				} else {
					el.classList.remove('expanded');
					el.classList.add('truncated');
					if (toggle) toggle.textContent = 'Show more';
				}
			};
		}
		if (typeof window.copyToClipboard !== 'function') {
			window.copyToClipboard = function(btn) {
				var container = btn && btn.closest ? btn.closest('.code-container') : null;
				var code = container ? container.querySelector('code, pre') : null;
				if (!code || !navigator.clipboard) return;
				navigator.clipboard.writeText(code.textContent).then(function() {
					var copyIcon = btn.querySelector('.copy-icon');
					var checkIcon = btn.querySelector('.check-icon');
					if (!copyIcon || !checkIcon) return;
					copyIcon.classList.add('hidden');
					checkIcon.classList.remove('hidden');
					setTimeout(function() {
						copyIcon.classList.remove('hidden');
						checkIcon.classList.add('hidden');
					}, 1500);
				}).catch(function() {});
			};
		}
	}

	async function loadSessions() {
		if (sessionsLoaded) return;
		if (window.dashboardSessionIndex && Object.keys(window.dashboardSessionIndex).length > 0) {
			sessionsStore = Object.keys(window.dashboardSessionIndex).map(function(id) { return window.dashboardSessionIndex[id]; });
			sessionsLoaded = true;
			return;
		}
		var res = await fetch(requestURL('/api/sessions', {limit: 500}), {headers: {'Accept': 'application/json'}});
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
		var res = await fetch(requestURL('/api/sessions/' + encodeURIComponent(id), {}), fetchOpts || {headers: {'Accept': 'application/json'}});
		if (!res.ok) throw new Error('session failed');
		var detail = await res.json();
		var normalized = normalizeSessionDetail(detail);
		if (normalized && normalized.id) {
			sessionsStore.push(normalized);
			window.dashboardSessionIndex[normalized.id] = normalized;
		}
		return normalized;
	}

	async function loadSessionTranscript(id, fetchOpts) {
		var res = await fetch(requestURL('/sessions/' + encodeURIComponent(id) + '/conversation', {}), fetchOpts || {headers: {'Accept': 'text/html'}});
		if (!res.ok) throw new Error('transcript failed');
		return res.text();
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
		// Static loading state. Transcript HTML below is server-rendered by Beacon.
		events.innerHTML = '<div class="inspector-state">Loading transcript...</div>';
		renderSummary(null);
		if (closeButton) closeButton.focus({preventScroll: true});
		try {
			var jsonFetchOpts = {headers: {'Accept': 'application/json'}};
			var htmlFetchOpts = {headers: {'Accept': 'text/html'}};
			if (inspectorController) {
				jsonFetchOpts.signal = inspectorController.signal;
				htmlFetchOpts.signal = inspectorController.signal;
			}
			var session = await loadSessionSummary(id, jsonFetchOpts);
			if (seq !== inspectorSeq) return;
			renderSummary(session);
			var transcriptHTML = await loadSessionTranscript(id, htmlFetchOpts);
			if (seq !== inspectorSeq) return;
			ensureTranscriptHelpers();
			events.innerHTML = transcriptHTML.trim() || '<div class="inspector-state">No transcript content found</div>';
		} catch (err) {
			if (err && err.name === 'AbortError') return;
			if (seq !== inspectorSeq) return;
			// Static error state.
			events.innerHTML = '<div class="inspector-state inspector-state-error">Unable to load this transcript preview</div>';
		}
	}

	window.closeSessionInspector = function(options) {
		options = options || {};
		var restoreFocus = options.restoreFocus !== false;
		inspectorSeq++;
		if (inspectorController) {
			inspectorController.abort();
			inspectorController = null;
		}
		abortPayloadFetches();
		inspector.classList.add('hidden');
		selectedSessionId = '';
		if (restoreFocus) {
			var restoreTarget = validInspectorRestoreTarget(inspectorLauncher);
			if (!restoreTarget && inspectorLauncherSession) {
				var sessionURL = requestURL('/sessions/' + encodeURIComponent(inspectorLauncherSession), {});
				var matches = Array.from(document.querySelectorAll('.session-row-open[data-session-link="' + cssEscape(sessionURL) + '"], a[href="' + cssEscape(sessionURL) + '"]'));
				restoreTarget = matches.map(validInspectorRestoreTarget).find(Boolean) || null;
			}
			if (!restoreTarget) {
				restoreTarget = document.getElementById('dashboard-session-search') || document.getElementById('dashboard-title') || document.getElementById('timeline-toggle-btn');
			}
			if (restoreTarget && typeof restoreTarget.focus === 'function') {
				restoreTarget.focus({preventScroll: true});
			}
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
			window.closeSessionInspector({restoreFocus: false});
		}
		var close = evt.target.closest && evt.target.closest('[data-inspector-close]');
		if (close) {
			evt.preventDefault();
			window.closeSessionInspector();
			return;
		}
		var link = evt.target.closest && evt.target.closest('a[href^="/sessions/"]');
		if (link && link.id !== 'inspector-full-link' && !link.closest('#activity-feed') && !link.closest('[data-transcript-link]')) {
			evt.preventDefault();
			window.goToSession(link.getAttribute('href'), link);
			return;
		}
		var truncationToggle = evt.target.closest && evt.target.closest('[data-truncate-toggle]');
		if (truncationToggle) {
			evt.preventDefault();
			ensureTranscriptHelpers();
			window.toggleTruncation(truncationToggle.closest('.truncatable'));
			return;
		}
		var copyButton = evt.target.closest && evt.target.closest('[data-copy-to-clipboard]');
		if (copyButton) {
			evt.preventDefault();
			ensureTranscriptHelpers();
			window.copyToClipboard(copyButton);
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
		fetch(requestURL('/api/tool-payloads/' + encodeURIComponent(btn.getAttribute('data-event-id')), {}), payloadOpts)
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
	});
})();
