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
