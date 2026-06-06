import { test, type Browser, type Page } from '@playwright/test';
import { execSync } from 'node:child_process';
import { mkdirSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { performance as nodePerformance } from 'node:perf_hooks';
import {
  emitDashboardEvent,
  installDashboardFixtures,
  waitForDashboardSessionsResponse,
} from './fixtures/dashboard';

type ViewportCase = {
  name: string;
  width: number;
  height: number;
};

type MetricSample = {
  name: string;
  value_ms?: number;
  value?: number;
  unit: 'ms' | 'count' | 'score' | 'bytes';
  viewport: string;
  repeat: number;
};

type TableRenderExpectation = {
  mode: 'sessions' | 'search';
  sessionIDs: string[];
  firstSessionID: string;
  firstSnippet: string;
  empty: boolean;
  unavailable: boolean;
};

type APIResourceSummary = {
  path: string;
  count: number;
  median_ms: number;
  p95_ms: number;
  max_ms: number;
  transfer_bytes: number;
};

type APIResourceEntry = {
  path: string;
  start_time_ms: number;
  response_end_ms: number;
  duration_ms: number;
  transfer_bytes: number;
};

type BrowserPerfIteration = {
  viewport: string;
  repeat: number;
  navigation: Record<string, number>;
  paints: Record<string, number>;
  api_resources: APIResourceSummary[];
  api_waterfall: APIResourceEntry[];
  long_tasks: {
    count: number;
    max_ms: number;
    total_ms: number;
  };
  layout_shifts: {
    count: number;
    cumulative_score: number;
    max_score: number;
  };
  metrics: MetricSample[];
};

type BrowserPerfReport = {
  schema: 'beacon.browser_performance.v1';
  generated_at: string;
  git_revision: string;
  mode: 'fixtures' | 'external';
  base_url: string;
  repeats: number;
  viewports: ViewportCase[];
  iterations: BrowserPerfIteration[];
  summary: Array<{
    name: string;
    viewport: string;
    unit: MetricSample['unit'];
    samples: number;
    min: number;
    median: number;
    p95: number;
    max: number;
  }>;
};

const useFixtures = process.env.BEACON_BROWSER_PERF_FIXTURES !== '0';
const repeats = positiveInt(process.env.BEACON_BROWSER_PERF_REPEATS, 3);
const outputPath = resolve(process.env.BEACON_BROWSER_PERF_OUTPUT || 'test-results/perf/browser-performance.json');
const searchQuery = process.env.BEACON_BROWSER_PERF_SEARCH_QUERY || (useFixtures ? 'dashboard payload' : 'beacon');
const eventSearchQuery = process.env.BEACON_BROWSER_PERF_EVENT_QUERY || (useFixtures ? 'many' : searchQuery);
const viewports: ViewportCase[] = [
  { name: 'desktop', width: 1440, height: 900 },
  { name: 'mobile', width: 390, height: 844 },
];

test.describe.configure({ mode: 'serial' });
test.setTimeout(Math.max(180_000, repeats * viewports.length * 30_000));

test('records dashboard browser performance', async ({ browser, baseURL }) => {
  const iterations: BrowserPerfIteration[] = [];

  for (const viewport of viewports) {
    for (let repeat = 1; repeat <= repeats; repeat += 1) {
      iterations.push(await measureIteration(browser, baseURL || '', viewport, repeat));
    }
  }

  const report: BrowserPerfReport = {
    schema: 'beacon.browser_performance.v1',
    generated_at: new Date().toISOString(),
    git_revision: gitRevision(),
    mode: useFixtures ? 'fixtures' : 'external',
    base_url: baseURL || '',
    repeats,
    viewports,
    iterations,
    summary: summarize(iterations.flatMap((iteration) => iteration.metrics)),
  };

  mkdirSync(dirname(outputPath), { recursive: true });
  writeFileSync(outputPath, `${JSON.stringify(report, null, 2)}\n`);
  printSummary(report);
});

async function measureIteration(browser: Browser, baseURL: string, viewport: ViewportCase, repeat: number): Promise<BrowserPerfIteration> {
  const context = await browser.newContext({ baseURL, viewport: { width: viewport.width, height: viewport.height } });
  const page = await context.newPage();
  page.setDefaultTimeout(5_000);
  page.setDefaultNavigationTimeout(15_000);
  await installPerfObservers(page);
  if (useFixtures) {
    await installDashboardFixtures(page, { mockEventSource: true, scenario: viewport.name === 'mobile' ? 'many-active' : 'search-many' });
  }

  const metrics: MetricSample[] = [];
  try {
    metrics.push(await timeMetric(page, 'dashboard.cold_load.ready', viewport.name, repeat, async () => {
      await page.goto('/', { waitUntil: 'domcontentloaded' });
      await waitForDashboardReady(page, useFixtures);
    }));
    const coldRuntime = await collectRuntimeMetrics(page);

    metrics.push(await timeMetric(page, 'dashboard.warm_reload.ready', viewport.name, repeat, async () => {
      await page.reload({ waitUntil: 'domcontentloaded' });
      await waitForDashboardReady(page, useFixtures);
    }));

    await measureSearchFlows(page, metrics, viewport.name, repeat);
    await measureInteractionFlows(page, metrics, viewport.name, repeat);

    const runtime = await collectRuntimeMetrics(page);
    return {
      viewport: viewport.name,
      repeat,
      navigation: runtime.navigation,
      paints: runtime.paints,
      api_resources: runtime.api_resources,
      api_waterfall: runtime.api_waterfall,
      long_tasks: {
        count: runtime.long_tasks.count,
        max_ms: runtime.long_tasks.max_ms,
        total_ms: runtime.long_tasks.total_ms,
      },
      layout_shifts: runtime.layout_shifts,
      metrics: [
        ...metrics,
        {
          name: 'browser.long_tasks.count',
          value: runtime.long_tasks.count,
          unit: 'count',
          viewport: viewport.name,
          repeat,
        },
        {
          name: 'browser.long_tasks.max',
          value_ms: runtime.long_tasks.max_ms,
          unit: 'ms',
          viewport: viewport.name,
          repeat,
        },
        {
          name: 'browser.layout_shift.cumulative',
          value: runtime.layout_shifts.cumulative_score,
          unit: 'score',
          viewport: viewport.name,
          repeat,
        },
        {
          name: 'dashboard.cold_load.api_max',
          value_ms: maxAPIResource(coldRuntime.api_resources),
          unit: 'ms',
          viewport: viewport.name,
          repeat,
        },
      ],
    };
  } finally {
    await context.close();
  }
}

async function measureSearchFlows(page: Page, metrics: MetricSample[], viewport: string, repeat: number) {
  await scrollSearchIntoView(page);

  metrics.push(await timeMetric(page, 'search.session.input_to_rows', viewport, repeat, async () => {
    const response = waitForRequiredResponse(
      page,
      (url) => url.pathname === '/api/dashboard/sessions' &&
        url.searchParams.get('q') === searchQuery &&
        url.searchParams.get('state') === 'completed',
    );
    await setSearchInput(page, searchQuery);
    const data = await (await response).json().catch(() => null);
    await waitForSearchTableSettled(page, tableExpectation('sessions', data));
  }));

  metrics.push(await timeMetric(page, 'search.event.input_to_rows', viewport, repeat, async () => {
    const kind = page.locator('#dashboard-search-kind');
    if (await kind.count()) {
      const current = await kind.inputValue();
      if (current !== 'event') {
        const response = waitForOptionalResponse(
          page,
          (url) => url.pathname === '/api/dashboard/search' && url.searchParams.get('event_kind') === 'event',
        );
        await kind.selectOption('event');
        await response;
      }
    }
    const response = waitForRequiredResponse(
      page,
      (url) => url.pathname === '/api/dashboard/search' &&
        url.searchParams.get('q') === eventSearchQuery &&
        url.searchParams.get('event_kind') === 'event',
    );
    await setSearchInput(page, eventSearchQuery);
    const data = await (await response).json().catch(() => null);
    await waitForSearchTableSettled(page, tableExpectation('search', data));
  }));
}

async function measureInteractionFlows(page: Page, metrics: MetricSample[], viewport: string, repeat: number) {
  metrics.push(await timeMetric(page, 'interaction.chart_range_to_paint', viewport, repeat, async () => {
    const response = page.waitForResponse((candidate) => {
      const url = new URL(candidate.url());
      return candidate.status() === 200 && url.pathname === '/api/dashboard/charts' && url.searchParams.get('chart_range') === '7d';
    }).catch(() => undefined);
    const button = page.locator('#dashboard-chart-range-control').getByRole('button', { name: '7d' });
    if (await button.count()) await button.click();
    await response;
    await afterNextPaint(page);
  }));

  metrics.push(await timeMetric(page, 'interaction.active_sort_to_paint', viewport, repeat, async () => {
    const sort = page.locator('#active-session-sort');
    if (await sort.count()) await sort.selectOption('tokens');
    await afterNextPaint(page);
  }));

  if (useFixtures) {
    metrics.push(await timeMetric(page, 'interaction.active_sse_to_paint', viewport, repeat, async () => {
      const response = waitForDashboardSessionsResponse(page, (url) => url.searchParams.get('state') === 'active');
      await emitDashboardEvent(page, 'active-sessions-update');
      await response.catch(() => undefined);
      await afterNextPaint(page);
    }));
  }

  metrics.push(await timeMetric(page, 'interaction.inspector_open.ready', viewport, repeat, async () => {
    const opener = page.locator('.session-row-open').first();
    if (await opener.count()) {
      await opener.click();
      await page.locator('#session-inspector').waitFor({ state: 'visible' });
      await page.locator('#session-inspector #chat-view, #session-inspector .inspector-state-error').first().waitFor({ state: 'visible' });
    }
    await afterNextPaint(page);
  }));
}

async function waitForDashboardReady(page: Page, expectRows: boolean) {
  await page.locator('#dashboard-wrap').waitFor({ state: 'visible' });
  await page.locator('#dashboard-session-search').waitFor({ state: 'visible' });
  await page.locator('#active-sessions').waitFor({ state: 'visible' });
  await page.locator('#completed-sessions').waitFor({ state: 'visible' });
  if (expectRows) {
    await page.waitForFunction(() => document.querySelectorAll('#dashboard-analytics-summary > div').length >= 4);
    await page.waitForFunction(() => document.querySelectorAll('#completed-sessions tr[data-session-link]').length > 0);
  } else {
    await page.waitForFunction(() => {
      const label = document.getElementById('dashboard-connection-label')?.textContent || '';
      const summaryReady = document.querySelectorAll('#dashboard-analytics-summary > div').length >= 4;
      const rows = document.getElementById('completed-sessions');
      const tableReady = rows ? !/Loading/i.test(rows.textContent || '') : false;
      return /Live|Static|Disconnected/.test(label) && (summaryReady || tableReady);
    }, undefined, { timeout: 3_000 }).catch(() => undefined);
  }
  await afterNextPaint(page);
}

async function waitForSearchTableSettled(page: Page, expectation: TableRenderExpectation) {
  await page.waitForFunction((expected) => {
    const table = document.getElementById('completed-table');
    const rows = document.getElementById('completed-sessions');
    const input = document.getElementById('dashboard-session-search') as HTMLInputElement | null;
    if (!table || !rows || !input) return false;
    const text = rows.textContent || '';
    if (/Searching table sessions and events|Loading/i.test(text)) return false;
    if (table.getAttribute('data-table-mode') !== expected.mode) return false;
    if (input.value.trim().length === 0) return false;
    if (expected.unavailable) return /Search is not connected|Search unavailable/i.test(text);
    if (expected.empty) return /No matching|No sessions/i.test(text);
    if (expected.mode === 'sessions' && expected.sessionIDs.length > 0) {
      const renderedIDs = Array.from(rows.querySelectorAll('tr[data-session-link]')).map((row) => {
        const href = row.getAttribute('data-session-link') || '';
        return decodeURIComponent(href.replace(/^\/sessions\//, ''));
      });
      return renderedIDs.length === expected.sessionIDs.length &&
        renderedIDs.every((id, index) => id === expected.sessionIDs[index]);
    }
    if (expected.mode === 'search' && expected.firstSessionID) {
      const matchingRows = Array.from(rows.querySelectorAll('tr[data-search-row]')).filter((row) => {
        return row.getAttribute('data-session-id') === expected.firstSessionID;
      });
      if (expected.firstSnippet) return matchingRows.some((row) => (row.textContent || '').includes(expected.firstSnippet));
      return matchingRows.length > 0;
    }
    return rows.querySelector('tr[data-session-link], tr[data-search-row]') !== null;
  }, expectation, { timeout: 10_000 });
  await afterNextPaint(page);
}

function tableExpectation(mode: 'sessions' | 'search', data: unknown): TableRenderExpectation {
  const payload = data as {
    state?: string;
    items?: Array<{
      id?: string;
      session_id?: string;
      snippet?: string;
      session_title?: string;
    }>;
  } | null;
  const items = Array.isArray(payload?.items) ? payload.items : [];
  const first = items[0] || {};
  const sessionIDs = items.map((item) => item.id || item.session_id || '').filter((id) => id !== '');
  return {
    mode,
    sessionIDs,
    firstSessionID: first.id || first.session_id || '',
    firstSnippet: String(first.snippet || first.session_title || '').slice(0, 80),
    empty: items.length === 0,
    unavailable: payload?.state === 'unavailable',
  };
}

async function waitForRequiredResponse(page: Page, predicate: (url: URL) => boolean, timeout = 10_000) {
  const response = await page.waitForResponse((candidate) => {
    if (candidate.status() >= 400) return false;
    return predicate(new URL(candidate.url()));
  }, { timeout });
  if (!response.ok()) throw new Error(`Expected successful response for ${response.url()}, got ${response.status()}`);
  return response;
}

async function waitForOptionalResponse(page: Page, predicate: (url: URL) => boolean, timeout = 2_000) {
  const response = page.waitForResponse((candidate) => {
    if (candidate.status() >= 400) return false;
    return predicate(new URL(candidate.url()));
  }, { timeout }).catch(() => undefined);
  await Promise.race([response, page.waitForTimeout(timeout)]);
}

async function setSearchInput(page: Page, value: string) {
  await page.evaluate((nextValue) => {
    const node = document.getElementById('dashboard-session-search');
    if (!node) return;
    const input = node as HTMLInputElement;
    input.focus({ preventScroll: true });
    input.value = nextValue;
    input.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'insertText', data: nextValue }));
    input.dispatchEvent(new Event('change', { bubbles: true }));
  }, value);
}

