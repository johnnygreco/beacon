import { expect, type Locator, type Page, type Request, type Response, type Route } from '@playwright/test';
import { validateContract } from '../../contracts/api-contracts.cjs';

export const TEST_SESSION_ID = 'session-older-001';
export const ACTIVE_SESSION_ID = 'active-parent-001';
export const TEST_EVENT_ID = 'event-older-001';
export const SEARCH_SESSION_ID = 'session-search-001';

type Scenario = 'default' | 'empty' | 'error-heavy' | 'many-active';

type DashboardFixtureOptions = {
  scenario?: Scenario;
  activeScenarioSequence?: Array<Scenario | 'error'>;
  failOnce?: Array<'active' | 'completed' | 'activity' | 'charts' | 'search'>;
  searchUnavailable?: boolean;
  searchDelayMs?: number;
  searchDelayByQuery?: Record<string, number>;
  disableEventSource?: boolean;
  mockEventSource?: boolean;
};

type DashboardSearchURLPredicate = (url: URL) => boolean;
type DashboardScrollSnapshot = {
  windowY: number;
  mainContentTop: number;
  dashboardTop: number;
  dashboardClientHeight: number;
  dashboardScrollHeight: number;
};
type DashboardScrollRecorderReport = {
  initial: DashboardScrollSnapshot;
  current: DashboardScrollSnapshot;
  maxWindowDelta: number;
  maxMainContentDelta: number;
  maxDashboardDelta: number;
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
    context_tokens: 42000,
    context_window_tokens: 200000,
    context_estimate: true,
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
        context_tokens: 7400,
        context_window_tokens: 200000,
        context_estimate: true,
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

function durationToSeconds(duration: string) {
  const text = String(duration || '');
  const hours = Number(text.match(/(\d+)\s*h/)?.[1] || 0);
  const minutes = Number(text.match(/(\d+)\s*m/)?.[1] || 0);
  const seconds = Number(text.match(/(\d+)\s*s/)?.[1] || 0);
  return hours * 3600 + minutes * 60 + seconds;
}

function manyActiveSessions() {
  const contextCases = [
    { context_tokens: 42_000, context_window_tokens: 200_000, context_estimate: true, last_model: 'claude-sonnet-4', provider: 'anthropic' },
    { context_tokens: 165_000, context_window_tokens: 200_000, context_estimate: true, last_model: 'claude-sonnet-4', provider: 'anthropic' },
    { context_tokens: 221_000, context_window_tokens: 200_000, context_estimate: true, last_model: 'claude-sonnet-4', provider: 'anthropic' },
    { context_tokens: 58_000, context_window_tokens: 0, context_estimate: true, last_model: 'local-experimental-32k', provider: 'openai' },
  ];
  return Array.from({ length: 8 }, (_, i) => {
    const contextCase = contextCases[i] || {
      context_tokens: 95_000 + i * 11_000,
      context_window_tokens: i % 2 === 0 ? 200_000 : 1_050_000,
      context_estimate: true,
      last_model: i % 2 === 0 ? 'claude-sonnet-4' : 'gpt-5.4-codex',
      provider: i % 2 === 0 ? 'anthropic' : 'openai',
    };
    return {
      ...activeSessions[0],
      ...contextCase,
      id: `active-parent-${String(i + 1).padStart(3, '0')}`,
      title: `Live queue item ${i + 1}`,
      total_tokens: 15000 + i * 4300,
      tool_call_count: 3 + i,
      child_sessions: [],
    };
  });
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
      values: [1000, 3800, 7200, 14000, 30000, 36000, 32000],
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
      values: [800, 2400, 5800, 9000, 12000, 13000, 18000],
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
      values: [400, 700, 1200, 2800, 3900, 4000, 3500],
      total_tokens: 16500,
      tool_call_count: 8,
      error_count: errorHeavy ? 4 : 0,
      call_count: 6,
    },
  ];
  const summary = empty ? { total_tokens: 0, model_count: 0, tool_call_count: 0, call_count: 0, error_rate: 0, error_count: 0 } : {
    total_tokens: errorHeavy ? 201500 : 201500,
    model_count: 3,
    tool_call_count: 45,
    call_count: 31,
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

function dashboardSearchBaseResults() {
  return [
    {
      result_type: 'event',
      event_uid: 'event-search-001',
      session_id: SEARCH_SESSION_ID,
      event_kind: 'message',
      snippet: 'Dashboard payload search surfaced the exact migration note inside the assistant response.',
      provider: 'anthropic',
      model: 'claude-sonnet-4',
      score: 3.18,
      timestamp: iso(2),
      relative_time: '2d ago',
      session_title: 'Dashboard payload search',
      working_dir: '/Users/example/projects/beacon/search',
    },
    {
      result_type: 'event',
      event_uid: 'event-search-002',
      session_id: SEARCH_SESSION_ID,
      event_kind: 'tool_call',
      snippet: 'internal/views/pages/dashboard.templ',
      tool_name: 'Read',
      provider: 'openai',
      model: 'gpt-5.4-codex',
      score: 2.44,
      timestamp: iso(2, 1),
      relative_time: '2d ago',
      session_title: 'Tool fixture search',
      working_dir: '/Users/example/projects/beacon/search',
    },
    {
      result_type: 'event',
      event_uid: 'event-search-003',
      session_id: 'session-search-002',
      event_kind: 'error',
      snippet: 'Recoverable search timeout while loading a large result set.',
      provider: 'openai',
      model: 'gpt-5.4-codex',
      score: 1.72,
      timestamp: iso(2, 2),
      relative_time: '2d ago',
      session_title: 'Search timeout diagnosis',
      working_dir: '/Users/example/projects/beacon',
    },
  ];
}

function dashboardSearchManyResults() {
  const base = dashboardSearchBaseResults();
  return Array.from({ length: 35 }, (_, i) => {
    const result = { ...base[i % base.length] };
    result.event_uid = `event-many-${String(i + 1).padStart(3, '0')}`;
    result.session_id = i % 2 === 0 ? SEARCH_SESSION_ID : 'session-search-many';
    result.snippet = `Many-result fixture item ${i + 1} for pagination and visual density checks.`;
    result.score = 3 - i * 0.03;
    result.timestamp = iso(2, Math.floor(i / 6));
    result.relative_time = '2d ago';
    return result;
  });
}

function dashboardSearchMatchesText(result: ReturnType<typeof dashboardSearchBaseResults>[number], query: string) {
  const normalized = query.toLowerCase().trim();
  if (normalized === '' || normalized === 'many') return true;
  const haystack = [
    result.snippet,
    result.event_kind,
    result.tool_name || '',
    result.session_id,
    result.session_title || '',
    result.model || '',
    result.provider || '',
    result.working_dir || '',
  ].join(' ').toLowerCase();
  return normalized.split(/\s+/).filter(Boolean).every((token) => haystack.includes(token));
}

function dashboardSearchForRequest(url: URL, scenario: Scenario) {
  const query = (url.searchParams.get('q') || '').trim();
  const range = url.searchParams.get('range') || '';
  const eventKind = url.searchParams.get('event_kind') || '';
  const sessionID = (url.searchParams.get('session_id') || '').toLowerCase();
  const sort = url.searchParams.get('sort') || 'relevance';
  const limit = Number(url.searchParams.get('limit') || 30);
  const active = query !== '' || eventKind !== '' || sessionID !== '' || sort !== 'relevance' || limit !== 30;
  if (!active) {
    return { state: 'idle', sort, limit, has_more: false, items: [] };
  }
  if (scenario === 'empty') {
    return { state: 'ready', query, range, event_kind: eventKind, session_id: sessionID, sort, limit, has_more: false, items: [] };
  }
  const source = query.toLowerCase() === 'many' ? dashboardSearchManyResults() : dashboardSearchBaseResults();
  const acceptedKinds = eventKind === 'error' ? new Set(['error', 'tool_error']) : new Set(eventKind ? [eventKind] : []);
  const eventResults = source.filter((result) => {
    if (!dashboardSearchMatchesText(result, query)) return false;
    if (acceptedKinds.size > 0 && !acceptedKinds.has(result.event_kind)) return false;
    if (sessionID && !result.session_id.toLowerCase().startsWith(sessionID)) return false;
    return true;
  });
  const sortedEvents = [...eventResults].sort((a, b) => {
    if (sort === 'newest') return new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime();
    if (sort === 'oldest') return new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime();
    return b.score - a.score;
  });
  const seenSessions = new Set(sortedEvents.map((result) => result.session_id));
  const sessionResults = query && !eventKind
    ? baseCompletedSessions
      .filter((session) => {
        if (seenSessions.has(session.id)) return false;
        if (sessionID && !session.id.toLowerCase().startsWith(sessionID)) return false;
        const haystack = [session.id, session.title, session.last_model, session.working_dir, session.provider].join(' ').toLowerCase();
        return query.toLowerCase().split(/\s+/).filter(Boolean).every((token) => haystack.includes(token));
      })
      .map((session) => ({
        result_type: 'session',
        event_uid: '',
        session_id: session.id,
        event_kind: 'session',
        snippet: `Session metadata: ${session.title} | ${session.working_dir} | ${session.provider} | ${session.last_model}`,
        provider: session.provider,
        model: session.last_model,
        score: 0,
        timestamp: session.ended_at,
        relative_time: 'completed',
        session_title: session.title,
        working_dir: session.working_dir,
      }))
    : [];
  const sortedSessionResults = [...sessionResults].sort((a, b) => {
    if (sort === 'oldest') return new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime();
    return new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime();
  });
  const sorted = [...sortedEvents, ...sortedSessionResults];
  if (sort === 'newest') {
    sorted.sort((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime());
  } else if (sort === 'oldest') {
    sorted.sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime());
  }
  return {
    state: 'ready',
    query,
    range,
    event_kind: eventKind,
    session_id: sessionID,
    sort,
    limit,
    has_more: sorted.length > limit,
    items: sorted.slice(0, limit),
  };
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
  const sort = url.searchParams.get('sort') || 'ended';
  const asc = (url.searchParams.get('direction') || 'desc') === 'asc';
  const sorted = [...source].sort((a, b) => {
    const value = (s: typeof baseCompletedSessions[number]) => {
      switch (sort) {
        case 'name': return s.title;
        case 'provider': return s.provider;
        case 'model': return s.last_model;
        case 'tokens': return s.total_tokens;
        case 'turns': return s.turn_count;
        case 'tools': return s.tool_call_count;
        case 'duration': return durationToSeconds(s.duration);
        case 'project': return s.working_dir;
        case 'id': return s.id;
        case 'ended':
        default: return new Date(s.ended_at).getTime();
      }
    };
    const av = value(a);
    const bv = value(b);
    const cmp = typeof av === 'number' && typeof bv === 'number'
      ? av - bv
      : String(av).localeCompare(String(bv), undefined, { sensitivity: 'base' });
    return asc ? cmp : -cmp;
  });
  return {
    items: sorted.slice(offset, offset + limit),
    hasMore: offset + limit < sorted.length,
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

async function fulfillJSON(route: Route, data: unknown, status = 200, contractName = '') {
  if (contractName) validateContract(contractName, data);
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(data),
  });
}

function transcriptFixtureHTML(sessionID = TEST_SESSION_ID) {
  return `<!doctype html>
<html lang="en" class="dark">
  <head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Session ${sessionID} | Beacon</title>
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
              <p class="font-mono text-sm text-gray-500 mt-1">${sessionID}</p>
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
  let activeRequestCount = 0;

  if (options.mockEventSource) {
    await page.addInitScript(() => {
      class FakeEventSource {
        url: string;
        readyState = 1;
        listeners: Record<string, Array<(event: MessageEvent) => void>> = {};
        onopen: ((event: Event) => void) | null = null;
        onerror: ((event: Event) => void) | null = null;

        constructor(url: string) {
          this.url = url;
          ((window as unknown as { __beaconEventSources?: FakeEventSource[] }).__beaconEventSources ||= []).push(this);
          setTimeout(() => this.onopen?.(new Event('open')), 0);
        }

        addEventListener(type: string, listener: (event: MessageEvent) => void) {
          (this.listeners[type] ||= []).push(listener);
        }

        removeEventListener(type: string, listener: (event: MessageEvent) => void) {
          this.listeners[type] = (this.listeners[type] || []).filter((candidate) => candidate !== listener);
        }

        close() {
          this.readyState = 2;
        }

        emit(type: string, data = '{"dirty":true}') {
          const event = new MessageEvent(type, { data });
          for (const listener of this.listeners[type] || []) listener(event);
        }
      }
      Object.defineProperty(window, 'EventSource', { value: FakeEventSource, configurable: true });
    });
  } else if (options.disableEventSource !== false) {
    await page.addInitScript(() => {
      Object.defineProperty(window, 'EventSource', { value: undefined, configurable: true });
    });
  }

  await page.route('**/api/dashboard/sessions**', async (route) => {
    const url = new URL(route.request().url());
    const state = url.searchParams.get('state') || 'completed';
    const failureKey = state === 'active' ? 'active' : 'completed';
    if (failures.delete(failureKey)) return fulfillJSON(route, { error: 'fixture failure' }, 500);
    if (state === 'active') {
      const activeSequence = options.activeScenarioSequence || [scenario];
      const activeScenario = activeSequence[Math.min(activeRequestCount, activeSequence.length - 1)];
      activeRequestCount += 1;
      if (activeScenario === 'error') return fulfillJSON(route, { error: 'fixture failure' }, 500);
      return fulfillJSON(route, { state: 'active', range: '', offset: 0, limit: 30, has_more: false, items: activeForScenario(activeScenario) }, 200, 'APIDashboardSessionsResponse');
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
    }, 200, 'APIDashboardSessionsResponse');
  });

  await page.route('**/api/dashboard/search**', async (route) => {
    const url = new URL(route.request().url());
    const query = (url.searchParams.get('q') || '').trim();
    const delay = options.searchDelayByQuery?.[query] ?? options.searchDelayMs ?? 0;
    if (delay > 0) {
      await new Promise((resolve) => setTimeout(resolve, delay));
    }
    if (failures.delete('search')) return fulfillJSON(route, { error: 'fixture failure' }, 500);
    if (options.searchUnavailable) {
      return fulfillJSON(route, {
        state: 'unavailable',
        query: url.searchParams.get('q') || '',
        range: url.searchParams.get('range') || '',
        event_kind: url.searchParams.get('event_kind') || '',
        session_id: url.searchParams.get('session_id') || '',
        sort: url.searchParams.get('sort') || 'relevance',
        limit: Number(url.searchParams.get('limit') || 30),
        has_more: false,
        items: [],
      }, 200, 'APIDashboardSearchResponse');
    }
    return fulfillJSON(route, dashboardSearchForRequest(url, scenario), 200, 'APIDashboardSearchResponse');
  });

  await page.route('**/api/dashboard/charts**', async (route) => {
    if (failures.delete('charts')) return fulfillJSON(route, { error: 'fixture failure' }, 500);
    return fulfillJSON(route, chartPayload(scenario), 200, 'APIDashboardCharts');
  });

  await page.route('**/api/dashboard/activity**', async (route) => {
    if (failures.delete('activity')) return fulfillJSON(route, { error: 'fixture failure' }, 500);
    const url = new URL(route.request().url());
    const kinds = (url.searchParams.get('event_kind') || '').split(',').filter(Boolean);
    const items = activityItems(scenario).filter((item) => kinds.length === 0 || kinds.includes(item.type));
    return fulfillJSON(route, items, 200, 'APIActivityItem[]');
  });

  await page.route('**/api/sessions?**', async (route) => {
    return fulfillJSON(route, [...baseCompletedSessions, ...activeForScenario(scenario)], 200, 'APISessionSummary[]');
  });

  await page.route('**/api/sessions/*/subagents', async (route) => {
    return fulfillJSON(route, childSessions, 200, 'APISessionSummary[]');
  });

  await page.route('**/api/sessions/*/events**', async (route) => {
    return fulfillJSON(route, eventsForSession(), 200, 'APISessionEvent[]');
  });

  await page.route('**/api/sessions/*', async (route) => {
    const url = new URL(route.request().url());
    const id = decodeURIComponent(url.pathname.split('/').pop() || '');
    const session = [...baseCompletedSessions, ...activeForScenario(scenario), ...childSessions].find((s) => s.id === id) || baseCompletedSessions[0];
    return fulfillJSON(route, { session }, 200, 'APISessionDetail');
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
    }, 200, 'APIToolPayload');
  });

  await page.route('**/sessions/**', async (route) => {
    const url = new URL(route.request().url());
    const match = url.pathname.match(/^\/sessions\/([^/]+)$/);
    if (match) {
      return route.fulfill({ status: 200, contentType: 'text/html', body: transcriptFixtureHTML(decodeURIComponent(match[1])) });
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

function isDashboardSearchURL(rawURL: string, predicate?: DashboardSearchURLPredicate) {
  const url = new URL(rawURL);
  return url.pathname === '/api/dashboard/search' && (!predicate || predicate(url));
}

export function waitForDashboardSearchRequest(page: Page, predicate?: DashboardSearchURLPredicate) {
  return page.waitForRequest((request: Request) => isDashboardSearchURL(request.url(), predicate));
}

export async function waitForDashboardSearchResponse(page: Page, predicate?: DashboardSearchURLPredicate) {
  const response = await page.waitForResponse((candidate: Response) => {
    return candidate.status() === 200 && isDashboardSearchURL(candidate.url(), predicate);
  });
  expect(response.ok()).toBe(true);
  return response;
}

export async function fillDashboardSearchAndWait(page: Page, value: string, predicate?: DashboardSearchURLPredicate) {
  const responsePromise = waitForDashboardSearchResponse(page, predicate || ((url) => url.searchParams.get('q') === value));
  await page.locator('#dashboard-session-search').fill(value);
  return responsePromise;
}

export async function triggerDashboardSearchAndWait(
  page: Page,
  action: () => Promise<unknown>,
  predicate?: DashboardSearchURLPredicate,
) {
  const requestPromise = waitForDashboardSearchRequest(page, predicate);
  const responsePromise = waitForDashboardSearchResponse(page, predicate);
  await action();
  const [request] = await Promise.all([requestPromise, responsePromise]);
  return new URL(request.url());
}

export async function waitForDashboardSearchRows(page: Page, count: number) {
  await expect(page.locator('#completed-sessions tr[data-search-row]')).toHaveCount(count);
}

export async function readDashboardScroll(page: Page): Promise<DashboardScrollSnapshot> {
  return page.evaluate(() => {
    const owner = document.getElementById('dashboard-main');
    const mainContent = document.getElementById('main-content');
    return {
      windowY: Math.round(window.scrollY || window.pageYOffset || 0),
      mainContentTop: Math.round(mainContent?.scrollTop || 0),
      dashboardTop: Math.round(owner?.scrollTop || 0),
      dashboardClientHeight: Math.round(owner?.clientHeight || 0),
      dashboardScrollHeight: Math.round(owner?.scrollHeight || 0),
    };
  });
}

async function startDashboardScrollRecorder(page: Page) {
  await page.evaluate(() => {
    type Snapshot = {
      windowY: number;
      mainContentTop: number;
      dashboardTop: number;
      dashboardClientHeight: number;
      dashboardScrollHeight: number;
    };
    type Recorder = {
      initial: Snapshot;
      current: Snapshot;
      maxWindowDelta: number;
      maxMainContentDelta: number;
      maxDashboardDelta: number;
      cleanup: () => void;
      sample: () => void;
    };
    const read = (): Snapshot => {
      const owner = document.getElementById('dashboard-main');
      const mainContent = document.getElementById('main-content');
      return {
        windowY: Math.round(window.scrollY || window.pageYOffset || 0),
        mainContentTop: Math.round(mainContent?.scrollTop || 0),
        dashboardTop: Math.round(owner?.scrollTop || 0),
        dashboardClientHeight: Math.round(owner?.clientHeight || 0),
        dashboardScrollHeight: Math.round(owner?.scrollHeight || 0),
      };
    };
    const initial = read();
    const listeners: Array<() => void> = [];
    const recorder: Recorder = {
      initial,
      current: initial,
      maxWindowDelta: 0,
      maxMainContentDelta: 0,
      maxDashboardDelta: 0,
      cleanup: () => {
        for (const remove of listeners) remove();
      },
      sample: () => {
        const current = read();
        recorder.current = current;
        recorder.maxWindowDelta = Math.max(recorder.maxWindowDelta, Math.abs(current.windowY - initial.windowY));
        recorder.maxMainContentDelta = Math.max(recorder.maxMainContentDelta, Math.abs(current.mainContentTop - initial.mainContentTop));
        recorder.maxDashboardDelta = Math.max(recorder.maxDashboardDelta, Math.abs(current.dashboardTop - initial.dashboardTop));
      },
    };
    const addScrollListener = (target: Window | HTMLElement | null) => {
      if (!target) return;
      const handler = () => recorder.sample();
      target.addEventListener('scroll', handler, { passive: true });
      listeners.push(() => target.removeEventListener('scroll', handler));
    };
    addScrollListener(window);
    addScrollListener(document.getElementById('main-content'));
    addScrollListener(document.getElementById('dashboard-main'));
    (window as Window & { __beaconDashboardScrollRecorder?: Recorder }).__beaconDashboardScrollRecorder = recorder;
  });
}

async function stopDashboardScrollRecorder(page: Page): Promise<DashboardScrollRecorderReport> {
  return page.evaluate(() => {
    const key = '__beaconDashboardScrollRecorder';
    const recorder = (window as Window & {
      __beaconDashboardScrollRecorder?: DashboardScrollRecorderReport & { cleanup?: () => void; sample?: () => void };
    })[key];
    if (!recorder) throw new Error('Dashboard scroll recorder was not started');
    recorder.sample?.();
    recorder.cleanup?.();
    delete (window as Window & { __beaconDashboardScrollRecorder?: unknown })[key];
    return {
      initial: recorder.initial,
      current: recorder.current,
      maxWindowDelta: recorder.maxWindowDelta,
      maxMainContentDelta: recorder.maxMainContentDelta,
      maxDashboardDelta: recorder.maxDashboardDelta,
    };
  });
}

export async function expectDashboardScrollNear(page: Page, expected: DashboardScrollSnapshot, tolerance = 2) {
  await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => resolve())));
  const current = await readDashboardScroll(page);
  expect(current.windowY).toBe(0);
  expect(current.mainContentTop).toBe(0);
  expect(Math.abs(current.dashboardTop - expected.dashboardTop)).toBeLessThanOrEqual(tolerance);
  return current;
}

export async function expectDashboardScrollStableDuring(page: Page, action: () => Promise<unknown>, tolerance = 2) {
  const before = await readDashboardScroll(page);
  expect(before.windowY).toBe(0);
  expect(before.mainContentTop).toBe(0);
  await startDashboardScrollRecorder(page);
  try {
    await action();
    await page.evaluate(() => new Promise<void>((resolve) => {
      requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
    }));
  } finally {
    const recorder = await stopDashboardScrollRecorder(page);
    expect(recorder.maxWindowDelta).toBeLessThanOrEqual(tolerance);
    expect(recorder.maxMainContentDelta).toBeLessThanOrEqual(tolerance);
    expect(recorder.maxDashboardDelta).toBeLessThanOrEqual(tolerance);
  }
  const after = await expectDashboardScrollNear(page, before, tolerance);
  return { before, after };
}

export async function scrollDashboardMainToSearch(page: Page) {
  await page.locator('#dashboard-main').evaluate((main) => {
    const target = document.getElementById('dashboard-search');
    if (!target) return;
    const mainRect = main.getBoundingClientRect();
    const targetRect = target.getBoundingClientRect();
    main.scrollTop += targetRect.top - mainRect.top - 24;
  });
  await page.waitForFunction(() => {
    const owner = document.getElementById('dashboard-main');
    const search = document.getElementById('dashboard-search');
    if (!owner || !search) return false;
    const ownerRect = owner.getBoundingClientRect();
    const searchRect = search.getBoundingClientRect();
    return searchRect.top >= ownerRect.top && searchRect.top < ownerRect.bottom;
  });
  const scroll = await readDashboardScroll(page);
  expect(scroll.windowY).toBe(0);
  expect(scroll.dashboardTop).toBeGreaterThan(0);
  return scroll;
}

export async function emitDashboardEvent(page: Page, type: string) {
  await page.evaluate((eventType) => {
    const sources = (window as unknown as {
      __beaconEventSources?: Array<{ url: string; emit: (type: string) => void }>;
    }).__beaconEventSources || [];
    const source = sources.find((candidate) => candidate.url.includes('/sse/dashboard'));
    if (!source) throw new Error('Dashboard EventSource fixture was not installed');
    source.emit(eventType);
  }, type);
}

export async function readCompletedRegionMetrics(page: Page) {
  return page.evaluate(() => {
    const search = document.getElementById('dashboard-search');
    const region = search?.parentElement;
    const tableScroller = document.getElementById('completed-table')?.parentElement;
    if (!region || !tableScroller) return { height: 0, tableGap: 0 };
    const regionRect = region.getBoundingClientRect();
    const tableRect = tableScroller.getBoundingClientRect();
    return {
      height: Math.round(regionRect.height),
      tableGap: Math.round(regionRect.bottom - tableRect.bottom),
    };
  });
}

export async function expectNoHorizontalOverflow(page: Page) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1);
  expect(overflow).toBe(false);
}

export async function expectDashboardTokenChartReady(page: Page) {
  const metrics = await page.locator('#dashboardTokenCumulativeChart').evaluate((canvas) => {
    const shell = canvas.closest('.dashboard-compact-chart');
    const surface = canvas.closest('.completed-table-surface');
    const searchHeader = canvas.closest('#dashboard-search');
    const canvasRect = canvas.getBoundingClientRect();
    const shellRect = shell?.getBoundingClientRect();
    const surfaceRect = surface?.getBoundingClientRect();
    return {
      canvasHeight: Math.round(canvasRect.height),
      canvasWidth: Math.round(canvasRect.width),
      shellHeight: Math.round(shellRect?.height || 0),
      shellWidth: Math.round(shellRect?.width || 0),
      surfaceWidth: Math.round(surfaceRect?.width || 0),
      inCompletedSurface: Boolean(surface),
      inSearchHeader: Boolean(searchHeader),
    };
  });
  expect(metrics.canvasHeight).toBeGreaterThan(180);
  expect(metrics.canvasWidth).toBeGreaterThan(0);
  expect(metrics.shellHeight).toBeGreaterThan(metrics.canvasHeight);
  expect(metrics.shellWidth).toBeGreaterThan(0);
  expect(metrics.shellWidth).toBeLessThanOrEqual(metrics.surfaceWidth);
  expect(metrics.inCompletedSurface).toBe(true);
  expect(metrics.inSearchHeader).toBe(true);
  await expect(page.locator('.dashboard-analytics-grid')).toHaveCount(0);
  await expect(page.locator('#dashboardModelActivityChart')).toHaveCount(0);
  await expect(page.locator('#dashboard-model-metric-control')).toHaveCount(0);
  await expect(page.locator('#dashboard-model-activity-data')).toHaveCount(0);
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
