// The DOM keeps timeline-* IDs for compatibility; the UI presents this as the Activity Bar.
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
		var label = collapsed ? 'Expand activity bar' : 'Collapse activity bar';
		btn.setAttribute('aria-label', label);
		btn.setAttribute('title', label);
	}

	function syncDivider() {
		if (!divider) return;
		var parsed = parseInt(sidebar.style.width, 10);
		var width = isCollapsed() ? 0 : (Number.isFinite(parsed) ? parsed : DEFAULT_WIDTH);
		divider.setAttribute('aria-valuenow', String(width));
		divider.setAttribute('aria-valuetext', width > 0 ? ('Activity bar width ' + width + ' pixels') : 'Activity bar collapsed');
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