async function scrollSearchIntoView(page: Page) {
  await page.evaluate(() => {
    const main = document.getElementById('dashboard-main');
    const search = document.getElementById('dashboard-search');
    if (!main || !search) return;
    const mainRect = main.getBoundingClientRect();
    const searchRect = search.getBoundingClientRect();
    main.scrollTop += searchRect.top - mainRect.top - 24;
  }).catch(() => undefined);
  await afterNextPaint(page);
}

async function installPerfObservers(page: Page) {
  await page.addInitScript(() => {
    const state = {
      longTasks: [] as Array<{ startTime: number; duration: number; name: string }>,
      layoutShifts: [] as Array<{ startTime: number; value: number; hadRecentInput: boolean }>,
    };
    Object.defineProperty(window, '__beaconBrowserPerf', { value: state, configurable: true });
    try {
      new PerformanceObserver((list) => {
        for (const entry of list.getEntries()) {
          state.longTasks.push({ startTime: entry.startTime, duration: entry.duration, name: entry.name });
        }
      }).observe({ type: 'longtask', buffered: true });
    } catch {
    }
    try {
      new PerformanceObserver((list) => {
        for (const entry of list.getEntries()) {
          const shift = entry as PerformanceEntry & { value?: number; hadRecentInput?: boolean };
          state.layoutShifts.push({
            startTime: shift.startTime,
            value: Number(shift.value || 0),
            hadRecentInput: Boolean(shift.hadRecentInput),
          });
        }
      }).observe({ type: 'layout-shift', buffered: true });
    } catch {
    }
  });
}

