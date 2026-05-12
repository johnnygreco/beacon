import { expect, type Locator, type Page, type Route } from '@playwright/test';

export const TEST_SESSION_ID = 'session-older-001';
export const ACTIVE_SESSION_ID = 'active-parent-001';
export const TEST_EVENT_ID = 'event-older-001';

type Scenario = 'default' | 'empty' | 'error-heavy' | 'many-active';

type DashboardFixtureOptions = {
  scenario?: Scenario;
  failOnce?: Array<'active' | 'completed' | 'activity' | 'charts'>;
};

const fixedNow = '2026-05-09T18:00:00.000Z';

function iso(daysAgo: number, hours = 0): string {
  const d = new Date(fixedNow);
  d.setUTCDate(d.getUTCDate() - daysAgo);
  d.setUTCHours(d.getUTCHours() + hours);
  return d.toISOString();
}

const baseCompletedSessions = Array.from({ length: 31 }, (_, i) => {
  if (i === 0) {
    return {
      id: TEST_SESSION_ID,
      title: 'Legacy migration replay',
      source: 'claude-code',
      provider: 'anthropic',
      status: 'completed',
      started_at: iso(26, -1),
      ended_at: iso(26),
      duration: '38m 12s',
      turn_count: 18,
      total_tokens: 123456,
      input_tokens: 62000,
      output_tokens: 47000,
      cache_read_tokens: 14456,
      cache_create_tokens: 0,
      tool_call_count: 14,
      mcp_call_count: 2,
      error_count: 1,
      last_model: 'claude-sonnet-4-super-long-model-name',
      working_dir: '/Users/example/projects/beacon/very/long/dashboard/migration/worktree',
      has_session_end: true,
      subagent_count: 2,
    };
  }
  return {
    id: `session-completed-${String(i).padStart(3, '0')}`,
    title: i % 3 === 0 ? 'Dashboard fixture validation' : `Completed agent run ${i}`,
    source: i % 2 === 0 ? 'codex' : 'claude-code',
    provider: i % 2 === 0 ? 'openai' : 'anthropic',
    status: 'completed',
    started_at: iso(0, -i - 1),
    ended_at: iso(0, -i),
    duration: `${8 + i}m`,
    turn_count: 4 + i,
    total_tokens: 18_000 + i * 913,
    input_tokens: 9_000 + i * 410,
    output_tokens: 7_500 + i * 321,
    cache_read_tokens: 1_500 + i * 50,
    cache_create_tokens: 0,
    tool_call_count: 2 + (i % 7),
    mcp_call_count: i % 4 === 0 ? 1 : 0,
    error_count: i % 11 === 0 ? 1 : 0,
    last_model: i % 2 === 0 ? 'gpt-5.4-codex' : 'claude-sonnet-4',
    working_dir: i % 2 === 0 ? '/Users/example/projects/beacon' : '/Users/example/projects/beacon/subsystems/dashboard',
    has_session_end: true,
    subagent_count: 0,
  };
});

const childSessions = [
  {
    ...baseCompletedSessions[4],
    id: 'subagent-older-001',
    title: 'Subagent layout audit',
    parent_session_id: TEST_SESSION_ID,
    subagent_count: 0,
    total_tokens: 12000,
    tool_call_count: 5,
  },
  {
    ...baseCompletedSessions[5],
    id: 'subagent-older-002',
    title: 'Subagent accessibility pass',
    parent_session_id: TEST_SESSION_ID,
    subagent_count: 0,
    total_tokens: 8400,
    tool_call_count: 4,
  },
];

