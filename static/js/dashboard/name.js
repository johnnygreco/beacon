(function(global) {
	var dashboard = global.BeaconDashboard = global.BeaconDashboard || {};
	var storageKey = 'beacon-dashboard-name';
	var maxNameLength = 80;
	var fallbackDocumentTitle = 'Dashboard';
	var fallbackHeading = 'Beacon Realtime Dashboard';

	function normalizeDashboardName(value) {
		var text = String(value == null ? '' : value)
			.replace(/[\u0000-\u001f\u007f-\u009f]/g, function(ch) {
				return /[\t\n\v\f\r\u001c-\u001f\u0085]/.test(ch) ? ' ' : '';
			})
			.replace(/\s+/g, ' ')
			.trim();
		var chars = Array.from(text);
		if (chars.length > maxNameLength) return chars.slice(0, maxNameLength).join('');
		return text;
	}

	function clampInputValue(input) {
		var chars = Array.from(input.value || '');
		if (chars.length > maxNameLength) {
			input.value = chars.slice(0, maxNameLength).join('');
		}
	}

	function readStoredOverride() {
		try {
			var raw = global.localStorage.getItem(storageKey) || '';
			var normalized = normalizeDashboardName(raw);
			if (normalized) {
				if (normalized !== raw) global.localStorage.setItem(storageKey, normalized);
				return normalized;
			}
			if (raw) global.localStorage.removeItem(storageKey);
		} catch (err) {
		}
		return '';
	}

	function writeStoredOverride(value) {
		var normalized = normalizeDashboardName(value);
		try {
			if (normalized) {
				global.localStorage.setItem(storageKey, normalized);
			} else {
				global.localStorage.removeItem(storageKey);
			}
		} catch (err) {
		}
		return normalized;
	}

	function initDashboardName() {
		var control = document.querySelector('[data-dashboard-name-control]');
		if (!control) return;

		var titleEl = document.getElementById('dashboard-title');
		var input = document.getElementById('dashboard-name-input');
		var editButton = document.getElementById('dashboard-name-edit');
		var defaultName = normalizeDashboardName(control.getAttribute('data-dashboard-default-name') || '');
		var headingFallback = normalizeDashboardName(control.getAttribute('data-dashboard-fallback-heading') || fallbackHeading) || fallbackHeading;
		var storedOverride = readStoredOverride();

		function effectiveName() {
			return storedOverride || defaultName;
		}

		function renderName(syncInput) {
			var name = effectiveName();
			if (titleEl) titleEl.textContent = name || headingFallback;
			document.title = (name || fallbackDocumentTitle) + ' | Beacon';
			control.setAttribute('data-dashboard-effective-name', name);
			if (input) {
				input.placeholder = defaultName || headingFallback;
				if (syncInput) input.value = name;
			}
		}

		function startEditing() {
			if (!input) return;
			input.value = storedOverride || effectiveName();
			control.classList.add('is-editing');
			if (titleEl) titleEl.classList.add('hidden');
			input.classList.remove('hidden');
			input.focus();
			input.select();
		}

		function finishEditing() {
			if (!input) return;
			input.classList.add('hidden');
			if (titleEl) titleEl.classList.remove('hidden');
			control.classList.remove('is-editing');
			renderName(true);
		}

		if (editButton) {
			editButton.addEventListener('click', startEditing);
		}
		if (input) {
			input.addEventListener('input', function() {
				clampInputValue(input);
				storedOverride = writeStoredOverride(input.value);
				renderName(false);
			});
			input.addEventListener('keydown', function(evt) {
				if (evt.key === 'Enter' || evt.key === 'Escape') {
					evt.preventDefault();
					finishEditing();
				}
			});
		}
		control.addEventListener('focusout', function(evt) {
			var next = evt.relatedTarget || null;
			if (next && typeof control.contains === 'function' && control.contains(next)) return;
			finishEditing();
		});

		renderName(true);
	}

	dashboard.name = {
		init: initDashboardName,
		normalize: normalizeDashboardName,
		storageKey: storageKey
	};

	if (document.readyState === 'loading') {
		document.addEventListener('DOMContentLoaded', initDashboardName, {once: true});
	} else {
		initDashboardName();
	}
})(typeof window !== 'undefined' ? window : globalThis);
