(function(global) {
	var dashboard = global.BeaconDashboard = global.BeaconDashboard || {};

	function escapeHTML(value) {
		return String(value == null ? '' : value).replace(/[&<>"']/g, function(ch) {
			return ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'})[ch];
		});
	}

	function escapeAttr(value) {
		return escapeHTML(value).replace(/`/g, '&#96;');
	}

	function numericValue(value, fallback) {
		var n = Number(value);
		if (Number.isFinite(n)) return n;
		n = Number(fallback || 0);
		return Number.isFinite(n) ? n : 0;
	}

	function nonNegativeInt(value, fallback) {
		return Math.max(0, Math.floor(numericValue(value, fallback)));
	}

	function cssEscape(value) {
		if (global.CSS && global.CSS.escape) return global.CSS.escape(value);
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

	function providerShort(provider) {
		if (provider === 'anthropic') return 'Claude Code';
		if (provider === 'openai') return 'Codex';
		return provider || '';
	}

	function formatTokens(n) {
		n = numericValue(n, 0);
		if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M';
		if (n >= 1000) return (n / 1000).toFixed(1) + 'K';
		return String(n);
	}

	function relativeTime(value, now) {
		var t = value ? new Date(value) : null;
		if (!t || isNaN(t.getTime())) return '';
		var base = now == null ? Date.now() : Number(now);
		var seconds = Math.max(0, Math.floor((base - t.getTime()) / 1000));
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

	function absoluteTime(value) {
		var t = value ? new Date(value) : null;
		if (!t || isNaN(t.getTime())) return '';
		return t.toLocaleString(undefined, {
			year: 'numeric',
			month: 'numeric',
			day: 'numeric',
			hour: 'numeric',
			minute: '2-digit'
		});
	}

	function durationSeconds(session) {
		var start = session && session.started_at ? new Date(session.started_at) : null;
		var end = session && session.ended_at ? new Date(session.ended_at) : null;
		if (!start || !end || isNaN(start.getTime()) || isNaN(end.getTime())) return 0;
		return Math.max(0, Math.floor((end.getTime() - start.getTime()) / 1000));
	}

	function requestURL(path, params) {
		var query = new URLSearchParams();
		Object.keys(params || {}).forEach(function(key) {
			var value = params[key];
			if (value !== undefined && value !== null && (value !== '' || key === 'range' || key.slice(-6) === '_range')) query.set(key, value);
		});
		var qs = query.toString();
		return path + (qs ? '?' + qs : '');
	}

	dashboard.utils = {
		escapeHTML: escapeHTML,
		escapeAttr: escapeAttr,
		numericValue: numericValue,
		nonNegativeInt: nonNegativeInt,
		cssEscape: cssEscape,
		shortID: shortID,
		shortModel: shortModel,
		providerShort: providerShort,
		formatTokens: formatTokens,
		relativeTime: relativeTime,
		absoluteTime: absoluteTime,
		durationSeconds: durationSeconds,
		requestURL: requestURL
	};

	if (typeof module !== 'undefined' && module.exports) {
		module.exports = dashboard.utils;
	}
})(typeof window !== 'undefined' ? window : globalThis);