const activeSessions = [
  {
    id: ACTIVE_SESSION_ID,
    title: 'Realtime dashboard smoke run',
    source: 'claude-code',
    provider: 'anthropic',
    status: 'active',
    started_at: iso(0, -1),
    ended_at: fixedNow,
    duration: '4m 22s',
    turn_count: 7,
    total_tokens: 42000,
    input_tokens: 21000,
    output_tokens: 16000,
    cache_read_tokens: 5000,
    cache_create_tokens: 0,
    tool_call_count: 6,
    mcp_call_count: 0,
    error_count: 0,
    last_model: 'claude-sonnet-4',
    working_dir: '/Users/example/projects/beacon',
    has_session_end: false,
    subagent_count: 1,
    child_sessions: [
      {
        id: 'active-child-001',
        title: 'Live child worker',
        source: 'claude-code',
        provider: 'anthropic',
        status: 'active',
        started_at: iso(0, -1),
        ended_at: fixedNow,
        duration: '2m',
        turn_count: 2,
        total_tokens: 7400,
        input_tokens: 3600,
        output_tokens: 2600,
        cache_read_tokens: 1200,
        cache_create_tokens: 0,
        tool_call_count: 2,
        mcp_call_count: 0,
        error_count: 0,
        last_model: 'claude-haiku-4',
        working_dir: '/Users/example/projects/beacon',
        parent_session_id: ACTIVE_SESSION_ID,
        has_session_end: false,
        subagent_count: 0,
      },
    ],
  },
];

function manyActiveSessions() {
  return Array.from({ length: 8 }, (_, i) => ({
    ...activeSessions[0],
    id: `active-parent-${String(i + 1).padStart(3, '0')}`,
    title: `Live queue item ${i + 1}`,
    provider: i % 2 === 0 ? 'anthropic' : 'openai',
    last_model: i % 2 === 0 ? 'claude-sonnet-4' : 'gpt-5.4-codex',
    total_tokens: 15000 + i * 4300,
    tool_call_count: 3 + i,
    child_sessions: [],
  }));
}

const labels = [
  '2026-05-09T12:00:00.000Z',
  '2026-05-09T13:00:00.000Z',
  '2026-05-09T14:00:00.000Z',
  '2026-05-09T15:00:00.000Z',
  '2026-05-09T16:00:00.000Z',
  '2026-05-09T17:00:00.000Z',
  '2026-05-09T18:00:00.000Z',
];

function chartPayload(scenario: Scenario) {
  const errorHeavy = scenario === 'error-heavy';
  const empty = scenario === 'empty';
  const datasets = empty ? [] : [
    {
      label: 'claude-sonnet-4',
      provider: 'anthropic',
      provider_label: 'Claude Code',
      model: 'claude-sonnet-4',
      values: [1000, 4800, 12000, 26000, 56000, 92000, 124000],
      total_tokens: 124000,
      tool_call_count: 24,
      error_count: errorHeavy ? 9 : 1,
      call_count: 16,
    },
    {
      label: 'gpt-5.4-codex',
      provider: 'openai',
      provider_label: 'Codex',
      model: 'gpt-5.4-codex',
      values: [800, 3200, 9000, 18000, 30000, 43000, 61000],
      total_tokens: 61000,
      tool_call_count: 13,
      error_count: errorHeavy ? 5 : 0,
      call_count: 9,
    },
    {
      label: 'claude-haiku-4',
      provider: 'anthropic',
      provider_label: 'Claude Code',
      model: 'claude-haiku-4',
      values: [400, 1100, 2300, 5100, 9000, 13000, 16500],
      total_tokens: 16500,
      tool_call_count: 8,
      error_count: errorHeavy ? 4 : 0,
      call_count: 6,
    },
  ];
  const summary = empty ? { total_tokens: 0, model_count: 0, tool_call_count: 0, error_rate: 0, error_count: 0 } : {
    total_tokens: errorHeavy ? 201500 : 201500,
    model_count: 3,
    tool_call_count: 45,
    error_rate: errorHeavy ? 13.8 : 1.9,
    error_count: errorHeavy ? 18 : 2,
  };
  return {
    range: '24h',
    token_cumulative: {
      labels,
      datasets,
      summary,
      time_unit: 'hour',
      bucket_minutes: 60,
    },
    model_activity: {
      labels,
      summary,
      time_unit: 'hour',
      bucket_minutes: 60,
      metrics: {
        error_rate: {
          label: 'Error Rate',
          unit: '%',
          datasets: datasets.map((d, i) => ({
            ...d,
            values: empty ? [] : (errorHeavy ? [0, 4 + i, 8 + i, 12 + i, 15 + i, 10 + i, 18 + i] : [0, 0, 1 + i * 0.2, 2 + i * 0.1, 1.3, 0.8, 1.9]),
          })),
        },
        errors: {
          label: 'Errors',
          unit: 'errors',
          datasets: datasets.map((d, i) => ({
            ...d,
            values: empty ? [] : (errorHeavy ? [0, i, 2 + i, 3 + i, 5 + i, 4 + i, 6 + i] : [0, 0, i === 0 ? 1 : 0, 0, 1, 0, 0]),
          })),
        },
        tool_calls: {
          label: 'Tool Calls',
          unit: 'calls',
          datasets: datasets.map((d, i) => ({
            ...d,
            values: empty ? [] : [1 + i, 3 + i, 4 + i, 6 + i, 8 + i, 10 + i, 13 + i],
          })),
        },
      },
    },
  };
}