async function collectRuntimeMetrics(page: Page) {
  return page.evaluate(() => {
    const numberValue = (value: unknown) => {
      const n = Number(value || 0);
      return Number.isFinite(n) ? Math.round(n * 100) / 100 : 0;
    };
    const navigationEntry = performance.getEntriesByType('navigation')[0] as PerformanceNavigationTiming | undefined;
    const navigation = navigationEntry ? {
      dom_content_loaded_ms: numberValue(navigationEntry.domContentLoadedEventEnd - navigationEntry.startTime),
      load_event_ms: numberValue(navigationEntry.loadEventEnd - navigationEntry.startTime),
      response_end_ms: numberValue(navigationEntry.responseEnd - navigationEntry.startTime),
      transfer_size_bytes: numberValue(navigationEntry.transferSize),
    } : {};
    const paints: Record<string, number> = {};
    for (const paint of performance.getEntriesByType('paint')) {
      paints[paint.name.replace(/-/g, '_')] = numberValue(paint.startTime);
    }
    const resourceGroups = new Map<string, Array<PerformanceResourceTiming>>();
    const apiWaterfall: Array<{
      path: string;
      start_time_ms: number;
      response_end_ms: number;
      duration_ms: number;
      transfer_bytes: number;
    }> = [];
    for (const entry of performance.getEntriesByType('resource') as PerformanceResourceTiming[]) {
      const url = new URL(entry.name);
      if (!url.pathname.startsWith('/api/')) continue;
      const key = `${url.pathname}${url.search ? '?' + url.searchParams.toString() : ''}`;
      const existing = resourceGroups.get(key) || [];
      existing.push(entry);
      resourceGroups.set(key, existing);
      apiWaterfall.push({
        path: key,
        start_time_ms: numberValue(entry.startTime),
        response_end_ms: numberValue(entry.responseEnd),
        duration_ms: numberValue(entry.duration),
        transfer_bytes: numberValue(entry.transferSize),
      });
    }
    const percentile = (values: number[], p: number) => {
      if (values.length === 0) return 0;
      const sorted = [...values].sort((a, b) => a - b);
      const idx = Math.min(sorted.length - 1, Math.ceil((p / 100) * sorted.length) - 1);
      return numberValue(sorted[idx]);
    };
    const apiResources = Array.from(resourceGroups.entries()).map(([path, entries]) => {
      const durations = entries.map((entry) => numberValue(entry.duration));
      return {
        path,
        count: entries.length,
        median_ms: percentile(durations, 50),
        p95_ms: percentile(durations, 95),
        max_ms: numberValue(Math.max(...durations, 0)),
        transfer_bytes: entries.reduce((sum, entry) => sum + numberValue(entry.transferSize), 0),
      };
    }).sort((a, b) => b.max_ms - a.max_ms || b.count - a.count);
    const state = (window as Window & {
      __beaconBrowserPerf?: {
        longTasks: Array<{ duration: number }>;
        layoutShifts: Array<{ value: number; hadRecentInput: boolean }>;
      };
    }).__beaconBrowserPerf;
    const longTasks = state?.longTasks || [];
    const shifts = (state?.layoutShifts || []).filter((entry) => !entry.hadRecentInput);
    return {
      navigation,
      paints,
      api_resources: apiResources,
      api_waterfall: apiWaterfall.sort((a, b) => a.start_time_ms - b.start_time_ms || a.path.localeCompare(b.path)),
      long_tasks: {
        count: longTasks.length,
        max_ms: numberValue(Math.max(...longTasks.map((entry) => entry.duration), 0)),
        total_ms: numberValue(longTasks.reduce((sum, entry) => sum + entry.duration, 0)),
      },
      layout_shifts: {
        count: shifts.length,
        cumulative_score: numberValue(shifts.reduce((sum, entry) => sum + entry.value, 0)),
        max_score: numberValue(Math.max(...shifts.map((entry) => entry.value), 0)),
      },
    };
  });
}

