(function(global) {
	var charts = global.BeaconCharts = global.BeaconCharts || {};

	function colorWithAlpha(color, alpha) {
		color = String(color || '').trim();
		if (/^#[0-9a-f]{6}$/i.test(color)) {
			var r = parseInt(color.slice(1, 3), 16);
			var g = parseInt(color.slice(3, 5), 16);
			var b = parseInt(color.slice(5, 7), 16);
			return 'rgba(' + r + ', ' + g + ', ' + b + ', ' + alpha + ')';
		}
		if (/^#[0-9a-f]{3}$/i.test(color)) {
			var rr = parseInt(color.charAt(1) + color.charAt(1), 16);
			var gg = parseInt(color.charAt(2) + color.charAt(2), 16);
			var bb = parseInt(color.charAt(3) + color.charAt(3), 16);
			return 'rgba(' + rr + ', ' + gg + ', ' + bb + ', ' + alpha + ')';
		}
		return color;
	}

	function formatCompactNumber(value) {
		value = Number(value || 0);
		if (value >= 1000000) return (value / 1000000).toFixed(1).replace(/\.0$/, '') + 'M';
		if (value >= 1000) return (value / 1000).toFixed(1).replace(/\.0$/, '') + 'K';
		return value.toLocaleString();
	}

	function formatRateTick(value) {
		value = Number(value || 0);
		if (value >= 10) return value.toFixed(0) + '%';
		if (value > 0) return value.toFixed(1) + '%';
		return '0%';
	}

	function providerDisplayName(provider) {
		if (provider === 'anthropic') return 'Claude Code';
		if (provider === 'openai') return 'Codex';
		if (provider === 'unknown') return 'Unknown';
		return provider || '';
	}

	charts.utils = {
		colorWithAlpha: colorWithAlpha,
		formatCompactNumber: formatCompactNumber,
		formatRateTick: formatRateTick,
		providerDisplayName: providerDisplayName
	};

	if (typeof module !== 'undefined' && module.exports) {
		module.exports = charts.utils;
	}
})(typeof window !== 'undefined' ? window : globalThis);
