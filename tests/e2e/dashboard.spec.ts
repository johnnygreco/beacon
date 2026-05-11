import { expect, test } from '@playwright/test';
import {
  ACTIVE_SESSION_ID,
  TEST_EVENT_ID,
  TEST_SESSION_ID,
  attachPageGuards,
  expectEqualDashboardChartHeights,
  expectLogAndModelControlsAligned,
  expectNoHorizontalOverflow,
  gotoDashboard,
  installDashboardFixtures,
  waitForCompletedRows,
} from './fixtures/dashboard';

test.describe('dashboard battle-tested workflows', () => {
  test('loads cleanly and keeps chart geometry across supported viewports', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page);

    for (const viewport of [
      { width: 1440, height: 900 },
      { width: 1280, height: 800 },
      { width: 1024, height: 768 },
      { width: 900, height: 700 },
      { width: 390, height: 844 },
      { width: 320, height: 568 },
    ]) {
      await page.setViewportSize(viewport);
      await gotoDashboard(page);
      await expectNoHorizontalOverflow(page);
      await expectEqualDashboardChartHeights(page);
      await expect(page.locator('nav a[href="/search"]')).toHaveCount(0);
      await expect(page.locator('#dashboard-session-search')).toBeVisible();
    }

    await guards.expectClean();
  });

  test('sorts completed sessions with keyboard-operable headers', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page);
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);

    const endedHeader = page.locator('#completed-table th[data-sort-key="ended"]');
    const tokensHeader = page.locator('#completed-table th[data-sort-key="tokens"]');
    const tokensSortButton = tokensHeader.getByRole('button', { name: 'Tokens' });

    await expect(endedHeader).toHaveAttribute('aria-sort', 'descending');
    const firstIDBeforeSort = await page.locator('#completed-sessions tr[data-session-link]').first().getAttribute('data-sort-id');
    await tokensSortButton.focus();
    await expect(tokensSortButton).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(tokensHeader.locator('.sort-arrow')).toHaveClass(/active/);
    await expect(tokensHeader).toHaveAttribute('aria-sort', 'descending');
    await expect(endedHeader).toHaveAttribute('aria-sort', 'none');
    const firstIDAfterEnterSort = await page.locator('#completed-sessions tr[data-session-link]').first().getAttribute('data-sort-id');
    expect(firstIDAfterEnterSort).not.toBe(firstIDBeforeSort);

    await page.keyboard.press('Space');
    await expect(tokensHeader).toHaveAttribute('aria-sort', 'ascending');
    const firstIDAfterSpaceSort = await page.locator('#completed-sessions tr[data-session-link]').first().getAttribute('data-sort-id');
    expect(firstIDAfterSpaceSort).not.toBe(firstIDAfterEnterSort);

    await guards.expectClean();
  });

  test('exercises dashboard controls, search, sorting, pagination, subagents, and timeline sizing', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);

    await expectLogAndModelControlsAligned(page);

    await page.getByRole('button', { name: '7d' }).click();
    await expect(page.locator('#dashboard-range-caption')).toHaveText('Last 7 days');
    await expect(page.getByRole('button', { name: '7d' })).toHaveAttribute('aria-pressed', 'true');

    const allChartsRequest = page.waitForRequest((request) => {
      const url = new URL(request.url());
      return url.pathname === '/api/dashboard/charts' && url.searchParams.has('range') && url.searchParams.get('range') === '';
    });
    const allActivityRequest = page.waitForRequest((request) => {
      const url = new URL(request.url());
      return url.pathname === '/api/dashboard/activity' && url.searchParams.has('range') && url.searchParams.get('range') === '';
    });
    await page.locator('#dashboard-range-control').getByRole('button', { name: 'All' }).click();
    await allChartsRequest;
    await allActivityRequest;
    await expect(page.locator('#dashboard-range-caption')).toHaveText('All time');
    await expect(page.locator('#dashboard-range-control').getByRole('button', { name: 'All' })).toHaveAttribute('aria-pressed', 'true');

    await page.locator('#dashboardTokenCumulativeChart-model-dropdown .model-dropdown-trigger').click();
    await expect(page.locator('#dashboardTokenCumulativeChart-model-dropdown .model-dropdown-panel')).toBeVisible();
    await page.locator('#dashboardTokenCumulativeChart-model-dropdown input[data-model]').first().uncheck();
    await expect(page.locator('#dashboardTokenCumulativeChart-model-dropdown .model-dropdown-label')).toHaveText('Models (2/3)');
    await page.mouse.click(20, 20);
    await expect(page.locator('#dashboardTokenCumulativeChart-model-dropdown .model-dropdown-panel')).toHaveClass(/hidden/);

    await page.locator('#dashboard-model-metric-control').getByRole('button', { name: 'Tools' }).click();
    await expect(page.locator('#dashboard-model-metric-control').getByRole('button', { name: 'Tools' })).toHaveAttribute('aria-pressed', 'true');

    await page.locator('#dashboardTokenCumulativeChart-log-toggle').click();
    await expect(page.locator('#dashboardTokenCumulativeChart-log-toggle')).toHaveAttribute('aria-pressed', 'true');
    await expectLogAndModelControlsAligned(page);

    await page.locator('#timeline-toggle-btn').click();
    await expect(page.locator('#timeline-sidebar')).toHaveClass(/collapsed/);
    await expect(page.locator('#timeline-toggle-btn')).toHaveAttribute('aria-expanded', 'false');
    await expectEqualDashboardChartHeights(page);
    expect(await page.evaluate(() => localStorage.getItem('beacon-timeline-width'))).toBe('0');

    await page.locator('#timeline-toggle-btn').click();
    await expect(page.locator('#timeline-sidebar')).not.toHaveClass(/collapsed/);
    await expect(page.locator('#timeline-toggle-btn')).toHaveAttribute('aria-expanded', 'true');
    await page.waitForFunction(() => {
      const sidebar = document.getElementById('timeline-sidebar');
      return sidebar ? Math.round(sidebar.getBoundingClientRect().width) >= 370 : false;
    });

    const divider = page.locator('#sidebar-divider');
    const box = await divider.boundingBox();
    expect(box).not.toBeNull();
    if (box) {
      const dragY = box.y + box.height / 2;
      await page.mouse.move(box.x + 1, dragY);
      await page.mouse.down();
      await page.mouse.move(980, dragY);
      await page.mouse.up();
    }
    await page.waitForFunction(() => Number(localStorage.getItem('beacon-timeline-width') || 0) > 390);
    await expectEqualDashboardChartHeights(page);
    const savedWidth = await page.evaluate(() => Number(localStorage.getItem('beacon-timeline-width') || 0));
    expect(savedWidth).toBeGreaterThan(390);
    expect(savedWidth).toBeLessThanOrEqual(700);

    await divider.dblclick();
    expect(await page.evaluate(() => localStorage.getItem('beacon-timeline-width'))).toBe('380');
    await expectEqualDashboardChartHeights(page);

    await page.locator('#dashboard-session-search').fill('migration');
    await expect(page.locator('#completed-session-status')).toHaveText(/1 search result/);
    await waitForCompletedRows(page, 1);
    await expect(page.locator('#dashboard-search-clear')).toBeVisible();
    await page.locator('#dashboard-search-clear').click();
    await waitForCompletedRows(page, 30);

    await page.locator('#dashboard-session-search').fill('dashboard payload');
    await expect(page.locator('#completed-session-status')).toHaveText(/1 search result/);
    await waitForCompletedRows(page, 1);
    await expect(page.locator('#completed-sessions tr[data-session-link]').first()).toHaveAttribute('data-sort-id', TEST_SESSION_ID);
    await page.locator('#dashboard-search-clear').click();
    await waitForCompletedRows(page, 30);

    const tokensHeader = page.locator('#completed-table th[data-sort-key="tokens"]');
    const tokensSortButton = tokensHeader.getByRole('button', { name: 'Tokens' });
    await expect(page.locator('#completed-table th[data-sort-key="ended"]')).toHaveAttribute('aria-sort', 'descending');
    const firstIDBeforeSort = await page.locator('#completed-sessions tr[data-session-link]').first().getAttribute('data-sort-id');
    await tokensSortButton.focus();
    await expect(tokensSortButton).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(tokensHeader.locator('.sort-arrow')).toHaveClass(/active/);
    await expect(tokensHeader).toHaveAttribute('aria-sort', 'descending');
    await expect(page.locator('#completed-table th[data-sort-key="ended"]')).toHaveAttribute('aria-sort', 'none');
    const firstIDAfterEnterSort = await page.locator('#completed-sessions tr[data-session-link]').first().getAttribute('data-sort-id');
    expect(firstIDAfterEnterSort).not.toBe(firstIDBeforeSort);
    await page.keyboard.press('Space');
    await expect(tokensHeader).toHaveAttribute('aria-sort', 'ascending');
    const firstIDAfterSpaceSort = await page.locator('#completed-sessions tr[data-session-link]').first().getAttribute('data-sort-id');
    expect(firstIDAfterSpaceSort).not.toBe(firstIDAfterEnterSort);

    await page.locator('.json-page-btn', { hasText: 'Next' }).click();
    await waitForCompletedRows(page, 1);
    await expect(page.locator('#completed-sessions tr[data-session-link]').first()).toHaveAttribute('data-sort-id', 'session-completed-030');
    await page.locator('.json-page-btn', { hasText: 'Previous' }).click();
    await waitForCompletedRows(page, 30);

    await page.locator(`button.json-subagent-toggle[data-session-id="${TEST_SESSION_ID}"]`).click();
    await expect(page.locator(`tr[data-parent="${TEST_SESSION_ID}"]`)).toHaveCount(2);
    await expect(page.locator(`button.json-subagent-toggle[data-session-id="${TEST_SESSION_ID}"]`)).toHaveAttribute('aria-expanded', 'true');

    await page.keyboard.press('t');
    await expect(page.locator('#timeline-sidebar')).toHaveClass(/collapsed/);
    await page.keyboard.press('t');
    await expect(page.locator('#timeline-sidebar')).not.toHaveClass(/collapsed/);

    const themeSelect = page.locator('#dashboard-theme-select');
    await expect(themeSelect).toBeVisible();
    expect(await themeSelect.locator('option').count()).toBeGreaterThanOrEqual(20);
    await themeSelect.selectOption('codex-aurora');
    await expect(page.locator('html')).toHaveAttribute('data-dashboard-theme', 'codex-aurora');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-theme'))).toBe('codex-aurora');
    expect(await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--dash-accent').trim())).toBe('#7dd3fc');

    await gotoDashboard(page);
    await expect(page.locator('html')).toHaveAttribute('data-dashboard-theme', 'codex-aurora');
    await expect(themeSelect).toHaveValue('codex-aurora');

    await guards.expectClean();
  });

  test('opens inspector from active and completed sessions, loads older summaries, and expands payloads', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page);
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);

    const activeLink = page.locator(`#active-sessions a[href="/sessions/${ACTIVE_SESSION_ID}"]`).first();
    await activeLink.focus();
    await page.keyboard.press('Enter');
    await expect(page.locator('#session-inspector')).toBeVisible();
    await expect(page.locator('#inspector-full-link')).toHaveText('View Transcript');
    await page.keyboard.press('Escape');
    await expect(page.locator('#session-inspector')).toHaveClass(/hidden/);
    await expect(activeLink).toBeFocused();

    await page.evaluate((id) => {
      (window as unknown as { dashboardSessionIndex: Record<string, unknown>; goToSession: (url: string) => void }).dashboardSessionIndex = {};
      (window as unknown as { goToSession: (url: string) => void }).goToSession(`/sessions/${id}`);
    }, TEST_SESSION_ID);
    await expect(page.locator('#session-inspector')).toBeVisible();
    await expect(page.locator('#inspector-summary')).toContainText('38m 12s');
    await expect(page.locator('#inspector-summary')).toContainText('123456');
    await expect(page.locator('#inspector-summary')).not.toContainText('Loading');
    await expect(page.locator('#session-inspector [aria-label="Close"]')).toBeFocused();

    await page.locator('#session-inspector .payload-btn').first().click();
    await expect(page.locator('#session-inspector .payload-target').first()).toContainText('internal/views/pages/dashboard.templ');

    await page.locator('#inspector-full-link').click();
    await expect(page).toHaveURL(new RegExp(`/sessions/${TEST_SESSION_ID}$`));
    await expect(page.locator('#btn-collapse-all')).toBeVisible();

    await guards.expectClean();
  });

  test('filters timeline, deep-links to transcript events, and verifies transcript controls', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page);
    await gotoDashboard(page);

    await page.locator('#timeline-sidebar').getByRole('button', { name: 'Errors' }).click();
    await expect(page.locator('#timeline-sidebar').getByRole('button', { name: 'Errors' })).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#activity-feed a[data-type="error"], #activity-feed a[data-type="tool_error"]')).toHaveCount(2);

    await page.getByRole('button', { name: 'All' }).last().click();
    await expect(page.locator('#activity-feed a[data-transcript-link]').first()).toBeVisible();
    await page.locator('#activity-feed a[data-transcript-link]').first().click();
    await expect(page).toHaveURL(new RegExp(`/sessions/${TEST_SESSION_ID}#${TEST_EVENT_ID}`));
    await expect(page.locator(`#${TEST_EVENT_ID}`)).toBeVisible();

    await expect(page.locator('#chat-view details')).toHaveCount(3);
    await page.locator('#btn-collapse-all').click();
    await expect(page.locator('#chat-view details[open]')).toHaveCount(0);
    await page.locator('#btn-expand-all').click();
    await expect(page.locator('#chat-view details[open]')).toHaveCount(3);

    await page.getByRole('button', { name: 'Timeline' }).click();
    await expect(page.locator('#timeline-view')).toBeVisible();
    await expect(page.locator('#chat-view')).toHaveClass(/hidden/);
    await expect(page.locator('#btn-expand-all')).toHaveClass(/hidden/);
    await page.getByRole('button', { name: 'Chat' }).click();
    await expect(page.locator('#chat-view')).toBeVisible();

    await guards.expectClean();
  });

  test('shows local retry states for failed dashboard APIs', async ({ page }) => {
    await installDashboardFixtures(page, { failOnce: ['active', 'completed', 'activity', 'charts'] });
    await gotoDashboard(page);

    await expect(page.locator('#active-sessions')).toContainText('Unable to load active sessions');
    await expect(page.locator('#completed-sessions')).toContainText('Unable to load completed sessions');
    await expect(page.locator('#activity-feed')).toContainText('Unable to load activity');
    await expect(page.locator('#dashboard-analytics-summary')).toContainText('0');

    await page.locator('#active-sessions button', { hasText: 'Retry' }).click();
    await expect(page.locator('#active-sessions')).toContainText('Realtime dashboard smoke run');
    await page.locator('#completed-sessions button', { hasText: 'Retry' }).click();
    await waitForCompletedRows(page, 30);
    await page.locator('#activity-feed button', { hasText: 'Retry' }).click();
    await expect(page.locator('#activity-feed a[data-transcript-link]')).toHaveCount(4);
    await page.locator('#dashboard-refresh-btn').click();
    await expect(page.locator('#dashboard-analytics-summary')).toContainText('201.5K');
  });

  test('meets local interaction performance budgets with deterministic fixtures', async ({ page }) => {
    await installDashboardFixtures(page);
    const start = Date.now();
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);
    expect(Date.now() - start).toBeLessThan(2000);

    const rangeStart = Date.now();
    await page.getByRole('button', { name: '1h' }).click();
    await expect(page.locator('#dashboard-range-caption')).toHaveText('Last hour');
    await expect(page.locator('#completed-session-status')).toContainText('shown');
    expect(Date.now() - rangeStart).toBeLessThan(800);

    const searchStart = Date.now();
    await page.locator('#dashboard-session-search').fill('migration');
    await waitForCompletedRows(page, 1);
    expect(Date.now() - searchStart).toBeLessThan(800);

    const resizeStart = Date.now();
    await page.locator('#timeline-toggle-btn').click();
    await expectEqualDashboardChartHeights(page);
    expect(Date.now() - resizeStart).toBeLessThan(800);
  });
});
