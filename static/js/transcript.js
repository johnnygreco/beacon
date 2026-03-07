// Transcript-specific JS for session detail page.
// Only loaded on /sessions/{id}.

(function() {
  'use strict';

  // --- Gradient-fade truncation ---
  function initTruncation(root) {
    root = root || document;
    root.querySelectorAll('.truncatable').forEach(function(el) {
      var content = el.querySelector('.truncatable-content');
      if (content && content.scrollHeight > 350) {
        el.classList.add('truncated');
      }
    });
  }

  window.toggleTruncation = function(el) {
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

  // --- Syntax highlighting (lazy) ---
  function initHighlighting(root) {
    root = root || document;
    if (typeof hljs === 'undefined') return;
    var blocks = root.querySelectorAll('pre code:not(.hljs)');
    if (blocks.length === 0) return;

    var i = 0;
    function highlightNext(deadline) {
      while (i < blocks.length && (deadline.timeRemaining() > 5 || deadline.didTimeout)) {
        hljs.highlightElement(blocks[i]);
        i++;
      }
      if (i < blocks.length) {
        requestIdleCallback(highlightNext, { timeout: 1000 });
      }
    }

    if (typeof requestIdleCallback !== 'undefined') {
      requestIdleCallback(highlightNext, { timeout: 2000 });
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
    });
  };

  // --- View toggle (Chat/Timeline) ---
  window.switchView = function(view, btn) {
    var chatView = document.getElementById('chat-view');
    var timelineView = document.getElementById('timeline-view');
    var expandBtn = document.getElementById('btn-expand-all');
    var collapseBtn = document.getElementById('btn-collapse-all');

    if (view === 'chat') {
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

    var buttons = btn.parentElement.querySelectorAll('button[onclick^="switchView"]');
    buttons.forEach(function(b) {
      b.classList.remove('bg-blue-500/20', 'text-blue-400', 'border-blue-500/40');
      b.classList.add('bg-gray-800', 'text-gray-500', 'border-gray-700');
    });
    btn.classList.remove('bg-gray-800', 'text-gray-500', 'border-gray-700');
    btn.classList.add('bg-blue-500/20', 'text-blue-400', 'border-blue-500/40');
  };

  // --- HTMX integration ---
  document.addEventListener('htmx:afterSwap', function(e) {
    initTruncation(e.detail.target);
    initHighlighting(e.detail.target);
  });

  // --- Init on page load ---
  document.addEventListener('DOMContentLoaded', function() {
    initTruncation();
    initHighlighting();
  });

})();
