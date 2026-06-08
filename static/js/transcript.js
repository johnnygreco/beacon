// Transcript-specific JS for session detail page.
// Only loaded on /sessions/{id}.

(function() {
  'use strict';

  var currentTranscriptView = 'chat';
  var detailOpenState = null;
  var dashboardReturnStateKey = 'beacon-dashboard-return-state-v1';

  function storageGet(key) {
    try { return window.sessionStorage.getItem(key); } catch (err) { return null; }
  }

  function dashboardReturnHref() {
    var raw = storageGet(dashboardReturnStateKey);
    if (!raw) return '';
    try {
      var state = JSON.parse(raw);
      var age = Date.now() - Number(state.savedAt || 0);
      if (age < 0 || age > 24 * 60 * 60 * 1000) return '';
      var dashboardURL = new URL(String(state.url || '/'), window.location.origin);
      if (dashboardURL.origin !== window.location.origin || dashboardURL.pathname !== '/') return '';
      var transcriptPath = String(state.transcriptPath || '');
      if (transcriptPath) {
        var transcriptURL = new URL(transcriptPath, window.location.origin);
        if (transcriptURL.origin !== window.location.origin || transcriptURL.pathname !== window.location.pathname) return '';
        if (normalizedSearch(transcriptURL.search) !== normalizedSearch(window.location.search || '')) return '';
      }
      return dashboardURL.pathname + dashboardURL.search;
    } catch (err) {
      return '';
    }
  }

  function normalizedSearch(search) {
    var params = new URLSearchParams(search || '');
    var pairs = [];
    params.forEach(function(value, key) {
      pairs.push([key, value]);
    });
    pairs.sort(function(a, b) {
      if (a[0] === b[0]) return a[1] < b[1] ? -1 : (a[1] > b[1] ? 1 : 0);
      return a[0] < b[0] ? -1 : 1;
    });
    return pairs.map(function(pair) {
      return encodeURIComponent(pair[0]) + '=' + encodeURIComponent(pair[1]);
    }).join('&');
  }

  function initDashboardReturnLinks() {
    var href = dashboardReturnHref();
    if (!href) return;
    document.querySelectorAll('.transcript-back-link').forEach(function(link) {
      link.setAttribute('href', href);
    });
  }

  // --- Truncation toggle (initial state set server-side via class) ---
  window.toggleTruncation = function(el) {
    if (!el) return;
    if (el.classList.contains('truncated')) {
      el.classList.remove('truncated');
      el.classList.add('expanded');
      el.querySelector('.truncate-toggle').textContent = 'Show less';
    } else {
      el.classList.remove('expanded');
      el.classList.add('truncated');
      el.querySelector('.truncate-toggle').textContent = 'Show more';
    }
  };

  // --- Syntax highlighting (visibility-driven) ---
  function initHighlighting(root) {
    root = root || document;
    if (typeof hljs === 'undefined') return;
    var blocks = root.querySelectorAll('pre code:not(.hljs)');
    if (blocks.length === 0) return;

    if (typeof IntersectionObserver !== 'undefined') {
      var observer = new IntersectionObserver(function(entries) {
        entries.forEach(function(entry) {
          if (entry.isIntersecting) {
            hljs.highlightElement(entry.target);
            observer.unobserve(entry.target);
          }
        });
      });
      blocks.forEach(function(el) { observer.observe(el); });
    } else {
      blocks.forEach(function(el) { hljs.highlightElement(el); });
    }
  }

  // --- Expand / Collapse All ---
  window.expandAll = function() {
    document.querySelectorAll('#chat-view details').forEach(function(d) {
      d.open = true;
    });
  };

  window.collapseAll = function() {
    document.querySelectorAll('#chat-view details').forEach(function(d) {
      d.open = false;
    });
  };

  // --- Copy to clipboard ---
  window.copyToClipboard = function(btn) {
    var container = btn.closest('.code-container');
    var code = container ? container.querySelector('code, pre') : null;
    if (!code) return;

    navigator.clipboard.writeText(code.textContent).then(function() {
      var copyIcon = btn.querySelector('.copy-icon');
      var checkIcon = btn.querySelector('.check-icon');
      if (copyIcon && checkIcon) {
        copyIcon.classList.add('hidden');
        checkIcon.classList.remove('hidden');
        setTimeout(function() {
          copyIcon.classList.remove('hidden');
          checkIcon.classList.add('hidden');
        }, 1500);
      }
    }).catch(function() {});
  };

  // --- View toggle (Chat/Timeline) ---
  function setTranscriptView(view, btn) {
    var chatView = document.getElementById('chat-view');
    var timelineView = document.getElementById('timeline-view');
    var expandBtn = document.getElementById('btn-expand-all');
    var collapseBtn = document.getElementById('btn-collapse-all');

    currentTranscriptView = view === 'timeline' ? 'timeline' : 'chat';

    if (btn && btn.parentElement) {
      var buttons = btn.parentElement.querySelectorAll('button[onclick^="switchView"]');
      buttons.forEach(function(b) {
        b.classList.remove('bg-blue-500/20', 'text-blue-400', 'border-blue-500/40');
        b.classList.add('bg-gray-800', 'text-gray-500', 'border-gray-700');
        b.setAttribute('aria-pressed', 'false');
      });
      btn.classList.remove('bg-gray-800', 'text-gray-500', 'border-gray-700');
      btn.classList.add('bg-blue-500/20', 'text-blue-400', 'border-blue-500/40');
      btn.setAttribute('aria-pressed', 'true');
    }

    if (!chatView || !timelineView) return;

    if (currentTranscriptView === 'chat') {
      chatView.classList.remove('hidden');
      timelineView.classList.add('hidden');
      if (expandBtn) expandBtn.classList.remove('hidden');
      if (collapseBtn) collapseBtn.classList.remove('hidden');
    } else {
      timelineView.classList.remove('hidden');
      chatView.classList.add('hidden');
      if (expandBtn) expandBtn.classList.add('hidden');
      if (collapseBtn) collapseBtn.classList.add('hidden');
    }
  }

  window.switchView = function(view, btn) {
    setTranscriptView(view, btn);
  };

  // --- HTMX integration ---
  function transcriptViewFromButton(btn) {
    if (!btn) return currentTranscriptView;
    var action = btn.getAttribute('onclick') || '';
    return action.indexOf('timeline') !== -1 ? 'timeline' : 'chat';
  }

  function restoreTranscriptViewAfterSwap(target) {
    var container = document.getElementById('conversation-container');
    if (!container) return;
    if (target && target !== container && !container.contains(target) && !(target.contains && target.contains(container))) return;
    var activeButton = document.querySelector('.transcript-controls button[aria-pressed="true"][onclick^="switchView"]');
    setTranscriptView(transcriptViewFromButton(activeButton), activeButton);
  }

  function snapshotDetails() {
    var container = document.getElementById('conversation-container');
    if (!container) return;
    detailOpenState = {};
    container.querySelectorAll('#chat-view details[id]').forEach(function(detail) {
      detailOpenState[detail.id] = detail.open;
    });
  }

  function restoreDetails() {
    if (!detailOpenState) return;
    Object.keys(detailOpenState).forEach(function(id) {
      var detail = document.getElementById(id);
      if (detail && detail.tagName === 'DETAILS') detail.open = detailOpenState[id];
    });
    detailOpenState = null;
  }

  function initConversationObserver() {
    var container = document.getElementById('conversation-container');
    if (!container || container.dataset.transcriptObserver === 'true') return;
    container.dataset.transcriptObserver = 'true';
    var observer = new MutationObserver(function() {
      initHighlighting(container);
      restoreTranscriptViewAfterSwap(container);
    });
    observer.observe(container, { childList: true });
  }

  document.addEventListener('htmx:beforeSwap', function(e) {
    var container = document.getElementById('conversation-container');
    if (container && (e.detail.target === container || container.contains(e.detail.target))) {
      snapshotDetails();
    }
  });

  document.addEventListener('htmx:afterSwap', function(e) {
    initHighlighting(e.detail.target);
    restoreDetails();
    initConversationObserver();
    window.requestAnimationFrame(function() {
      restoreTranscriptViewAfterSwap(e.detail.target);
    });
  });

  document.addEventListener('htmx:afterSettle', function(e) {
    initConversationObserver();
    restoreTranscriptViewAfterSwap(e.detail.target);
  });

  // --- Init highlighting ---
  initDashboardReturnLinks();
  initConversationObserver();
  initHighlighting();

})();
