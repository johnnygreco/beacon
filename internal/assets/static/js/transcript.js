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
    if (!code || !navigator.clipboard) return;

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
      var buttons = btn.parentElement.querySelectorAll('button[data-transcript-view]');
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

  // --- Trace annotations ---
  var annotationState = {
    loaded: false,
    loading: false,
    error: '',
    items: [],
    activeTarget: null,
    opener: null,
    loadPromise: null,
    loadRequestID: 0,
    mutationVersion: 0,
    reloadAfterLoad: false,
  };

  function escapeHTML(value) {
    return String(value == null ? '' : value)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#39;');
  }

  function currentSessionID() {
    var wrap = document.getElementById('transcript-wrap');
    return wrap ? String(wrap.dataset.sessionId || '').trim() : '';
  }

  function scopedPath(path, params) {
    var url = new URL(path, window.location.origin);
    var scope = new URLSearchParams(window.location.search || '');
    scope.forEach(function(value, key) {
      if (!url.searchParams.has(key)) url.searchParams.append(key, value);
    });
    Object.keys(params || {}).forEach(function(key) {
      var value = params[key];
      if (value !== undefined && value !== null && value !== '') {
        url.searchParams.set(key, String(value));
      }
    });
    return url.pathname + url.search;
  }

  function normalizeAnnotationLabels(value) {
    var raw = Array.isArray(value) ? value.join(',') : String(value || '');
    var seen = {};
    var labels = [];
    raw.split(',').forEach(function(part) {
      var label = part.trim().toLowerCase().replace(/\s+/g, '-');
      if (!label || seen[label]) return;
      seen[label] = true;
      labels.push(label);
    });
    labels.sort();
    return labels;
  }

  function annotationTargetKey(target) {
    if (!target) return '';
    var targetType = String(target.target_type || target.targetType || '').trim().toLowerCase();
    if (targetType === 'event') {
      return 'event:' + String(target.event_uid || target.eventUID || '').trim();
    }
    return 'session:' + String(target.session_id || target.sessionID || currentSessionID()).trim();
  }

  function normalizeAnnotation(annotation) {
    var item = annotation || {};
    return {
      annotation_id: String(item.annotation_id || ''),
      target_type: String(item.target_type || 'session'),
      session_id: String(item.session_id || ''),
      event_uid: String(item.event_uid || ''),
      source: String(item.source || 'ui'),
      category: String(item.category || ''),
      outcome: String(item.outcome || ''),
      quality_score: Number(item.quality_score || 0),
      confidence: Number(item.confidence || 0),
      needs_followup: Boolean(item.needs_followup),
      labels: Array.isArray(item.labels) ? item.labels.slice() : [],
      note: String(item.note || ''),
      status: String(item.status || 'active'),
      updated_at: String(item.updated_at || ''),
    };
  }

  function annotationTargetFromElement(el) {
    if (!el) return null;
    var targetType = String(el.dataset.annotationTarget || 'session').trim().toLowerCase();
    var sessionID = String(el.dataset.annotationSessionId || currentSessionID()).trim();
    var eventUID = String(el.dataset.annotationEventUid || '').trim();
    if (targetType === 'event' && !eventUID) return null;
    if (!sessionID) return null;
    return {
      target_type: targetType === 'event' ? 'event' : 'session',
      session_id: sessionID,
      event_uid: eventUID,
    };
  }

  function annotationsForTarget(target) {
    var key = annotationTargetKey(target);
    if (!key) return [];
    return annotationState.items.filter(function(item) {
      return item.status !== 'deleted' && annotationTargetKey(item) === key;
    });
  }

  function annotationCountText(count) {
    return count === 1 ? '1 annotation' : count + ' annotations';
  }

  function updateAnnotationButton(button) {
    var target = annotationTargetFromElement(button);
    var count = annotationsForTarget(target).length;
    var countEl = button.querySelector('[data-annotation-count]');
    if (countEl) {
      countEl.textContent = String(count);
      countEl.classList.toggle('hidden', count === 0);
    }
    button.dataset.annotationHasItems = count > 0 ? 'true' : 'false';
    button.disabled = annotationState.loading && !annotationState.loaded;
    updateAnnotationButtonLabel(button, target, count);
  }

  function updateAnnotationButtonLabel(button, target, count) {
    var labelEl = button.querySelector('.annotation-button-label');
    var label = String(button.dataset.annotationLabel || (labelEl ? labelEl.textContent : '') || 'Annotate').trim();
    if (!label) label = 'Annotate';
    if (target && target.target_type === 'event') {
      label += ' event ' + target.event_uid.slice(0, 12);
    } else if (!/\bsession\b/i.test(label)) {
      label += ' session';
    }
    button.setAttribute('aria-label', label + ', ' + annotationCountText(count));
  }

  function updateAnnotationSummary(summary) {
    var target = annotationTargetFromElement(summary);
    var countEl = summary.querySelector('[data-annotation-summary-count]');
    if (!countEl) return;
    if (annotationState.error) {
      countEl.textContent = 'Annotations unavailable';
      return;
    }
    if (annotationState.loading && !annotationState.loaded) {
      countEl.textContent = 'Loading annotations';
      return;
    }
    countEl.textContent = annotationCountText(annotationsForTarget(target).length);
  }

  function refreshAnnotationControls(root) {
    root = root || document;
    root.querySelectorAll('[data-annotation-button]').forEach(updateAnnotationButton);
    root.querySelectorAll('[data-annotation-summary]').forEach(updateAnnotationSummary);
  }

  function fetchJSON(path, options) {
    return fetch(path, options || {}).then(function(res) {
      if (res.ok) return res.json();
      return res.json().then(function(body) {
        throw new Error(body && body.error ? body.error : 'Request failed');
      }).catch(function(err) {
        if (err && err.message && err.message !== 'Request failed') throw err;
        throw new Error('Request failed');
      });
    });
  }

  function loadAnnotations(options) {
    options = options || {};
    var sessionID = currentSessionID();
    if (!sessionID) return Promise.resolve();
    if (annotationState.loading) {
      if (options.force) annotationState.reloadAfterLoad = true;
      return annotationState.loadPromise || Promise.resolve();
    }
    var requestID = annotationState.loadRequestID + 1;
    var mutationVersion = annotationState.mutationVersion;
    annotationState.loadRequestID = requestID;
    annotationState.loading = true;
    annotationState.error = '';
    refreshAnnotationControls();
    annotationState.loadPromise = fetchJSON(scopedPath('/api/sessions/' + encodeURIComponent(sessionID) + '/annotations', { limit: 500 }), {
      headers: { Accept: 'application/json' },
    }).then(function(data) {
      if (requestID !== annotationState.loadRequestID || mutationVersion !== annotationState.mutationVersion) return;
      annotationState.items = (data.items || []).map(normalizeAnnotation);
      annotationState.loaded = true;
      annotationState.error = '';
    }).catch(function() {
      if (requestID !== annotationState.loadRequestID || mutationVersion !== annotationState.mutationVersion) return;
      annotationState.error = 'Unable to load annotations';
    }).finally(function() {
      if (requestID !== annotationState.loadRequestID) return;
      annotationState.loading = false;
      var shouldReload = annotationState.reloadAfterLoad;
      annotationState.reloadAfterLoad = false;
      annotationState.loadPromise = null;
      refreshAnnotationControls();
      renderAnnotationDrawer();
      if (shouldReload) return loadAnnotations({ force: true });
    });
    return annotationState.loadPromise;
  }

  function drawerEl() {
    return document.getElementById('annotation-drawer');
  }

  function annotationForm() {
    var drawer = drawerEl();
    return drawer ? drawer.querySelector('[data-annotation-form]') : null;
  }

  function setAnnotationMessage(text, state) {
    var drawer = drawerEl();
    var message = drawer ? drawer.querySelector('[data-annotation-message]') : null;
    if (!message) return;
    message.textContent = text || '';
    if (state) message.dataset.state = state;
    else message.removeAttribute('data-state');
  }

  function resetAnnotationForm() {
    var form = annotationForm();
    if (!form) return;
    form.reset();
    form.elements.annotation_id.value = '';
    form.elements.quality_score.value = '0';
    form.elements.confidence.value = '0';
    var save = form.querySelector('[data-annotation-save]');
    if (save) save.textContent = 'Save annotation';
    setAnnotationMessage('', '');
  }

  function formValues(form) {
    return {
      category: form.elements.category.value,
      outcome: form.elements.outcome.value,
      quality_score: form.elements.quality_score.value,
      confidence: form.elements.confidence.value,
      labels: form.elements.labels.value,
      note: form.elements.note.value,
      needs_followup: form.elements.needs_followup.checked,
    };
  }

  function boundedInt(value, min, max) {
    var parsed = parseInt(value, 10);
    if (!Number.isFinite(parsed)) parsed = 0;
    if (parsed < min) return min;
    if (parsed > max) return max;
    return parsed;
  }

  function buildAnnotationPayload(target, values, includeTarget) {
    var payload = {
      author_type: 'human',
      source: 'ui',
      category: String(values.category || '').trim(),
      outcome: String(values.outcome || '').trim(),
      quality_score: boundedInt(values.quality_score, 0, 5),
      confidence: boundedInt(values.confidence, 0, 100),
      needs_followup: Boolean(values.needs_followup),
      labels: normalizeAnnotationLabels(values.labels),
      note: String(values.note || '').trim(),
    };
    if (includeTarget) {
      payload.target_type = target.target_type;
      payload.session_id = target.session_id;
      if (target.target_type === 'event') payload.event_uid = target.event_uid;
    }
    return payload;
  }

  function targetTitle(target) {
    if (!target) return 'Annotations';
    if (target.target_type === 'event') return 'Event ' + target.event_uid.slice(0, 12);
    return 'Session';
  }

  function annotationDrawerOpen() {
    var drawer = drawerEl();
    return Boolean(drawer && !drawer.classList.contains('hidden'));
  }

  function annotationFocusableElements(drawer) {
    var panel = drawer ? drawer.querySelector('.annotation-panel') : null;
    if (!panel) return [];
    return Array.prototype.slice.call(panel.querySelectorAll(
      'a[href], button, input, select, textarea, [tabindex]:not([tabindex="-1"])'
    )).filter(function(el) {
      if (el.disabled || el.getAttribute('aria-hidden') === 'true') return false;
      return el.type !== 'hidden' && !el.closest('[hidden], .hidden');
    });
  }

  function trapAnnotationFocus(evt) {
    var drawer = drawerEl();
    if (!drawer || drawer.classList.contains('hidden')) return;
    var focusable = annotationFocusableElements(drawer);
    if (focusable.length === 0) {
      evt.preventDefault();
      return;
    }
    var first = focusable[0];
    var last = focusable[focusable.length - 1];
    if (!drawer.contains(document.activeElement)) {
      evt.preventDefault();
      first.focus();
      return;
    }
    if (evt.shiftKey && document.activeElement === first) {
      evt.preventDefault();
      last.focus();
    } else if (!evt.shiftKey && document.activeElement === last) {
      evt.preventDefault();
      first.focus();
    }
  }

  function openAnnotationDrawer(target, opener) {
    var drawer = drawerEl();
    if (!drawer || !target) return;
    annotationState.activeTarget = target;
    annotationState.opener = opener && typeof opener.focus === 'function' ? opener : document.activeElement;
    drawer.classList.remove('hidden');
    drawer.setAttribute('aria-hidden', 'false');
    var kicker = drawer.querySelector('[data-annotation-target-label]');
    var title = drawer.querySelector('#annotation-drawer-title');
    if (kicker) kicker.textContent = target.target_type === 'event' ? 'Event annotation' : 'Session annotation';
    if (title) title.textContent = targetTitle(target);
    resetAnnotationForm();
    renderAnnotationDrawer();
    loadAnnotations();
    window.requestAnimationFrame(function() {
      var note = drawer.querySelector('textarea[name="note"]');
      if (note) note.focus();
    });
  }

  function closeAnnotationDrawer() {
    var drawer = drawerEl();
    if (!drawer || drawer.classList.contains('hidden')) return;
    var opener = annotationState.opener;
    drawer.classList.add('hidden');
    drawer.setAttribute('aria-hidden', 'true');
    annotationState.activeTarget = null;
    annotationState.opener = null;
    resetAnnotationForm();
    if (opener && document.contains(opener) && typeof opener.focus === 'function') {
      window.requestAnimationFrame(function() {
        opener.focus();
      });
    }
  }

  function annotationChips(annotation) {
    var chips = [];
    if (annotation.category) chips.push(annotation.category);
    if (annotation.outcome) chips.push(annotation.outcome);
    if (annotation.quality_score > 0) chips.push('quality ' + annotation.quality_score);
    if (annotation.confidence > 0) chips.push(annotation.confidence + '% confidence');
    if (annotation.needs_followup) chips.push('follow-up');
    annotation.labels.forEach(function(label) { chips.push(label); });
    if (chips.length === 0) return '';
    return '<div class="annotation-chip-list">' + chips.map(function(chip) {
      return '<span class="annotation-chip">' + escapeHTML(chip) + '</span>';
    }).join('') + '</div>';
  }

  function annotationCardHTML(annotation) {
    return '<article class="annotation-card" data-annotation-id="' + escapeHTML(annotation.annotation_id) + '">' +
      '<div class="annotation-card-top">' +
        '<span class="annotation-chip">' + escapeHTML(annotation.source || 'ui') + '</span>' +
        '<div class="annotation-card-actions">' +
          '<button type="button" class="annotation-card-button" data-annotation-edit="' + escapeHTML(annotation.annotation_id) + '">Edit</button>' +
          '<button type="button" class="annotation-card-button" data-variant="danger" data-annotation-delete="' + escapeHTML(annotation.annotation_id) + '">Delete</button>' +
        '</div>' +
      '</div>' +
      annotationChips(annotation) +
      '<p class="annotation-card-note">' + escapeHTML(annotation.note || '') + '</p>' +
    '</article>';
  }

  function renderAnnotationDrawer() {
    var drawer = drawerEl();
    if (!drawer || drawer.classList.contains('hidden') || !annotationState.activeTarget) return;
    var list = drawer.querySelector('[data-annotation-list]');
    if (!list) return;
    if (annotationState.error) {
      list.innerHTML = '<div class="annotation-list-error">Unable to load annotations</div>';
      return;
    }
    if (annotationState.loading && !annotationState.loaded) {
      list.innerHTML = '<div class="annotation-empty">Loading annotations</div>';
      return;
    }
    var items = annotationsForTarget(annotationState.activeTarget);
    if (items.length === 0) {
      list.innerHTML = '<div class="annotation-empty">No annotations yet</div>';
      return;
    }
    list.innerHTML = items.map(annotationCardHTML).join('');
  }

  function setAnnotationSaving(isSaving) {
    var form = annotationForm();
    if (!form) return;
    form.querySelectorAll('button, input, textarea, select').forEach(function(el) {
      if (el.matches('[data-annotation-close]')) return;
      el.disabled = isSaving;
    });
    var save = form.querySelector('[data-annotation-save]');
    if (save) save.textContent = isSaving ? 'Saving...' : (form.elements.annotation_id.value ? 'Update annotation' : 'Save annotation');
  }

  function saveAnnotation(form) {
    var target = annotationState.activeTarget;
    if (!target) return;
    var id = String(form.elements.annotation_id.value || '').trim();
    var payload = buildAnnotationPayload(target, formValues(form), id === '');
    var path = id ? '/api/annotations/' + encodeURIComponent(id) : '/api/annotations';
    setAnnotationSaving(true);
    setAnnotationMessage('', '');
    fetchJSON(scopedPath(path), {
      method: id ? 'PATCH' : 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    }).then(function() {
      annotationState.mutationVersion += 1;
      resetAnnotationForm();
      setAnnotationMessage(id ? 'Annotation updated' : 'Annotation saved', '');
      return loadAnnotations({ force: true });
    }).catch(function(err) {
      setAnnotationMessage(err && err.message ? err.message : 'Unable to save annotation', 'error');
    }).finally(function() {
      setAnnotationSaving(false);
    });
  }

  function editAnnotation(id) {
    var form = annotationForm();
    if (!form) return;
    var annotation = annotationState.items.find(function(item) { return item.annotation_id === id; });
    if (!annotation) return;
    form.elements.annotation_id.value = annotation.annotation_id;
    form.elements.category.value = annotation.category;
    form.elements.outcome.value = annotation.outcome;
    form.elements.quality_score.value = String(annotation.quality_score || 0);
    form.elements.confidence.value = String(annotation.confidence || 0);
    form.elements.labels.value = annotation.labels.join(', ');
    form.elements.note.value = annotation.note;
    form.elements.needs_followup.checked = annotation.needs_followup;
    var save = form.querySelector('[data-annotation-save]');
    if (save) save.textContent = 'Update annotation';
    setAnnotationMessage('', '');
    form.elements.note.focus();
  }

  function deleteAnnotation(id) {
    if (!id) return;
    setAnnotationMessage('', '');
    fetchJSON(scopedPath('/api/annotations/' + encodeURIComponent(id)), {
      method: 'DELETE',
      headers: { Accept: 'application/json' },
    }).then(function() {
      annotationState.mutationVersion += 1;
      return loadAnnotations({ force: true });
    }).catch(function(err) {
      setAnnotationMessage(err && err.message ? err.message : 'Unable to delete annotation', 'error');
    });
  }

  function initAnnotationControls(root) {
    refreshAnnotationControls(root || document);
    if (!annotationState.loaded && !annotationState.loading && currentSessionID()) {
      loadAnnotations();
    }
  }

  window.__beaconTranscriptAnnotations = {
    annotationCountText: annotationCountText,
    annotationTargetKey: annotationTargetKey,
    buildAnnotationPayload: buildAnnotationPayload,
    normalizeAnnotationLabels: normalizeAnnotationLabels,
  };

  // --- HTMX integration ---
  function transcriptViewFromButton(btn) {
    if (!btn) return currentTranscriptView;
    return btn.getAttribute('data-transcript-view') === 'timeline' ? 'timeline' : 'chat';
  }

  function restoreTranscriptViewAfterSwap(target) {
    var container = document.getElementById('conversation-container');
    if (!container) return;
    if (target && target !== container && !container.contains(target) && !(target.contains && target.contains(container))) return;
    var activeButton = document.querySelector('.transcript-controls button[aria-pressed="true"][data-transcript-view]');
    setTranscriptView(transcriptViewFromButton(activeButton), activeButton);
  }

  function scrollHashIntoViewAfterSwap(target) {
    if (!window.location.hash) return;
    if (!target || target.id !== 'conversation-container') return;
    var id = window.location.hash.substring(1);
    var el = document.getElementById(id);
    if (!el) {
      el = document.getElementById('conversation-container');
    }
    if (!el) return;
    setTimeout(function() {
      var parent = el.closest ? el.closest('details') : null;
      while (parent) {
        parent.open = true;
        parent = parent.parentElement ? parent.parentElement.closest('details') : null;
      }
      if (el.tagName === 'DETAILS') el.open = true;
      requestAnimationFrame(function() {
        el.scrollIntoView({ behavior: 'smooth', block: 'center' });
        if (el.id !== 'conversation-container') {
          el.style.outline = '2px solid rgba(59, 130, 246, 0.6)';
          el.style.outlineOffset = '4px';
          el.style.borderRadius = '8px';
          setTimeout(function() {
            el.style.outline = '';
            el.style.outlineOffset = '';
          }, 2500);
        }
      });
    }, 150);
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
      initAnnotationControls(container);
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
    initAnnotationControls(e.detail.target);
    restoreDetails();
    initConversationObserver();
    window.requestAnimationFrame(function() {
      restoreTranscriptViewAfterSwap(e.detail.target);
    });
  });

  document.addEventListener('htmx:afterSettle', function(e) {
    initConversationObserver();
    initAnnotationControls(e.detail.target);
    restoreTranscriptViewAfterSwap(e.detail.target);
    scrollHashIntoViewAfterSwap(e.detail.target);
  });

  document.addEventListener('click', function(evt) {
    var actionButton = evt.target.closest && evt.target.closest('[data-transcript-action]');
    if (actionButton) {
      evt.preventDefault();
      var action = actionButton.getAttribute('data-transcript-action') || '';
      if (action === 'expand-all') window.expandAll();
      else if (action === 'collapse-all') window.collapseAll();
      return;
    }
    var viewButton = evt.target.closest && evt.target.closest('[data-transcript-view]');
    if (viewButton) {
      evt.preventDefault();
      setTranscriptView(transcriptViewFromButton(viewButton), viewButton);
      return;
    }
    var copyButton = evt.target.closest && evt.target.closest('[data-copy-to-clipboard]');
    if (copyButton) {
      evt.preventDefault();
      window.copyToClipboard(copyButton);
      return;
    }
    var truncateButton = evt.target.closest && evt.target.closest('[data-truncate-toggle]');
    if (truncateButton) {
      evt.preventDefault();
      window.toggleTruncation(truncateButton.closest('.truncatable'));
      return;
    }
    var annotationButton = evt.target.closest && evt.target.closest('[data-annotation-button]');
    if (annotationButton) {
      evt.preventDefault();
      evt.stopPropagation();
      openAnnotationDrawer(annotationTargetFromElement(annotationButton), annotationButton);
      return;
    }
    var annotationClose = evt.target.closest && evt.target.closest('[data-annotation-close]');
    if (annotationClose) {
      evt.preventDefault();
      closeAnnotationDrawer();
      return;
    }
    var annotationEdit = evt.target.closest && evt.target.closest('[data-annotation-edit]');
    if (annotationEdit) {
      evt.preventDefault();
      editAnnotation(annotationEdit.getAttribute('data-annotation-edit'));
      return;
    }
    var annotationDelete = evt.target.closest && evt.target.closest('[data-annotation-delete]');
    if (annotationDelete) {
      evt.preventDefault();
      deleteAnnotation(annotationDelete.getAttribute('data-annotation-delete'));
      return;
    }
    var annotationNew = evt.target.closest && evt.target.closest('[data-annotation-new]');
    if (annotationNew) {
      evt.preventDefault();
      resetAnnotationForm();
    }
  });

  document.addEventListener('submit', function(evt) {
    var form = evt.target.closest && evt.target.closest('[data-annotation-form]');
    if (!form) return;
    evt.preventDefault();
    saveAnnotation(form);
  });

  document.addEventListener('keydown', function(evt) {
    if (evt.key === 'Escape' && annotationDrawerOpen()) {
      evt.preventDefault();
      closeAnnotationDrawer();
    } else if (evt.key === 'Tab') {
      trapAnnotationFocus(evt);
    }
  });

  // --- Init highlighting ---
  initDashboardReturnLinks();
  initConversationObserver();
  initAnnotationControls();
  initHighlighting();

})();
