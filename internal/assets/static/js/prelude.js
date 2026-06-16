(function() {
	var fallbackTheme = 'codex-dark';
	var support = {
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
	var theme = fallbackTheme;
	try {
		theme = localStorage.getItem('beacon-dashboard-resolved-theme') || theme;
		var base = localStorage.getItem('beacon-dashboard-theme');
		var appearance = localStorage.getItem('beacon-dashboard-preferred-appearance') || localStorage.getItem('beacon-dashboard-appearance') || 'dark';
		if (base && support[base]) {
			theme = support[base][appearance] || support[base].dark || support[base].light || theme;
		}
		if (localStorage.getItem('beacon-timeline-width') === '0') {
			document.documentElement.setAttribute('data-beacon-timeline-collapsed', 'true');
		}
	} catch (err) {}
	if (!/^[a-z0-9-]+$/.test(theme)) theme = fallbackTheme;
	document.documentElement.setAttribute('data-dashboard-theme', theme);
})();

(function() {
	if (!window.console || typeof window.console.error !== 'function' || window.__beaconConsoleFilterInstalled) return;
	var originalError = window.console.error.bind(window.console);
	window.__beaconUnloading = false;
	window.addEventListener('beforeunload', function() {
		window.__beaconUnloading = true;
	}, {capture: true});
	window.console.error = function() {
		if (window.__beaconUnloading && arguments.length === 1 && arguments[0] === '[object Event]') return;
		if (window.__beaconUnloading && arguments.length === 1 && arguments[0] && typeof arguments[0] === 'object') {
			var tag = Object.prototype.toString.call(arguments[0]);
			if (tag === '[object Event]' || tag === '[object ErrorEvent]') return;
		}
		if (arguments.length === 1 && arguments[0] && typeof arguments[0] === 'object') {
			var event = arguments[0];
			var eventTag = Object.prototype.toString.call(event);
			var eventTarget = event.target || event.currentTarget || event.srcElement;
			var targetTag = Object.prototype.toString.call(eventTarget);
			var looksLikeEventSource = eventTarget &&
				(targetTag === '[object EventSource]' ||
					eventTarget.constructor && eventTarget.constructor.name === 'EventSource' ||
					(typeof eventTarget.close === 'function' && typeof eventTarget.url === 'string'));
			if (eventTag === '[object Event]' && event.type === 'error' && looksLikeEventSource) return;
		}
		originalError.apply(window.console, arguments);
	};
	window.__beaconConsoleFilterInstalled = true;
})();
