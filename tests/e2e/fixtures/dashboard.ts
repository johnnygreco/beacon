import { expect, type Locator, type Page, type Request, type Response, type Route } from '@playwright/test';
import { validateContract } from '../../contracts/api-contracts.cjs';

export const TEST_SESSION_ID = 'session-older-001';
export const ACTIVE_SESSION_ID = 'active-parent-001';
export const TEST_EVENT_ID = 'event-older-001';
export const SEARCH_SESSION_ID = 'session-search-001';

type Scenario =
  | 'default'
  | 'empty'
  | 'error-heavy'
  | 'many-active'
  | 'long-active'
  | 'search-many'
  | 'collapsed-activity'
  | 'resized-activity'
  | 'light-theme'
  | 'fixed-dark-theme';

type DashboardFixtureOptions = {
  scenario?: Scenario;
  activeScenarioSequence?: Array<Scenario | 'error'>;
  failOnce?: Array<'active' | 'completed' | 'activity' | 'charts' | 'search'>;
  searchUnavailable?: boolean;
  searchDelayMs?: number;
  searchDelayByQuery?: Record<string, number>;
  chartDelayMs?: number;
  disableEventSource?: boolean;
  mockEventSource?: boolean;
};

type DashboardSearchURLPredicate = (url: URL) => boolean;
type DashboardSessionsURLPredicate = (url: URL) => boolean;
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
const localNodeID = 'node-local';
const localCollectorID = 'collector-local';

function advancedMachineScenario(scenario: Scenario): boolean {
  return scenario === 'error-heavy' || scenario === 'many-active' || scenario === 'long-active';
}

function localizeMachineIdentity<T extends Record<string, unknown>>(item: T): T {
  const localized = {
    ...item,
    node_id: localNodeID,
    collector_id: localCollectorID,
  } as T;
  if (Array.isArray(localized.child_sessions)) {
    localized.child_sessions = (localized.child_sessions as Array<Record<string, unknown>>).map(localizeMachineIdentity);
  }
  return localized;
}