function activityItems(scenario: Scenario) {
  if (scenario === 'empty') return [];
  const base = [
    {
      id: TEST_EVENT_ID,
      type: 'tool_call',
      summary: 'Read dashboard fixture payload',
      session_id: TEST_SESSION_ID,
      provider: 'anthropic',
      timestamp: iso(0, -1),
      relative_time: '26d ago',
    },
    {
      id: 'event-message-001',
      type: 'message',
      summary: 'Agent summarized the dashboard state',
      session_id: 'session-completed-001',
      provider: 'openai',
      timestamp: iso(0, -2),
      relative_time: '2h ago',
    },
    {
      id: 'event-error-001',
      type: 'error',
      summary: 'Model request returned a recoverable error',
      session_id: 'session-completed-011',
      provider: 'anthropic',
      timestamp: iso(0, -3),
      relative_time: '3h ago',
    },
    {
      id: 'event-tool-error-001',
      type: 'tool_error',
      summary: 'Shell command exited non-zero',
      session_id: 'session-completed-022',
      provider: 'openai',
      timestamp: iso(0, -4),
      relative_time: '4h ago',
    },
  ];
  return scenario === 'error-heavy'
    ? [...base, ...Array.from({ length: 7 }, (_, i) => ({
      id: `event-error-heavy-${i}`,
      type: i % 2 === 0 ? 'error' : 'tool_error',
      summary: `Repeated error burst ${i + 1}`,
      session_id: `session-completed-${String(i + 1).padStart(3, '0')}`,
      provider: i % 2 === 0 ? 'anthropic' : 'openai',
      timestamp: iso(0, -5 - i),
      relative_time: `${5 + i}h ago`,
    }))]
    : base;
}

function completedForRequest(url: URL, scenario: Scenario) {
  if (scenario === 'empty') return { items: [], hasMore: false };
  const query = (url.searchParams.get('q') || '').toLowerCase();
  const offset = Number(url.searchParams.get('offset') || 0);
  const limit = Number(url.searchParams.get('limit') || 30);
  const source = query
    ? baseCompletedSessions.filter((s) => {
      const metadata = [s.id, s.title, s.last_model, s.working_dir, s.provider].join(' ').toLowerCase();
      const indexedEventText = s.id === TEST_SESSION_ID ? 'read dashboard fixture payload' : '';
      const queryTokens = query.split(/\s+/).filter(Boolean);
      return metadata.includes(query) || (queryTokens.length > 0 && queryTokens.every((token) => indexedEventText.includes(token)));
    })
    : baseCompletedSessions;
  return {
    items: source.slice(offset, offset + limit),
    hasMore: offset + limit < source.length,
  };
}

function eventsForSession() {
  return [
    {
      event_uid: TEST_EVENT_ID,
      session_id: TEST_SESSION_ID,
      event_kind: 'tool_call',
      payload_type: 'tool_use',
      actor_role: 'assistant',
      timestamp: iso(26),
      text_preview: 'Read dashboard fixture payload',
      tool_name: 'Read',
      tool_use_id: 'toolu_fixture_read',
      model: 'claude-sonnet-4',
      tokens: 420,
      duration_ms: 61,
      input_preview: '{"file_path":"internal/views/pages/dashboard.templ"}',
      output_preview: '{"lines":42}',
    },
    {
      event_uid: 'event-older-002',
      session_id: TEST_SESSION_ID,
      event_kind: 'message',
      payload_type: 'assistant',
      actor_role: 'assistant',
      timestamp: iso(26, 1),
      text_preview: 'The dashboard search and timeline wiring are under test.',
      tool_name: '',
      tool_use_id: '',
      model: 'claude-sonnet-4',
      tokens: 180,
      duration_ms: 0,
    },
  ];
}

