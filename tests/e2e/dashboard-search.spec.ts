import { expect, test, type Page } from '@playwright/test';
import {
  SEARCH_SESSION_ID,
  attachPageGuards,
  emitDashboardEvent,
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

test.describe('dashboard search workflows', () => {
  test('redirects legacy search URL back to the dashboard search table', async ({ page }) => {
    await installDashboardFixtures(page);
    await page.goto('/search', { waitUntil: 'domcontentloaded' });
    await expect(page).toHaveURL(/\/#dashboard-search$/);
    await expect(page.locator('#dashboard-session-search')).toBeVisible();
  });

  test('exercises query, filters, clearing, reset, and pagination', async ({ page }) => {
    const guards = attachPageGuards(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await gotoDashboardSearch(page);
    await expectNoHorizontalOverflow(page);

    await page.keyboard.press('/');
    await expect(page.locator('#dashboard-session-search')).toBeFocused();

    await fillDashboardSearchAndWait(page, 'dashboard payload');
    await waitForDashboardSearchRows(page, 1);
    await expect(page.locator('#completed-sessions tr[data-search-row]').first()).toContainText('Dashboard payload search');
    await expect(page.locator('#dashboard-search-clear')).toHaveCount(0);

    const escapeClearResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() && url.pathname === '/api/dashboard/sessions' && url.searchParams.get('state') === 'completed';
    });
    await page.keyboard.press('Escape');
    await escapeClearResponse;
    await expect(page.locator('#dashboard-session-search')).toHaveValue('');
    await expect(page.locator('#dashboard-session-search')).toBeFocused();
    await waitForCompletedRows(page, 30);

    await fillDashboardSearchAndWait(page, 'search');
    await waitForDashboardSearchRows(page, 3);

    await triggerDashboardSearchAndWait(
      page,
      () => page.locator('[data-search-event-kind="tool_call"]').click(),
      (url) => url.searchParams.get('q') === 'search' && url.searchParams.get('event_kind') === 'tool_call',
    );
    await waitForDashboardSearchRows(page, 1);
    await expect(page.locator('#completed-sessions tr[data-search-row]').first()).toHaveAttribute('data-event-kind', 'tool_call');

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
      () => page.locator('[data-search-range="7d"]').click(),
      (url) => url.searchParams.get('range') === '7d',
    );
    await expect(page.locator('[data-search-range="7d"]')).toHaveAttribute('aria-pressed', 'true');

    expect(filteredRequest.searchParams.get('q')).toBe('search');
    expect(filteredRequest.searchParams.get('event_kind')).toBe('tool_call');
    expect(filteredRequest.searchParams.get('session_id')).toBe(SEARCH_SESSION_ID);
    expect(filteredRequest.searchParams.get('sort')).toBe('newest');
    expect(filteredRequest.searchParams.get('range')).toBe('7d');

    await page.locator('#dashboard-search-reset').click();
    await expect(page.locator('#dashboard-session-search')).toHaveValue('');
    await expect(page.locator('#dashboard-search-session')).toHaveValue('');
    await expect(page.locator('#dashboard-search-sort')).toHaveValue('relevance');
    await expect(page.locator('[data-search-event-kind=""]')).toHaveAttribute('aria-pressed', 'true');
    await waitForCompletedRows(page, 30);

    await fillDashboardSearchAndWait(page, 'many');
    await waitForDashboardSearchRows(page, 30);
    await expect(page.getByRole('button', { name: 'Show more' })).toBeVisible();
    await expectSearchVerticalFlow(page);

    await triggerDashboardSearchAndWait(
      page,
      () => page.getByRole('button', { name: 'Show more' }).click(),
      (url) => url.searchParams.get('q') === 'many' && url.searchParams.get('limit') === '60',
    );
    await waitForDashboardSearchRows(page, 35);
    await expect(page.getByRole('button', { name: 'Show more' })).toHaveCount(0);

    const metadataRequest = await triggerDashboardSearchAndWait(
      page,
      () => page.locator('#dashboard-session-search').fill('claude-sonnet-4-super-long-model-name'),
      (url) => url.searchParams.get('q') === 'claude-sonnet-4-super-long-model-name',
    );
    expect(metadataRequest.searchParams.get('limit')).toBe('30');
    await waitForDashboardSearchRows(page, 1);
    await expect(page.locator('#completed-sessions tr[data-search-row]').first()).toHaveAttribute('data-event-kind', 'session');
    await expect(page.locator('#completed-sessions tr[data-search-row]').first()).toContainText('Session metadata');

    await guards.expectClean();
  });

  test('keeps desktop scroll fixed while typing search with delayed responses', async ({ page }) => {
    const guards = attachPageGuards(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await installDashboardFixtures(page, { searchDelayMs: 350 });
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);
    await expectNoHorizontalOverflow(page);

    const baseline = await scrollDashboardMainToSearch(page);
    const input = page.locator('#dashboard-session-search');
    await input.focus();
    await expect(input).toBeFocused();

    const finalResponse = waitForDashboardSearchResponse(page, (url) => url.searchParams.get('q') === 'dashboard payload');
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

    await fillDashboardSearchAndWait(page, 'dashboard payload');
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
    await expectDashboardScrollNear(page, beforeEscape);

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
        'dashboard payload': 50,
      },
    });
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);

    const baseline = await scrollDashboardMainToSearch(page);
    const input = page.locator('#dashboard-session-search');
    await input.focus();

    const staleRequest = waitForDashboardSearchRequest(page, (url) => url.searchParams.get('q') === 'dash');
    await expectDashboardScrollStableDuring(page, async () => {
      await input.fill('dash');
      await input.press('Enter');
      await staleRequest;
      await expect(page.locator('#completed-sessions')).toContainText('Searching sessions and events');
    });

    const latestResponse = waitForDashboardSearchResponse(page, (url) => url.searchParams.get('q') === 'dashboard payload');
    await expectDashboardScrollStableDuring(page, async () => {
      await input.fill('dashboard payload');
      await input.press('Enter');
      await latestResponse;
      await waitForDashboardSearchRows(page, 1);
    });
    await expect(page.locator('#completed-sessions tr[data-search-row]').first()).toContainText('Dashboard payload search');
    await expectDashboardScrollNear(page, baseline);

    const afterLatest = await readDashboardScroll(page);
    await page.waitForTimeout(800);
    await expect(page.locator('#completed-sessions tr[data-search-row]')).toHaveCount(1);
    await expect(page.locator('#completed-sessions tr[data-search-row]').first()).toContainText('Dashboard payload search');
    await expectDashboardScrollNear(page, afterLatest);

    await guards.expectClean();
  });

  test('keeps desktop scroll fixed across search controls and live dashboard updates', async ({ page }) => {
    const guards = attachPageGuards(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await installDashboardFixtures(page, { mockEventSource: true });
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);
    await scrollDashboardMainToSearch(page);

    await expectDashboardScrollStableDuring(page, async () => {
      await triggerDashboardSearchAndWait(
        page,
        () => page.locator('#dashboard-session-search').fill('search'),
        (url) => url.searchParams.get('q') === 'search',
      );
      await waitForDashboardSearchRows(page, 3);
    });

    await expectDashboardScrollStableDuring(page, async () => {
      await triggerDashboardSearchAndWait(
        page,
        () => page.locator('[data-search-event-kind="tool_call"]').click(),
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
      const chartsResponse = page.waitForResponse((response) => {
        const url = new URL(response.url());
        return response.ok() && url.pathname === '/api/dashboard/charts' && url.searchParams.get('range') === '7d';
      });
      const activityResponse = page.waitForResponse((response) => {
        const url = new URL(response.url());
        return response.ok() && url.pathname === '/api/dashboard/activity' && url.searchParams.get('range') === '7d';
      });
      const sessionsResponse = page.waitForResponse((response) => {
        const url = new URL(response.url());
        return response.ok() && url.pathname === '/api/dashboard/sessions' && url.searchParams.get('range') === '7d';
      });
      await page.locator('#dashboard-range-control').getByRole('button', { name: '7d' }).evaluate((button) => {
        (button as HTMLButtonElement).click();
      });
      await Promise.all([chartsResponse, activityResponse, sessionsResponse]);
      await waitForCompletedRows(page, 30);
    });

    await expectDashboardScrollStableDuring(page, async () => {
      await triggerDashboardSearchAndWait(
        page,
        () => page.locator('#dashboard-session-search').fill('many'),
        (url) => url.searchParams.get('q') === 'many',
      );
      await waitForDashboardSearchRows(page, 30);
    });

    await expectDashboardScrollStableDuring(page, async () => {
      await triggerDashboardSearchAndWait(
        page,
        () => page.getByRole('button', { name: 'Show more' }).evaluate((button) => {
          (button as HTMLButtonElement).click();
        }),
        (url) => url.searchParams.get('q') === 'many' && url.searchParams.get('limit') === '60',
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
      const searchResponse = waitForDashboardSearchResponse(page, (url) => url.searchParams.get('q') === 'many');
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

  test('shows loading and error retry states in the table area', async ({ page }) => {
    await installDashboardFixtures(page, { failOnce: ['search'], searchDelayMs: 500 });
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);

    const failedResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return url.pathname === '/api/dashboard/search' && url.searchParams.get('q') === 'dashboard payload' && response.status() === 500;
    });
    await page.locator('#dashboard-session-search').fill('dashboard payload');
    await page.locator('#dashboard-session-search').press('Enter');
    await expect(page.locator('#completed-sessions')).toContainText('Searching sessions and events');
    await failedResponse;
    await expect(page.locator('#completed-sessions')).toContainText('Unable to search sessions and events');
    await expect(page.locator('#completed-session-status')).toContainText('Search failed');

    const retryResponse = waitForDashboardSearchResponse(page, (url) => url.searchParams.get('q') === 'dashboard payload');
    await page.getByRole('button', { name: 'Retry' }).click();
    await retryResponse;
    await waitForDashboardSearchRows(page, 1);
  });

  test('shows unavailable state in the table area', async ({ page }) => {
    await installDashboardFixtures(page, { searchUnavailable: true });
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);
    await fillDashboardSearchAndWait(page, 'dashboard payload');
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
      await fillDashboardSearchAndWait(page, 'many');
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