function topologyItems<T extends Record<string, unknown>>(items: T[], scenario: Scenario): T[] {
  return advancedMachineScenario(scenario) ? items : items.map(localizeMachineIdentity);
}

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
      source: 'source-a',
      node_id: 'node-a',
      collector_id: 'collector-a',
      source_id: 'source-a',
      runtime: 'runtime-a',
      project_key: 'beacon',
      project_path: '/Users/example/projects/beacon/very/long/dashboard/migration/worktree',
      provider: 'provider-a',
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
      attention_state: 'error',
      attention_score: 100,
      attention_reasons: ['errors'],
      last_model: 'generic-model-a-super-long-model-name',
      total_cost_usd: 0.42,
      cost_event_count: 1,
      cost_provenance: 'event_cost_usd',
      working_dir: '/Users/example/projects/beacon/very/long/dashboard/migration/worktree',
      has_session_end: true,
      subagent_count: 2,
    };
  }
  return {
    id: `session-completed-${String(i).padStart(3, '0')}`,
    title: i % 3 === 0 ? 'Dashboard fixture validation' : `Completed agent run ${i}`,
    source: i % 2 === 0 ? 'source-b' : 'source-a',
    node_id: i % 2 === 0 ? 'node-b' : 'node-a',
    collector_id: i % 2 === 0 ? 'collector-b' : 'collector-a',
    source_id: i % 2 === 0 ? 'source-b' : 'source-a',
    runtime: i % 2 === 0 ? 'runtime-b' : 'runtime-a',
    project_key: i % 2 === 0 ? 'beacon' : 'dashboard',
    project_path: i % 2 === 0 ? '/Users/example/projects/beacon' : '/Users/example/projects/beacon/subsystems/dashboard',
    provider: i % 2 === 0 ? 'provider-b' : 'provider-a',
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
    attention_state: i % 11 === 0 ? 'error' : 'completed',
    attention_score: i % 11 === 0 ? 100 : 0,
    attention_reasons: i % 11 === 0 ? ['errors'] : [],
    last_model: i % 2 === 0 ? 'generic-model-b' : 'generic-model-a',
    total_cost_usd: i % 2 === 0 ? 0.18 + i / 100 : 0,
    cost_event_count: i % 2 === 0 ? 1 : 0,
    cost_provenance: i % 2 === 0 ? 'event_cost_usd' : 'none',
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
    source: 'source-a',
    node_id: 'node-a',
    collector_id: 'collector-a',
    source_id: 'source-a',
    runtime: 'runtime-a',
    project_key: 'beacon',
    project_path: '/Users/example/projects/beacon',
    provider: 'provider-a',
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
    attention_state: 'running',
    attention_score: 0,
    attention_reasons: [],
    last_model: 'generic-model-a',
    total_cost_usd: 0.12,
    cost_event_count: 1,
    cost_provenance: 'event_cost_usd',
    working_dir: '/Users/example/projects/beacon',
    has_session_end: false,
    subagent_count: 1,
    child_sessions: [
      {
        id: 'active-child-001',
        title: 'Live child worker',
        source: 'source-a',
        node_id: 'node-a',
        collector_id: 'collector-a',
        source_id: 'source-a',
        runtime: 'runtime-a',
        project_key: 'beacon',
        project_path: '/Users/example/projects/beacon',
        provider: 'provider-a',
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
        attention_state: 'running',
        attention_score: 0,
        attention_reasons: [],
        last_model: 'generic-model-c',
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

function rangeFixtureLabel(range: string) {
  if (range === '7d') return '7d range fixture';
  if (range === '30d') return '30d range fixture';
  return '';
}

function panelRange(url: URL, keys: string[], fallback = '24h') {
  for (const key of keys) {
    if (!url.searchParams.has(key)) continue;
    const value = url.searchParams.get(key) || '';
    return value === 'all' ? '' : value;
  }
  return fallback;
}

function manyActiveSessions() {
  const modelCases = [
    { last_model: 'generic-model-a', provider: 'provider-a' },
    { last_model: 'generic-model-c', provider: 'provider-a' },
    { last_model: 'generic-model-b', provider: 'provider-b' },
    { last_model: 'local-experimental-32k', provider: 'provider-b' },
  ];
  return Array.from({ length: 8 }, (_, i) => {
    const modelCase = modelCases[i] || {
      last_model: i % 2 === 0 ? 'generic-model-a' : 'generic-model-b',
      provider: i % 2 === 0 ? 'provider-a' : 'provider-b',
    };
    return {
      ...activeSessions[0],
      ...modelCase,
      id: `active-parent-${String(i + 1).padStart(3, '0')}`,
      title: `Live queue item ${i + 1}`,
      source: i % 3 === 0 ? 'source-b' : (modelCase.provider === 'provider-b' ? 'source-b' : 'source-a'),
      node_id: i % 3 === 0 ? 'node-c' : (modelCase.provider === 'provider-b' ? 'node-b' : 'node-a'),
      collector_id: i % 3 === 0 ? 'collector-c' : (modelCase.provider === 'provider-b' ? 'collector-b' : 'collector-a'),
      source_id: i % 3 === 0 ? 'source-c' : (modelCase.provider === 'provider-b' ? 'source-b' : 'source-a'),
      runtime: i % 3 === 0 ? 'runtime-c' : (modelCase.provider === 'provider-b' ? 'runtime-b' : 'runtime-a'),
      project_key: i % 3 === 0 ? 'project-c' : 'beacon',
      project_path: i % 3 === 0 ? '/srv/agents/work/project-c' : '/Users/example/projects/beacon',
      total_tokens: 15000 + i * 4300,
      tool_call_count: 3 + i,
      error_count: i === 2 ? 3 : 0,
      attention_state: i === 2 ? 'error' : 'running',
      attention_score: i === 2 ? 300 : 0,
      attention_reasons: i === 2 ? ['errors'] : [],
      child_sessions: [],
    };
  });
}

function longActiveSessions() {
  const longTitle = 'Realtime dashboard overflow validation with a very long queued agent title that should truncate cleanly';
  const longModel = 'generic-model-a-super-long-model-name-with-overflow-sentinel-and-extra-routing-metadata';
  const longPath = '/Users/example/projects/beacon/worktrees/layout-refresh/with/a/deeply/nested/dashboard/path/that/should/not/push/the/card/wider';
  return manyActiveSessions().slice(0, 4).map((session, i) => ({
    ...session,
    id: `active-long-${String(i + 1).padStart(3, '0')}`,
    title: `${longTitle} ${i + 1}`,
    duration: i === 0 ? '123h 45m 59s' : `${48 + i}h ${17 + i}m`,
    total_tokens: 9_876_543 + i * 123_456,
    turn_count: 1234 + i * 111,
    tool_call_count: 987 + i * 77,
    error_count: i === 1 ? 42 : 0,
    last_model: i % 2 === 0 ? longModel : 'generic-model-b-extremely-long-routing-label-for-active-card-overflow-check',
    working_dir: `${longPath}/${i + 1}`,
    child_sessions: i === 0 ? [{
      ...activeSessions[0].child_sessions[0],
      id: 'active-long-child-001',
      parent_session_id: 'active-long-001',
      title: 'Overflow validation child with an intentionally long title',
      last_model: longModel,
      working_dir: `${longPath}/child`,
      duration: '99h 1m',
      tool_call_count: 321,
    }] : [],
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

function chartRangeFactor(range: string) {
	if (range === '1h') return 0.25;
	if (range === '7d') return 3;
  if (range === '30d') return 8;
  if (range === '') return 13;
	return 1;
}

function scopeValues(url: URL | undefined, singular: string, plural: string) {
	if (!url) return [];
	const values = [
		...url.searchParams.getAll(singular),
		...url.searchParams.getAll(plural),
	].flatMap((value) => value.split(','));
	return [...new Set(values.map((value) => value.trim()).filter(Boolean))];
}

function scopeMetadata(url?: URL) {
	const filters: Record<string, string[]> = {};
	const entries: Array<[string, string, string]> = [
		['node_ids', 'node_id', 'node_ids'],
		['collector_ids', 'collector_id', 'collector_ids'],
		['source_ids', 'source_id', 'source_ids'],
		['source_names', 'source_name', 'source_names'],
		['runtimes', 'runtime', 'runtimes'],
		['project_keys', 'project_key', 'project_keys'],
	];
	for (const [field, singular, plural] of entries) {
		const values = scopeValues(url, singular, plural);
		if (values.length > 0) filters[field] = values;
	}
	return { auth_scope_applied: false, filters };
}

function matchesScopeValue(value: unknown, values: string[]) {
  if (values.length === 0) return true;
  return values.includes(String(value || '').trim());
}

function sessionMatchesScope(session: Record<string, unknown>, url?: URL) {
  const sourceName = Object.prototype.hasOwnProperty.call(session, 'source')
    ? session.source
    : session.source_name;
  return matchesScopeValue(session.node_id, scopeValues(url, 'node_id', 'node_ids')) &&
    matchesScopeValue(session.collector_id, scopeValues(url, 'collector_id', 'collector_ids')) &&
    matchesScopeValue(session.source_id, scopeValues(url, 'source_id', 'source_ids')) &&
    matchesScopeValue(sourceName, scopeValues(url, 'source_name', 'source_names')) &&
    matchesScopeValue(session.runtime, scopeValues(url, 'runtime', 'runtimes')) &&
    matchesScopeValue(session.project_key, scopeValues(url, 'project_key', 'project_keys'));
}

function searchResultMatchesScope(result: Record<string, unknown>, url?: URL) {
  return matchesScopeValue(result.node_id, scopeValues(url, 'node_id', 'node_ids')) &&
    matchesScopeValue(result.collector_id, scopeValues(url, 'collector_id', 'collector_ids')) &&
    matchesScopeValue(result.source_id, scopeValues(url, 'source_id', 'source_ids')) &&
    matchesScopeValue(result.source_name, scopeValues(url, 'source_name', 'source_names')) &&
    matchesScopeValue(result.runtime, scopeValues(url, 'runtime', 'runtimes')) &&
    matchesScopeValue(result.project_key, scopeValues(url, 'project_key', 'project_keys'));
}

function filterSessionsByScope<T extends Record<string, unknown>>(items: T[], url?: URL) {
  return items.filter((item) => sessionMatchesScope(item, url));
}

function matchesAnyScopeValue(values: unknown[], selected: string[]) {
  if (selected.length === 0) return true;
  const normalized = new Set(values.map((value) => String(value || '').trim()).filter(Boolean));
  return selected.some((value) => normalized.has(value));
}

function fleetNodeMatchesScope(node: {
  node_id?: string;
  collectors?: string[];
  runtimes?: string[];
  projects?: string[];
  sources?: string[];
  sources_detail?: Array<{ source_id?: string; source_name?: string }>;
}, url?: URL) {
  const sourceDetails = node.sources_detail || [];
  const sourceIDs = sourceDetails.map((source) => source.source_id || '');
  const sourceNames = [
    ...(node.sources || []),
    ...sourceDetails.map((source) => source.source_name || ''),
  ];
  return matchesScopeValue(node.node_id, scopeValues(url, 'node_id', 'node_ids')) &&
    matchesAnyScopeValue(node.collectors || [], scopeValues(url, 'collector_id', 'collector_ids')) &&
    matchesAnyScopeValue(sourceIDs, scopeValues(url, 'source_id', 'source_ids')) &&
    matchesAnyScopeValue(sourceNames, scopeValues(url, 'source_name', 'source_names')) &&
    matchesAnyScopeValue(node.runtimes || [], scopeValues(url, 'runtime', 'runtimes')) &&
    matchesAnyScopeValue(node.projects || [], scopeValues(url, 'project_key', 'project_keys'));
}

function chartPayload(scenario: Scenario, range = '24h', url?: URL) {
  const errorHeavy = scenario === 'error-heavy';
  const empty = scenario === 'empty';
  const factor = chartRangeFactor(range);
  const datasets = empty ? [] : [
    {
      label: 'generic-model-a',
      provider: 'provider-a',
      provider_label: 'Provider A',
      model: 'generic-model-a',
      values: [1000, 3800, 7200, 14000, 30000, 36000, 32000].map((value) => Math.round(value * factor)),
      total_tokens: Math.round(124000 * factor),
      tool_call_count: Math.round(24 * factor),
      error_count: errorHeavy ? 9 : 1,
      call_count: Math.round(16 * factor),
    },
    {
      label: 'generic-model-b',
      provider: 'provider-b',
      provider_label: 'Provider B',
      model: 'generic-model-b',
      values: [800, 2400, 5800, 9000, 12000, 13000, 18000].map((value) => Math.round(value * factor)),
      total_tokens: Math.round(61000 * factor),
      tool_call_count: Math.round(13 * factor),
      error_count: errorHeavy ? 5 : 0,
      call_count: Math.round(9 * factor),
    },
    {
      label: 'generic-model-c',
      provider: 'provider-a',
      provider_label: 'Provider A',
      model: 'generic-model-c',
      values: [400, 700, 1200, 2800, 3900, 4000, 3500].map((value) => Math.round(value * factor)),
      total_tokens: Math.round(16500 * factor),
      tool_call_count: Math.round(8 * factor),
      error_count: errorHeavy ? 4 : 0,
      call_count: Math.round(6 * factor),
    },
  ];
  const summary = empty ? { total_tokens: 0, model_count: 0, tool_call_count: 0, call_count: 0, error_rate: 0, error_count: 0 } : {
    total_tokens: Math.round(201500 * factor),
    model_count: 3,
    tool_call_count: Math.round(45 * factor),
    call_count: Math.round(31 * factor),
    error_rate: errorHeavy ? 13.8 : 1.9,
    error_count: errorHeavy ? 18 : 2,
	};
	return {
		range,
		scope: scopeMetadata(url),
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
        total_tokens: {
          label: 'Total Tokens',
          unit: 'tokens',
          datasets,
        },
        input_tokens: {
          label: 'Input Tokens',
          unit: 'tokens',
          datasets: datasets.map((d) => ({
            ...d,
            values: empty ? [] : d.values.map((value) => Math.round(value * 0.52)),
          })),
        },
        output_tokens: {
          label: 'Output Tokens',
          unit: 'tokens',
          datasets: datasets.map((d) => ({
            ...d,
            values: empty ? [] : d.values.map((value) => Math.round(value * 0.38)),
          })),
        },
        cache_read_tokens: {
          label: 'Cache Read Tokens',
          unit: 'tokens',
          datasets: datasets.map((d) => ({
            ...d,
            values: empty ? [] : d.values.map((value) => Math.round(value * 0.1)),
          })),
        },
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
          unit: 'tool calls',
          datasets: datasets.map((d, i) => ({
            ...d,
            values: empty ? [] : [1 + i, 3 + i, 4 + i, 6 + i, 8 + i, 10 + i, 13 + i],
          })),
        },
      },
    },
  };
}

function activityItems(scenario: Scenario, range = '24h') {
  if (scenario === 'empty') return [];
  const rangeLabel = rangeFixtureLabel(range);
  const base = [
    {
      id: TEST_EVENT_ID,
      type: 'tool_call',
      summary: rangeLabel ? `${rangeLabel}: Read dashboard fixture payload` : 'Read dashboard fixture payload',
      session_id: TEST_SESSION_ID,
      node_id: 'node-a',
      collector_id: 'collector-a',
      source_id: 'source-a',
      source_name: 'source-a',
      runtime: 'runtime-a',
      provider: 'provider-a',
      timestamp: iso(0, -1),
      relative_time: '26d ago',
    },
    {
      id: 'event-message-001',
      type: 'message',
      summary: 'Agent summarized the dashboard state',
      session_id: 'session-completed-001',
      node_id: 'node-b',
      collector_id: 'collector-b',
      source_id: 'source-b',
      source_name: 'source-b',
      runtime: 'runtime-b',
      provider: 'provider-b',
      timestamp: iso(0, -2),
      relative_time: '2h ago',
    },
    {
      id: 'event-error-001',
      type: 'error',
      summary: 'Model request returned a recoverable error',
      session_id: 'session-completed-011',
      node_id: 'node-a',
      collector_id: 'collector-a',
      source_id: 'source-a',
      source_name: 'source-a',
      runtime: 'runtime-a',
      provider: 'provider-a',
      timestamp: iso(0, -3),
      relative_time: '3h ago',
    },
    {
      id: 'event-tool-error-001',
      type: 'tool_error',
      summary: 'Shell command exited non-zero',
      session_id: 'session-completed-022',
      node_id: 'node-b',
      collector_id: 'collector-b',
      source_id: 'source-b',
      source_name: 'source-b',
      runtime: 'runtime-b',
      provider: 'provider-b',
      timestamp: iso(0, -4),
      relative_time: '4h ago',
    },
  ];
  const items = scenario === 'error-heavy'
    ? [...base, ...Array.from({ length: 7 }, (_, i) => ({
      id: `event-error-heavy-${i}`,
      type: i % 2 === 0 ? 'error' : 'tool_error',
      summary: `Repeated error burst ${i + 1}`,
      session_id: `session-completed-${String(i + 1).padStart(3, '0')}`,
      node_id: i % 2 === 0 ? 'node-a' : 'node-b',
      collector_id: i % 2 === 0 ? 'collector-a' : 'collector-b',
      source_id: i % 2 === 0 ? 'source-a' : 'source-b',
      source_name: i % 2 === 0 ? 'source-a' : 'source-b',
      runtime: i % 2 === 0 ? 'runtime-a' : 'runtime-b',
      provider: i % 2 === 0 ? 'provider-a' : 'provider-b',
      timestamp: iso(0, -5 - i),
      relative_time: `${5 + i}h ago`,
    }))]
    : base;
  return topologyItems(items, scenario);
}

function fleetPayload(scenario: Scenario, url?: URL) {
  const empty = scenario === 'empty';
  const nodes = empty ? [] : (advancedMachineScenario(scenario) ? [
    {
      node_id: 'node-a',
      label: 'Node A',
	      status: 'online',
	      collector_count: 1,
	      missing_heartbeat_collectors: 0,
	      collectors: ['collector-a'],
      sources: ['source-a'],
      runtimes: ['runtime-a'],
      projects: ['beacon', 'dashboard'],
      active_sessions: scenario === 'many-active' ? 3 : 1,
      attention_sessions: scenario === 'error-heavy' ? 2 : 1,
      total_sessions: 18,
      total_tokens: 240000,
      error_count: scenario === 'error-heavy' ? 7 : 1,
      queue_depth: 0,
      spool_bytes: 0,
      active_files: 2,
      heartbeat_error_count: 0,
      last_seen_label: 'just now',
      heartbeat_status: 'online',
      sources_detail: [
        {
          collector_id: 'collector-a',
          source_id: 'source-a',
          source_name: 'source-a',
          status: 'online',
          queue_depth: 0,
          spool_bytes: 0,
          active_files: 2,
          error_count: 0,
        },
      ],
    },
    {
      node_id: 'node-b',
      label: 'Node B',
	      status: scenario === 'error-heavy' ? 'stale' : 'online',
	      collector_count: 1,
	      missing_heartbeat_collectors: 0,
	      collectors: ['collector-b'],
      sources: ['source-b'],
      runtimes: ['runtime-b'],
      projects: ['beacon'],
      active_sessions: scenario === 'many-active' ? 3 : 0,
      attention_sessions: scenario === 'error-heavy' ? 4 : 0,
      total_sessions: 22,
      total_tokens: 310000,
      error_count: scenario === 'error-heavy' ? 9 : 1,
      queue_depth: scenario === 'error-heavy' ? 9 : 1,
      spool_bytes: scenario === 'error-heavy' ? 65536 : 2048,
      active_files: 5,
      heartbeat_error_count: scenario === 'error-heavy' ? 2 : 0,
      last_seen_label: scenario === 'error-heavy' ? '4m ago' : '35s ago',
      heartbeat_status: scenario === 'error-heavy' ? 'stale' : 'online',
      sources_detail: [
        {
          collector_id: 'collector-b',
          source_id: 'source-b',
          source_name: 'source-b',
          status: scenario === 'error-heavy' ? 'stale' : 'online',
          queue_depth: scenario === 'error-heavy' ? 9 : 1,
          spool_bytes: scenario === 'error-heavy' ? 65536 : 2048,
          active_files: 5,
          error_count: scenario === 'error-heavy' ? 2 : 0,
        },
      ],
    },
    {
      node_id: 'node-c',
      label: 'Node C',
	      status: 'offline',
	      collector_count: 1,
	      missing_heartbeat_collectors: 0,
	      collectors: ['collector-c'],
      sources: ['source-b'],
      runtimes: ['runtime-c'],
      projects: ['project-c'],
      active_sessions: scenario === 'many-active' ? 2 : 0,
      attention_sessions: scenario === 'many-active' ? 1 : 0,
      total_sessions: 6,
      total_tokens: 92000,
      error_count: 0,
      queue_depth: 0,
      spool_bytes: 0,
      active_files: 0,
      heartbeat_error_count: 0,
      last_seen_label: '18m ago',
      heartbeat_status: 'offline',
      sources_detail: [
        {
          collector_id: 'collector-c',
          source_id: 'source-c',
          source_name: 'source-b',
          status: 'missing',
          queue_depth: 0,
          spool_bytes: 0,
          active_files: 0,
          error_count: 0,
        },
      ],
    },
  ] : [
    {
      node_id: localNodeID,
      label: 'Local machine',
      status: 'online',
      collector_count: 1,
      missing_heartbeat_collectors: 0,
      collectors: [localCollectorID],
      sources: ['source-a', 'source-b'],
      runtimes: ['runtime-a', 'runtime-b'],
      projects: ['beacon', 'dashboard'],
      active_sessions: 1,
      attention_sessions: 1,
      total_sessions: 40,
      total_tokens: 550000,
      error_count: 2,
      queue_depth: 0,
      spool_bytes: 0,
      active_files: 7,
      heartbeat_error_count: 0,
      last_seen_label: 'just now',
      heartbeat_status: 'online',
      sources_detail: [
        {
          collector_id: localCollectorID,
          source_id: 'source-a',
          source_name: 'source-a',
          status: 'online',
          queue_depth: 0,
          spool_bytes: 0,
          active_files: 3,
          error_count: 0,
        },
        {
          collector_id: localCollectorID,
          source_id: 'source-b',
          source_name: 'source-b',
          status: 'online',
          queue_depth: 0,
          spool_bytes: 0,
          active_files: 4,
          error_count: 0,
        },
      ],
    },
  ]);
	  const filtered = nodes.filter((node) => fleetNodeMatchesScope(node, url));
	  const totals = filtered.reduce((acc, node) => {
	    acc.node_count += 1;
	    acc.collector_count += node.collector_count;
	    acc.missing_heartbeat_collectors += node.missing_heartbeat_collectors;
	    const healthCollectors = Math.max(0, node.collector_count - node.missing_heartbeat_collectors);
	    if (node.status === 'online') acc.online_collectors += healthCollectors;
	    else if (node.status === 'stale') acc.stale_collectors += healthCollectors;
	    else if (node.status !== 'active') acc.offline_collectors += healthCollectors;
	    acc.active_sessions += node.active_sessions;
    acc.attention_sessions += node.attention_sessions;
    acc.total_sessions += node.total_sessions;
    acc.total_tokens += node.total_tokens;
    acc.queue_depth += node.queue_depth;
    acc.spool_bytes += node.spool_bytes;
    acc.heartbeat_error_count += node.heartbeat_error_count;
    return acc;
  }, {
    node_count: 0,
    collector_count: 0,
    online_collectors: 0,
    stale_collectors: 0,
	    offline_collectors: 0,
	    missing_heartbeat_collectors: 0,
	    active_sessions: 0,
    attention_sessions: 0,
    total_sessions: 0,
    total_tokens: 0,
    queue_depth: 0,
    spool_bytes: 0,
    heartbeat_error_count: 0,
  });
  return { scope: scopeMetadata(url), totals, nodes: filtered };
}

function dashboardSearchBaseResults() {
  return [
    {
      result_type: 'event',
      event_uid: 'event-search-001',
      session_id: SEARCH_SESSION_ID,
      event_kind: 'message',
      snippet: 'Dashboard payload search surfaced the exact migration note inside the assistant response.',
      provider: 'provider-a',
      model: 'generic-model-a',
      node_id: 'node-a',
      collector_id: 'collector-a',
      source_id: 'source-a',
      source_name: 'source-a',
      runtime: 'runtime-a',
      project_key: 'beacon',
      project_path: '/Users/example/projects/beacon/search',
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
      provider: 'provider-b',
      model: 'generic-model-b',
      node_id: 'node-b',
      collector_id: 'collector-b',
      source_id: 'source-b',
      source_name: 'source-b',
      runtime: 'runtime-b',
      project_key: 'beacon',
      project_path: '/Users/example/projects/beacon/search',
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
      provider: 'provider-b',
      model: 'generic-model-b',
      node_id: 'node-c',
      collector_id: 'collector-c',
      source_id: 'source-c',
      source_name: 'source-b',
      runtime: 'runtime-c',
      project_key: 'project-c',
      project_path: '/srv/agents/work/project-c',
      score: 1.72,
      timestamp: iso(2, 2),
      relative_time: '2d ago',
      session_title: 'Search timeout diagnosis',
      working_dir: '/Users/example/projects/beacon',
    },
  ];
}

function dashboardSearchManyResults(range = '') {
  const base = dashboardSearchBaseResults();
  const rangeLabel = rangeFixtureLabel(range);
  return Array.from({ length: 35 }, (_, i) => {
    const result = { ...base[i % base.length] };
    result.event_uid = `event-many-${String(i + 1).padStart(3, '0')}`;
    result.session_id = i % 2 === 0 ? SEARCH_SESSION_ID : 'session-search-many';
    result.snippet = `${rangeLabel ? `${rangeLabel}: ` : ''}Many-result fixture item ${i + 1} for pagination and visual density checks.`;
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
  const range = panelRange(url, ['completed_range', 'range', 'search_range'], '');
  const eventKind = url.searchParams.get('event_kind') || '';
  const sessionID = (url.searchParams.get('session_id') || '').toLowerCase();
  const sort = url.searchParams.get('sort') || 'relevance';
  const limit = Number(url.searchParams.get('limit') || 30);
  const scope = scopeMetadata(url);
  const active = query !== '' || eventKind !== '' || sessionID !== '' || sort !== 'relevance' || limit !== 30 || Object.keys(scope.filters).length > 0;
  if (!active) {
    return { state: 'idle', sort, limit, scope, has_more: false, items: [] };
  }
  if (scenario === 'empty') {
    return { state: 'ready', query, range, event_kind: eventKind, session_id: sessionID, sort, limit, scope, has_more: false, items: [] };
  }
  const denseSearchFixture = scenario === 'search-many';
  const source = topologyItems(denseSearchFixture || query.toLowerCase() === 'many'
    ? dashboardSearchManyResults(range)
    : dashboardSearchBaseResults(), scenario);
  const acceptedKinds = eventKind === 'error'
    ? new Set(['error', 'tool_error'])
    : new Set(eventKind && eventKind !== 'event' && eventKind !== 'session' ? [eventKind] : []);
  const eventResults = eventKind === 'session' ? [] : source.filter((result) => {
    if (!denseSearchFixture && !dashboardSearchMatchesText(result, query)) return false;
    if (!searchResultMatchesScope(result, url)) return false;
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
  const includeSessionResults = eventKind === 'session' || (query && !eventKind);
  const sessionResults = includeSessionResults
    ? topologyItems(baseCompletedSessions, scenario)
      .filter((session) => {
        if (seenSessions.has(session.id)) return false;
        if (!sessionMatchesScope(session, url)) return false;
        if (sessionID && !session.id.toLowerCase().startsWith(sessionID)) return false;
        if (!query) return true;
        const haystack = [session.id, session.title, session.last_model, session.working_dir, session.provider].join(' ').toLowerCase();
        const queryTokens = query.toLowerCase().split(/\s+/).filter(Boolean);
        const indexedEventText = session.id === TEST_SESSION_ID ? 'read dashboard fixture payload migration' : '';
        return queryTokens.every((token) => haystack.includes(token)) ||
          queryTokens.every((token) => indexedEventText.includes(token));
      })
      .map((session) => ({
        result_type: 'session',
        event_uid: '',
        session_id: session.id,
        node_id: session.node_id,
        collector_id: session.collector_id,
        source_id: session.source_id,
        source_name: session.source,
        runtime: session.runtime,
        project_key: session.project_key,
        project_path: session.project_path,
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
    scope,
    has_more: sorted.length > limit,
    items: sorted.slice(0, limit),
  };
}

function completedForRequest(url: URL, scenario: Scenario) {
  if (scenario === 'empty') return { items: [], hasMore: false };
  const query = (url.searchParams.get('q') || '').toLowerCase();
  const range = panelRange(url, ['completed_range', 'range', 'search_range']);
  const offset = Number(url.searchParams.get('offset') || 0);
  const limit = Number(url.searchParams.get('limit') || 30);
  const sessions = topologyItems(baseCompletedSessions, scenario);
  let source = filterSessionsByScope(query
    ? sessions.filter((s) => {
      const metadata = [s.id, s.title, s.last_model, s.working_dir, s.provider].join(' ').toLowerCase();
      const indexedEventText = s.id === TEST_SESSION_ID ? 'read dashboard fixture payload' : '';
      const queryTokens = query.split(/\s+/).filter(Boolean);
      return metadata.includes(query) || (queryTokens.length > 0 && queryTokens.every((token) => indexedEventText.includes(token)));
    })
    : sessions, url);
  const rangeLabel = rangeFixtureLabel(range);
  if (rangeLabel && !query && offset === 0) {
    source = [
      {
        ...topologyItems([baseCompletedSessions[1]], scenario)[0],
        id: `session-range-${range || 'all'}`,
        title: `${rangeLabel} completed session`,
        ended_at: iso(0, 2),
        working_dir: `/Users/example/projects/beacon/${range || 'all'}-range`,
      },
      ...source,
    ];
  }
  const sort = url.searchParams.get('sort') || 'ended';
  const asc = (url.searchParams.get('direction') || 'desc') === 'asc';
  const sorted = [...source].sort((a, b) => {
    const value = (s: typeof baseCompletedSessions[number]) => {
      switch (sort) {
        case 'name': return s.title;
        case 'node': return s.node_id;
        case 'runtime': return s.runtime;
        case 'model': return s.last_model;
        case 'tokens': return s.total_tokens;
        case 'turns': return s.turn_count;
        case 'tools': return s.tool_call_count;
        case 'errors': return s.error_count;
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
      model: 'generic-model-a',
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
      model: 'generic-model-a',
      tokens: 180,
      duration_ms: 0,
    },
  ];
}

function activeForScenario(scenario: Scenario, url?: URL) {
  if (scenario === 'empty') return [];
  if (scenario === 'many-active') return filterSessionsByScope(manyActiveSessions(), url);
  if (scenario === 'long-active') return filterSessionsByScope(longActiveSessions(), url);
  return filterSessionsByScope(topologyItems(activeSessions, scenario), url);
}

function initialStorageForScenario(scenario: Scenario): Record<string, string> {
  switch (scenario) {
    case 'collapsed-activity':
      return {
        'beacon-timeline-width': '0',
        'beacon-timeline-prev-width': '420',
      };
    case 'resized-activity':
      return {
        'beacon-timeline-width': '520',
        'beacon-timeline-prev-width': '520',
      };
    case 'light-theme':
      return {
        'beacon-dashboard-theme': 'catppuccin',
        'beacon-dashboard-appearance': 'light',
        'beacon-dashboard-preferred-appearance': 'light',
        'beacon-dashboard-resolved-theme': 'catppuccin-light',
      };
    case 'fixed-dark-theme':
      return {
        'beacon-dashboard-theme': 'dracula',
        'beacon-dashboard-appearance': 'dark',
        'beacon-dashboard-preferred-appearance': 'dark',
        'beacon-dashboard-resolved-theme': 'dracula-dark',
      };
    default:
      return {};
  }
}

async function fulfillJSON(route: Route, data: unknown, status = 200, contractName = '') {
  if (contractName) validateContract(contractName, data);
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(data),
  });
}

function conversationFixtureHTML(sessionID = TEST_SESSION_ID) {
  const eventID = sessionID === TEST_SESSION_ID ? TEST_EVENT_ID : `event-${sessionID}`;
  return `
    <div id="chat-view" class="transcript-chat-view space-y-3">
      <details id="${eventID}" open class="rounded border border-gray-700 p-3 bg-gray-800/30">
        <summary class="cursor-pointer">Read dashboard fixture payload</summary>
        <div class="code-container relative mt-3">
          <pre><code>{"file_path":"internal/views/pages/dashboard.templ"}</code></pre>
          <button type="button" onclick="copyToClipboard(this)" title="Copy to clipboard" aria-label="Copy to clipboard">
            <span class="copy-icon">Copy</span><span class="check-icon hidden">Copied</span>
          </button>
        </div>
      </details>
      <details id="${eventID}-assistant" open class="rounded border border-gray-700 p-3 bg-gray-800/30"><summary>Assistant summary</summary><p>Dashboard state summarized.</p></details>
      <details id="${eventID}-result" open class="rounded border border-gray-700 p-3 bg-gray-800/30"><summary>Tool result</summary><p>Payload loaded.</p></details>
    </div>
    <div id="timeline-view" class="transcript-timeline-view hidden rounded border border-gray-700 p-3">
      <a href="#${eventID}" class="text-blue-400">Read dashboard fixture payload</a>
    </div>
  `;
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
          ${conversationFixtureHTML(sessionID)}
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

  const initialStorage = initialStorageForScenario(scenario);
  if (Object.keys(initialStorage).length > 0) {
    await page.addInitScript((values) => {
      for (const [key, value] of Object.entries(values as Record<string, string>)) {
        localStorage.setItem(key, value);
      }
    }, initialStorage);
  }

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
			return fulfillJSON(route, {
				state: 'active',
				range: '',
				offset: 0,
				limit: 30,
				scope: scopeMetadata(url),
				has_more: false,
				items: activeForScenario(activeScenario, url),
			}, 200, 'APIDashboardSessionsResponse');
		}
		const completed = completedForRequest(url, scenario);
		return fulfillJSON(route, {
      state: 'completed',
      range: panelRange(url, ['completed_range', 'range', 'search_range']),
			query: url.searchParams.get('q') || '',
			offset: Number(url.searchParams.get('offset') || 0),
			limit: Number(url.searchParams.get('limit') || 30),
			scope: scopeMetadata(url),
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
        range: panelRange(url, ['completed_range', 'range', 'search_range'], ''),
        event_kind: url.searchParams.get('event_kind') || '',
			session_id: url.searchParams.get('session_id') || '',
			sort: url.searchParams.get('sort') || 'relevance',
			limit: Number(url.searchParams.get('limit') || 30),
			scope: scopeMetadata(url),
			has_more: false,
			items: [],
		}, 200, 'APIDashboardSearchResponse');
    }
    return fulfillJSON(route, dashboardSearchForRequest(url, scenario), 200, 'APIDashboardSearchResponse');
  });

  await page.route('**/api/dashboard/charts**', async (route) => {
    const url = new URL(route.request().url());
    if (options.chartDelayMs && options.chartDelayMs > 0) {
      await new Promise((resolve) => setTimeout(resolve, options.chartDelayMs));
		}
		if (failures.delete('charts')) return fulfillJSON(route, { error: 'fixture failure' }, 500);
		const range = panelRange(url, ['chart_range', 'range']);
		return fulfillJSON(route, chartPayload(scenario, range, url), 200, 'APIDashboardCharts');
	});

  await page.route('**/api/dashboard/fleet**', async (route) => {
    const url = new URL(route.request().url());
    return fulfillJSON(route, fleetPayload(scenario, url), 200, 'APIDashboardFleetResponse');
  });

  await page.route('**/api/dashboard/activity**', async (route) => {
    if (failures.delete('activity')) return fulfillJSON(route, { error: 'fixture failure' }, 500);
    const url = new URL(route.request().url());
    const kinds = (url.searchParams.get('event_kind') || '').split(',').filter(Boolean);
    const range = panelRange(url, ['activity_range', 'range']);
    const items = filterSessionsByScope(activityItems(scenario, range), url)
      .filter((item) => kinds.length === 0 || kinds.includes(item.type));
    return fulfillJSON(route, items, 200, 'APIActivityItem[]');
  });

  await page.route('**/api/sessions?**', async (route) => {
    return fulfillJSON(route, [...topologyItems(baseCompletedSessions, scenario), ...activeForScenario(scenario)], 200, 'APISessionSummary[]');
  });

  await page.route('**/api/sessions/*/subagents', async (route) => {
    return fulfillJSON(route, topologyItems(childSessions, scenario), 200, 'APISessionSummary[]');
  });

  await page.route('**/api/sessions/*/events**', async (route) => {
    return fulfillJSON(route, eventsForSession(), 200, 'APISessionEvent[]');
  });

  await page.route('**/api/sessions/*', async (route) => {
    const url = new URL(route.request().url());
    const id = decodeURIComponent(url.pathname.split('/').pop() || '');
    const session = [...topologyItems(baseCompletedSessions, scenario), ...activeForScenario(scenario), ...topologyItems(childSessions, scenario)].find((s) => s.id === id) || topologyItems([baseCompletedSessions[0]], scenario)[0];
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

  await page.route('**/sessions/*/conversation', async (route) => {
    const url = new URL(route.request().url());
    const match = url.pathname.match(/^\/sessions\/([^/]+)\/conversation$/);
    const id = match ? decodeURIComponent(match[1]) : TEST_SESSION_ID;
    return route.fulfill({ status: 200, contentType: 'text/html', body: conversationFixtureHTML(id) });
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

export async function gotoDashboard(page: Page, path = '/') {
  await page.goto(path, { waitUntil: 'domcontentloaded' });
  await expect(page.getByRole('heading', { name: 'Beacon Realtime Dashboard' })).toBeVisible();
  await expect(page.locator('#dashboard-analytics-summary > div')).toHaveCount(4);
  await expect(page.locator('#dashboard-last-updated')).toHaveCount(0);
  await expect(page.locator('#dashboard-connection-label')).toContainText(/Live|Connecting|Static|Disconnected/);
}

export async function waitForCompletedRows(page: Page, count = 30) {
  await expect(page.locator('#completed-sessions tr[data-session-link]')).toHaveCount(count);
}

function isDashboardSearchURL(rawURL: string, predicate?: DashboardSearchURLPredicate) {
  const url = new URL(rawURL);
  return url.pathname === '/api/dashboard/search' && (!predicate || predicate(url));
}

function isDashboardSessionsURL(rawURL: string, predicate?: DashboardSessionsURLPredicate) {
  const url = new URL(rawURL);
  return url.pathname === '/api/dashboard/sessions' && (!predicate || predicate(url));
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

export function waitForDashboardSessionsRequest(page: Page, predicate?: DashboardSessionsURLPredicate) {
  return page.waitForRequest((request: Request) => isDashboardSessionsURL(request.url(), predicate));
}

export async function waitForDashboardSessionsResponse(page: Page, predicate?: DashboardSessionsURLPredicate) {
  const response = await page.waitForResponse((candidate: Response) => {
    return candidate.status() === 200 && isDashboardSessionsURL(candidate.url(), predicate);
  });
  expect(response.ok()).toBe(true);
  return response;
}

export async function fillDashboardSearchAndWait(page: Page, value: string, predicate?: DashboardSearchURLPredicate) {
  const responsePromise = waitForDashboardSearchResponse(page, predicate || ((url) => url.searchParams.get('q') === value));
  await page.locator('#dashboard-session-search').fill(value);
  return responsePromise;
}

export async function fillDashboardSessionSearchAndWait(page: Page, value: string, predicate?: DashboardSessionsURLPredicate) {
  const responsePromise = waitForDashboardSessionsResponse(page, predicate || ((url) => url.searchParams.get('q') === value));
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

export async function triggerDashboardSessionsAndWait(
  page: Page,
  action: () => Promise<unknown>,
  predicate?: DashboardSessionsURLPredicate,
) {
  const requestPromise = waitForDashboardSessionsRequest(page, predicate);
  const responsePromise = waitForDashboardSessionsResponse(page, predicate);
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
  await page.waitForFunction(({ expectedTop, allowed }) => {
    const owner = document.getElementById('dashboard-main');
    const mainContent = document.getElementById('main-content');
    const windowY = Math.round(window.scrollY || window.pageYOffset || 0);
    const mainContentTop = Math.round(mainContent?.scrollTop || 0);
    const dashboardTop = Math.round(owner?.scrollTop || 0);
    return windowY === 0 &&
      mainContentTop === 0 &&
      Math.abs(dashboardTop - expectedTop) <= allowed;
  }, { expectedTop: expected.dashboardTop, allowed: tolerance }, { timeout: 1000 });
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
  await page.waitForFunction(() => {
    const owner = document.getElementById('dashboard-main');
    const search = document.getElementById('dashboard-search');
    return Boolean(owner && search && owner.scrollHeight > owner.clientHeight);
  });
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
    return owner.scrollTop > 0 && searchRect.top >= ownerRect.top && searchRect.top < ownerRect.bottom;
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
    const region = search?.closest('.completed-table-surface');
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
    const analytics = canvas.closest('.dashboard-analytics-panel');
    const surface = canvas.closest('.completed-table-surface');
    const searchHeader = canvas.closest('#dashboard-search');
    const summary = document.getElementById('dashboard-analytics-summary');
    const metricSelect = document.getElementById('dashboard-chart-metric');
    const rangeControl = document.getElementById('dashboard-chart-range-control');
    const refresh = document.getElementById('dashboard-chart-refresh-btn');
    const canvasRect = canvas.getBoundingClientRect();
    const shellRect = shell?.getBoundingClientRect();
    const analyticsRect = analytics?.getBoundingClientRect();
    return {
      canvasHeight: Math.round(canvasRect.height),
      canvasWidth: Math.round(canvasRect.width),
      shellHeight: Math.round(shellRect?.height || 0),
      shellWidth: Math.round(shellRect?.width || 0),
      analyticsWidth: Math.round(analyticsRect?.width || 0),
      inAnalyticsPanel: Boolean(analytics),
      inCompletedSurface: Boolean(surface),
      inSearchHeader: Boolean(searchHeader),
      summaryInAnalyticsPanel: Boolean(summary?.closest('.dashboard-analytics-panel')),
      summaryInCompletedSurface: Boolean(summary?.closest('.completed-table-surface')),
      summaryInSearchHeader: Boolean(summary?.closest('#dashboard-search')),
      metricSelectInAnalyticsPanel: Boolean(metricSelect?.closest('.dashboard-analytics-panel')),
      metricSelectInSearchHeader: Boolean(metricSelect?.closest('#dashboard-search')),
      rangeControlInAnalyticsPanel: Boolean(rangeControl?.closest('.dashboard-analytics-panel')),
      refreshInAnalyticsPanel: Boolean(refresh?.closest('.dashboard-analytics-panel')),
    };
  });
  expect(metrics.canvasHeight).toBeGreaterThan(180);
  expect(metrics.canvasWidth).toBeGreaterThan(0);
  expect(metrics.shellHeight).toBeGreaterThan(metrics.canvasHeight);
  expect(metrics.shellWidth).toBeGreaterThan(0);
  expect(metrics.shellWidth).toBeLessThanOrEqual(metrics.analyticsWidth);
  expect(metrics.inAnalyticsPanel).toBe(true);
  expect(metrics.inCompletedSurface).toBe(false);
  expect(metrics.inSearchHeader).toBe(false);
  expect(metrics.summaryInAnalyticsPanel).toBe(true);
  expect(metrics.summaryInCompletedSurface).toBe(false);
  expect(metrics.summaryInSearchHeader).toBe(false);
  expect(metrics.metricSelectInAnalyticsPanel).toBe(true);
  expect(metrics.metricSelectInSearchHeader).toBe(false);
  expect(metrics.rangeControlInAnalyticsPanel).toBe(true);
  expect(metrics.refreshInAnalyticsPanel).toBe(true);
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
    page.locator('#dashboard-connection-label'),
    page.locator('#dashboard-connection-indicator'),
    page.locator('#sse-indicator'),
  ];
}
