import { expect, test, type Page } from '@playwright/test';
import {
  ACTIVE_SESSION_ID,
  SEARCH_SESSION_ID,
  TEST_EVENT_ID,
  TEST_SESSION_ID,
  attachPageGuards,
  expectEqualDashboardChartHeights,
  expectLogAndModelControlsAligned,
  expectNoHorizontalOverflow,
  fillDashboardSearchAndWait,
  gotoDashboard,
  installDashboardFixtures,
  waitForCompletedRows,
  waitForDashboardSearchRows,
} from './fixtures/dashboard';

async function installTranscriptRealtimeFixture(page: Page) {
  let conversationVersion = 0;
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

  await page.route(`**/sessions/${TEST_SESSION_ID}/conversation`, async (route) => {
    conversationVersion += 1;
    await route.fulfill({
      status: 200,
      contentType: 'text/html',
      body: `
        <div id="chat-view" class="transcript-chat-view">
          <details id="${TEST_EVENT_ID}" open><summary>Realtime update ${conversationVersion}</summary><p>Version ${conversationVersion}</p></details>
        </div>
        <div id="timeline-view" class="transcript-timeline-view hidden">
          <a href="#${TEST_EVENT_ID}">Timeline update ${conversationVersion}</a>
        </div>
      `,
    });
  });

  await page.route(`**/sessions/${TEST_SESSION_ID}`, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'text/html',
      body: `<!doctype html>
        <html lang="en" data-dashboard-theme="codex-dark">
          <head>
            <meta charset="UTF-8">
            <meta name="viewport" content="width=device-width, initial-scale=1.0">
            <title>Realtime Transcript | Beacon</title>
            <script src="/static/js/vendor/htmx.min.js"></script>
            <script src="/static/js/vendor/htmx-ext-sse.js"></script>
            <link rel="stylesheet" href="/static/css/tailwind.css">
            <link rel="stylesheet" href="/static/css/custom.css">
          </head>
          <body data-page="transcript" class="bg-gray-900 text-gray-100 min-h-screen">
            <main id="main-content" class="min-h-screen w-full p-6 overflow-y-auto">
              <div id="transcript-wrap" hx-ext="sse" sse-connect="/sse/session/${TEST_SESSION_ID}" class="transcript-page space-y-6">
                <section class="transcript-conversation">
                  <div class="transcript-conversation-header flex items-center justify-between gap-3">
                    <h2>Conversation</h2>
                    <div class="transcript-controls flex items-center gap-2">
                      <button type="button" id="btn-expand-all" onclick="expandAll()">Expand All</button>
                      <button type="button" id="btn-collapse-all" onclick="collapseAll()">Collapse All</button>
                      <button type="button" onclick="switchView('chat', this)" aria-pressed="true" class="bg-blue-500/20 text-blue-400 border-blue-500/40">Chat</button>
                      <button type="button" onclick="switchView('timeline', this)" aria-pressed="false" class="bg-gray-800 text-gray-500 border-gray-700">Timeline</button>
                    </div>
                  </div>
                  <div id="conversation-container" hx-get="/sessions/${TEST_SESSION_ID}/conversation" hx-trigger="load, sse:conversation-update" hx-swap="innerHTML">
                    Loading conversation...
                  </div>
                </section>
              </div>
            </main>
            <script src="/static/js/transcript.js"></script>
          </body>
        </html>`,
    });
  });
}

async function expectDashboardSearchInputInView(page: Page) {
  const visible = await page.locator('#dashboard-session-search').evaluate((input) => {
    const owner = document.getElementById('dashboard-main');
    const inputRect = input.getBoundingClientRect();
    const ownerRect = owner?.getBoundingClientRect() || { top: 0, bottom: window.innerHeight };
    return inputRect.top >= ownerRect.top && inputRect.bottom <= ownerRect.bottom;
  });
  expect(visible).toBe(true);
}