function activeForScenario(scenario: Scenario) {
  if (scenario === 'empty') return [];
  if (scenario === 'many-active') return manyActiveSessions();
  return activeSessions;
}

async function fulfillJSON(route: Route, data: unknown, status = 200) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(data),
  });
}

function transcriptFixtureHTML() {
  return `<!doctype html>
<html lang="en" class="dark">
  <head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Session ${TEST_SESSION_ID} | Beacon</title>
    <script>
      (function() {
        var fallbackTheme = 'codex-dark';
        var theme = fallbackTheme;
        try { theme = localStorage.getItem('beacon-dashboard-resolved-theme') || theme; } catch (err) {}
        if (!/^[a-z0-9-]+$/.test(theme)) theme = fallbackTheme;
        document.documentElement.setAttribute('data-dashboard-theme', theme);
      })();
    </script>
    <link rel="stylesheet" href="/static/css/tailwind.css">
    <link rel="stylesheet" href="/static/css/custom.css">
  </head>
  <body data-page="transcript" class="bg-gray-900 text-gray-100 min-h-screen">
    <main id="main-content" class="min-h-screen w-full p-6 overflow-y-auto">
      <div id="transcript-wrap" class="transcript-page space-y-6">
        <section class="transcript-header border border-gray-700">
          <div class="flex flex-wrap items-start justify-between gap-4">
            <div class="min-w-0 flex-1">
              <div class="transcript-breadcrumb">
                <a href="/" class="transcript-back-link">
                  <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 19l-7-7m0 0l7-7m-7 7h18"></path>
                  </svg>
                  Dashboard
                </a>
                <span aria-hidden="true">/</span>
                <span>Transcript</span>
              </div>
              <h1 class="text-2xl font-semibold text-gray-100 mt-2">Legacy migration replay</h1>
              <p class="font-mono text-sm text-gray-500 mt-1">${TEST_SESSION_ID}</p>
            </div>
            <span class="px-3 py-1 text-sm font-semibold uppercase rounded-full bg-gray-600/40 text-gray-400">Completed</span>
          </div>
          <div class="transcript-metric-grid text-sm">
            <div class="transcript-stat"><span class="text-gray-500">Duration</span><p class="text-gray-200 font-medium">38m 12s</p></div>
            <div class="transcript-stat"><span class="text-gray-500">Total Tokens</span><p class="text-gray-200 font-medium">123.5K</p></div>
            <div class="transcript-stat"><span class="text-gray-500">Cache Tokens</span><p class="text-gray-200 font-medium">31.1K</p></div>
            <div class="transcript-stat"><span class="text-gray-500">Turns</span><p class="text-gray-200 font-medium">14</p></div>
            <div class="transcript-stat"><span class="text-gray-500">Tool Calls</span><p class="text-gray-200 font-medium">42</p></div>
          </div>
        </section>
        <section class="transcript-conversation">
          <div class="transcript-conversation-header flex items-center justify-between gap-3">
            <h2 class="text-lg font-semibold text-gray-200">Conversation</h2>
            <div class="transcript-controls flex items-center gap-2">
              <button type="button" id="btn-expand-all" onclick="expandAll()" class="px-2 py-1 text-xs rounded border border-gray-700 text-gray-300">Expand All</button>
              <button type="button" id="btn-collapse-all" onclick="collapseAll()" class="px-2 py-1 text-xs rounded border border-gray-700 text-gray-300">Collapse All</button>
              <button type="button" onclick="switchView('chat', this)" aria-pressed="true" class="px-3 py-1.5 text-sm rounded-md font-medium border bg-blue-500/20 text-blue-400 border-blue-500/40">Chat</button>
              <button type="button" onclick="switchView('timeline', this)" aria-pressed="false" class="px-3 py-1.5 text-sm rounded-md font-medium border bg-gray-800 text-gray-500 border-gray-700">Timeline</button>
            </div>
          </div>
          <div id="chat-view" class="transcript-chat-view space-y-3">
            <details id="${TEST_EVENT_ID}" open class="rounded border border-gray-700 p-3 bg-gray-800/30">
              <summary class="cursor-pointer">Read dashboard fixture payload</summary>
              <div class="code-container relative mt-3">
                <pre><code>{"file_path":"internal/views/pages/dashboard.templ"}</code></pre>
                <button type="button" onclick="copyToClipboard(this)" title="Copy to clipboard" aria-label="Copy to clipboard">
                  <span class="copy-icon">Copy</span><span class="check-icon hidden">Copied</span>
                </button>
              </div>
            </details>
            <details id="event-older-002" open class="rounded border border-gray-700 p-3 bg-gray-800/30"><summary>Assistant summary</summary><p>Dashboard state summarized.</p></details>
            <details id="event-older-003" open class="rounded border border-gray-700 p-3 bg-gray-800/30"><summary>Tool result</summary><p>Payload loaded.</p></details>
          </div>
          <div id="timeline-view" class="transcript-timeline-view hidden rounded border border-gray-700 p-3">
            <a href="#${TEST_EVENT_ID}" class="text-blue-400">Read dashboard fixture payload</a>
          </div>
        </section>
      </div>
    </main>
    <script src="/static/js/transcript.js"></script>
  </body>
</html>`;
}