async function timeMetric(page: Page, name: string, viewport: string, repeat: number, action: () => Promise<void>): Promise<MetricSample> {
  const start = nodePerformance.now();
  await action();
  const end = nodePerformance.now();
  return {
    name,
    value_ms: round(end - start),
    unit: 'ms',
    viewport,
    repeat,
  };
}

async function afterNextPaint(page: Page) {
  await page.evaluate(() => new Promise<void>((resolve) => {
    requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
  }));
}

function summarize(metrics: MetricSample[]) {
  const groups = new Map<string, MetricSample[]>();
  for (const metric of metrics) {
    const key = `${metric.name}\u0000${metric.viewport}\u0000${metric.unit}`;
    groups.set(key, [...(groups.get(key) || []), metric]);
  }
  return Array.from(groups.entries()).map(([, samples]) => {
    const values = samples.map(metricValue).filter((value) => Number.isFinite(value)).sort((a, b) => a - b);
    return {
      name: samples[0].name,
      viewport: samples[0].viewport,
      unit: samples[0].unit,
      samples: values.length,
      min: round(values[0] || 0),
      median: round(percentileSorted(values, 50)),
      p95: round(percentileSorted(values, 95)),
      max: round(values[values.length - 1] || 0),
    };
  }).sort((a, b) => a.name.localeCompare(b.name));
}

