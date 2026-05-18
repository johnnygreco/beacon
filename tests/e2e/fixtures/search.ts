import { type Page, type Route } from '@playwright/test';

export const SEARCH_SESSION_ID = 'session-search-001';

type SearchFixtureResult = {
  eventUID: string;
  sessionID: string;
  kind: string;
  kindLabel: string;
  snippet: string;
  provider: string;
  providerClass: string;
  model: string;
  tool?: string;
  score: string;
  time: string;
};

const searchResults: SearchFixtureResult[] = [
  {
    eventUID: 'event-search-001',
    sessionID: SEARCH_SESSION_ID,
    kind: 'message',
    kindLabel: 'Message',
    snippet: 'Dashboard payload search surfaced the exact migration note inside the assistant response.',
    provider: 'Claude Code',
    providerClass: 'bg-orange-500/15 text-orange-300',
    model: 'sonnet-4',
    score: '3.18',
    time: '2h ago',
  },
  {
    eventUID: 'event-search-002',
    sessionID: SEARCH_SESSION_ID,
    kind: 'tool_call',
    kindLabel: 'Tool call',
    snippet: 'internal/views/pages/search.templ',
    provider: 'Codex',
    providerClass: 'bg-cyan-500/15 text-cyan-400',
    model: 'gpt-5.4-codex',
    tool: 'Read',
    score: '2.44',
    time: '1h ago',
  },
  {
    eventUID: 'event-search-003',
    sessionID: 'session-search-002',
    kind: 'error',
    kindLabel: 'Error',
    snippet: 'Recoverable search timeout while loading a large result set.',
    provider: 'Codex',
    providerClass: 'bg-cyan-500/15 text-cyan-400',
    model: 'gpt-5.4-codex',
    score: '1.72',
    time: '12m ago',
  },
];

function escapeHTML(value: string) {
  return value
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;');
}

function kindIcon(kind: string) {
  if (kind === 'tool_call') {
    return '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M14.7 6.3a4 4 0 0 0-5.4 5.4L4 17v3h3l5.3-5.3a4 4 0 0 0 5.4-5.4l-3 3-3-3 3-3z"></path></svg>';
  }
  if (kind === 'error') {
    return '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v4m0 4h.01M10.3 4.3 2.8 17.2A2 2 0 0 0 4.5 20h15a2 2 0 0 0 1.7-2.8L13.7 4.3a2 2 0 0 0-3.4 0z"></path></svg>';
  }
  return '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 6h14M5 12h10M5 18h7"></path></svg>';
}

function renderCard(result: SearchFixtureResult) {
  const tool = result.tool
    ? `<span class="search-result-tool" title="${escapeHTML(result.tool)}">${escapeHTML(result.tool)}</span>`
    : '';
  return `<a href="/sessions/${escapeHTML(result.sessionID)}#${escapeHTML(result.eventUID)}" class="search-result-card" data-event-kind="${escapeHTML(result.kind)}" data-session-id="${escapeHTML(result.sessionID)}">
    <div class="search-result-card-head">
      <div class="search-result-kind is-${escapeHTML(result.kind.replaceAll('_', '-'))}" aria-hidden="true">${kindIcon(result.kind)}</div>
      <div class="search-result-main">
        <div class="search-result-title-line">
          <span class="search-result-kind-label">${escapeHTML(result.kindLabel)}</span>
          ${tool}
        </div>
        <div class="search-result-meta">
          <span class="search-provider-badge ${escapeHTML(result.providerClass)}">${escapeHTML(result.provider)}</span>
          <span class="search-result-model">${escapeHTML(result.model)}</span>
          <span class="search-result-session">${escapeHTML(result.sessionID.slice(0, 8))}</span>
          <span>${escapeHTML(result.time)}</span>
          <span>${escapeHTML(result.score)}</span>
        </div>
      </div>
      <span class="search-result-arrow" aria-hidden="true">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 17 17 7M9 7h8v8"></path>
        </svg>
      </span>
    </div>
    <p class="search-result-snippet">${escapeHTML(result.snippet)}</p>
  </a>`;
}

function manyResults() {
  return Array.from({ length: 35 }, (_, index) => ({
    ...searchResults[index % searchResults.length],
    eventUID: `event-many-${String(index + 1).padStart(3, '0')}`,
    sessionID: index % 2 === 0 ? SEARCH_SESSION_ID : 'session-search-many',
    snippet: `Many-result fixture item ${index + 1} for pagination and visual density checks.`,
    score: (3 - index * 0.03).toFixed(2),
  }));
}

function filterResults(url: URL) {
  const query = (url.searchParams.get('q') || '').toLowerCase().trim();
  const eventKind = url.searchParams.get('event_kind') || '';
  const sessionID = (url.searchParams.get('session_id') || '').toLowerCase().trim();
  let source = query === 'many' ? manyResults() : searchResults;

  if (query && query !== 'many') {
    const tokens = query.split(/\s+/).filter(Boolean);
    source = source.filter((result) => {
      const haystack = [result.snippet, result.kindLabel, result.tool || '', result.sessionID, result.model].join(' ').toLowerCase();
      return tokens.every((token) => haystack.includes(token));
    });
  }
  if (eventKind) source = source.filter((result) => result.kind === eventKind);
  if (sessionID) source = source.filter((result) => result.sessionID.toLowerCase().startsWith(sessionID));
  return source;
}

function renderSearchResults(url: URL) {
  const query = (url.searchParams.get('q') || '').trim();
  const eventKind = url.searchParams.get('event_kind') || '';
  const sessionID = (url.searchParams.get('session_id') || '').trim();
  if (!query && !eventKind && !sessionID) {
    return `<div class="search-empty-state" data-search-state="idle"><div class="search-empty-icon" aria-hidden="true"></div><p>Type a phrase, path, tool, or session fragment.</p></div>`;
  }

  const limit = Number(url.searchParams.get('limit') || 30);
  const results = filterResults(url);
  const visible = results.slice(0, limit);
  const hasMore = results.length > visible.length;
  if (visible.length === 0) {
    return `<div class="search-empty-state" data-search-state="empty"><div class="search-empty-icon" aria-hidden="true"></div><p>No matching events.</p><span>Try fewer filters or a shorter phrase.</span></div>`;
  }

  return `<div class="search-results-status">
      <span>${visible.length} shown${hasMore ? ' / more available' : ''}</span>
      ${hasMore ? '<button type="button" onclick="increaseLimit()" class="search-show-more">Show more</button>' : ''}
    </div>
    ${visible.map(renderCard).join('')}`;
}

export async function installSearchFixtures(page: Page) {
  const requests: URL[] = [];
  await page.route('**/search/results**', async (route: Route) => {
    const url = new URL(route.request().url());
    requests.push(url);
    await route.fulfill({
      status: 200,
      contentType: 'text/html',
      body: renderSearchResults(url),
    });
  });
  return { requests };
}