export async function installDashboardFixtures(page: Page, options: DashboardFixtureOptions = {}) {
  const scenario = options.scenario || 'default';
  const failures = new Set(options.failOnce || []);

  await page.addInitScript(() => {
    Object.defineProperty(window, 'EventSource', { value: undefined, configurable: true });
  });

  await page.route('**/api/dashboard/sessions**', async (route) => {
    const url = new URL(route.request().url());
    const state = url.searchParams.get('state') || 'completed';
    const failureKey = state === 'active' ? 'active' : 'completed';
    if (failures.delete(failureKey)) return fulfillJSON(route, { error: 'fixture failure' }, 500);
    if (state === 'active') {
      return fulfillJSON(route, { state: 'active', range: '', offset: 0, limit: 30, has_more: false, items: activeForScenario(scenario) });
    }
    const completed = completedForRequest(url, scenario);
    return fulfillJSON(route, {
      state: 'completed',
      range: url.searchParams.get('range') || '24h',
      query: url.searchParams.get('q') || '',
      offset: Number(url.searchParams.get('offset') || 0),
      limit: Number(url.searchParams.get('limit') || 30),
      has_more: completed.hasMore,
      items: completed.items,
    });
  });

  await page.route('**/api/dashboard/charts**', async (route) => {
    if (failures.delete('charts')) return fulfillJSON(route, { error: 'fixture failure' }, 500);
    return fulfillJSON(route, chartPayload(scenario));
  });

  await page.route('**/api/dashboard/activity**', async (route) => {
    if (failures.delete('activity')) return fulfillJSON(route, { error: 'fixture failure' }, 500);
    const url = new URL(route.request().url());
    const kinds = (url.searchParams.get('event_kind') || '').split(',').filter(Boolean);
    const items = activityItems(scenario).filter((item) => kinds.length === 0 || kinds.includes(item.type));
    return fulfillJSON(route, items);
  });

  await page.route('**/api/sessions?**', async (route) => {
    return fulfillJSON(route, [...baseCompletedSessions, ...activeSessions]);
  });

  await page.route('**/api/sessions/*/subagents', async (route) => {
    return fulfillJSON(route, childSessions);
  });

  await page.route('**/api/sessions/*/events**', async (route) => {
    return fulfillJSON(route, eventsForSession());
  });

  await page.route('**/api/sessions/*', async (route) => {
    const url = new URL(route.request().url());
    const id = decodeURIComponent(url.pathname.split('/').pop() || '');
    const session = [...baseCompletedSessions, ...activeSessions, ...childSessions].find((s) => s.id === id) || baseCompletedSessions[0];
    return fulfillJSON(route, { session });
  });

  await page.route('**/api/tool-payloads/*', async (route) => {
    return fulfillJSON(route, {
      event_uid: TEST_EVENT_ID,
      tool_name: 'Read',
      tool_phase: 'result',
      input_json: '{"file_path":"internal/views/pages/dashboard.templ"}',
      output_json: '{"lines":42,"status":"ok"}',
      input_preview: 'dashboard.templ',
      output_preview: '42 lines',
    });
  });

  await page.route('**/sessions/**', async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === `/sessions/${TEST_SESSION_ID}`) {
      return route.fulfill({ status: 200, contentType: 'text/html', body: transcriptFixtureHTML() });
    }
    return route.fallback();
  });
}

