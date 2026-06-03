import { expect, test, type Page, type Request } from '@playwright/test';
import {
  SEARCH_SESSION_ID,
  TEST_SESSION_ID,
  attachPageGuards,
  emitDashboardEvent,
  expectDashboardTokenChartReady,
  expectDashboardScrollNear,
  expectDashboardScrollStableDuring,
  expectNoHorizontalOverflow,
  fillDashboardSearchAndWait,
  gotoDashboard,
  installDashboardFixtures,
  readCompletedRegionMetrics,
  readDashboardScroll,
  scrollDashboardMainToSearch,
  triggerDashboardSearchAndWait,
  waitForDashboardSearchRequest,
  waitForCompletedRows,
  waitForDashboardSearchResponse,
  waitForDashboardSearchRows,
} from './fixtures/dashboard';

async function gotoDashboardSearch(page: Page) {
  await installDashboardFixtures(page);
  await gotoDashboard(page);
  await waitForCompletedRows(page, 30);
}

async function selectDashboardSearchType(page: Page, type: string) {
  const current = await page.locator('#dashboard-search-kind').inputValue();
  if (current === type) return;
  await triggerDashboardSearchAndWait(
    page,
    () => page.locator('#dashboard-search-kind').selectOption(type),
    (url) => url.searchParams.get('event_kind') === type,
  );
}

async function fillDashboardEventSearchAndWait(page: Page, value: string, predicate?: (url: URL) => boolean) {
  await selectDashboardSearchType(page, 'event');
  return fillDashboardSearchAndWait(
    page,
    value,
    predicate || ((url) => url.searchParams.get('q') === value && url.searchParams.get('event_kind') === 'event'),
  );
}

async function readSearchOffsetInDashboard(page: Page) {
  return page.evaluate(() => {
    const owner = document.getElementById('dashboard-main');
    const search = document.getElementById('dashboard-search');
    if (!owner || !search) return { offset: 0, dashboardTop: 0, windowY: 0, mainContentTop: 0 };
    const ownerRect = owner.getBoundingClientRect();
    const searchRect = search.getBoundingClientRect();
    return {
      offset: Math.round(searchRect.top - ownerRect.top),
      dashboardTop: Math.round(owner.scrollTop),
      windowY: Math.round(window.scrollY || window.pageYOffset || 0),
      mainContentTop: Math.round(document.getElementById('main-content')?.scrollTop || 0),
    };
  });
}

async function expectSearchVerticalFlow(page: Page) {
  const overlaps = await page.evaluate(() => {
    const selectors = [
      '.completed-table-heading',
      '#completed-table thead',
      ...Array.from(document.querySelectorAll('#completed-sessions tr[data-search-row]')).slice(0, 12).map((_, index) => `#completed-sessions tr[data-search-row]:nth-of-type(${index + 1})`),
    ];
    const rects = selectors
      .map((selector) => {
        const el = document.querySelector(selector);
        if (!el) return null;
        const rect = el.getBoundingClientRect();
        return { selector, top: rect.top, bottom: rect.bottom };
      })
      .filter(Boolean) as Array<{ selector: string; top: number; bottom: number }>;
    const failures: string[] = [];
    for (let i = 1; i < rects.length; i++) {
      if (rects[i].top < rects[i - 1].bottom - 1) {
        failures.push(`${rects[i - 1].selector} overlaps ${rects[i].selector}`);
      }
    }
    return failures;
  });
  expect(overlaps).toEqual([]);
}

async function readDashboardChartTotal(page: Page) {
  return page.evaluate(() => {
    const chart = (window as Window & { dashboardTokenCumulativeChart?: { data: { datasets: Array<{ data: number[] }> } } }).dashboardTokenCumulativeChart;
    return (chart?.data.datasets || []).reduce((sum, dataset) => {
      return sum + (dataset.data || []).reduce((datasetSum, value) => datasetSum + Number(value || 0), 0);
    }, 0);
  });
}

async function readDashboardChartState(page: Page) {
  return page.evaluate(() => {
    const chart = (window as Window & {
      dashboardTokenCumulativeChart?: {
        $dashboardMetricKind?: string;
        $dashboardMetricUnit?: string;
        options: { scales: { y: { title?: { text?: string } } } };
        data: { datasets: Array<{ data: number[] }> };
      };
    }).dashboardTokenCumulativeChart;
    const total = (chart?.data.datasets || []).reduce((sum, dataset) => {
      return sum + (dataset.data || []).reduce((datasetSum, value) => datasetSum + Number(value || 0), 0);
    }, 0);
    return {
      metricKind: chart?.$dashboardMetricKind || '',
      metricUnit: chart?.$dashboardMetricUnit || '',
      yTitle: chart?.options.scales.y.title?.text || '',
      total,
    };
  });
}

async function expectDashboardChartMetricState(page: Page, metricKind: string, metricUnit: string, yTitle: RegExp | string) {
  await expect(page.locator('#dashboard-chart-metric')).toHaveValue(metricKind);
  const state = await readDashboardChartState(page);
  expect(state.metricKind).toBe(metricKind);
  expect(state.metricUnit).toBe(metricUnit);
  if (typeof yTitle === 'string') expect(state.yTitle).toBe(yTitle);
  else expect(state.yTitle).toMatch(yTitle);
  expect(state.total).toBeGreaterThan(0);
  return state;
}

async function collectDashboardRequestsDuring(page: Page, action: () => Promise<unknown>) {
  const requests: string[] = [];
  const handler = (request: Request) => {
    const url = new URL(request.url());
    if (url.pathname.startsWith('/api/dashboard/')) requests.push(`${url.pathname}?${url.searchParams.toString()}`);
  };
  page.on('request', handler);
  try {
    await action();
    await page.waitForTimeout(75);
  } finally {
    page.off('request', handler);
  }
  return requests;
}

async function scrollDashboardMainToBottom(page: Page) {
  await page.locator('#dashboard-main').evaluate((main) => {
    main.scrollTop = main.scrollHeight;
  });
  await page.waitForFunction(() => {
    const owner = document.getElementById('dashboard-main');
    if (!owner) return false;
    const maxTop = Math.max(0, owner.scrollHeight - owner.clientHeight);
    return maxTop > 0 && Math.abs(owner.scrollTop - maxTop) <= 4;
  });
  const scroll = await readDashboardScroll(page);
  expect(scroll.windowY).toBe(0);
  expect(scroll.mainContentTop).toBe(0);
  expect(scroll.dashboardTop).toBeGreaterThan(0);
  return scroll;
}

