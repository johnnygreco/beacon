// --- Dashboard theme selector ---
(function() {
	var STORAGE_KEY = 'beacon-dashboard-theme';
	var APPEARANCE_KEY = 'beacon-dashboard-appearance';
	var RESOLVED_KEY = 'beacon-dashboard-resolved-theme';
	var FALLBACK_THEME = 'codex';
	var FALLBACK_APPEARANCE = 'dark';
	var THEME_SUPPORT = {
		'absolutely': { light: 'absolutely-light', dark: 'absolutely-dark' },
		'catppuccin': { light: 'catppuccin-light', dark: 'catppuccin-dark' },
		'codex': { light: 'codex-light', dark: 'codex-dark' },
		'everforest': { light: 'everforest-light', dark: 'everforest-dark' },
		'github': { light: 'github-light', dark: 'github-dark' },
		'gruvbox': { light: 'gruvbox-light', dark: 'gruvbox-dark' },
		'linear': { light: 'linear-light', dark: 'linear-dark' },
		'notion': { light: 'notion-light', dark: 'notion-dark' },
		'one': { light: 'one-light', dark: 'one-dark' },
		'rose-pine': { light: 'rose-pine-light', dark: 'rose-pine-dark' },
		'raycast': { light: 'raycast-light', dark: 'raycast-dark' },
		'solarized': { light: 'solarized-light', dark: 'solarized-dark' },
		'vercel': { light: 'vercel-light', dark: 'vercel-dark' },
		'vs-code-plus': { light: 'vs-code-plus-light', dark: 'vs-code-plus-dark' },
		'xcode': { light: 'xcode-light', dark: 'xcode-dark' },
		'ayu': { dark: 'ayu-dark' },
		'dracula': { dark: 'dracula-dark' },
		'lobster': { dark: 'lobster-dark' },
		'material': { dark: 'material-dark' },
		'matrix': { dark: 'matrix-dark' },
		'monokai': { dark: 'monokai-dark' },
		'night-owl': { dark: 'night-owl-dark' },
		'nord': { dark: 'nord-dark' },
		'oscurange': { dark: 'oscurange-dark' },
		'sentry': { dark: 'sentry-dark' },
		'temple': { dark: 'temple-dark' },
		'tokyo-night': { dark: 'tokyo-night-dark' },
		'proof': { light: 'proof-light' }
	};
	var APPEARANCE_IDS = ['light', 'dark'];
	var THEME_IDS = Object.keys(THEME_SUPPORT);

	function hasTheme(id) {
		return Object.prototype.hasOwnProperty.call(THEME_SUPPORT, id);
	}

	function hasAppearance(mode) {
		return APPEARANCE_IDS.indexOf(mode) !== -1;
	}

	function storageGet(key) {
		try { return localStorage.getItem(key); } catch (err) { return null; }
	}

	function storageSet(key, value) {
		try { localStorage.setItem(key, value); } catch (err) {}
	}

	function normalizedAppearance(mode) {
		return hasAppearance(mode) ? mode : FALLBACK_APPEARANCE;
	}

	function supportedAppearance(theme, appearance) {
		if (!hasTheme(theme)) theme = FALLBACK_THEME;
		var support = THEME_SUPPORT[theme];
		appearance = normalizedAppearance(appearance);
		if (support[appearance]) return appearance;
		return support.dark ? 'dark' : 'light';
	}

	function isFixedTheme(theme) {
		if (!hasTheme(theme)) theme = FALLBACK_THEME;
		var support = THEME_SUPPORT[theme];
		return !(support.light && support.dark);
	}

	function resolveTheme(theme, appearance) {
		if (!hasTheme(theme)) theme = FALLBACK_THEME;
		var support = THEME_SUPPORT[theme];
		var preferred = supportedAppearance(theme, appearance);
		return support[preferred] || support.dark || support.light || THEME_SUPPORT[FALLBACK_THEME].dark;
	}

	function syncThemeControl(theme, appearance) {
		var select = document.getElementById('dashboard-theme-select');
		var toggle = document.getElementById('dashboard-appearance-toggle');
		var swatch = document.getElementById('dashboard-theme-swatch');
		if (select) select.value = theme;
		appearance = supportedAppearance(theme, appearance);
		if (toggle) {
			var fixed = isFixedTheme(theme);
			var dark = appearance === 'dark';
			toggle.disabled = fixed;
			toggle.classList.toggle('is-fixed', fixed);
			toggle.classList.toggle('is-dark', dark);
			toggle.classList.toggle('is-light', !dark);
			toggle.setAttribute('aria-checked', dark ? 'true' : 'false');
			toggle.setAttribute('aria-label', 'Dark mode');
			var title = fixed
				? ((select && select.selectedOptions && select.selectedOptions[0] ? select.selectedOptions[0].textContent : theme) + ' is ' + appearance + ' only')
				: (dark ? 'Switch to light mode' : 'Switch to dark mode');
			toggle.setAttribute('title', title);
			var moon = toggle.querySelector('.appearance-icon-moon');
			var sun = toggle.querySelector('.appearance-icon-sun');
			if (moon) moon.classList.toggle('hidden', !dark);
			if (sun) sun.classList.toggle('hidden', dark);
		}
		if (swatch) {
			var label = select && select.selectedOptions && select.selectedOptions[0]
				? select.selectedOptions[0].textContent
				: theme;
			label += ' / ' + appearance;
			swatch.setAttribute('title', label);
		}
	}

	function applyTheme(theme, appearance, persist) {
		if (!hasTheme(theme)) theme = FALLBACK_THEME;
		appearance = supportedAppearance(theme, appearance);
		var resolved = resolveTheme(theme, appearance);
		document.documentElement.setAttribute('data-dashboard-theme', resolved);
		if (persist) {
			storageSet(STORAGE_KEY, theme);
			storageSet(APPEARANCE_KEY, appearance);
			if (!isFixedTheme(theme)) {
				storageSet('beacon-dashboard-preferred-appearance', appearance);
			}
		}
		storageSet(RESOLVED_KEY, resolved);
		syncThemeControl(theme, appearance);
		window.dispatchEvent(new CustomEvent('beacon:dashboard-theme-change', {
			detail: { theme: resolved, baseTheme: theme, appearance: appearance }
		}));
	}

	window.setDashboardTheme = function(theme) {
		var appearance = isFixedTheme(theme)
			? (storageGet(APPEARANCE_KEY) || FALLBACK_APPEARANCE)
			: (storageGet('beacon-dashboard-preferred-appearance') || storageGet(APPEARANCE_KEY) || FALLBACK_APPEARANCE);
		applyTheme(theme, appearance, true);
	};
	window.setDashboardAppearance = function(appearance) {
		applyTheme(storageGet(STORAGE_KEY) || FALLBACK_THEME, appearance, true);
	};
	window.toggleDashboardAppearance = function() {
		var theme = storageGet(STORAGE_KEY) || FALLBACK_THEME;
		var current = supportedAppearance(theme, storageGet(APPEARANCE_KEY) || FALLBACK_APPEARANCE);
		if (isFixedTheme(theme)) {
			applyTheme(theme, current, true);
			return;
		}
		applyTheme(theme, current === 'dark' ? 'light' : 'dark', true);
	};
	window.dashboardThemeIDs = THEME_IDS.slice();

	var initialTheme = storageGet(STORAGE_KEY) || FALLBACK_THEME;
	var initialAppearance = storageGet(APPEARANCE_KEY) || FALLBACK_APPEARANCE;
	applyTheme(initialTheme, initialAppearance, false);

	var select = document.getElementById('dashboard-theme-select');
	if (select) {
		select.addEventListener('change', function() {
			window.setDashboardTheme(select.value);
		});
	}
})();

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

	function escapeHTML(value) {
		return String(value == null ? '' : value).replace(/[&<>"']/g, function(ch) {
			return ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'})[ch];
		});
	}

	function shortID(id) {
		return id && id.length > 8 ? id.slice(0, 8) : (id || '');
	}

	function metric(label, value) {
		return '<div><p class="text-xs text-gray-500">' + label + '</p><p class="text-gray-200 font-medium">' + escapeHTML(value) + '</p></div>';
	}

	function renderSummary(session) {
		title.textContent = session ? shortID(session.id) : 'Session';
		subtitle.textContent = session ? session.id : selectedSessionId;
		fullLink.href = '/sessions/' + encodeURIComponent(selectedSessionId);
		if (!session) {
			summary.innerHTML = metric('Status', 'Loading');
			return;
		}
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

	function eventRow(event) {
		var meta = [event.event_kind, event.actor_role, event.tool_name, event.model].filter(Boolean).join(' · ');
		var payloadID = 'payload-' + escapeAttr(event.event_uid);
		var payloadButton = event.tool_name ? '<button type="button" class="payload-btn text-xs text-blue-400 hover:text-blue-300" data-event-id="' + escapeAttr(event.event_uid) + '" aria-expanded="false" aria-controls="' + payloadID + '">Payload</button>' : '';
		return '<div class="rounded border border-gray-800 bg-gray-900/60 p-3" data-event="' + escapeHTML(event.event_uid) + '">' +
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
			events.innerHTML = items.length ? items.map(eventRow).join('') : '<div class="text-sm text-gray-500">No events</div>';
		} catch (err) {
			if (err && err.name === 'AbortError') return;
			if (seq !== inspectorSeq) return;
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

// --- Sidebar drag-to-resize ---
(function() {
	var divider = document.getElementById('sidebar-divider');
	var sidebar = document.getElementById('timeline-sidebar');
	var wrap = document.getElementById('dashboard-wrap');
	var DIVIDER_WIDTH = 8;
	var SNAP_THRESHOLD = 100; // drag below this to collapse
	var MIN_WIDTH = 200;
	var MAX_WIDTH = 700;
	var DEFAULT_WIDTH = 380;
	var dragging = false;
	var resizeRAF = 0;

	function storageGet(key) {
		try { return localStorage.getItem(key); } catch (err) { return null; }
	}

	function storageSet(key, value) {
		try { localStorage.setItem(key, value); } catch (err) {}
	}

	function resizeCharts() {
		if (window.dashboardTokenCumulativeChart) window.dashboardTokenCumulativeChart.resize();
		if (window.dashboardModelActivityChart) window.dashboardModelActivityChart.resize();
	}

	function resizeChartsSoon() {
		requestAnimationFrame(function() {
			resizeCharts();
			setTimeout(resizeCharts, 220);
		});
	}

	function syncToggleButton() {
		var btn = document.getElementById('timeline-toggle-btn');
		if (!btn) return;
		var collapsed = isCollapsed();
		btn.setAttribute('aria-expanded', collapsed ? 'false' : 'true');
		var label = collapsed ? 'Expand activity timeline' : 'Collapse activity timeline';
		btn.setAttribute('aria-label', label);
		btn.setAttribute('title', label);
	}

	function syncDivider() {
		if (!divider) return;
		var parsed = parseInt(sidebar.style.width, 10);
		var width = isCollapsed() ? 0 : (Number.isFinite(parsed) ? parsed : DEFAULT_WIDTH);
		divider.setAttribute('aria-valuenow', String(width));
		divider.setAttribute('aria-valuetext', width > 0 ? ('Activity timeline width ' + width + ' pixels') : 'Activity timeline collapsed');
	}

	function maxWidthForViewport() {
		if (!wrap || !wrap.offsetWidth) return MAX_WIDTH;
		var maxAllowed = Math.floor(wrap.offsetWidth * 0.5);
		if (maxAllowed < MIN_WIDTH) return MIN_WIDTH;
		return Math.min(MAX_WIDTH, maxAllowed);
	}

	function clampWidth(w) {
		w = parseInt(w, 10) || DEFAULT_WIDTH;
		return Math.min(maxWidthForViewport(), Math.max(MIN_WIDTH, w));
	}

	function syncSidebarAccessibility() {
		var collapsed = isCollapsed();
		sidebar.toggleAttribute('inert', collapsed);
		sidebar.setAttribute('aria-hidden', collapsed ? 'true' : 'false');
	}

	function setSidebarWidth(w) {
		w = Number(w) === 0 ? 0 : clampWidth(w);
		sidebar.style.width = w + 'px';
		sidebar.style.minWidth = w + 'px';
		syncDivider();
	}

	function collapse() {
		var current = parseInt(sidebar.style.width, 10) || DEFAULT_WIDTH;
		if (current > 0) {
			storageSet('beacon-timeline-prev-width', current);
		}
		sidebar.classList.add('collapsed');
		document.documentElement.setAttribute('data-beacon-timeline-collapsed', 'true');
		storageSet('beacon-timeline-width', '0');
		if (sidebar.contains(document.activeElement)) {
			var btn = document.getElementById('timeline-toggle-btn');
			(btn || divider).focus({preventScroll: true});
		}
		syncSidebarAccessibility();
		syncToggleButton();
		syncDivider();
	}

	function expand(w) {
		w = clampWidth(w);
		sidebar.classList.remove('collapsed');
		document.documentElement.removeAttribute('data-beacon-timeline-collapsed');
		setSidebarWidth(w);
		storageSet('beacon-timeline-width', w);
		syncSidebarAccessibility();
		syncToggleButton();
		syncDivider();
	}

	function isCollapsed() {
		return sidebar.classList.contains('collapsed');
	}

	// Restore saved state
	var savedWidth = storageGet('beacon-timeline-width');
	if (savedWidth === '0') {
		sidebar.classList.add('collapsed');
		document.documentElement.setAttribute('data-beacon-timeline-collapsed', 'true');
	} else {
		document.documentElement.removeAttribute('data-beacon-timeline-collapsed');
		var w = parseInt(savedWidth, 10);
		if (w && w >= MIN_WIDTH) {
			setSidebarWidth(w);
		}
	}
	syncSidebarAccessibility();
	syncToggleButton();
	syncDivider();

	// Responsive: constrain sidebar on small screens
	function constrainForViewport() {
		if (isCollapsed()) return;
		var current = parseInt(sidebar.style.width, 10) || DEFAULT_WIDTH;
		setSidebarWidth(current);
	}
	constrainForViewport();
	window.addEventListener('resize', function() {
		constrainForViewport();
		resizeCharts();
	});

	// Use pointer events for unified mouse + touch support
	divider.addEventListener('pointerdown', function(e) {
		e.preventDefault();
		divider.setPointerCapture(e.pointerId);
		dragging = true;
		// If collapsed, uncollapse so drag can set a width
		if (isCollapsed()) {
			sidebar.classList.remove('collapsed');
			document.documentElement.removeAttribute('data-beacon-timeline-collapsed');
			syncSidebarAccessibility();
			setSidebarWidth(0);
		}
		document.body.style.cursor = 'col-resize';
		document.body.style.userSelect = 'none';
		wrap.classList.add('sidebar-resizing');
	});

	document.addEventListener('pointermove', function(e) {
		if (!dragging) return;
		var wrapRect = wrap.getBoundingClientRect();
		var rawWidth = wrapRect.right - e.clientX - DIVIDER_WIDTH;
		// Snap zone: show collapse intent
		if (rawWidth < SNAP_THRESHOLD) {
			setSidebarWidth(0);
		} else {
			setSidebarWidth(rawWidth);
		}
		// Debounced chart resize during drag
		if (!resizeRAF) {
			resizeRAF = requestAnimationFrame(function() {
				resizeCharts();
				resizeRAF = 0;
			});
		}
	});

	document.addEventListener('pointerup', function() {
		if (!dragging) return;
		dragging = false;
		document.body.style.cursor = '';
		document.body.style.userSelect = '';
		wrap.classList.remove('sidebar-resizing');
		var currentWidth = parseInt(sidebar.style.width, 10);
		if (currentWidth < SNAP_THRESHOLD) {
			collapse();
		} else {
			currentWidth = clampWidth(currentWidth);
			setSidebarWidth(currentWidth);
			storageSet('beacon-timeline-width', currentWidth);
			syncToggleButton();
			syncDivider();
		}
		resizeCharts();
	});

	// Double-click to reset to default width (or expand if collapsed)
	divider.addEventListener('dblclick', function() {
		expand(DEFAULT_WIDTH);
		resizeChartsSoon();
	});

	divider.addEventListener('keydown', function(e) {
		var step = e.shiftKey ? 80 : 24;
		var current = isCollapsed() ? 0 : (parseInt(sidebar.style.width, 10) || DEFAULT_WIDTH);
		if (e.key === 'ArrowLeft') {
			e.preventDefault();
			expand(current + step);
			resizeChartsSoon();
		} else if (e.key === 'ArrowRight') {
			e.preventDefault();
			var next = current - step;
			if (next < MIN_WIDTH) collapse();
			else expand(next);
			resizeChartsSoon();
		} else if (e.key === 'Home') {
			e.preventDefault();
			collapse();
			resizeChartsSoon();
		} else if (e.key === 'End') {
			e.preventDefault();
			expand(DEFAULT_WIDTH);
			resizeChartsSoon();
		} else if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			window.toggleTimelineSidebar();
		}
	});

	window.toggleTimelineSidebar = function() {
		if (isCollapsed()) {
			var restored = parseInt(storageGet('beacon-timeline-prev-width'), 10) || DEFAULT_WIDTH;
			expand(restored);
		} else {
			collapse();
		}
		resizeChartsSoon();
	};

	// Keyboard shortcut: T toggles collapsed/expanded
	document.addEventListener('keydown', function(e) {
			if (e.key === 'T' || e.key === 't') {
				if (e.metaKey || e.ctrlKey || e.altKey || e.shiftKey) return;
				if (e.target.closest && e.target.closest('input, textarea, select, button, a, [contenteditable="true"]')) return;
				e.preventDefault();
				if (isCollapsed()) {
					var restored = parseInt(storageGet('beacon-timeline-prev-width'), 10) || DEFAULT_WIDTH;
					expand(restored);
				} else {
					collapse();
				}
				resizeChartsSoon();
			}
		});
})();

// --- JSON dashboard stores ---
var currentActivityFilter = 'all';
var currentRange = '24h';
var currentDashboardMetric = 'error_rate';
var currentCompletedOffset = 0;
var currentSearchQuery = '';
var currentSearchRange = '';
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
window.dashboardSessionIndex = window.dashboardSessionIndex || {};

function escapeHTML(value) {
	return String(value == null ? '' : value).replace(/[&<>"']/g, function(ch) {
		return ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'})[ch];
	});
}

function escapeAttr(value) {
	return escapeHTML(value).replace(/`/g, '&#96;');
}

function cssEscape(value) {
	if (window.CSS && CSS.escape) return CSS.escape(value);
	return String(value || '').replace(/["\\]/g, '\\$&');
}

function shortID(id) {
	return id && id.length > 8 ? id.slice(0, 8) : (id || '');
}

function shortModel(model) {
	model = String(model || '').replace(/^claude-/, '');
	var idx = model.indexOf('-202');
	return idx > 0 ? model.slice(0, idx) : model;
}

function modelChip(model) {
	if (!model) return '';
	return '<span class="px-1.5 py-0.5 rounded bg-gray-700/80 text-gray-300 max-w-full truncate" title="' + escapeAttr(model) + '">' + escapeHTML(shortModel(model)) + '</span>';
}

function providerShort(provider) {
	if (provider === 'anthropic') return 'Claude Code';
	if (provider === 'openai') return 'Codex';
	return provider || '';
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

function formatTokens(n) {
	n = Number(n || 0);
	if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M';
	if (n >= 1000) return (n / 1000).toFixed(1) + 'K';
	return String(n);
}

function relativeTime(value) {
	var t = value ? new Date(value) : null;
	if (!t || isNaN(t.getTime())) return '';
	var seconds = Math.max(0, Math.floor((Date.now() - t.getTime()) / 1000));
	if (seconds < 60) return 'just now';
	if (seconds < 3600) return Math.floor(seconds / 60) + 'm ago';
	if (seconds < 86400) return Math.floor(seconds / 3600) + 'h ago';
	if (seconds < 30 * 86400) return Math.floor(seconds / 86400) + 'd ago';
	return t.toLocaleString(undefined, {
		year: 'numeric',
		month: 'numeric',
		day: 'numeric',
		hour: 'numeric',
		minute: '2-digit'
	});
}

function durationSeconds(session) {
	var start = session.started_at ? new Date(session.started_at) : null;
	var end = session.ended_at ? new Date(session.ended_at) : null;
	if (!start || !end || isNaN(start.getTime()) || isNaN(end.getTime())) return 0;
	return Math.max(0, Math.floor((end.getTime() - start.getTime()) / 1000));
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
	el.innerHTML = html;
	el.__beaconRenderSignature = html;
	return true;
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

function requestURL(path, params) {
	var query = new URLSearchParams();
	Object.keys(params || {}).forEach(function(key) {
		var value = params[key];
		if (value !== undefined && value !== null && (value !== '' || key === 'range')) query.set(key, value);
	});
	var qs = query.toString();
	return path + (qs ? '?' + qs : '');
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
	var rowClass = isSubagent ? 'border-b border-gray-800/50 cursor-pointer transition-colors bg-gray-800/20' : 'border-b border-gray-800/50 cursor-pointer transition-colors';
	var nameCellClass = isSubagent ? 'py-1.5 px-3 text-sm text-gray-400 whitespace-nowrap pl-10' : 'py-2 px-3 text-sm text-gray-300 whitespace-nowrap';
	var mobileMeta = formatTokens(session.total_tokens) + ' tok · ' + Number(session.turn_count || 0) + ' turns · ' + Number(session.tool_call_count || 0) + ' tools · ' + (session.duration || relativeTime(session.ended_at));
	var toggle = '';
	if (!isSubagent && session.subagent_count > 0) {
		toggle = '<button type="button" class="json-subagent-toggle text-gray-500 hover:text-gray-300 transition-colors flex-shrink-0" data-session-id="' + escapeAttr(session.id) + '" title="' + session.subagent_count + ' subagents" aria-label="Toggle subagents" aria-expanded="false"><svg class="w-3.5 h-3.5 transition-transform" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7"></path></svg></button>';
	}
	var subPrefix = isSubagent ? '<span class="w-1.5 h-1.5 rounded-full bg-blue-400/50 flex-shrink-0"></span><span class="text-blue-400/70 text-xs">sub</span>' : '';
	var subCount = !isSubagent && session.subagent_count > 0 ? '<span class="text-[10px] text-blue-400/60 font-normal">+' + session.subagent_count + ' sub</span>' : '';
	var sessionURL = '/sessions/' + encodeURIComponent(session.id);
	var titleButton = '<button type="button" class="session-row-open text-left transition-colors hover:text-blue-300 focus-visible:text-blue-300" data-session-link="' + sessionURL + '" aria-label="Open session ' + escapeAttr(sessionTitle(session)) + '">' + escapeHTML(sessionTitle(session)) + '</button>';
	var rowActionAttrs = ' data-session-link="' + sessionURL + '"';
	var attrs = isSubagent ? ' data-parent="' + escapeAttr(parentID) + '"' : ' id="session-row-' + escapeAttr(session.id) + '"' +
		' data-sort-name="' + escapeAttr(sessionTitle(session)) + '"' +
		' data-sort-provider="' + escapeAttr(providerShort(session.provider)) + '"' +
		' data-sort-model="' + escapeAttr(session.last_model || '') + '"' +
		' data-sort-tokens="' + Number(session.total_tokens || 0) + '"' +
		' data-sort-turns="' + Number(session.turn_count || 0) + '"' +
		' data-sort-tools="' + Number(session.tool_call_count || 0) + '"' +
		' data-sort-duration="' + durationSeconds(session) + '"' +
		' data-sort-project="' + escapeAttr(session.working_dir || '') + '"' +
		' data-sort-ended="' + Math.floor(new Date(session.ended_at || 0).getTime() / 1000 || 0) + '"' +
		' data-sort-id="' + escapeAttr(session.id) + '"';
	return '<tr' + attrs + rowActionAttrs + ' class="' + rowClass + '">' +
		'<td class="' + nameCellClass + '"><span class="inline-flex items-center gap-1.5">' + toggle + subPrefix + titleButton + subCount + '</span><span class="mobile-session-meta hidden">' + escapeHTML(mobileMeta) + '</span></td>' +
		'<td class="py-2 px-3 text-xs whitespace-nowrap">' + (isSubagent ? '' : providerBadge(session.provider)) + '</td>' +
		'<td class="py-2 px-3 text-xs text-gray-400 max-w-[160px] truncate" title="' + escapeAttr(session.last_model || '') + '">' + escapeHTML(shortModel(session.last_model || '')) + '</td>' +
		'<td class="py-2 px-3 text-right text-xs text-gray-400 tabular-nums">' + formatTokens(session.total_tokens) + '</td>' +
		'<td class="py-2 px-3 text-right text-xs text-gray-400 tabular-nums">' + Number(session.turn_count || 0) + '</td>' +
		'<td class="py-2 px-3 text-right text-xs text-gray-400 tabular-nums">' + Number(session.tool_call_count || 0) + '</td>' +
		'<td class="py-2 px-3 text-right text-xs text-gray-400 tabular-nums whitespace-nowrap">' + escapeHTML(session.duration || '') + '</td>' +
		'<td class="py-2 px-3 text-xs text-gray-500 max-w-[180px] truncate" title="' + escapeAttr(session.working_dir || '') + '">' + escapeHTML(session.working_dir || '') + '</td>' +
		'<td class="py-2 px-3 text-right text-xs text-gray-500 tabular-nums whitespace-nowrap">' + relativeTime(session.ended_at) + '</td>' +
		'<td class="py-2 px-3 text-xs text-gray-600 font-mono whitespace-nowrap">' + escapeHTML(shortID(session.id)) + '</td>' +
		'</tr>';
}

function renderCompleted(response) {
	var tbody = document.getElementById('completed-sessions');
	var title = document.getElementById('completed-table-title');
	if (title) title.textContent = 'Completed Sessions';
	setCompletedTableMode('sessions');
	response.items = (response.items || []).filter(validSession);
	if ((response.items || []).length === 0 && response.offset > 0) {
		loadCompletedSessions(Math.max(0, response.offset - (response.limit || completedPageSize)));
		return;
	}
	var rows = (response.items || []).map(function(session) { return completedRow(session, false, ''); });
	var status = document.getElementById('completed-session-status');
	if ((response.items || []).length > 0 || response.offset > 0) {
		var prev = response.offset > 0 ? '<button type="button" class="json-page-btn px-3 py-1 text-xs rounded border border-gray-600 text-gray-400 hover:text-gray-200 hover:border-gray-500 transition-colors" data-offset="' + Math.max(0, response.offset - response.limit) + '">Previous</button>' : '';
		var next = response.has_more ? '<button type="button" class="json-page-btn px-3 py-1 text-xs rounded border border-gray-600 text-gray-400 hover:text-gray-200 hover:border-gray-500 transition-colors" data-offset="' + (response.offset + response.limit) + '">Next</button>' : '';
		var start = response.offset + 1;
		var end = response.offset + (response.items || []).length;
		rows.push('<tr class="border-none" data-pagination-row><td colspan="10" class="py-3"><div class="flex items-center justify-center gap-4">' + prev + '<span class="text-xs text-gray-500 tabular-nums">Showing ' + start + '-' + end + (response.has_more ? '+' : '') + '<\/span>' + next + '<\/div><\/td><\/tr>');
	}
	if (rows.length === 0) {
		rows.push('<tr><td colspan="10" class="text-center py-4"><span class="text-sm text-gray-500">' + (currentSearchQuery ? 'No sessions match your search' : 'No completed sessions') + '<\/span><\/td><\/tr>');
	}
	if (status) {
		var count = (response.items || []).length;
		var countLabel = count + (response.has_more ? '+' : '');
		status.textContent = currentSearchQuery ? (countLabel + ' search result' + (count === 1 && !response.has_more ? '' : 's') + ' in ' + rangeLabel(currentRange)) : (countLabel + ' shown for ' + rangeLabel(currentRange));
	}
	var changed = setHTMLIfChanged(tbody, rows.join(''));
	if (changed) updateCompletedSortIndicators();
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
	var score = currentSearchSort === 'relevance' && Number(item.score || 0) > 0 ? Number(item.score).toFixed(2) : '';
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
	var tbody = document.getElementById('completed-sessions');
	var status = document.getElementById('completed-session-status');
	var title = document.getElementById('completed-table-title');
	if (title) title.textContent = 'Search Results';
	setCompletedTableMode('search');
	response.items = response.items || [];
	var rows = response.items.map(searchRow);
	if (response.has_more) {
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
			status.textContent = count + (response.has_more ? '+' : '') + ' search result' + (count === 1 && !response.has_more ? '' : 's');
		}
	}
}

function renderDashboardSearchLoading() {
	var tbody = document.getElementById('completed-sessions');
	var status = document.getElementById('completed-session-status');
	var title = document.getElementById('completed-table-title');
	if (title) title.textContent = 'Search Results';
	setCompletedTableMode('search');
	setHTMLIfChanged(tbody, '<tr><td colspan="6" class="text-center py-4"><span class="text-sm text-gray-500">Searching sessions and events...</span></td></tr>');
	if (status) status.textContent = 'Searching sessions and events...';
}

function renderActive(response) {
	var wrap = document.getElementById('active-sessions');
	var items = (response.items || []).filter(validSession);
	var dot = items.length ? '<span class="relative flex h-2 w-2"><span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-400 opacity-75"></span><span class="relative inline-flex rounded-full h-2 w-2 bg-green-500"></span></span>' : '<span class="relative flex h-2 w-2"><span class="relative inline-flex rounded-full h-2 w-2 bg-gray-600"></span></span>';
	var count = items.length ? '<span class="text-xs font-normal text-gray-500">(' + items.length + ')</span>' : '';
	var cards = items.map(activeCard).join('');
	if (!cards) {
		cards = '<div class="text-center py-6 col-span-full"><p class="text-sm text-gray-500">No active sessions</p><p class="text-xs text-gray-600 mt-1">Sessions appear here when agents are running</p></div>';
	}
	setHTMLIfChanged(wrap, '<h2 class="text-lg font-semibold text-gray-200 mb-3 flex items-center gap-2">' + dot + 'Active Sessions ' + count + '</h2><div class="grid grid-cols-1 lg:grid-cols-2 gap-3">' + cards + '</div>');
}

function activeCard(session) {
	var live = session.status === 'active';
	var sub = !!session.parent_session_id;
	var border = sub ? (live ? 'border-blue-500/50' : 'border-red-500/40') : (live ? 'border-green-500/50' : 'border-red-500/30 border-dashed');
	var liveColor = sub ? 'blue' : 'green';
	var statusDot = live ? '<span class="relative flex h-2 w-2 flex-shrink-0"><span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-' + liveColor + '-400 opacity-75"></span><span class="relative inline-flex rounded-full h-2 w-2 bg-' + liveColor + '-500"></span></span>' : '<span class="relative flex h-2 w-2 flex-shrink-0"><span class="relative inline-flex rounded-full h-2 w-2 bg-red-500/60"></span></span>';
	if (sub) {
		return '<a href="/sessions/' + encodeURIComponent(session.id) + '" class="block rounded-lg overflow-hidden bg-gray-800/40 border-l-2 px-4 py-3 hover:bg-gray-700/20 transition-colors ' + border + '">' +
			'<div class="flex items-center justify-between gap-3"><div class="flex items-center gap-2 min-w-0">' + statusDot + '<span class="font-medium text-gray-100 truncate">' + escapeHTML(sessionTitle(session)) + '</span><span class="text-xs text-gray-600 font-mono flex-shrink-0">' + escapeHTML(shortID(session.id)) + '</span></div>' +
			'<span class="px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide rounded flex-shrink-0 ' + (live ? 'bg-blue-500/15 text-blue-400' : 'bg-red-500/15 text-red-400') + '">' + (live ? 'Sub' : 'Idle') + '</span></div>' +
			'<div class="flex items-center gap-2 text-xs text-gray-500 mt-1 ml-4 flex-wrap"><span class="text-blue-400/50">↑ ' + escapeHTML(shortID(session.parent_session_id)) + '</span>' + modelChip(session.last_model || '') + '<span>' + escapeHTML(session.duration || '') + '</span><span class="text-gray-700">·</span><span>' + Number(session.turn_count || 0) + ' turns</span><span class="text-gray-700">·</span><span>' + formatTokens(session.total_tokens) + ' tok</span><span class="text-gray-700">·</span><span>' + Number(session.tool_call_count || 0) + ' tools</span></div>' +
			(session.working_dir ? '<p class="text-[11px] text-gray-600 truncate mt-0.5 ml-4" title="' + escapeAttr(session.working_dir) + '">' + escapeHTML(session.working_dir) + '</p>' : '') +
			'</a>';
	}
	var childHTML = '';
	if ((session.child_sessions || []).length > 0) {
		childHTML = '<div class="border-t border-gray-700/30 px-4 py-2"><div class="text-[10px] uppercase tracking-wider text-blue-400/50 mb-1">' + (session.child_sessions.length === 1 ? '1 subagent' : session.child_sessions.length + ' subagents') + '</div>' + (session.child_sessions || []).map(function(child) {
			var childLive = child.status === 'active';
			var childDot = childLive ? '<span class="relative flex h-1.5 w-1.5 flex-shrink-0"><span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-blue-400 opacity-75"></span><span class="relative inline-flex rounded-full h-1.5 w-1.5 bg-blue-500"></span></span>' : '<span class="w-1.5 h-1.5 rounded-full bg-red-500/60 flex-shrink-0"></span>';
			var childModel = child.last_model ? '<span class="text-gray-400 truncate min-w-0 flex-1" title="' + escapeAttr(child.last_model) + '">' + escapeHTML(shortModel(child.last_model)) + '</span>' : '';
			return '<a href="/sessions/' + encodeURIComponent(child.id) + '" class="flex items-center gap-2 text-xs py-1 px-2 -mx-1 rounded hover:bg-gray-700/30 transition-colors min-w-0">' + childDot + '<span class="text-gray-500 font-mono flex-shrink-0">' + escapeHTML(shortID(child.id)) + '</span>' + childModel + '<span class="ml-auto flex items-center text-gray-500 tabular-nums flex-shrink-0"><span class="w-14 text-right">' + escapeHTML(child.duration || '') + '</span><span class="w-12 text-right">' + formatTokens(child.total_tokens) + '</span><span class="w-8 text-right">' + Number(child.tool_call_count || 0) + 't</span></span></a>';
		}).join('') + '</div>';
	}
	return '<div class="rounded-lg overflow-hidden border-l-2 bg-gray-800/60 ' + border + '">' +
		'<a href="/sessions/' + encodeURIComponent(session.id) + '" class="block px-4 py-3 hover:bg-gray-700/20 transition-colors">' +
		'<div class="flex items-center justify-between"><div class="flex items-center gap-2 min-w-0">' + statusDot + '<span class="font-medium text-gray-100 truncate">' + escapeHTML(sessionTitle(session)) + '</span><span class="text-xs text-gray-600 font-mono flex-shrink-0">' + escapeHTML(shortID(session.id)) + '</span></div><div class="flex items-center gap-1.5 flex-shrink-0 ml-2">' + providerBadge(session.provider) + '<span class="px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide rounded ' + (live ? 'bg-green-500/15 text-green-400' : 'bg-red-500/15 text-red-400') + '">' + (live ? 'Live' : 'Idle') + '</span></div></div>' +
		'<div class="flex items-center gap-2 text-xs text-gray-500 mt-1 ml-4 flex-wrap">' + modelChip(session.last_model || '') + '<span>' + escapeHTML(session.duration || '') + '</span><span class="text-gray-700">·</span><span>' + Number(session.turn_count || 0) + ' turns</span><span class="text-gray-700">·</span><span>' + formatTokens(session.total_tokens) + ' tok</span><span class="text-gray-700">·</span><span>' + Number(session.tool_call_count || 0) + ' tools</span></div>' +
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
		return '<a href="' + url + '" data-type="' + escapeAttr(item.type) + '" data-transcript-link="true" class="block relative py-2 pl-4 hover:bg-gray-800/50 rounded-lg transition-colors group"><div class="absolute left-[-8px] top-3.5 w-2.5 h-2.5 rounded-full ring-2 ring-gray-900 ' + activityDotColor(item.type) + '"></div><p class="text-sm text-gray-300 group-hover:text-gray-100 transition-colors mb-1">' + escapeHTML(item.summary) + '</p><div class="flex items-center gap-2 flex-wrap"><span class="px-1.5 py-0.5 rounded text-xs flex-shrink-0 ' + activityBadgeStyle(item.type) + '">' + escapeHTML(activityLabel(item.type)) + '</span>' + provider + sid + '<span class="text-xs text-gray-600 flex-shrink-0">' + escapeHTML(item.relative_time || relativeTime(item.timestamp)) + '</span></div></a>';
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
	n = Number(n || 0);
	if (n >= 10) return n.toFixed(1) + '%';
	if (n > 0) return n.toFixed(2) + '%';
	return '0%';
}

function renderAnalyticsSummary(summary) {
	var wrap = document.getElementById('dashboard-analytics-summary');
	if (!wrap) return;
	summary = summary || {};
	setHTMLIfChanged(wrap, [
		summaryTile('Tokens', formatTokens(summary.total_tokens), 'Cumulative across shown models'),
		summaryTile('Models', Number(summary.model_count || 0), 'Selectable series'),
		summaryTile('Tool Calls', formatTokens(summary.tool_call_count), rangeLabel(currentRange)),
		summaryTile('Error Rate', formatPercent(summary.error_rate), Number(summary.error_count || 0) + ' errors')
	].join(''));
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
		var tbody = document.getElementById('completed-sessions');
		var status = document.getElementById('completed-session-status');
		var title = document.getElementById('completed-table-title');
		if (title) title.textContent = 'Search Results';
		setCompletedTableMode('search');
		setHTMLIfChanged(tbody, '<tr><td colspan="6" class="text-center py-4"><span class="text-sm text-red-400">Unable to search sessions and events. <button type="button" class="underline" onclick="loadDashboardSearch()">Retry</button></span></td></tr>');
		if (status) status.textContent = 'Search failed';
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
		var tbody = document.getElementById('completed-sessions');
		setHTMLIfChanged(tbody, '<tr><td colspan="10" class="text-center py-4"><span class="text-sm text-red-400">Unable to load completed sessions. <button type="button" class="underline" onclick="loadCompletedSessions(currentCompletedOffset)">Retry</button></span></td></tr>');
		if (status) status.textContent = 'Unable to load sessions';
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
	if (tableHead) sessionTableHeadHTML = tableHead.innerHTML;
	var searchInput = document.getElementById('dashboard-session-search');
	var searchClear = document.getElementById('dashboard-search-clear');
	var searchFocus = document.getElementById('dashboard-search-focus');
	var searchSession = document.getElementById('dashboard-search-session');
	var searchSort = document.getElementById('dashboard-search-sort');
	var searchReset = document.getElementById('dashboard-search-reset');
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
			if (searchInput) searchInput.focus();
		});
	}
	if (searchFocus) {
		searchFocus.addEventListener('click', function() {
			if (searchInput) searchInput.focus();
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
			if (searchInput) searchInput.focus();
		});
	}
	document.addEventListener('keydown', function(evt) {
		var tagName = document.activeElement ? document.activeElement.tagName : '';
		if (evt.key === '/' && !evt.ctrlKey && !evt.metaKey && ['INPUT', 'TEXTAREA', 'SELECT'].indexOf(tagName) === -1) {
			evt.preventDefault();
			if (searchInput) searchInput.focus();
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

// --- Table sorting ---
function sortCompletedTable(control, column, preserveDirection) {
	var th = control && control.closest ? control.closest('th[data-sort-key]') : control;
	if (!th) return;
	if (sortColumn !== column) {
		sortColumn = column;
		// Default ascending for text, descending for numbers
		sortAsc = ['name', 'provider', 'model', 'project', 'id'].indexOf(column) >= 0;
	} else if (!preserveDirection) {
		sortAsc = !sortAsc;
	}
	updateCompletedSortIndicators();
	if (!preserveDirection) {
		sortCurrentCompletedRows(column);
		loadCompletedSessions(0);
	}
}

function sortCurrentCompletedRows(column) {
	var tbody = document.getElementById('completed-sessions');
	if (!tbody) return;
	var rows = Array.from(tbody.querySelectorAll('tr[data-sort-ended]:not([data-parent])'));
	var numericCols = ['tokens', 'turns', 'tools', 'duration', 'ended'];
	var isNumeric = numericCols.indexOf(column) >= 0;
	var paginationRow = tbody.querySelector('tr[data-pagination-row]');
	var subagentRows = Array.from(tbody.querySelectorAll('tr[data-parent]'));
	var subagentsByParent = {};
	subagentRows.forEach(function(row) {
		var pid = row.getAttribute('data-parent');
		if (!subagentsByParent[pid]) subagentsByParent[pid] = [];
		subagentsByParent[pid].push(row);
		row.remove();
	});
	rows.sort(function(a, b) {
		var aVal = a.getAttribute('data-sort-' + column) || '';
		var bVal = b.getAttribute('data-sort-' + column) || '';
		if (isNumeric) {
			var diff = (parseFloat(aVal) || 0) - (parseFloat(bVal) || 0);
			return sortAsc ? diff : -diff;
		}
		var cmp = aVal.localeCompare(bVal, undefined, {sensitivity: 'base'});
		return sortAsc ? cmp : -cmp;
	});
	rows.forEach(function(row) {
		if (paginationRow) tbody.insertBefore(row, paginationRow);
		else tbody.appendChild(row);
		var parentID = row.id.replace('session-row-', '');
		(subagentsByParent[parentID] || []).forEach(function(subRow) {
			row.after(subRow);
			row = subRow;
		});
	});
}

function updateCompletedSortIndicators() {
	document.querySelectorAll('#completed-table th[data-sort-key]').forEach(function(header) {
		header.setAttribute('aria-sort', 'none');
		var headerArrow = header.querySelector('.sort-arrow');
		if (headerArrow) {
			headerArrow.classList.remove('active');
			headerArrow.textContent = '▼';
		}
	});
	var th = document.querySelector('#completed-table th[data-sort-key="' + sortColumn + '"]');
	if (!th) return;
	th.setAttribute('aria-sort', sortAsc ? 'ascending' : 'descending');
	var arrow = th.querySelector('.sort-arrow');
	if (arrow) {
		arrow.classList.add('active');
		arrow.textContent = sortAsc ? '▲' : '▼';
	}
}