test.describe('dashboard battle-tested workflows', () => {
  test('edits dashboard name, persists tab title, clears fallback, and renders unsafe text safely', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page);
    await gotoDashboard(page);

    await expect(page).toHaveTitle('Dashboard | Beacon');
    await expect(page.locator('#dashboard-title')).toHaveText('Beacon Realtime Dashboard');

    await page.locator('#dashboard-name-edit').click();
    await expect(page.locator('#dashboard-name-input')).toBeVisible();
    await page.locator('#dashboard-name-input').fill('  Workstation A  ');
    await expect(page).toHaveTitle('Workstation A | Beacon');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-name'))).toBe('Workstation A');
    await page.keyboard.press('Enter');
    await expect(page.locator('#dashboard-title')).toHaveText('Workstation A');

    await page.reload({ waitUntil: 'domcontentloaded' });
    await expect(page.locator('#dashboard-analytics-summary > div')).toHaveCount(4);
    await expect(page.locator('#dashboard-title')).toHaveText('Workstation A');
    await expect(page).toHaveTitle('Workstation A | Beacon');

    await page.locator('#dashboard-name-edit').click();
    await page.locator('#dashboard-name-clear').click();
    await expect(page).toHaveTitle('Dashboard | Beacon');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-name'))).toBeNull();
    await page.keyboard.press('Enter');
    await expect(page.locator('#dashboard-title')).toHaveText('Beacon Realtime Dashboard');

    const unsafeName = '<script>window.__beaconNameExecuted = true</script>';
    await page.locator('#dashboard-name-edit').click();
    await page.locator('#dashboard-name-input').fill(unsafeName);
    await expect(page).toHaveTitle(`${unsafeName} | Beacon`);
    await page.keyboard.press('Enter');
    await expect(page.locator('#dashboard-title')).toHaveText(unsafeName);
    expect(await page.evaluate(() => (window as Window & { __beaconNameExecuted?: boolean }).__beaconNameExecuted)).toBeUndefined();
    await expect(page.locator('script', { hasText: 'window.__beaconNameExecuted' })).toHaveCount(0);

    await guards.expectClean();
  });

  test('configured dashboard name remains the fallback when clearing a local override', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page);
    await gotoDashboard(page);
    await page.evaluate(() => localStorage.clear());
    await page.setContent(`
      <!doctype html>
      <html>
        <head><title>Configured Station | Beacon</title></head>
        <body>
          <div
            class="dashboard-name-control"
            data-dashboard-name-control
            data-dashboard-default-name="Configured Station"
            data-dashboard-fallback-heading="Configured Station"
          >
            <h1 id="dashboard-title" data-dashboard-title>Configured Station</h1>
            <button type="button" id="dashboard-name-edit" aria-label="Edit dashboard name" aria-controls="dashboard-name-input">Edit</button>
            <label for="dashboard-name-input">Dashboard name</label>
            <input id="dashboard-name-input" data-dashboard-name-input type="text" maxlength="80" class="hidden" />
            <button type="button" id="dashboard-name-clear" class="hidden" aria-label="Clear custom dashboard name">Clear</button>
          </div>
        </body>
      </html>
    `);
    await page.addScriptTag({ path: 'static/js/dashboard/name.js' });

    await expect(page).toHaveTitle('Configured Station | Beacon');
    await expect(page.locator('#dashboard-title')).toHaveText('Configured Station');
    await expect(page.locator('#dashboard-name-clear')).toHaveClass(/hidden/);

    await page.locator('#dashboard-name-edit').click();
    await expect(page.locator('#dashboard-name-input')).toHaveValue('Configured Station');
    await expect(page.locator('#dashboard-name-clear')).toHaveClass(/hidden/);

    await page.locator('#dashboard-name-input').fill('Custom\u0085Name');
    await expect(page).toHaveTitle('Custom Name | Beacon');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-name'))).toBe('Custom Name');
    await expect(page.locator('#dashboard-name-clear')).not.toHaveClass(/hidden/);

    await page.locator('#dashboard-name-clear').click();
    await expect(page).toHaveTitle('Configured Station | Beacon');
    await expect(page.locator('#dashboard-title')).toHaveText('Configured Station');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-name'))).toBeNull();
    await expect(page.locator('#dashboard-name-clear')).toHaveClass(/hidden/);

    await guards.expectClean();
  });

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
      await expect(page.locator('#sidebar')).toHaveCount(0);
      await expect(page.locator('nav a[href="/search"]')).toHaveCount(0);
      await expect(page.locator('#dashboard-session-search')).toBeVisible();
    }

    await guards.expectClean();
  });

  test('keeps the dashboard search control visible, keyboard reachable, and contained', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page);

    await page.setViewportSize({ width: 1440, height: 900 });
    await gotoDashboard(page);
    await page.locator('#dashboard-main').evaluate((main) => {
      main.scrollTop = 0;
    });
    await page.locator('#dashboard-search-focus').click();
    await expect(page.locator('#dashboard-session-search')).toBeFocused();
    await expectDashboardSearchInputInView(page);
    await page.locator('#dashboard-main').evaluate((main) => {
      main.scrollTop = 0;
    });
    await page.evaluate(() => {
      (document.activeElement as HTMLElement | null)?.blur();
    });
    await page.keyboard.press('/');
    await expect(page.locator('#dashboard-session-search')).toBeFocused();
    await expectDashboardSearchInputInView(page);

    for (const viewport of [
      { width: 390, height: 844 },
      { width: 320, height: 568 },
    ]) {
      await page.setViewportSize(viewport);
      await gotoDashboard(page);

      const searchButton = page.locator('#dashboard-search-focus');
      await expect(searchButton).toBeVisible();
      await expect(searchButton).toHaveAttribute('aria-label', 'Focus search');
      await expectNoHorizontalOverflow(page);

      const metrics = await searchButton.evaluate((el) => {
        const rect = el.getBoundingClientRect();
        return {
          left: Math.floor(rect.left),
          right: Math.ceil(rect.right),
          width: Math.round(rect.width),
          viewportWidth: window.innerWidth,
        };
      });
      expect(metrics.left).toBeGreaterThanOrEqual(0);
      expect(metrics.right).toBeLessThanOrEqual(metrics.viewportWidth);
      expect(metrics.width).toBeGreaterThanOrEqual(32);
    }

    await page.keyboard.press('Tab');
    for (let i = 0; i < 20 && !(await page.locator('#dashboard-search-focus').evaluate((el) => el === document.activeElement)); i++) {
      await page.keyboard.press('Tab');
    }
    await expect(page.locator('#dashboard-search-focus')).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(page.locator('#dashboard-session-search')).toBeFocused();
    await expect(page).toHaveURL(/\/$/);

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

  test('keeps chart hover labels anchored away from cursor travel', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page);
    await page.setViewportSize({ width: 1280, height: 800 });
    await gotoDashboard(page);

    const canvas = page.locator('#dashboardTokenCumulativeChart');
    await expect(canvas).toBeVisible();
    await page.waitForFunction(() => Boolean((window as Window & { dashboardTokenCumulativeChart?: any }).dashboardTokenCumulativeChart?.chartArea?.right));
    const points = await page.evaluate(() => {
      const chart = (window as Window & { dashboardTokenCumulativeChart?: any }).dashboardTokenCumulativeChart;
      return chart.data.datasets.slice(0, 2).map((dataset: { label: string }, datasetIndex: number) => {
        const elements = chart.getDatasetMeta(datasetIndex).data;
        const point = elements[Math.min(3 + datasetIndex, elements.length - 1)];
        return { label: dataset.label, x: point.x, y: point.y };
      });
    });
    expect(points.length).toBeGreaterThanOrEqual(2);

    const box = await canvas.boundingBox();
    expect(box).not.toBeNull();
    if (!box) return;
    const readTooltip = async () => page.evaluate(() => {
      const chart = (window as Window & { dashboardTokenCumulativeChart?: any }).dashboardTokenCumulativeChart;
      const tooltip = chart.tooltip;
      return {
        opacity: tooltip.opacity,
        caretX: Math.round(tooltip.caretX),
        caretY: Math.round(tooltip.caretY),
        body: (tooltip.body || []).map((line: { lines: string[] }) => line.lines.join(' ')).join('\n'),
      };
    });

    await page.mouse.move(box.x + points[0].x, box.y + points[0].y);
    await page.waitForFunction(() => (window as Window & { dashboardTokenCumulativeChart?: any }).dashboardTokenCumulativeChart.tooltip.opacity === 1);
    const firstTooltip = await readTooltip();

    await page.mouse.move(box.x + points[1].x, box.y + points[1].y);
    await page.waitForFunction(() => (window as Window & { dashboardTokenCumulativeChart?: any }).dashboardTokenCumulativeChart.tooltip.opacity === 1);
    const secondTooltip = await readTooltip();

    expect(firstTooltip.opacity).toBe(1);
    expect(secondTooltip.opacity).toBe(1);
    expect(secondTooltip.caretX).toBe(firstTooltip.caretX);
    expect(secondTooltip.caretY).toBe(firstTooltip.caretY);
    expect(secondTooltip.body).not.toBe(firstTooltip.body);

    await guards.expectClean();
  });

  test('exercises dashboard controls, search, sorting, pagination, subagents, and timeline sizing', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);

    await expectLogAndModelControlsAligned(page);

    await page.locator('#dashboard-range-control').getByRole('button', { name: '7d' }).click();
    await expect(page.locator('#dashboard-range-caption')).toHaveText('Last 7 days');
    await expect(page.locator('#dashboard-range-control').getByRole('button', { name: '7d' })).toHaveAttribute('aria-pressed', 'true');

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

    const modelDropdown = page.locator('#dashboardTokenCumulativeChart-model-dropdown');
    const modelDropdownTrigger = modelDropdown.locator('.model-dropdown-trigger');
    const modelDropdownPanel = modelDropdown.locator('.model-dropdown-panel');
    await modelDropdownTrigger.click();
    await expect(modelDropdownPanel).toBeVisible();
    await expect(modelDropdownTrigger).toHaveAttribute('aria-expanded', 'true');
    await page.keyboard.press('Escape');
    await expect(modelDropdownPanel).toHaveClass(/hidden/);
    await expect(modelDropdownTrigger).toHaveAttribute('aria-expanded', 'false');
    await expect(modelDropdownTrigger).toBeFocused();
    await modelDropdownTrigger.click();
    await expect(modelDropdownPanel).toBeVisible();
    await modelDropdown.locator('input[data-model]').first().uncheck();
    await expect(modelDropdown.locator('.model-dropdown-label')).toHaveText('Models (2/3)');
    await page.mouse.click(20, 20);
    await expect(modelDropdownPanel).toHaveClass(/hidden/);
    await expect(modelDropdownTrigger).toHaveAttribute('aria-expanded', 'false');

    await page.locator('#dashboard-model-metric-control').getByRole('button', { name: 'Tools' }).click();
    await expect(page.locator('#dashboard-model-metric-control').getByRole('button', { name: 'Tools' })).toHaveAttribute('aria-pressed', 'true');

    await page.locator('#dashboardTokenCumulativeChart-log-toggle').click();
    await expect(page.locator('#dashboardTokenCumulativeChart-log-toggle')).toHaveAttribute('aria-pressed', 'true');
    await expectLogAndModelControlsAligned(page);

    await page.locator('#timeline-toggle-btn').click();
    await expect(page.locator('#timeline-sidebar')).toHaveClass(/collapsed/);
    await expect(page.locator('#timeline-toggle-btn')).toHaveAttribute('aria-expanded', 'false');
    await expect(page.locator('#timeline-sidebar')).toHaveAttribute('inert', '');
    await expect(page.locator('#timeline-sidebar')).toHaveAttribute('aria-hidden', 'true');
    await expect(page.locator('html')).toHaveAttribute('data-beacon-timeline-collapsed', 'true');
    await expectEqualDashboardChartHeights(page);
    expect(await page.evaluate(() => localStorage.getItem('beacon-timeline-width'))).toBe('0');

    await page.locator('#timeline-toggle-btn').click();
    await expect(page.locator('#timeline-sidebar')).not.toHaveClass(/collapsed/);
    await expect(page.locator('#timeline-toggle-btn')).toHaveAttribute('aria-expanded', 'true');
    await expect(page.locator('#timeline-sidebar')).not.toHaveAttribute('inert', '');
    await expect(page.locator('#timeline-sidebar')).not.toHaveAttribute('aria-hidden', 'true');
    await expect(page.locator('html')).not.toHaveAttribute('data-beacon-timeline-collapsed', 'true');
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

    await divider.focus();
    await expect(divider).toBeFocused();
    await expect(divider).toHaveAttribute('role', 'separator');
    await page.keyboard.press('ArrowLeft');
    await expect(divider).toHaveAttribute('aria-valuenow', '404');
    expect(await page.evaluate(() => localStorage.getItem('beacon-timeline-width'))).toBe('404');
    await page.keyboard.press('Home');
    await expect(page.locator('#timeline-sidebar')).toHaveClass(/collapsed/);
    await expect(divider).toHaveAttribute('aria-valuenow', '0');
    await page.keyboard.press('End');
    await expect(page.locator('#timeline-sidebar')).not.toHaveClass(/collapsed/);
    await expect(divider).toHaveAttribute('aria-valuenow', '380');
    await expectEqualDashboardChartHeights(page);

    await fillDashboardSearchAndWait(page, 'migration');
    await expect(page.locator('#completed-session-status')).toHaveText(/2 search results/);
    await waitForDashboardSearchRows(page, 2);
    await expect(page.locator('#dashboard-search-clear')).toBeVisible();
    await page.locator('#dashboard-search-clear').click();
    await waitForCompletedRows(page, 30);

    await fillDashboardSearchAndWait(page, 'dashboard payload');
    await expect(page.locator('#completed-session-status')).toHaveText(/1 search result/);
    await waitForDashboardSearchRows(page, 1);
    await expect(page.locator('#completed-sessions tr[data-search-row]').first()).toHaveAttribute('data-session-id', SEARCH_SESSION_ID);
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
    await expect(page.locator('#completed-sessions tr[data-session-link]').first()).toHaveAttribute('data-sort-id', TEST_SESSION_ID);
    await page.locator('.json-page-btn', { hasText: 'Previous' }).click();
    await waitForCompletedRows(page, 30);

    await page.locator('.json-page-btn', { hasText: 'Next' }).click();
    await waitForCompletedRows(page, 1);
    await page.locator(`button.json-subagent-toggle[data-session-id="${TEST_SESSION_ID}"]`).click();
    await expect(page.locator(`tr[data-parent="${TEST_SESSION_ID}"]`)).toHaveCount(2);
    await expect(page.locator(`button.json-subagent-toggle[data-session-id="${TEST_SESSION_ID}"]`)).toHaveAttribute('aria-expanded', 'true');
    await page.locator('#dashboard-wrap').click({ position: { x: 20, y: 20 } });

    await page.keyboard.press('t');
    await expect(page.locator('#timeline-sidebar')).toHaveClass(/collapsed/);
    await page.keyboard.press('t');
    await expect(page.locator('#timeline-sidebar')).not.toHaveClass(/collapsed/);

    const themeSelect = page.locator('#dashboard-theme-select');
    const appearanceToggle = page.locator('#dashboard-appearance-toggle');
    await expect(themeSelect).toBeVisible();
    await expect(appearanceToggle).toBeVisible();
    await expect(page.locator('#dashboard-appearance-select')).toHaveCount(0);
    await expect(page.locator('#dashboard-theme-control')).not.toContainText('Theme');
    await expect(page.locator('#dashboard-refresh-btn')).toHaveText('');
    await expect(page.locator('#timeline-toggle-btn')).toHaveText('');
    await expect(page.locator('#dashboard-search-clear')).toHaveText('');
    await expect(appearanceToggle).toHaveAttribute('role', 'switch');
    await expect(appearanceToggle).toHaveAttribute('aria-label', 'Dark mode');
    expect(await themeSelect.locator('option').count()).toBeGreaterThanOrEqual(28);
    await themeSelect.selectOption('catppuccin');
    await expect(page.locator('html')).toHaveAttribute('data-dashboard-theme', 'catppuccin-dark');
    await expect(appearanceToggle).toHaveAttribute('title', 'Switch to light mode');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-theme'))).toBe('catppuccin');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-appearance'))).toBe('dark');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-resolved-theme'))).toBe('catppuccin-dark');
    expect(await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--dash-accent').trim())).toBe('#cba6f7');

    await appearanceToggle.click();
    await expect(page.locator('html')).toHaveAttribute('data-dashboard-theme', 'catppuccin-light');
    await expect(appearanceToggle).toHaveAttribute('aria-checked', 'false');
    await expect(appearanceToggle).toHaveAttribute('aria-label', 'Dark mode');
    await expect(appearanceToggle).toHaveAttribute('title', 'Switch to dark mode');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-appearance'))).toBe('light');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-preferred-appearance'))).toBe('light');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-resolved-theme'))).toBe('catppuccin-light');
    expect(await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--dash-accent').trim())).toBe('#8839ef');

    await themeSelect.selectOption('dracula');
    await expect(page.locator('html')).toHaveAttribute('data-dashboard-theme', 'dracula-dark');
    await expect(appearanceToggle).toBeDisabled();
    await expect(appearanceToggle).toHaveAttribute('aria-checked', 'true');
    await expect(appearanceToggle).toHaveAttribute('title', 'Dracula is dark only');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-appearance'))).toBe('dark');

    await themeSelect.selectOption('catppuccin');
    await expect(page.locator('html')).toHaveAttribute('data-dashboard-theme', 'catppuccin-light');
    await expect(appearanceToggle).toHaveAttribute('aria-checked', 'false');

    await gotoDashboard(page);
    await expect(page.locator('html')).toHaveAttribute('data-dashboard-theme', 'catppuccin-light');
    await expect(themeSelect).toHaveValue('catppuccin');
    await expect(appearanceToggle).toHaveAttribute('aria-checked', 'false');

    await page.goto(`/sessions/${TEST_SESSION_ID}`, { waitUntil: 'domcontentloaded' });
    await expect(page.locator('html')).toHaveAttribute('data-dashboard-theme', 'catppuccin-light');
    await expect(page.locator('body')).toHaveAttribute('data-page', 'transcript');
    await expect(page.locator('#sidebar')).toHaveCount(0);
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-theme'))).toBe('catppuccin');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-appearance'))).toBe('light');
    expect(await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--dash-accent').trim())).toBe('#8839ef');

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
    await expect(page.locator('#dashboard-main')).toHaveAttribute('inert', '');
    await expect(page.locator('#sidebar-divider')).toHaveAttribute('inert', '');
    await expect(page.locator('#timeline-sidebar')).toHaveAttribute('aria-hidden', 'true');
    await expect(page.locator('#inspector-full-link')).toHaveText('View Transcript');
    await page.keyboard.press('Escape');
    await expect(page.locator('#session-inspector')).toHaveClass(/hidden/);
    await expect(page.locator('#dashboard-main')).not.toHaveAttribute('inert', '');
    await expect(page.locator('#sidebar-divider')).not.toHaveAttribute('inert', '');
    await expect(page.locator('#timeline-sidebar')).not.toHaveAttribute('aria-hidden', 'true');
    await expect(activeLink).toBeFocused();

    await page.locator('#timeline-toggle-btn').click();
    await expect(page.locator('#timeline-sidebar')).toHaveClass(/collapsed/);
    await activeLink.focus();
    await page.keyboard.press('Enter');
    await expect(page.locator('#session-inspector')).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(page.locator('#session-inspector')).toHaveClass(/hidden/);
    await expect(page.locator('#timeline-sidebar')).toHaveAttribute('inert', '');
    await expect(page.locator('#timeline-sidebar')).toHaveAttribute('aria-hidden', 'true');
    await page.locator('#timeline-toggle-btn').click();
    await expect(page.locator('#timeline-sidebar')).not.toHaveClass(/collapsed/);

    await activeLink.focus();
    await page.keyboard.press('Enter');
    await expect(page.locator('#session-inspector')).toBeVisible();
    await activeLink.evaluate((el) => el.remove());
    await page.keyboard.press('Escape');
    await expect(page.locator('#session-inspector')).toHaveClass(/hidden/);
    await expect(page.locator('#dashboard-session-search')).toBeFocused();
    await expect(page.locator('#inspector-full-link')).not.toBeFocused();

    await page.locator('.json-page-btn', { hasText: 'Next' }).click();
    await waitForCompletedRows(page, 1);
    const completedRow = page.locator(`tr[data-sort-id="${TEST_SESSION_ID}"]`);
    const completedOpenButton = completedRow.locator('.session-row-open');
    await expect(completedOpenButton).toHaveAttribute('aria-label', /Open session/);
    await completedOpenButton.focus();
    await expect(completedOpenButton).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(page.locator('#session-inspector')).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(page.locator('#session-inspector')).toHaveClass(/hidden/);
    await completedOpenButton.focus();
    await page.keyboard.press('Space');
    await expect(page.locator('#session-inspector')).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(page.locator('#session-inspector')).toHaveClass(/hidden/);

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

  test('restores a collapsed timeline before dashboard paint after transcript navigation', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page);
    await page.addInitScript(() => {
      localStorage.setItem('beacon-timeline-width', '0');
      localStorage.setItem('beacon-timeline-prev-width', '420');
    });
    let releaseDashboardScript: (() => void) | undefined;
    await page.route('**/static/js/dashboard/timeline.js', async (route) => {
      await new Promise<void>((resolve) => {
        releaseDashboardScript = resolve;
      });
      await route.continue();
    });

    await page.goto(`/sessions/${TEST_SESSION_ID}`, { waitUntil: 'domcontentloaded' });
    const dashboardLoad = page.goto('/', { waitUntil: 'domcontentloaded' });
    await page.waitForSelector('#timeline-sidebar', { state: 'attached' });
    await expect(page.locator('html')).toHaveAttribute('data-beacon-timeline-collapsed', 'true');
    let width = await page.locator('#timeline-sidebar').evaluate((el) => Math.round(el.getBoundingClientRect().width));
    expect(width).toBe(0);
    releaseDashboardScript?.();
    await dashboardLoad;
    await expect(page.locator('html')).toHaveAttribute('data-beacon-timeline-collapsed', 'true');
    await expect(page.locator('#timeline-sidebar')).toHaveClass(/collapsed/);
    width = await page.locator('#timeline-sidebar').evaluate((el) => Math.round(el.getBoundingClientRect().width));
    expect(width).toBe(0);

    await guards.expectClean();
  });

  test('filters timeline, deep-links to transcript events, and verifies transcript controls', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page);
    await gotoDashboard(page);

    await page.locator('#timeline-sidebar').getByRole('button', { name: 'Errors' }).click();
    await expect(page.locator('#timeline-sidebar').getByRole('button', { name: 'Errors' })).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#activity-feed a[data-type="error"], #activity-feed a[data-type="tool_error"]')).toHaveCount(2);

    await page.locator('#timeline-sidebar').getByRole('button', { name: 'All' }).click();
    await expect(page.locator('#activity-feed a[data-transcript-link]').first()).toBeVisible();
    await page.locator('#activity-feed a[data-transcript-link]').first().click();
    await expect(page).toHaveURL(new RegExp(`/sessions/${TEST_SESSION_ID}#${TEST_EVENT_ID}`));
    await expect(page.locator(`#${TEST_EVENT_ID}`)).toBeVisible();
    await expectNoHorizontalOverflow(page);

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

  test('refreshes transcript conversation smoothly from session SSE updates', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installTranscriptRealtimeFixture(page);

    await page.goto(`/sessions/${TEST_SESSION_ID}`, { waitUntil: 'domcontentloaded' });
    await expect(page.locator('#chat-view')).toContainText('Version 1');

    await page.getByRole('button', { name: 'Timeline' }).click();
    await expect(page.locator('#timeline-view')).toBeVisible();
    await expect(page.locator('#chat-view')).toHaveClass(/hidden/);

    await page.evaluate(() => {
      (window as unknown as { __beaconEventSources: Array<{ emit: (type: string) => void }> }).__beaconEventSources[0].emit('conversation-update');
    });
    await expect(page.locator('#timeline-view')).toContainText('Timeline update 2');
    await expect(page.locator('#timeline-view')).toBeVisible();
    await expect(page.locator('#chat-view')).toHaveClass(/hidden/);

    await page.getByRole('button', { name: 'Chat' }).click();
    await expect(page.locator('#chat-view')).toContainText('Version 2');

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
    await page.locator('#dashboard-range-control').getByRole('button', { name: '1h' }).click();
    await expect(page.locator('#dashboard-range-caption')).toHaveText('Last hour');
    await expect(page.locator('#completed-session-status')).toContainText('shown');
    expect(Date.now() - rangeStart).toBeLessThan(800);

    const searchStart = Date.now();
    await fillDashboardSearchAndWait(page, 'migration');
    await waitForDashboardSearchRows(page, 2);
    expect(Date.now() - searchStart).toBeLessThan(800);

    const resizeStart = Date.now();
    await page.locator('#timeline-toggle-btn').click();
    await expectEqualDashboardChartHeights(page);
    expect(Date.now() - resizeStart).toBeLessThan(800);
  });
});