export function attachPageGuards(page: Page) {
  const consoleEvents: Array<{ type: string; text: string; location: unknown }> = [];
  const failedRequests: Array<{ url: string; failure: string }> = [];
  const badResponses: Array<{ url: string; status: number }> = [];

  page.on('console', (msg) => {
    if (['error', 'warning'].includes(msg.type())) {
      consoleEvents.push({ type: msg.type(), text: msg.text(), location: msg.location() });
    }
  });
  page.on('requestfailed', (request) => {
    failedRequests.push({ url: request.url(), failure: request.failure()?.errorText || '' });
  });
  page.on('response', (response) => {
    if (response.status() >= 400) {
      badResponses.push({ url: response.url(), status: response.status() });
    }
  });

  return {
    consoleEvents,
    failedRequests,
    badResponses,
    async expectClean() {
      expect(consoleEvents).toEqual([]);
      expect(failedRequests.filter((r) => !r.failure.includes('ERR_ABORTED'))).toEqual([]);
      expect(badResponses).toEqual([]);
    },
  };
}

export async function gotoDashboard(page: Page) {
  await page.goto('/', { waitUntil: 'domcontentloaded' });
  await expect(page.getByRole('heading', { name: 'Beacon Realtime Dashboard' })).toBeVisible();
  await expect(page.locator('#dashboard-analytics-summary > div')).toHaveCount(4);
  await expect(page.locator('#dashboard-last-updated')).toContainText(/Updated|Not refreshed yet/);
}

export async function waitForCompletedRows(page: Page, count = 30) {
  await expect(page.locator('#completed-sessions tr[data-session-link]')).toHaveCount(count);
}

export async function expectNoHorizontalOverflow(page: Page) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1);
  expect(overflow).toBe(false);
}

export async function expectEqualDashboardChartHeights(page: Page) {
  const heights = await page.evaluate(() => {
    const cardFor = (id: string) => {
      const canvas = document.getElementById(id);
      const card = canvas?.closest('.bg-gray-800');
      return card ? Math.round(card.getBoundingClientRect().height) : 0;
    };
    return {
      token: cardFor('dashboardTokenCumulativeChart'),
      health: cardFor('dashboardModelActivityChart'),
    };
  });
  expect(Math.abs(heights.token - heights.health)).toBeLessThanOrEqual(6);
}

export async function expectLogAndModelControlsAligned(page: Page) {
  await expect(page.locator('#dashboardTokenCumulativeChart-model-dropdown .model-dropdown-trigger')).toBeVisible();
  const metrics = await page.evaluate(() => {
    const log = document.querySelector('#dashboardTokenCumulativeChart-log-toggle')?.getBoundingClientRect();
    const model = document.querySelector('#dashboardTokenCumulativeChart-model-dropdown .model-dropdown-trigger')?.getBoundingClientRect();
    return {
      logHeight: log ? Math.round(log.height) : 0,
      modelHeight: model ? Math.round(model.height) : 0,
      gap: log && model ? Math.round(log.left - model.right) : 0,
    };
  });
  expect(metrics.logHeight).toBeGreaterThan(0);
  expect(Math.abs(metrics.logHeight - metrics.modelHeight)).toBeLessThanOrEqual(2);
  expect(metrics.gap).toBeGreaterThanOrEqual(6);
}

export function visualMasks(page: Page): Locator[] {
  return [
    page.locator('#dashboard-last-updated'),
    page.locator('#dashboard-connection-label'),
    page.locator('#sse-indicator'),
  ];
}