function printSummary(report: BrowserPerfReport) {
  console.log(`\nBeacon browser performance (${report.mode}, repeats=${report.repeats}, output=${outputPath})`);
  for (const item of report.summary) {
    const suffix = item.unit === 'ms' ? 'ms' : item.unit;
    console.log(`${item.name} [${item.viewport}]: median=${item.median}${suffix} p95=${item.p95}${suffix} max=${item.max}${suffix} samples=${item.samples}`);
  }
}

function maxAPIResource(resources: APIResourceSummary[]) {
  return round(Math.max(...resources.map((resource) => resource.max_ms), 0));
}

function metricValue(metric: MetricSample) {
  if (metric.unit === 'ms') return Number(metric.value_ms || 0);
  return Number(metric.value || 0);
}

function percentileSorted(values: number[], p: number) {
  if (values.length === 0) return 0;
  const idx = Math.min(values.length - 1, Math.ceil((p / 100) * values.length) - 1);
  return values[idx];
}

function positiveInt(value: string | undefined, fallback: number) {
  const n = Number(value || '');
  return Number.isInteger(n) && n > 0 ? n : fallback;
}

function round(value: number) {
  return Math.round(value * 100) / 100;
}

function gitRevision() {
  try {
    return execSync('git rev-parse --short HEAD', { encoding: 'utf8' }).trim();
  } catch {
    return process.env.GITHUB_SHA?.slice(0, 12) || 'unknown';
  }
}