test.describe('dashboard search workflows', () => {
  test('redirects legacy search URL back to the dashboard search table', async ({ page }) => {
    await installDashboardFixtures(page);
    await page.goto('/search', { waitUntil: 'domcontentloaded' });
    await expect(page).toHaveURL(/\/#dashboard-search$/);
    await expect(page.locator('#dashboard-session-search')).toBeVisible();
  });

  test('loads direct dashboard URLs without search params in sessions mode', async ({ page }) => {
    await installDashboardFixtures(page);
    const searchRequests: string[] = [];
    page.on('request', (request) => {
      const url = new URL(request.url());
      if (url.pathname === '/api/dashboard/search') searchRequests.push(url.search);
    });

    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);
    await expect(page.locator('#completed-table')).toHaveAttribute('data-table-mode', 'sessions');
    await expect(page.locator('#completed-table-title')).toHaveText('Completed Sessions');
    await expect(page.locator('#dashboard-search-kind')).toHaveValue('session');
    expect(searchRequests).toEqual([]);
  });

  test('ignores stale search sort params when no table filter is active', async ({ page }) => {
    await installDashboardFixtures(page);
    const searchRequests: string[] = [];
    page.on('request', (request) => {
      const url = new URL(request.url());
      if (url.pathname === '/api/dashboard/search') searchRequests.push(url.search);
    });

    await page.goto('/?event_kind=&search_sort=newest&search_limit=60', { waitUntil: 'domcontentloaded' });
    await waitForCompletedRows(page, 30);
    await expect(page.locator('#completed-table')).toHaveAttribute('data-table-mode', 'sessions');
    await expect(page.locator('#completed-table-title')).toHaveText('Completed Sessions');
    await expect(page.locator('#dashboard-search-kind')).toHaveValue('session');
    await expect(page.locator('#dashboard-search-sort')).toHaveValue('relevance');
    await expect.poll(() => new URL(page.url()).searchParams.get('search_sort')).toBeNull();
    await expect.poll(() => new URL(page.url()).searchParams.get('search_limit')).toBeNull();
    expect(searchRequests).toEqual([]);
  });

  test('hydrates direct session search URLs as session search results', async ({ page }) => {
    await installDashboardFixtures(page);
    const query = 'claude-sonnet-4-super-long-model-name';
    const searchResponse = waitForDashboardSearchResponse(page, (url) =>
      url.searchParams.get('q') === query &&
      url.searchParams.get('event_kind') === 'session'
    );

    await page.goto(`/?q=${encodeURIComponent(query)}&event_kind=session`, { waitUntil: 'domcontentloaded' });
    await searchResponse;
    await expect(page.locator('#dashboard-search-kind')).toHaveValue('session');
    await expect(page.locator('#completed-table')).toHaveAttribute('data-table-mode', 'search');
    await waitForDashboardSearchRows(page, 1);
    await expect(page.locator('#completed-sessions tr[data-search-row]').first()).toHaveAttribute('data-event-kind', 'session');
  });

  test('hydrates direct query URLs as default session search results', async ({ page }) => {
    await installDashboardFixtures(page);
    const query = 'claude-sonnet-4-super-long-model-name';
    const searchResponse = waitForDashboardSearchResponse(page, (url) =>
      url.searchParams.get('q') === query &&
      url.searchParams.get('event_kind') === 'session'
    );

    await page.goto(`/?q=${encodeURIComponent(query)}`, { waitUntil: 'domcontentloaded' });
    await searchResponse;
    await expect(page.locator('#dashboard-search-kind')).toHaveValue('session');
    await expect(page.locator('#completed-table')).toHaveAttribute('data-table-mode', 'search');
    await waitForDashboardSearchRows(page, 1);
    await expect(page.locator('#completed-sessions tr[data-search-row]').first()).toHaveAttribute('data-event-kind', 'session');
  });

  test('searches full session contents while table type remains Sessions', async ({ page }) => {
    await installDashboardFixtures(page);
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);

    await fillDashboardSearchAndWait(
      page,
      'read dashboard fixture payload',
      (url) => url.searchParams.get('q') === 'read dashboard fixture payload' && url.searchParams.get('event_kind') === 'session',
    );

    await expect(page.locator('#dashboard-search-kind')).toHaveValue('session');
    await expect(page.locator('#completed-table')).toHaveAttribute('data-table-mode', 'search');
    await waitForDashboardSearchRows(page, 1);
    await expect(page.locator('#completed-sessions tr[data-search-row]').first()).toHaveAttribute('data-event-kind', 'session');
    await expect(page.locator('#completed-sessions tr[data-search-row]').first()).toHaveAttribute('data-session-id', TEST_SESSION_ID);
  });

  test('exercises query, filters, clearing, reset, and pagination', async ({ page }) => {
    const guards = attachPageGuards(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await gotoDashboardSearch(page);
    await expectNoHorizontalOverflow(page);
    await expect(page.locator('#completed-table')).toHaveAttribute('data-table-mode', 'sessions');
    await expect(page.getByLabel('Table type')).toHaveValue('session');

    await page.keyboard.press('/');
    await expect(page.locator('#dashboard-session-search')).toBeFocused();

    await fillDashboardSearchAndWait(
      page,
      'claude-sonnet-4-super-long-model-name',
      (url) => url.searchParams.get('q') === 'claude-sonnet-4-super-long-model-name' && url.searchParams.get('event_kind') === 'session',
    );
    await waitForDashboardSearchRows(page, 1);
    await expect(page.locator('#completed-table')).toHaveAttribute('data-table-mode', 'search');
    await expect(page.locator('#completed-sessions tr[data-search-row]').first()).toHaveAttribute('data-event-kind', 'session');
    await expect(page.locator('#completed-sessions tr[data-search-row]').first()).toContainText('Session metadata');
    await expect(page.locator('#dashboard-search-clear')).toHaveCount(0);

    const escapeClearResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() && url.pathname === '/api/dashboard/sessions' && url.searchParams.get('state') === 'completed';
    });
    await page.keyboard.press('Escape');
    await escapeClearResponse;
    await expect(page.locator('#dashboard-session-search')).toHaveValue('');
    await expect(page.locator('#dashboard-session-search')).toBeFocused();
    await expect(page.locator('#dashboard-search-kind')).toHaveValue('session');
    await expect(page.locator('#completed-table')).toHaveAttribute('data-table-mode', 'sessions');
    await waitForCompletedRows(page, 30);

    await fillDashboardEventSearchAndWait(page, 'search');
    await waitForDashboardSearchRows(page, 3);
    await expect(page.locator('#completed-table')).toHaveAttribute('data-table-mode', 'search');

    await triggerDashboardSearchAndWait(
      page,
      () => page.locator('#dashboard-search-kind').selectOption('tool_call'),
      (url) => url.searchParams.get('q') === 'search' && url.searchParams.get('event_kind') === 'tool_call',
    );
    await waitForDashboardSearchRows(page, 1);
    await expect(page.locator('#completed-sessions tr[data-search-row]').first()).toHaveAttribute('data-event-kind', 'tool_call');
    await expect(page.getByLabel('Table type')).toHaveValue('tool_call');

    await triggerDashboardSearchAndWait(
      page,
      () => page.locator('#dashboard-search-session').fill(SEARCH_SESSION_ID),
      (url) => url.searchParams.get('session_id') === SEARCH_SESSION_ID,
    );
    await waitForDashboardSearchRows(page, 1);

    await triggerDashboardSearchAndWait(
      page,
      () => page.locator('#dashboard-search-sort').selectOption('newest'),
      (url) => url.searchParams.get('sort') === 'newest',
    );
    const filteredRequest = await triggerDashboardSearchAndWait(
      page,
      () => page.locator('#dashboard-range-control').getByRole('button', { name: '7d' }).click(),
      (url) => url.searchParams.get('completed_range') === '7d',
    );
    await expect(page.locator('#dashboard-range-control').getByRole('button', { name: '7d' })).toHaveAttribute('aria-pressed', 'true');

    expect(filteredRequest.searchParams.get('q')).toBe('search');
    expect(filteredRequest.searchParams.get('event_kind')).toBe('tool_call');
    expect(filteredRequest.searchParams.get('session_id')).toBe(SEARCH_SESSION_ID);
    expect(filteredRequest.searchParams.get('sort')).toBe('newest');
    expect(filteredRequest.searchParams.get('completed_range')).toBe('7d');
    expect(new URL(page.url()).searchParams.get('search_range')).toBeNull();

    const resetResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() &&
        url.pathname === '/api/dashboard/sessions' &&
        url.searchParams.get('state') === 'completed' &&
        url.searchParams.get('completed_range') === '7d';
    });
    await page.locator('#dashboard-search-reset').click();
    await resetResponse;
    await expect(page.locator('#dashboard-session-search')).toHaveValue('');
    await expect(page.locator('#dashboard-search-session')).toHaveValue('');
    await expect(page.locator('#dashboard-search-kind')).toHaveValue('session');
    await expect(page.locator('#dashboard-search-sort')).toHaveValue('relevance');
    await expect(page.locator('#dashboard-range-control').getByRole('button', { name: '7d' })).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#completed-table')).toHaveAttribute('data-table-mode', 'sessions');
    await waitForCompletedRows(page, 30);

    await fillDashboardEventSearchAndWait(page, 'many');
    await waitForDashboardSearchRows(page, 30);
    await expect(page.getByRole('button', { name: 'Show more' })).toBeVisible();
    await expectSearchVerticalFlow(page);

    await triggerDashboardSearchAndWait(
      page,
      () => page.getByRole('button', { name: 'Show more' }).click(),
      (url) => url.searchParams.get('q') === 'many' && url.searchParams.get('event_kind') === 'event' && url.searchParams.get('limit') === '60',
    );
    await waitForDashboardSearchRows(page, 35);
    await expect(page.getByRole('button', { name: 'Show more' })).toHaveCount(0);

    await triggerDashboardSearchAndWait(
      page,
      () => page.locator('#dashboard-search-kind').selectOption('session'),
      (url) => url.searchParams.get('q') === 'many' && url.searchParams.get('event_kind') === 'session',
    );
    const metadataRequest = await triggerDashboardSearchAndWait(
      page,
      () => page.locator('#dashboard-session-search').fill('claude-sonnet-4-super-long-model-name'),
      (url) => url.searchParams.get('q') === 'claude-sonnet-4-super-long-model-name',
    );
    expect(metadataRequest.searchParams.get('limit')).toBe('30');
    expect(metadataRequest.searchParams.get('event_kind')).toBe('session');
    await waitForDashboardSearchRows(page, 1);
    await expect(page.locator('#completed-sessions tr[data-search-row]').first()).toHaveAttribute('data-event-kind', 'session');
    await expect(page.locator('#completed-sessions tr[data-search-row]').first()).toContainText('Session metadata');

    await guards.expectClean();
  });

  test('uses the search-many fixture scenario for dense search pagination', async ({ page }) => {
    const guards = attachPageGuards(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await installDashboardFixtures(page, { scenario: 'search-many' });
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);

    await fillDashboardEventSearchAndWait(page, 'fixture density');
    await waitForDashboardSearchRows(page, 30);
    await expect(page.locator('#completed-table-title')).toHaveText('Search Results');
    await expect(page.locator('#completed-session-status')).toHaveText('All time');
    await expect(page.locator('#completed-session-status')).not.toContainText(/search results?|shown/i);
    await expect(page.locator('#completed-sessions tr[data-search-row]').first()).toContainText('Many-result fixture item 1');
    await expect(page.getByRole('button', { name: 'Show more' })).toBeVisible();
    await expect(page.locator('[data-search-more-row]')).not.toContainText(/Showing/i);
    await expectNoHorizontalOverflow(page);

    await triggerDashboardSearchAndWait(
      page,
      () => page.getByRole('button', { name: 'Show more' }).click(),
      (url) => url.searchParams.get('q') === 'fixture density' && url.searchParams.get('event_kind') === 'event' && url.searchParams.get('limit') === '60',
    );
    await waitForDashboardSearchRows(page, 35);
    await expect(page.locator('#completed-session-status')).toHaveText('All time');
    await expect(page.getByRole('button', { name: 'Show more' })).toHaveCount(0);

    await guards.expectClean();
  });

  test('keeps chart range independent from table, search, activity, URL, and refresh', async ({ page }) => {
    const guards = attachPageGuards(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await gotoDashboardSearch(page);
    await expectDashboardTokenChartReady(page);
    await expectNoHorizontalOverflow(page);
    await expect(page.locator('#dashboard-range-control')).toHaveCount(1);
    await expect(page.locator('#dashboard-search #dashboard-range-control')).toHaveCount(1);
    await expect(page.locator('#dashboard-chart-range-control')).toHaveCount(1);
    await expect(page.locator('.dashboard-analytics-panel #dashboard-chart-range-control')).toHaveCount(1);
    await expect(page.locator('[data-search-range]')).toHaveCount(0);
    await expect(page.locator('#dashboard-chart-metric')).toHaveValue('total_tokens');

    const initialTotalState = await expectDashboardChartMetricState(page, 'total_tokens', 'tokens', /^(Total )?Tokens$/);

    const selectableMetrics: Array<[string, string, string]> = [
      ['input_tokens', 'tokens', 'Input Tokens'],
      ['output_tokens', 'tokens', 'Output Tokens'],
      ['cache_read_tokens', 'tokens', 'Cache Read Tokens'],
      ['tool_calls', 'tool calls', 'Tool Calls'],
      ['errors', 'errors', 'Errors'],
      ['error_rate', '%', 'Error Rate'],
      ['input_tokens', 'tokens', 'Input Tokens'],
    ];
    let initialInputState = initialTotalState;
    for (const [metric, unit, title] of selectableMetrics) {
      const metricRequests = await collectDashboardRequestsDuring(page, async () => {
        await page.locator('#dashboard-chart-metric').selectOption(metric);
      });
      expect(metricRequests).toEqual([]);
      const state = await expectDashboardChartMetricState(page, metric, unit, title);
      if (metric === 'input_tokens') initialInputState = state;
    }
    await page.waitForFunction(() => new URL(window.location.href).searchParams.get('chart_metric') === 'input_tokens');
    expect(initialInputState.total).toBeLessThan(initialTotalState.total);

    const chart7d = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() && url.pathname === '/api/dashboard/charts' && url.searchParams.get('chart_range') === '7d';
    });
    const chartRequests = await collectDashboardRequestsDuring(page, async () => {
      await page.locator('#dashboard-chart-range-control').getByRole('button', { name: '7d' }).click();
      await chart7d;
    });
    await waitForCompletedRows(page, 30);
    expect(chartRequests.some((request) => request.startsWith('/api/dashboard/charts?') && request.includes('chart_range=7d'))).toBe(true);
    expect(chartRequests.some((request) => request.startsWith('/api/dashboard/sessions?'))).toBe(false);
    expect(chartRequests.some((request) => request.startsWith('/api/dashboard/search?'))).toBe(false);
    expect(chartRequests.some((request) => request.startsWith('/api/dashboard/activity?'))).toBe(false);
    await expect(page.locator('#dashboard-chart-range-caption')).toHaveText('Last 7 days');
    await expect(page.locator('#dashboard-chart-range-control').getByRole('button', { name: '7d' })).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#dashboard-range-caption')).toHaveText('All time');
    await expect(page.locator('#completed-session-status')).toHaveText('All time');
    await expect(page.locator('#completed-session-status')).not.toContainText(/shown|search results?/i);
    await expect(page.locator('#timeline-sidebar .activity-bar-range')).toHaveText('(all)');
    await page.waitForFunction(() => new URL(window.location.href).searchParams.get('chart_range') === '7d');
    await page.waitForFunction(() => new URL(window.location.href).searchParams.get('chart_metric') === 'input_tokens');
    expect(new URL(page.url()).searchParams.get('range')).toBeNull();
    expect(new URL(page.url()).searchParams.get('search_range')).toBeNull();
    const chart7dState = await readDashboardChartState(page);
    expect(chart7dState.metricKind).toBe('input_tokens');
    expect(chart7dState.yTitle).toBe('Input Tokens');
    expect(chart7dState.total).toBeGreaterThan(initialInputState.total);
    const chart7dTotal = chart7dState.total;

    await fillDashboardEventSearchAndWait(page, 'many');
    await waitForDashboardSearchRows(page, 30);
    const search30d = waitForDashboardSearchResponse(page, (url) =>
      url.searchParams.get('q') === 'many' &&
      url.searchParams.get('event_kind') === 'event' &&
      url.searchParams.get('completed_range') === '30d' &&
      url.searchParams.get('limit') === '30'
    );
    const activity30d = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() && url.pathname === '/api/dashboard/activity' && url.searchParams.get('activity_range') === '30d';
    });
    const tableRequests = await collectDashboardRequestsDuring(page, async () => {
      await page.locator('#dashboard-range-control').getByRole('button', { name: '30d' }).click();
      await Promise.all([search30d, activity30d]);
    });
    await waitForDashboardSearchRows(page, 30);
    expect(tableRequests.some((request) => request.startsWith('/api/dashboard/search?') && request.includes('completed_range=30d'))).toBe(true);
    expect(tableRequests.some((request) => request.startsWith('/api/dashboard/activity?') && request.includes('activity_range=30d'))).toBe(true);
    expect(tableRequests.some((request) => request.startsWith('/api/dashboard/charts?'))).toBe(false);
    await expect(page.locator('#dashboard-range-caption')).toHaveText('Last 30 days');
    await expect(page.locator('#dashboard-chart-range-caption')).toHaveText('Last 7 days');
    await expect(page.locator('#timeline-sidebar .activity-bar-range')).toHaveText('(30d)');
    await expect(page.locator('#completed-session-status')).toHaveText('Last 30 days');
    await expect(page.locator('#completed-sessions tr[data-search-row]').first()).toContainText('30d range fixture');
    await expect(page.locator('#activity-feed')).toContainText('30d range fixture');
    await page.waitForFunction(() => new URL(window.location.href).searchParams.get('range') === '30d');
    await page.waitForFunction(() => new URL(window.location.href).searchParams.get('chart_range') === '7d');
    await page.waitForFunction(() => new URL(window.location.href).searchParams.get('chart_metric') === 'input_tokens');
    await page.waitForFunction(() => new URL(window.location.href).searchParams.get('q') === 'many');
    await page.waitForFunction(() => new URL(window.location.href).searchParams.get('event_kind') === 'event');
    expect(await readDashboardChartTotal(page)).toBe(chart7dTotal);

    const pinnedActivity = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() && url.pathname === '/api/dashboard/activity' && url.searchParams.get('activity_range') === '7d';
    });
    await page.goto('/?range=30d&activity_range=7d&q=many&event_kind=event&chart_range=1h&chart_metric=error_rate', { waitUntil: 'domcontentloaded' });
    await pinnedActivity;
    await waitForDashboardSearchRows(page, 30);
    await expect(page.locator('#dashboard-range-control').getByRole('button', { name: '30d' })).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#dashboard-chart-range-control').getByRole('button', { name: '1h' })).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#dashboard-chart-metric')).toHaveValue('error_rate');
    await page.waitForFunction(() => {
      const chart = (window as Window & { dashboardTokenCumulativeChart?: { $dashboardMetricKind?: string; $dashboardMetricUnit?: string; options: { scales: { y: { title?: { text?: string } } } } } }).dashboardTokenCumulativeChart;
      return chart?.$dashboardMetricKind === 'error_rate' && chart.$dashboardMetricUnit === '%' && chart.options.scales.y.title?.text === 'Error Rate';
    });
    await expect(page.locator('#timeline-sidebar .activity-bar-range')).toHaveText('(7d)');
    await expect(page.locator('#completed-sessions tr[data-search-row]').first()).toContainText('30d range fixture');
    await expect(page.locator('#activity-feed')).toContainText('7d range fixture');
    const pinnedTable = waitForDashboardSearchResponse(page, (url) =>
      url.searchParams.get('q') === 'many' &&
      url.searchParams.get('event_kind') === 'event' &&
      url.searchParams.get('completed_range') === '7d'
    );
    const pinnedRequests = await collectDashboardRequestsDuring(page, async () => {
      await page.locator('#dashboard-range-control').getByRole('button', { name: '7d' }).click();
      await pinnedTable;
    });
    expect(pinnedRequests.some((request) => request.startsWith('/api/dashboard/search?') && request.includes('completed_range=7d'))).toBe(true);
    expect(pinnedRequests.some((request) => request.startsWith('/api/dashboard/activity?'))).toBe(false);
    expect(pinnedRequests.some((request) => request.startsWith('/api/dashboard/charts?'))).toBe(false);
    await expect(page.locator('#timeline-sidebar .activity-bar-range')).toHaveText('(7d)');
    await expect(page.locator('#activity-feed')).toContainText('7d range fixture');
    await page.waitForFunction(() => new URL(window.location.href).searchParams.get('range') === '7d');
    await page.waitForFunction(() => new URL(window.location.href).searchParams.get('activity_range') === '7d');
    await page.waitForFunction(() => new URL(window.location.href).searchParams.get('chart_metric') === 'error_rate');

    const refreshCharts = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() && url.pathname === '/api/dashboard/charts' && url.searchParams.get('chart_range') === '1h';
    });
    await page.locator('#dashboard-chart-refresh-btn').click();
    await refreshCharts;
    await expectDashboardTokenChartReady(page);
    await expect(page.locator('#dashboard-chart-metric')).toHaveValue('error_rate');

    const legacySearch = waitForDashboardSearchResponse(page, (url) => url.searchParams.get('q') === 'many' && url.searchParams.get('event_kind') === 'event' && url.searchParams.get('completed_range') === '7d');
    await page.goto('/?search_range=7d&q=many&event_kind=event', { waitUntil: 'domcontentloaded' });
    await legacySearch;
    await waitForDashboardSearchRows(page, 30);
    await expect(page.locator('#dashboard-range-control').getByRole('button', { name: '7d' })).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#dashboard-chart-range-control').getByRole('button', { name: '24h' })).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#dashboard-chart-metric')).toHaveValue('total_tokens');
    await expect(page.locator('#completed-sessions tr[data-search-row]').first()).toContainText('7d range fixture');
    await page.waitForFunction(() => new URL(window.location.href).searchParams.get('range') === '7d');
    await page.waitForFunction(() => new URL(window.location.href).searchParams.get('chart_range') === null);
    await page.waitForFunction(() => new URL(window.location.href).searchParams.get('chart_metric') === null);
    await page.waitForFunction(() => new URL(window.location.href).searchParams.get('search_range') === null);

    await guards.expectClean();
  });

  test('keeps desktop scroll fixed while typing search with delayed responses', async ({ page }) => {
    const guards = attachPageGuards(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await installDashboardFixtures(page, { searchDelayMs: 350 });
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);
    await expectNoHorizontalOverflow(page);
    await selectDashboardSearchType(page, 'event');

    const baseline = await scrollDashboardMainToSearch(page);
    const input = page.locator('#dashboard-session-search');
    await input.focus();
    await expect(input).toBeFocused();

    const finalResponse = waitForDashboardSearchResponse(page, (url) => url.searchParams.get('q') === 'dashboard payload' && url.searchParams.get('event_kind') === 'event');
    for (const char of 'dashboard payload') {
      await input.pressSequentially(char);
      await page.waitForTimeout(280);
      await expectDashboardScrollNear(page, baseline);
    }

    await finalResponse;
    await waitForDashboardSearchRows(page, 1);
    await expect(page.locator('#completed-sessions tr[data-search-row]').first()).toContainText('Dashboard payload search');
    await expectDashboardScrollNear(page, baseline);

    await guards.expectClean();
  });

  test('clears text-only search with Escape without moving desktop scroll', async ({ page }) => {
    const guards = attachPageGuards(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await installDashboardFixtures(page);
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);
    await scrollDashboardMainToSearch(page);

    await fillDashboardSearchAndWait(
      page,
      'claude-sonnet-4-super-long-model-name',
      (url) => url.searchParams.get('q') === 'claude-sonnet-4-super-long-model-name' && url.searchParams.get('event_kind') === 'session',
    );
    await waitForDashboardSearchRows(page, 1);
    await expect(page.locator('#dashboard-session-search')).toBeFocused();
    const beforeEscape = await readDashboardScroll(page);
    const completedResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() && url.pathname === '/api/dashboard/sessions' && url.searchParams.get('state') === 'completed';
    });
    await page.keyboard.press('Escape');
    await completedResponse;
    await waitForCompletedRows(page, 30);
    await expect(page.locator('#dashboard-session-search')).toHaveValue('');
    await expect(page.locator('#dashboard-session-search')).toBeFocused();
    await expect(page.locator('#dashboard-search-kind')).toHaveValue('session');
    await expect(page.locator('#completed-table')).toHaveAttribute('data-table-mode', 'sessions');
    await expectDashboardScrollNear(page, beforeEscape);

    await guards.expectClean();
  });

  test('clears search through the input clear path without moving desktop scroll', async ({ page }) => {
    const guards = attachPageGuards(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await installDashboardFixtures(page);
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);
    await scrollDashboardMainToSearch(page);

    await fillDashboardSearchAndWait(
      page,
      'claude-sonnet-4-super-long-model-name',
      (url) => url.searchParams.get('q') === 'claude-sonnet-4-super-long-model-name' && url.searchParams.get('event_kind') === 'session',
    );
    await waitForDashboardSearchRows(page, 1);
    await expect(page.locator('#dashboard-search-clear')).toHaveCount(0);
    await expect(page.locator('#dashboard-session-search')).toHaveValue('claude-sonnet-4-super-long-model-name');

    const beforeClear = await readDashboardScroll(page);
    await expectDashboardScrollStableDuring(page, async () => {
      const completedResponse = page.waitForResponse((response) => {
        const url = new URL(response.url());
        return response.ok() && url.pathname === '/api/dashboard/sessions' && url.searchParams.get('state') === 'completed';
      });
      await page.locator('#dashboard-session-search').clear();
      await completedResponse;
      await waitForCompletedRows(page, 30);
    });
    await expect(page.locator('#dashboard-session-search')).toHaveValue('');
    await expectDashboardScrollNear(page, beforeClear);

    await guards.expectClean();
  });

  test('ignores stale delayed search responses without moving desktop scroll when requests cannot abort', async ({ page }) => {
    const guards = attachPageGuards(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.addInitScript(() => {
      Object.defineProperty(window, 'AbortController', { value: undefined, configurable: true });
    });
    await installDashboardFixtures(page, {
      searchDelayByQuery: {
        dash: 700,
        many: 50,
      },
    });
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);
    await selectDashboardSearchType(page, 'event');
    await fillDashboardEventSearchAndWait(page, 'many');
    await waitForDashboardSearchRows(page, 30);

    const baseline = await scrollDashboardMainToSearch(page);
    const input = page.locator('#dashboard-session-search');
    await input.focus();

    const staleRequest = waitForDashboardSearchRequest(page, (url) => url.searchParams.get('q') === 'dash' && url.searchParams.get('event_kind') === 'event');
    await expectDashboardScrollStableDuring(page, async () => {
      await input.fill('dash');
      await input.press('Enter');
      await staleRequest;
      await expect(page.locator('#completed-sessions')).toContainText('Searching table sessions and events');
    });

    const latestResponse = waitForDashboardSearchResponse(page, (url) => url.searchParams.get('q') === 'many' && url.searchParams.get('event_kind') === 'event');
    await expectDashboardScrollStableDuring(page, async () => {
      await input.fill('many');
      await input.press('Enter');
      await latestResponse;
      await waitForDashboardSearchRows(page, 30);
    });
    await expect(page.locator('#completed-sessions tr[data-search-row]').first()).toContainText('Many-result fixture item 1');
    await expectDashboardScrollNear(page, baseline);

    const afterLatest = await readDashboardScroll(page);
    await page.waitForTimeout(800);
    await expect(page.locator('#completed-sessions tr[data-search-row]')).toHaveCount(30);
    await expect(page.locator('#completed-sessions tr[data-search-row]').first()).toContainText('Many-result fixture item 1');
    await expectDashboardScrollNear(page, afterLatest);

    await guards.expectClean();
  });

  test('keeps bottom search results visible during repeated live refreshes', async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await installDashboardFixtures(page, { mockEventSource: true, searchDelayMs: 500 });
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);

    await fillDashboardEventSearchAndWait(page, 'many');
    await waitForDashboardSearchRows(page, 30);
    const moreResponse = waitForDashboardSearchResponse(page, (url) => url.searchParams.get('q') === 'many' && url.searchParams.get('event_kind') === 'event' && url.searchParams.get('limit') === '60');
    await page.getByRole('button', { name: 'Show more' }).click();
    await moreResponse;
    await waitForDashboardSearchRows(page, 35);
    await expect(page.locator('#completed-session-status')).toHaveText('All time');
    await page.evaluate(() => {
      const status = document.getElementById('completed-session-status');
      const rows = document.getElementById('completed-sessions');
      const samples: string[] = [];
      const rowCounts: number[] = [];
      const recordStatus = () => samples.push(status?.textContent || '');
      const recordRows = () => rowCounts.push(document.querySelectorAll('#completed-sessions tr[data-search-row]').length);
      const statusObserver = new MutationObserver(recordStatus);
      const rowObserver = new MutationObserver(recordRows);
      if (status) statusObserver.observe(status, { childList: true, characterData: true, subtree: true });
      if (rows) rowObserver.observe(rows, { childList: true, subtree: true });
      recordStatus();
      recordRows();
      (window as Window & {
        __beaconSearchStatusSamples?: string[];
        __beaconSearchRowCounts?: number[];
        __beaconSearchStatusObserver?: MutationObserver;
        __beaconSearchRowObserver?: MutationObserver;
      }).__beaconSearchStatusSamples = samples;
      (window as Window & { __beaconSearchRowCounts?: number[] }).__beaconSearchRowCounts = rowCounts;
      (window as Window & { __beaconSearchStatusObserver?: MutationObserver }).__beaconSearchStatusObserver = statusObserver;
      (window as Window & { __beaconSearchRowObserver?: MutationObserver }).__beaconSearchRowObserver = rowObserver;
    });

    const bottom = await scrollDashboardMainToBottom(page);
    for (let i = 0; i < 3; i += 1) {
      const refreshResponse = waitForDashboardSearchResponse(page, (url) => url.searchParams.get('q') === 'many' && url.searchParams.get('event_kind') === 'event' && url.searchParams.get('limit') === '60');
      await emitDashboardEvent(page, 'completed-sessions-update');
      await expect(page.locator('#completed-session-status')).toHaveText('All time');
      await expect(page.locator('#completed-sessions tr[data-search-row]')).toHaveCount(35);
      await refreshResponse;
      await waitForDashboardSearchRows(page, 35);
      await expectDashboardScrollNear(page, bottom, 4);
    }
    let failNextSearch = true;
    await page.route('**/api/dashboard/search**', async (route) => {
      if (!failNextSearch) return route.fallback();
      failNextSearch = false;
      return route.fulfill({
        status: 500,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'fixture failure' }),
      });
    });
    const failedRefresh = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.status() === 500 && url.pathname === '/api/dashboard/search' && url.searchParams.get('q') === 'many' && url.searchParams.get('event_kind') === 'event';
    });
    await emitDashboardEvent(page, 'completed-sessions-update');
    await expect(page.locator('#completed-session-status')).toHaveText('All time');
    await expect(page.locator('#completed-sessions tr[data-search-row]')).toHaveCount(35);
    await failedRefresh;
    await expect(page.locator('#completed-session-status')).toHaveText('All time');
    await expect(page.locator('#completed-sessions tr[data-search-row]')).toHaveCount(35);
    await expectDashboardScrollNear(page, bottom, 4);

    const statusSamples = await page.evaluate(() => {
      const store = window as Window & { __beaconSearchStatusSamples?: string[]; __beaconSearchStatusObserver?: MutationObserver };
      store.__beaconSearchStatusObserver?.disconnect();
      return store.__beaconSearchStatusSamples || [];
    });
    const rowCounts = await page.evaluate(() => {
      const store = window as Window & { __beaconSearchRowCounts?: number[]; __beaconSearchRowObserver?: MutationObserver };
      store.__beaconSearchRowObserver?.disconnect();
      return store.__beaconSearchRowCounts || [];
    });
    expect(statusSamples.some((sample) => /searching/i.test(sample))).toBe(false);
    expect(statusSamples.some((sample) => /failed/i.test(sample))).toBe(false);
    expect(statusSamples.every((sample) => sample === 'All time')).toBe(true);
    expect(Math.min(...rowCounts)).toBe(35);
  });

  test('keeps desktop scroll fixed across search controls and live dashboard updates', async ({ page }) => {
    const guards = attachPageGuards(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await installDashboardFixtures(page, { mockEventSource: true });
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);
    await scrollDashboardMainToSearch(page);
    await selectDashboardSearchType(page, 'event');

    await expectDashboardScrollStableDuring(page, async () => {
      await triggerDashboardSearchAndWait(
        page,
        () => page.locator('#dashboard-session-search').fill('search'),
        (url) => url.searchParams.get('q') === 'search' && url.searchParams.get('event_kind') === 'event',
      );
      await waitForDashboardSearchRows(page, 3);
    });

    await expectDashboardScrollStableDuring(page, async () => {
      await triggerDashboardSearchAndWait(
        page,
        () => page.locator('#dashboard-search-kind').selectOption('tool_call'),
        (url) => url.searchParams.get('q') === 'search' && url.searchParams.get('event_kind') === 'tool_call',
      );
      await waitForDashboardSearchRows(page, 1);
    });

    await expectDashboardScrollStableDuring(page, async () => {
      await triggerDashboardSearchAndWait(
        page,
        () => page.locator('#dashboard-search-session').fill(SEARCH_SESSION_ID),
        (url) => url.searchParams.get('session_id') === SEARCH_SESSION_ID,
      );
      await waitForDashboardSearchRows(page, 1);
    });

    await expectDashboardScrollStableDuring(page, async () => {
      await triggerDashboardSearchAndWait(
        page,
        () => page.locator('#dashboard-search-sort').selectOption('newest'),
        (url) => url.searchParams.get('sort') === 'newest',
      );
      await waitForDashboardSearchRows(page, 1);
    });

    await expectDashboardScrollStableDuring(page, async () => {
      const completedResponse = page.waitForResponse((response) => {
        const url = new URL(response.url());
        return response.ok() && url.pathname === '/api/dashboard/sessions' && url.searchParams.get('state') === 'completed';
      });
      await page.locator('#dashboard-search-reset').click();
      await completedResponse;
      await waitForCompletedRows(page, 30);
    });

    await expectDashboardScrollStableDuring(page, async () => {
      const activityResponse = page.waitForResponse((response) => {
        const url = new URL(response.url());
        return response.ok() && url.pathname === '/api/dashboard/activity' && url.searchParams.get('activity_range') === '7d';
      });
      const sessionsResponse = page.waitForResponse((response) => {
        const url = new URL(response.url());
        return response.ok() && url.pathname === '/api/dashboard/sessions' && url.searchParams.get('completed_range') === '7d';
      });
      await page.locator('#dashboard-range-control').getByRole('button', { name: '7d' }).evaluate((button) => {
        (button as HTMLButtonElement).click();
      });
      await Promise.all([activityResponse, sessionsResponse]);
      await waitForCompletedRows(page, 30);
    });

    await expectDashboardScrollStableDuring(page, async () => {
      const chartsResponse = page.waitForResponse((response) => {
        const url = new URL(response.url());
        return response.ok() && url.pathname === '/api/dashboard/charts' && url.searchParams.get('chart_range') === '7d';
      });
      await page.locator('#dashboard-chart-range-control').getByRole('button', { name: '7d' }).evaluate((button) => {
        (button as HTMLButtonElement).click();
      });
      await chartsResponse;
    });

    await selectDashboardSearchType(page, 'event');
    await expectDashboardScrollStableDuring(page, async () => {
      await triggerDashboardSearchAndWait(
        page,
        () => page.locator('#dashboard-session-search').fill('many'),
        (url) => url.searchParams.get('q') === 'many' && url.searchParams.get('event_kind') === 'event',
      );
      await waitForDashboardSearchRows(page, 30);
    });

    await expectDashboardScrollStableDuring(page, async () => {
      await triggerDashboardSearchAndWait(
        page,
        () => page.getByRole('button', { name: 'Show more' }).evaluate((button) => {
          (button as HTMLButtonElement).click();
        }),
        (url) => url.searchParams.get('q') === 'many' && url.searchParams.get('event_kind') === 'event' && url.searchParams.get('limit') === '60',
      );
      await waitForDashboardSearchRows(page, 35);
    });
    const expandedRegion = await readCompletedRegionMetrics(page);

    await expectDashboardScrollStableDuring(page, async () => {
      const activeResponse = page.waitForResponse((response) => {
        const url = new URL(response.url());
        return response.ok() && url.pathname === '/api/dashboard/sessions' && url.searchParams.get('state') === 'active';
      });
      await emitDashboardEvent(page, 'active-sessions-update');
      await activeResponse;
      await expect(page.locator('#active-sessions')).toContainText('Realtime dashboard smoke run');
    });

    await expectDashboardScrollStableDuring(page, async () => {
      const searchResponse = waitForDashboardSearchResponse(page, (url) => url.searchParams.get('q') === 'many' && url.searchParams.get('event_kind') === 'event');
      await emitDashboardEvent(page, 'completed-sessions-update');
      await searchResponse;
      await waitForDashboardSearchRows(page, 35);
    });

    await expectDashboardScrollStableDuring(page, async () => {
      const activityResponse = page.waitForResponse((response) => {
        const url = new URL(response.url());
        return response.ok() && url.pathname === '/api/dashboard/activity';
      });
      await emitDashboardEvent(page, 'activity-update');
      await activityResponse;
    });

    await expectDashboardScrollStableDuring(page, async () => {
      const chartsResponse = page.waitForResponse((response) => {
        const url = new URL(response.url());
        return response.ok() && url.pathname === '/api/dashboard/charts';
      });
      await emitDashboardEvent(page, 'dashboard-charts-update');
      await chartsResponse;
    });

    await expectDashboardScrollStableDuring(page, async () => {
      await page.evaluate(() => {
        (document.activeElement as HTMLElement | null)?.blur();
      });
      await page.keyboard.press('t');
      await expect(page.locator('#timeline-sidebar')).toHaveClass(/collapsed/);
      await page.keyboard.press('t');
      await expect(page.locator('#timeline-sidebar')).not.toHaveClass(/collapsed/);
    });

    await expectDashboardScrollStableDuring(page, async () => {
      const completedResponse = page.waitForResponse((response) => {
        const url = new URL(response.url());
        return response.ok() && url.pathname === '/api/dashboard/sessions' && url.searchParams.get('offset') === '0';
      });
      await page.locator('#dashboard-search-reset').click();
      await completedResponse;
      await waitForCompletedRows(page, 30);
    });

    await expectDashboardScrollStableDuring(page, async () => {
      const sortResponse = page.waitForResponse((response) => {
        const url = new URL(response.url());
        return response.ok() && url.pathname === '/api/dashboard/sessions' && url.searchParams.get('sort') === 'tokens';
      });
      const tokensSortButton = page.locator('#completed-table th[data-sort-key="tokens"]').getByRole('button', { name: 'Tokens' });
      await tokensSortButton.click();
      await sortResponse;
      await waitForCompletedRows(page, 30);
    });

    await expectDashboardScrollStableDuring(page, async () => {
      const nextResponse = page.waitForResponse((response) => {
        const url = new URL(response.url());
        return response.ok() && url.pathname === '/api/dashboard/sessions' && url.searchParams.get('offset') === '30';
      });
      await page.locator('.json-page-btn', { hasText: 'Next' }).evaluate((button) => {
        (button as HTMLButtonElement).click();
      });
      await nextResponse;
      await waitForCompletedRows(page, 1);
    });
    const oneRowRegion = await readCompletedRegionMetrics(page);
    expect(oneRowRegion.height).toBeLessThan(expandedRegion.height);

    await guards.expectClean();
  });

  test('keeps the table anchored when active sessions expand or error above it', async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await installDashboardFixtures(page, {
      mockEventSource: true,
      activeScenarioSequence: ['default', 'many-active', 'many-active', 'error'],
    });
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);
    await scrollDashboardMainToSearch(page);

    const before = await readSearchOffsetInDashboard(page);
    expect(before.offset).toBeGreaterThanOrEqual(0);
    expect(before.windowY).toBe(0);
    expect(before.mainContentTop).toBe(0);

    const activeGrowthResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() && url.pathname === '/api/dashboard/sessions' && url.searchParams.get('state') === 'active';
    });
    await emitDashboardEvent(page, 'active-sessions-update');
    await activeGrowthResponse;
    await expect(page.locator('#active-sessions')).toContainText('Live queue item 8');
    await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));

    const afterGrowth = await readSearchOffsetInDashboard(page);
    expect(Math.abs(afterGrowth.offset - before.offset)).toBeLessThanOrEqual(2);
    expect(Math.abs(afterGrowth.dashboardTop - before.dashboardTop)).toBeLessThanOrEqual(2);
    expect(afterGrowth.windowY).toBe(0);
    expect(afterGrowth.mainContentTop).toBe(0);

    const beforeSort = await readSearchOffsetInDashboard(page);
    const activeSortResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() && url.pathname === '/api/dashboard/sessions' && url.searchParams.get('state') === 'active';
    });
    await page.locator('#active-session-sort').selectOption('tokens');
    await activeSortResponse;
    await expect(page.locator('#active-sessions .active-session-card').first()).toHaveAttribute('data-active-session-id', 'active-parent-008');
    await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));

    const afterSort = await readSearchOffsetInDashboard(page);
    expect(Math.abs(afterSort.offset - beforeSort.offset)).toBeLessThanOrEqual(2);
    expect(Math.abs(afterSort.dashboardTop - beforeSort.dashboardTop)).toBeLessThanOrEqual(2);
    expect(afterSort.windowY).toBe(0);
    expect(afterSort.mainContentTop).toBe(0);

    const beforeError = await readSearchOffsetInDashboard(page);
    const activeErrorResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.status() === 500 && url.pathname === '/api/dashboard/sessions' && url.searchParams.get('state') === 'active';
    });
    await emitDashboardEvent(page, 'active-sessions-update');
    await activeErrorResponse;
    await expect(page.locator('#active-sessions')).toContainText('Unable to load active sessions');
    await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));

    const afterError = await readSearchOffsetInDashboard(page);
    expect(Math.abs(afterError.offset - beforeError.offset)).toBeLessThanOrEqual(2);
    expect(Math.abs(afterError.dashboardTop - beforeError.dashboardTop)).toBeLessThanOrEqual(2);
    expect(afterError.windowY).toBe(0);
    expect(afterError.mainContentTop).toBe(0);
  });

  test('shows loading and error retry states in the table area', async ({ page }) => {
    await installDashboardFixtures(page, { failOnce: ['search'], searchDelayMs: 500 });
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);
    const query = 'claude-sonnet-4-super-long-model-name';

    const failedResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return url.pathname === '/api/dashboard/search' && url.searchParams.get('q') === query && url.searchParams.get('event_kind') === 'session' && response.status() === 500;
    });
    await page.locator('#dashboard-session-search').fill(query);
    await page.locator('#dashboard-session-search').press('Enter');
    await expect(page.locator('#completed-sessions')).toContainText('Searching table sessions and events');
    await failedResponse;
    await expect(page.locator('#completed-sessions')).toContainText('Unable to search table sessions and events');
    await expect(page.locator('#completed-session-status')).toContainText('Search failed');

    const retryResponse = waitForDashboardSearchResponse(page, (url) => url.searchParams.get('q') === query && url.searchParams.get('event_kind') === 'session');
    await page.getByRole('button', { name: 'Retry' }).click();
    await retryResponse;
    await waitForDashboardSearchRows(page, 1);
  });

  test('shows unavailable state in the table area', async ({ page }) => {
    await installDashboardFixtures(page, { searchUnavailable: true });
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);
    await fillDashboardEventSearchAndWait(page, 'dashboard payload');
    await expect(page.locator('#completed-sessions')).toContainText('Search is not connected');
    await expect(page.locator('#completed-session-status')).toContainText('Search unavailable');
  });

  test('keeps the dashboard search table contained on narrow screens', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page);

    for (const viewport of [
      { width: 390, height: 844 },
      { width: 320, height: 568 },
    ]) {
      await page.setViewportSize(viewport);
      await gotoDashboard(page);
      await waitForCompletedRows(page, 30);
      await fillDashboardEventSearchAndWait(page, 'many');
      await waitForDashboardSearchRows(page, 30);
      await expectNoHorizontalOverflow(page);
      await expectSearchVerticalFlow(page);

      const metrics = await page.evaluate(() => {
        const tableScroller = document.getElementById('completed-table')?.parentElement?.getBoundingClientRect();
        const rows = Array.from(document.querySelectorAll('#completed-sessions tr[data-search-row]')).map((row) => row.getBoundingClientRect());
        return {
          tableLeft: tableScroller ? Math.round(tableScroller.left) : 0,
          tableRight: tableScroller ? Math.round(tableScroller.right) : 0,
          rowOverflow: rows.some((row) => row.top < 0 || row.bottom < 0),
        };
      });
      expect(metrics.tableLeft).toBeGreaterThanOrEqual(0);
      expect(metrics.tableRight).toBeLessThanOrEqual(viewport.width);
      expect(metrics.rowOverflow).toBe(false);
    }

    await guards.expectClean();
  });
});
