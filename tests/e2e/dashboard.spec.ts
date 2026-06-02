import { expect, test, type Page } from '@playwright/test';
import {
  ACTIVE_SESSION_ID,
  SEARCH_SESSION_ID,
  TEST_EVENT_ID,
  TEST_SESSION_ID,
  attachPageGuards,
  expectDashboardTokenChartReady,
  expectLogAndModelControlsAligned,
  expectNoHorizontalOverflow,
  fillDashboardSearchAndWait,
  gotoDashboard,
  installDashboardFixtures,
  readDashboardScroll,
  scrollDashboardMainToSearch,
  waitForDashboardSearchResponse,
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

async function readActiveSessionGeometry(page: Page) {
  return page.evaluate(() => {
    const panel = document.getElementById('active-sessions');
    const scroll = document.querySelector('#active-sessions .active-session-board-scroll');
    const grid = document.querySelector('#active-sessions .active-session-grid');
    const cards = Array.from(document.querySelectorAll('#active-sessions .active-session-card'));
    const panelRect = panel?.getBoundingClientRect();
    const scrollRect = scroll?.getBoundingClientRect();
    const gridRect = grid?.getBoundingClientRect();
    const cardRects = cards.map((card) => {
      const rect = card.getBoundingClientRect();
      return {
        left: Math.round(rect.left),
        right: Math.round(rect.right),
        top: Math.round(rect.top),
        width: Math.round(rect.width),
      };
    });
    const protrusions = cards.flatMap((card, cardIndex) => {
      const cardRect = card.getBoundingClientRect();
      return Array.from(card.querySelectorAll('.active-session-card-header, .active-session-title, .active-session-kicker, .active-session-meta-row, .active-session-tracker, .active-child-list, .active-child-row, .active-session-path'))
        .map((child) => {
          const rect = child.getBoundingClientRect();
          return {
            cardIndex,
            className: child.className,
            left: Math.round(rect.left),
            right: Math.round(rect.right),
            cardLeft: Math.round(cardRect.left),
            cardRight: Math.round(cardRect.right),
          };
        })
        .filter((item) => item.left < item.cardLeft - 1 || item.right > item.cardRight + 1);
    });
    return {
      viewportWidth: window.innerWidth,
      bodyScrollWidth: document.documentElement.scrollWidth,
      panelHeight: Math.round(panelRect?.height || 0),
      scrollClientHeight: Math.round(scroll?.clientHeight || 0),
      scrollHeight: Math.round(scroll?.scrollHeight || 0),
      scrollTop: Math.round(scroll?.scrollTop || 0),
      scrollRectTop: Math.round(scrollRect?.top || 0),
      scrollRectBottom: Math.round(scrollRect?.bottom || 0),
      gridLeft: Math.round(gridRect?.left || 0),
      gridRight: Math.round(gridRect?.right || 0),
      gridWidth: Math.round(gridRect?.width || 0),
      ids: cards.map((card) => card.getAttribute('data-active-session-id') || ''),
      cards: cardRects,
      firstRowCount: cardRects.filter((rect) => rect.top === cardRects[0]?.top).length,
      rowCount: new Set(cardRects.map((rect) => rect.top)).size,
      protrusions,
    };
  });
}

test.describe('dashboard battle-tested workflows', () => {
  test('edits dashboard name, persists tab title, clears fallback, and renders unsafe text safely', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page);
    await gotoDashboard(page);

    await expect(page).toHaveTitle('Dashboard | Beacon');
    await expect(page.locator('#dashboard-title')).toHaveText('Beacon Realtime Dashboard');
    await expect(page.locator('.dashboard-brand-mark')).toBeVisible();
    await expect(page.getByRole('group', { name: 'Dashboard controls' })).toBeVisible();
    await expect(page.getByRole('group', { name: 'Theme controls' })).toBeVisible();
    await expect(page.locator('#dashboard-name-clear')).toHaveCount(0);
    await expect(page.locator('[data-dashboard-name-control]')).toContainText('Beacon Realtime Dashboard');
    const restingNameMetrics = await page.locator('#dashboard-name-edit').evaluate((edit) => {
      const editRect = edit.getBoundingClientRect();
      const titleRect = document.getElementById('dashboard-title')?.getBoundingClientRect();
      return {
        editLeft: editRect.left,
        editRight: editRect.right,
        titleLeft: titleRect?.left || 0,
      };
    });
    expect(restingNameMetrics.editLeft).toBeLessThan(restingNameMetrics.titleLeft);
    expect(restingNameMetrics.editRight).toBeLessThanOrEqual(restingNameMetrics.titleLeft);

    await page.locator('#dashboard-name-edit').click();
    await expect(page.locator('#dashboard-name-input')).toBeVisible();
    await expect(page.locator('#dashboard-name-input')).toBeFocused();
    await page.keyboard.press('Shift+Tab');
    await expect(page.locator('#dashboard-name-edit')).toBeFocused();
    await page.keyboard.press('Tab');
    await expect(page.locator('#dashboard-name-input')).toBeFocused();
    await page.keyboard.press('Shift+Tab');
    await expect(page.locator('#dashboard-name-edit')).toBeFocused();
    await page.keyboard.press('Shift+Tab');
    await expect(page.locator('#dashboard-name-input')).toBeHidden();
    await page.locator('#dashboard-name-edit').click();
    await expect(page.locator('#dashboard-name-input')).toBeFocused();
    const editingNameMetrics = await page.locator('#dashboard-name-edit').evaluate((edit) => {
      const editRect = edit.getBoundingClientRect();
      const inputRect = document.getElementById('dashboard-name-input')?.getBoundingClientRect();
      return {
        editRight: editRect.right,
        inputLeft: inputRect?.left || 0,
      };
    });
    expect(editingNameMetrics.editRight).toBeLessThanOrEqual(editingNameMetrics.inputLeft);
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
    await page.locator('#dashboard-name-input').fill('');
    await expect(page).toHaveTitle('Dashboard | Beacon');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-name'))).toBeNull();
    await page.keyboard.press('Enter');
    await expect(page.locator('#dashboard-title')).toHaveText('Beacon Realtime Dashboard');

    await page.setViewportSize({ width: 1280, height: 800 });
    const longName = 'Beacon operations dashboard for west coast release train alpha bravo charlie';
    await page.locator('#dashboard-name-edit').click();
    await page.locator('#dashboard-name-input').fill(longName);
    await page.keyboard.press('Enter');
    await expect(page.locator('#dashboard-title')).toHaveText(longName);
    await expectNoHorizontalOverflow(page);
    const longNameHeaderMetrics = await page.locator('.dashboard-header').evaluate((header) => {
      const brandRect = header.querySelector('.dashboard-brand-mark')?.getBoundingClientRect();
      const titleRect = header.querySelector('#dashboard-title')?.getBoundingClientRect();
      const actionsRect = header.querySelector('.dashboard-header-actions')?.getBoundingClientRect();
      return {
        brandLeft: Math.floor(brandRect?.left || 0),
        brandRight: Math.ceil(brandRect?.right || 0),
        titleLeft: Math.floor(titleRect?.left || 0),
        titleRight: Math.ceil(titleRect?.right || 0),
        titleTop: Math.round(titleRect?.top || 0),
        actionsLeft: Math.floor(actionsRect?.left || 0),
        actionsTop: Math.round(actionsRect?.top || 0),
      };
    });
    expect(longNameHeaderMetrics.brandLeft).toBeGreaterThanOrEqual(0);
    expect(longNameHeaderMetrics.brandRight).toBeLessThanOrEqual(longNameHeaderMetrics.titleLeft);
    if (Math.abs(longNameHeaderMetrics.titleTop - longNameHeaderMetrics.actionsTop) <= 2) {
      expect(longNameHeaderMetrics.titleRight).toBeLessThanOrEqual(longNameHeaderMetrics.actionsLeft);
    }
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto('/', { waitUntil: 'domcontentloaded' });
    await expect(page.locator('#dashboard-analytics-summary > div')).toHaveCount(4);
    await expect(page.locator('#dashboard-title')).toHaveText(longName);
    await expectNoHorizontalOverflow(page);
    const mobileLongNameHeaderMetrics = await page.locator('.dashboard-header').evaluate((header) => {
      const brandRect = header.querySelector('.dashboard-brand-mark')?.getBoundingClientRect();
      const titleRect = header.querySelector('#dashboard-title')?.getBoundingClientRect();
      const actionsRect = header.querySelector('.dashboard-header-actions')?.getBoundingClientRect();
      return {
        brandLeft: Math.floor(brandRect?.left || 0),
        titleLeft: Math.floor(titleRect?.left || 0),
        titleRight: Math.ceil(titleRect?.right || 0),
        actionsLeft: Math.floor(actionsRect?.left || 0),
        actionsRight: Math.ceil(actionsRect?.right || 0),
        viewportWidth: window.innerWidth,
      };
    });
    expect(mobileLongNameHeaderMetrics.brandLeft).toBeGreaterThanOrEqual(0);
    expect(mobileLongNameHeaderMetrics.titleLeft).toBeGreaterThanOrEqual(0);
    expect(mobileLongNameHeaderMetrics.titleRight).toBeLessThanOrEqual(mobileLongNameHeaderMetrics.viewportWidth);
    expect(mobileLongNameHeaderMetrics.actionsLeft).toBeGreaterThanOrEqual(0);
    expect(mobileLongNameHeaderMetrics.actionsRight).toBeLessThanOrEqual(mobileLongNameHeaderMetrics.viewportWidth);

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
          </div>
        </body>
      </html>
    `);
    await page.addScriptTag({ path: 'static/js/dashboard/name.js' });

    await expect(page).toHaveTitle('Configured Station | Beacon');
    await expect(page.locator('#dashboard-title')).toHaveText('Configured Station');
    await expect(page.locator('#dashboard-name-clear')).toHaveCount(0);

    await page.locator('#dashboard-name-edit').click();
    await expect(page.locator('#dashboard-name-input')).toHaveValue('Configured Station');
    await expect(page.locator('#dashboard-name-clear')).toHaveCount(0);

    await page.locator('#dashboard-name-input').fill('Custom\u0085Name');
    await expect(page).toHaveTitle('Custom Name | Beacon');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-name'))).toBe('Custom Name');
    await expect(page.locator('#dashboard-name-clear')).toHaveCount(0);

    await page.locator('#dashboard-name-input').fill('');
    await page.keyboard.press('Enter');
    await expect(page).toHaveTitle('Configured Station | Beacon');
    await expect(page.locator('#dashboard-title')).toHaveText('Configured Station');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-name'))).toBeNull();
    await expect(page.locator('#dashboard-name-clear')).toHaveCount(0);

    await guards.expectClean();
  });

  test('loads cleanly and keeps chart geometry across supported viewports', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page);

    for (const viewport of [
      { width: 1600, height: 900 },
      { width: 1440, height: 900 },
      { width: 1280, height: 800 },
      { width: 1120, height: 800 },
      { width: 1100, height: 800 },
      { width: 1024, height: 768 },
      { width: 390, height: 844 },
      { width: 320, height: 568 },
    ]) {
      await page.setViewportSize(viewport);
      await gotoDashboard(page);
      await expectNoHorizontalOverflow(page);
      await expectDashboardTokenChartReady(page);
      await expect(page.getByRole('searchbox', { name: 'Search table sessions and events' })).toHaveAttribute('placeholder', 'Search table sessions and events');
      await expect(page.locator('.dashboard-search-filters')).toHaveAttribute('aria-label', 'Table filters');
      await expect(page.getByLabel('Message type')).toHaveValue('');
      await expect(page.locator('[data-search-event-kind]')).toHaveCount(0);
      const shellLayout = await page.evaluate(() => {
        const wrap = document.getElementById('dashboard-wrap')?.getBoundingClientRect();
        const main = document.getElementById('dashboard-main')?.getBoundingClientRect();
        const header = document.querySelector('.dashboard-header')?.getBoundingClientRect();
        const overview = document.querySelector('.dashboard-overview-row')?.getBoundingClientRect();
        const active = document.getElementById('active-sessions')?.getBoundingClientRect();
        const analytics = document.querySelector('.dashboard-analytics-panel')?.getBoundingClientRect();
        const completed = document.querySelector('.completed-table-surface')?.getBoundingClientRect();
        const sidebar = document.getElementById('timeline-sidebar')?.getBoundingClientRect();
        const title = document.querySelector('.completed-table-title')?.getBoundingClientRect();
        const search = document.querySelector('.dashboard-table-search')?.getBoundingClientRect();
        const controls = document.querySelector('.dashboard-table-left')?.getBoundingClientRect();
        const chart = document.querySelector('.dashboard-table-chart')?.getBoundingClientRect();
        const summary = document.getElementById('dashboard-analytics-summary')?.getBoundingClientRect();
        const filterOverflow = Array.from(document.querySelectorAll('.dashboard-search-filters, .dashboard-search-filter-group')).some((el) => {
          return el.scrollWidth > el.clientWidth + 1;
        });
        const summaryTextOverflow = Array.from(document.querySelectorAll('.dashboard-summary-label, .dashboard-summary-value, .dashboard-summary-subvalue')).some((el) => {
          return el.scrollWidth > el.clientWidth + 1;
        });
        return {
          wrapRight: Math.round(wrap?.right || 0),
          mainRight: Math.round(main?.right || 0),
          headerTop: Math.round(header?.top || 0),
          headerBottom: Math.round(header?.bottom || 0),
          overviewTop: Math.round(overview?.top || 0),
          overviewBottom: Math.round(overview?.bottom || 0),
          activeTop: Math.round(active?.top || 0),
          activeRight: Math.round(active?.right || 0),
          activeBottom: Math.round(active?.bottom || 0),
          analyticsTop: Math.round(analytics?.top || 0),
          analyticsLeft: Math.round(analytics?.left || 0),
          analyticsRight: Math.round(analytics?.right || 0),
          analyticsBottom: Math.round(analytics?.bottom || 0),
          completedTop: Math.round(completed?.top || 0),
          completedRight: Math.round(completed?.right || 0),
          sidebarTop: Math.round(sidebar?.top || 0),
          sidebarLeft: Math.round(sidebar?.left || 0),
          sidebarRight: Math.round(sidebar?.right || 0),
          titleTop: Math.round(title?.top || 0),
          titleBottom: Math.round(title?.bottom || 0),
          searchTop: Math.round(search?.top || 0),
          controlsTop: Math.round(controls?.top || 0),
          controlsBottom: Math.round(controls?.bottom || 0),
          summaryLeft: Math.round(summary?.left || 0),
          summaryRight: Math.round(summary?.right || 0),
          filterOverflow,
          summaryTextOverflow,
          chartTop: Math.round(chart?.top || 0),
          chartLeft: Math.round(chart?.left || 0),
          chartInSearchHeader: Boolean(document.getElementById('dashboardTokenCumulativeChart')?.closest('#dashboard-search')),
          chartInAnalyticsPanel: Boolean(document.getElementById('dashboardTokenCumulativeChart')?.closest('.dashboard-analytics-panel')),
          summaryInSearchHeader: Boolean(document.getElementById('dashboard-analytics-summary')?.closest('#dashboard-search')),
          summaryInAnalyticsPanel: Boolean(document.getElementById('dashboard-analytics-summary')?.closest('.dashboard-analytics-panel')),
          bodyOverflow: document.documentElement.scrollWidth > window.innerWidth + 1,
        };
      });
      expect(shellLayout.headerTop).toBeGreaterThanOrEqual(0);
      expect(shellLayout.overviewTop).toBeGreaterThanOrEqual(shellLayout.headerBottom);
      expect(shellLayout.completedTop).toBeGreaterThanOrEqual(shellLayout.overviewBottom);
      expect(shellLayout.titleTop).toBeLessThanOrEqual(shellLayout.searchTop);
      expect(shellLayout.titleBottom).toBeLessThanOrEqual(shellLayout.searchTop + 1);
      expect(shellLayout.summaryLeft).toBeGreaterThanOrEqual(0);
      expect(shellLayout.summaryRight).toBeLessThanOrEqual(viewport.width);
      expect(shellLayout.filterOverflow).toBe(false);
      expect(shellLayout.summaryTextOverflow).toBe(false);
      expect(shellLayout.chartInSearchHeader).toBe(false);
      expect(shellLayout.chartInAnalyticsPanel).toBe(true);
      expect(shellLayout.summaryInSearchHeader).toBe(false);
      expect(shellLayout.summaryInAnalyticsPanel).toBe(true);
      expect(shellLayout.bodyOverflow).toBe(false);
      expect(shellLayout.activeRight).toBeLessThanOrEqual(shellLayout.mainRight + 1);
      expect(shellLayout.analyticsRight).toBeLessThanOrEqual(shellLayout.mainRight + 1);
      expect(shellLayout.completedRight).toBeLessThanOrEqual(shellLayout.mainRight + 1);
      if (viewport.width > 1240) {
        expect(Math.abs(shellLayout.activeTop - shellLayout.analyticsTop)).toBeLessThanOrEqual(2);
        expect(shellLayout.activeRight).toBeLessThanOrEqual(shellLayout.analyticsLeft);
        expect(shellLayout.completedTop).toBeGreaterThanOrEqual(Math.max(shellLayout.activeBottom, shellLayout.analyticsBottom));
        expect(shellLayout.sidebarLeft).toBeGreaterThanOrEqual(shellLayout.mainRight);
        expect(Math.abs(shellLayout.sidebarRight - shellLayout.wrapRight)).toBeLessThanOrEqual(2);
      } else if (viewport.width > 1100) {
        expect(shellLayout.analyticsTop).toBeGreaterThanOrEqual(shellLayout.activeBottom);
        expect(shellLayout.completedTop).toBeGreaterThanOrEqual(shellLayout.analyticsBottom);
        expect(shellLayout.sidebarLeft).toBeGreaterThanOrEqual(shellLayout.mainRight);
        expect(Math.abs(shellLayout.sidebarRight - shellLayout.wrapRight)).toBeLessThanOrEqual(2);
      } else {
        expect(shellLayout.analyticsTop).toBeGreaterThanOrEqual(shellLayout.activeBottom);
        expect(shellLayout.sidebarTop).toBeGreaterThanOrEqual(shellLayout.completedTop);
      }
      await expect(page.locator('#sidebar')).toHaveCount(0);
      await expect(page.locator('nav a[href="/search"]')).toHaveCount(0);
      await expect(page.locator('#dashboard-session-search')).toBeVisible();
      await expect(page.locator('#dashboard-search #dashboard-range-control')).toHaveCount(1);
      await expect(page.locator('[data-search-range]')).toHaveCount(0);
    }

    await guards.expectClean();
  });

  test('promotes active sessions with compact live stat trackers', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await gotoDashboard(page);

    await expect(page.locator('#active-sessions')).toContainText('Realtime dashboard smoke run');
    const sectionOrder = await page.evaluate(() => {
      const active = document.getElementById('active-sessions')?.getBoundingClientRect();
      const summary = document.getElementById('dashboard-analytics-summary')?.getBoundingClientRect();
      const analytics = document.querySelector('.dashboard-analytics-panel')?.getBoundingClientRect();
      const surface = document.querySelector('.completed-table-surface')?.getBoundingClientRect();
      return {
        activeTop: active?.top || 0,
        activeRight: active?.right || 0,
        activeBottom: active?.bottom || 0,
        analyticsTop: analytics?.top || 0,
        analyticsLeft: analytics?.left || 0,
        analyticsBottom: analytics?.bottom || 0,
        summaryTop: summary?.top || 0,
        surfaceTop: surface?.top || 0,
        summaryInAnalyticsPanel: Boolean(document.getElementById('dashboard-analytics-summary')?.closest('.dashboard-analytics-panel')),
        summaryInSearchHeader: Boolean(document.getElementById('dashboard-analytics-summary')?.closest('#dashboard-search')),
      };
    });
    expect(sectionOrder.activeTop).toBeGreaterThan(0);
    expect(Math.abs(sectionOrder.activeTop - sectionOrder.analyticsTop)).toBeLessThanOrEqual(2);
    expect(sectionOrder.activeRight).toBeLessThanOrEqual(sectionOrder.analyticsLeft);
    expect(sectionOrder.summaryTop).toBeGreaterThanOrEqual(sectionOrder.analyticsTop);
    expect(sectionOrder.activeBottom).toBeLessThanOrEqual(sectionOrder.surfaceTop);
    expect(sectionOrder.analyticsBottom).toBeLessThanOrEqual(sectionOrder.surfaceTop);
    expect(sectionOrder.summaryInAnalyticsPanel).toBe(true);
    expect(sectionOrder.summaryInSearchHeader).toBe(false);

    const tracker = page.locator(`#active-sessions [href="/sessions/${ACTIVE_SESSION_ID}"] .active-session-tracker`);
    await expect(tracker).toHaveAttribute('aria-label', 'Active session live stats');
    await expect(tracker).toContainText('Run');
    await expect(tracker).toContainText('Turns');
    await expect(tracker).toContainText('Tools');
    await expect(tracker).not.toContainText('CTX');
    await expect(page.locator('#active-sessions .active-context')).toHaveCount(0);
    await expect(page.locator('#active-sessions [data-session-context]')).toHaveCount(0);
    await expect(page.locator('#active-sessions [role="progressbar"]')).toHaveCount(0);

    await guards.expectClean();
  });

  test('lays out active sessions in a bounded responsive grid', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page, { activeScenarioSequence: ['default', 'default', 'many-active'] });

    for (const viewport of [
      { width: 1440, height: 900 },
      { width: 1600, height: 900 },
    ]) {
      await page.setViewportSize(viewport);
      await gotoDashboard(page);
      await expect(page.locator('#active-sessions')).toContainText('Realtime dashboard smoke run');
      await expectNoHorizontalOverflow(page);

      const single = await readActiveSessionGeometry(page);
      expect(single.cards).toHaveLength(1);
      expect(single.cards[0].left).toBe(single.gridLeft);
      expect(single.cards[0].width).toBeGreaterThan(220);
      expect(single.cards[0].right).toBeLessThanOrEqual(single.gridRight);
      expect(single.cards[0].width).toBeLessThanOrEqual(single.gridWidth);
      expect(single.scrollClientHeight).toBeGreaterThan(0);
      expect(single.scrollHeight).toBeGreaterThan(0);
      expect(single.protrusions).toEqual([]);
    }

    const stableBeforeMany = await readActiveSessionGeometry(page);
    const completedBeforeMany = await page.locator('.completed-table-surface').boundingBox();
    await page.evaluate(() => (window as Window & { loadActiveSessions: () => Promise<void> }).loadActiveSessions());
    await expect(page.locator('#active-sessions')).toContainText('Live queue item 8');
    let many = await readActiveSessionGeometry(page);
    expect(many.cards).toHaveLength(8);
    expect(many.firstRowCount).toBeGreaterThanOrEqual(1);
    expect(many.rowCount).toBeGreaterThan(1);
    expect(many.panelHeight).toBe(stableBeforeMany.panelHeight);
    expect(many.scrollHeight).toBeGreaterThan(many.scrollClientHeight);
    expect(many.cards.every((card) => card.left >= many.gridLeft && card.right <= many.gridRight)).toBe(true);
    expect(many.cards.every((card) => card.width > 220)).toBe(true);
    expect(many.protrusions).toEqual([]);
    const completedAfterMany = await page.locator('.completed-table-surface').boundingBox();
    expect(Math.round(completedAfterMany?.y || 0)).toBe(Math.round(completedBeforeMany?.y || 0));

    const sort = page.locator('#active-session-sort');
    await expect(sort).toHaveValue('recent');
    await sort.focus();
    await expect(sort).toBeFocused();
    await sort.selectOption('tokens');
    await expect(sort).toHaveValue('tokens');
    await expect(page.locator('#active-sessions .active-session-card').first()).toHaveAttribute('data-active-session-id', 'active-parent-008');
    many = await readActiveSessionGeometry(page);
    expect(many.panelHeight).toBe(stableBeforeMany.panelHeight);
    expect(many.scrollHeight).toBeGreaterThan(many.scrollClientHeight);
    const completedAfterSort = await page.locator('.completed-table-surface').boundingBox();
    expect(Math.round(completedAfterSort?.y || 0)).toBe(Math.round(completedBeforeMany?.y || 0));

    for (const viewport of [
      { width: 390, height: 844 },
      { width: 320, height: 568 },
    ]) {
      await page.setViewportSize(viewport);
      await gotoDashboard(page);
      await expect(page.locator('#active-sessions')).toContainText('Live queue item 8');
      await expectNoHorizontalOverflow(page);
      many = await readActiveSessionGeometry(page);
      expect(many.cards).toHaveLength(8);
      expect(many.firstRowCount).toBe(1);
      expect(many.rowCount).toBe(8);
      expect(many.scrollHeight).toBeLessThanOrEqual(many.scrollClientHeight + 1);
      expect(many.cards.every((card) => Math.abs(card.width - many.gridWidth) <= 1)).toBe(true);
      expect(many.bodyScrollWidth).toBeLessThanOrEqual(many.viewportWidth);
      expect(many.protrusions).toEqual([]);
    }

    await guards.expectClean();
  });

  test('contains long active-session text, stats, paths, and child rows without layout overflow', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page, { scenario: 'long-active' });

    for (const viewport of [
      { width: 1440, height: 900 },
      { width: 1100, height: 800 },
      { width: 390, height: 844 },
      { width: 320, height: 568 },
    ]) {
      await page.setViewportSize(viewport);
      await gotoDashboard(page);
      await expect(page.locator('#active-sessions')).toContainText('overflow validation');
      await expectNoHorizontalOverflow(page);

      const geometry = await readActiveSessionGeometry(page);
      expect(geometry.cards).toHaveLength(4);
      expect(geometry.protrusions).toEqual([]);
      if (geometry.viewportWidth > 680) {
        expect(geometry.firstRowCount).toBeGreaterThanOrEqual(1);
        expect(geometry.cards.every((card) => card.left >= geometry.gridLeft && card.right <= geometry.gridRight)).toBe(true);
        expect(geometry.cards.every((card) => card.width > 220)).toBe(true);
      } else {
        expect(geometry.firstRowCount).toBe(1);
        expect(geometry.cards.every((card) => Math.abs(card.width - geometry.gridWidth) <= 1)).toBe(true);
      }

      const containment = await page.evaluate(() => {
        const selectors = [
          '.active-session-title',
          '.active-session-path',
          '.active-tracker-value',
          '.active-tracker-subvalue',
          '.active-child-main',
        ];
        return selectors.flatMap((selector) => Array.from(document.querySelectorAll(selector)).map((el) => {
          const style = window.getComputedStyle(el);
          const rect = el.getBoundingClientRect();
          const parent = el.closest('.active-session-card')?.getBoundingClientRect();
          return {
            selector,
            overflow: style.overflowX || style.overflow,
            textOverflow: style.textOverflow,
            whiteSpace: style.whiteSpace,
            insideCard: parent ? rect.left >= parent.left - 1 && rect.right <= parent.right + 1 : true,
          };
        }));
      });
      expect(containment.every((item) => item.insideCard)).toBe(true);
      expect(containment.some((item) => item.selector === '.active-session-title' && item.overflow === 'hidden' && item.textOverflow === 'ellipsis' && item.whiteSpace === 'nowrap')).toBe(true);
      expect(containment.some((item) => item.selector === '.active-session-path' && item.overflow === 'hidden' && item.textOverflow === 'ellipsis' && item.whiteSpace === 'nowrap')).toBe(true);
      await expect(page.locator('#active-sessions [data-session-context]')).toHaveCount(0);
    }

    await guards.expectClean();
  });

  test('keeps the dashboard search control visible, keyboard reachable, and contained', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page);

    await page.setViewportSize({ width: 1440, height: 900 });
    await gotoDashboard(page);
    await expect(page.locator('#dashboard-search-focus')).toHaveCount(0);
    await expect(page.locator('#dashboard-search-clear')).toHaveCount(0);
    await page.locator('#dashboard-main').evaluate((main) => {
      main.scrollTop = 0;
    });
    await page.evaluate(() => {
      (document.activeElement as HTMLElement | null)?.blur();
    });
    await page.keyboard.press('/');
    await expect(page.locator('#dashboard-session-search')).toBeFocused();
    await expectDashboardSearchInputInView(page);
    await page.evaluate(() => {
      const editable = document.createElement('div');
      editable.id = 'dashboard-contenteditable-probe';
      editable.setAttribute('contenteditable', 'true');
      editable.textContent = 'probe';
      document.body.appendChild(editable);
      editable.focus();
    });
    await expect(page.locator('#dashboard-contenteditable-probe')).toBeFocused();
    await page.keyboard.press('/');
    await expect(page.locator('#dashboard-contenteditable-probe')).toBeFocused();
    await expect(page.locator('#dashboard-contenteditable-probe')).toContainText('/');

    for (const viewport of [
      { width: 390, height: 844 },
      { width: 320, height: 568 },
    ]) {
      await page.setViewportSize(viewport);
      await gotoDashboard(page);

      await expect(page.locator('#dashboard-search-focus')).toHaveCount(0);
      await expect(page.locator('#dashboard-search-clear')).toHaveCount(0);
      await expectNoHorizontalOverflow(page);

      const metrics = await page.locator('#dashboard-refresh-btn').evaluate((el) => {
        const refreshRect = el.getBoundingClientRect();
        const timelineRect = document.getElementById('timeline-toggle-btn')?.getBoundingClientRect();
        return {
          refreshLeft: Math.floor(refreshRect.left),
          refreshRight: Math.ceil(refreshRect.right),
          timelineLeft: Math.floor(timelineRect?.left || 0),
          timelineRight: Math.ceil(timelineRect?.right || 0),
          viewportWidth: window.innerWidth,
        };
      });
      expect(metrics.refreshLeft).toBeGreaterThanOrEqual(0);
      expect(metrics.refreshRight).toBeLessThanOrEqual(metrics.viewportWidth);
      expect(metrics.timelineLeft).toBeGreaterThanOrEqual(0);
      expect(metrics.timelineRight).toBeLessThanOrEqual(metrics.viewportWidth);
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

  test('keeps chart hover labels anchored away from cursor travel', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page);
    await page.setViewportSize({ width: 1280, height: 800 });
    await gotoDashboard(page);

    const canvas = page.locator('#dashboardTokenCumulativeChart');
    await expect(canvas).toBeVisible();
    await canvas.scrollIntoViewIfNeeded();
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

    await page.locator('#dashboardTokenCumulativeChart-log-toggle').click();
    await expect(page.locator('#dashboardTokenCumulativeChart-log-toggle')).toHaveAttribute('aria-pressed', 'true');
    await expectLogAndModelControlsAligned(page);

    await page.locator('#timeline-toggle-btn').click();
    await expect(page.locator('#timeline-sidebar')).toHaveClass(/collapsed/);
    await expect(page.locator('#timeline-toggle-btn')).toHaveAttribute('aria-expanded', 'false');
    await expect(page.locator('#timeline-sidebar')).toHaveAttribute('inert', '');
    await expect(page.locator('#timeline-sidebar')).toHaveAttribute('aria-hidden', 'true');
    await expect(page.locator('html')).toHaveAttribute('data-beacon-timeline-collapsed', 'true');
    await expectDashboardTokenChartReady(page);
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
    await divider.focus();
    await page.keyboard.press('Shift+ArrowLeft');
    await page.waitForFunction(() => Number(localStorage.getItem('beacon-timeline-width') || 0) > 390);
    await expectDashboardTokenChartReady(page);
    const savedWidth = await page.evaluate(() => Number(localStorage.getItem('beacon-timeline-width') || 0));
    expect(savedWidth).toBeGreaterThan(390);
    expect(savedWidth).toBeLessThanOrEqual(700);

    await divider.dblclick();
    expect(await page.evaluate(() => localStorage.getItem('beacon-timeline-width'))).toBe('380');
    await expectDashboardTokenChartReady(page);

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
    await expectDashboardTokenChartReady(page);

    await fillDashboardSearchAndWait(page, 'migration');
    await expect(page.locator('#completed-session-status')).toHaveText(/2 search results/);
    await waitForDashboardSearchRows(page, 2);
    await expect(page.locator('#dashboard-search-clear')).toHaveCount(0);
    await page.keyboard.press('Escape');
    await waitForCompletedRows(page, 30);

    await fillDashboardSearchAndWait(page, 'dashboard payload');
    await expect(page.locator('#completed-session-status')).toHaveText(/1 search result/);
    await waitForDashboardSearchRows(page, 1);
    await expect(page.locator('#completed-sessions tr[data-search-row]').first()).toHaveAttribute('data-session-id', SEARCH_SESSION_ID);
    await page.keyboard.press('Escape');
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
    await page.evaluate(() => {
      const active = document.activeElement;
      if (active instanceof HTMLElement) active.blur();
    });

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
    await expect(page.locator('#timeline-toggle-btn')).toHaveAttribute('aria-controls', 'timeline-sidebar');
    await expect(page.locator('#dashboard-search-reset')).toHaveText('');
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

  test('restores dashboard range, pagination, and scroll from transcript breadcrumb', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);

    const rangeResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() && url.pathname === '/api/dashboard/sessions' && url.searchParams.get('range') === '7d';
    });
    await page.locator('#dashboard-range-control').getByRole('button', { name: '7d' }).click();
    await rangeResponse;
    await expect(page.locator('#dashboard-range-caption')).toHaveText('Last 7 days');

    await scrollDashboardMainToSearch(page);
    const completedRow = page.locator('#completed-sessions tr[data-session-link]').first();
    const firstPageSessionID = await completedRow.getAttribute('data-sort-id');
    expect(firstPageSessionID).toBeTruthy();
    await completedRow.locator('.session-row-open').click();
    await expect(page.locator('#session-inspector')).toBeVisible();
    const before = await readDashboardScroll(page);
    await page.locator('#inspector-full-link').click();
    await expect(page).toHaveURL(new RegExp(`/sessions/${firstPageSessionID}$`));
    await expect(page.locator('.transcript-back-link')).toHaveAttribute('href', /range=7d/);

    await page.locator('.transcript-back-link').click();
    await expect(page.getByRole('heading', { name: 'Beacon Realtime Dashboard' })).toBeVisible();
    await expect(page.locator('#dashboard-range-caption')).toHaveText('Last 7 days');
    await expect(page.locator('#dashboard-range-control').getByRole('button', { name: '7d' })).toHaveAttribute('aria-pressed', 'true');
    await waitForCompletedRows(page, 30);
    await expect(page.locator(`tr[data-sort-id="${firstPageSessionID}"]`)).toBeVisible();
    await page.waitForFunction((expected) => {
      const owner = document.getElementById('dashboard-main');
      return owner ? Math.abs(owner.scrollTop - expected) <= 4 : false;
    }, before.dashboardTop);
    const after = await readDashboardScroll(page);
    expect(Math.abs(after.dashboardTop - before.dashboardTop)).toBeLessThanOrEqual(4);
    expect(after.windowY).toBe(0);
    expect(after.mainContentTop).toBe(0);

    const tokensSortButton = page.locator('#completed-table th[data-sort-key="tokens"]').getByRole('button', { name: 'Tokens' });
    const tokensDescResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() &&
        url.pathname === '/api/dashboard/sessions' &&
        url.searchParams.get('sort') === 'tokens' &&
        url.searchParams.get('direction') === 'desc' &&
        url.searchParams.get('offset') === '0';
    });
    await tokensSortButton.click();
    await tokensDescResponse;
    await waitForCompletedRows(page, 30);
    const tokensAscResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() &&
        url.pathname === '/api/dashboard/sessions' &&
        url.searchParams.get('sort') === 'tokens' &&
        url.searchParams.get('direction') === 'asc' &&
        url.searchParams.get('offset') === '0';
    });
    await tokensSortButton.click();
    await tokensAscResponse;
    await waitForCompletedRows(page, 30);
    await expect(page.locator('#completed-table th[data-sort-key="tokens"]')).toHaveAttribute('aria-sort', 'ascending');

    const nextResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() &&
        url.pathname === '/api/dashboard/sessions' &&
        url.searchParams.get('sort') === 'tokens' &&
        url.searchParams.get('direction') === 'asc' &&
        url.searchParams.get('offset') === '30';
    });
    await page.locator('.json-page-btn', { hasText: 'Next' }).click();
    await nextResponse;
    await waitForCompletedRows(page, 1);
    await expect(page.locator(`tr[data-sort-id="${TEST_SESSION_ID}"]`)).toBeVisible();
    await page.locator(`tr[data-sort-id="${TEST_SESSION_ID}"] .session-row-open`).click();
    await expect(page.locator('#session-inspector')).toBeVisible();
    await page.locator('#inspector-full-link').click();
    await expect(page).toHaveURL(new RegExp(`/sessions/${TEST_SESSION_ID}$`));
    await expect(page.locator('.transcript-back-link')).toHaveAttribute('href', /range=7d/);
    await expect(page.locator('.transcript-back-link')).toHaveAttribute('href', /offset=30/);
    await expect(page.locator('.transcript-back-link')).toHaveAttribute('href', /sort=tokens/);
    await expect(page.locator('.transcript-back-link')).toHaveAttribute('href', /dir=asc/);

    await page.locator('.transcript-back-link').click();
    await expect(page.getByRole('heading', { name: 'Beacon Realtime Dashboard' })).toBeVisible();
    await waitForCompletedRows(page, 1);
    await expect(page.locator(`tr[data-sort-id="${TEST_SESSION_ID}"]`)).toBeVisible();
    await expect(page.locator('#completed-table th[data-sort-key="tokens"]')).toHaveAttribute('aria-sort', 'ascending');
    await page.waitForFunction(() => new URL(window.location.href).searchParams.get('offset') === '30');
    await page.waitForFunction(() => new URL(window.location.href).searchParams.get('sort') === 'tokens');
    await page.waitForFunction(() => new URL(window.location.href).searchParams.get('dir') === 'asc');

    await guards.expectClean();
  });

  test('restores search state and activity filters through transcript breadcrumbs', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page);
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);

    await fillDashboardSearchAndWait(page, 'many');
    await waitForDashboardSearchRows(page, 30);
    const moreResponse = waitForDashboardSearchResponse(page, (url) => url.searchParams.get('q') === 'many' && url.searchParams.get('limit') === '60');
    await page.getByRole('button', { name: 'Show more' }).click();
    await moreResponse;
    await waitForDashboardSearchRows(page, 35);
    await page.locator('#completed-sessions a[data-transcript-link]').first().click();
    await expect(page).toHaveURL(/\/sessions\/session-search-/);
    await expect(page.locator('.transcript-back-link')).toHaveAttribute('href', /q=many/);
    await expect(page.locator('.transcript-back-link')).toHaveAttribute('href', /search_limit=60/);

    await page.locator('.transcript-back-link').click();
    await expect(page.getByRole('heading', { name: 'Beacon Realtime Dashboard' })).toBeVisible();
    await expect(page.locator('#dashboard-session-search')).toHaveValue('many');
    await waitForDashboardSearchRows(page, 35);
    await page.waitForFunction(() => new URL(window.location.href).searchParams.get('search_limit') === '60');

    await page.locator('#dashboard-search-reset').click();
    await waitForCompletedRows(page, 30);
    const rangeResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() && url.pathname === '/api/dashboard/activity' && url.searchParams.get('range') === '7d';
    });
    await page.locator('#dashboard-range-control').getByRole('button', { name: '7d' }).click();
    await rangeResponse;
    await page.locator('#timeline-sidebar').getByRole('button', { name: 'Errors' }).click();
    await expect(page.locator('#activity-feed a[data-type="error"], #activity-feed a[data-type="tool_error"]')).toHaveCount(2);
    await page.locator('#activity-feed a[data-transcript-link]').first().click();
    await expect(page).toHaveURL(/\/sessions\/session-completed-/);
    await expect(page.locator('.transcript-back-link')).toHaveAttribute('href', /range=7d/);
    await expect(page.locator('.transcript-back-link')).toHaveAttribute('href', /activity=error/);

    await page.locator('.transcript-back-link').click();
    await expect(page.getByRole('heading', { name: 'Beacon Realtime Dashboard' })).toBeVisible();
    await expect(page.locator('#dashboard-range-caption')).toHaveText('Last 7 days');
    await expect(page.locator('#timeline-sidebar').getByRole('button', { name: 'Errors' })).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#activity-feed a[data-type="error"], #activity-feed a[data-type="tool_error"]')).toHaveCount(2);

    await guards.expectClean();
  });

  test('initializes dashboard from URL state and rejects unsafe return state', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page);

    const completedResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() &&
        url.pathname === '/api/dashboard/sessions' &&
        url.searchParams.get('range') === '7d' &&
        url.searchParams.get('offset') === '30' &&
        url.searchParams.get('sort') === 'tokens' &&
        url.searchParams.get('direction') === 'asc';
    });
    await page.goto('/?range=7d&offset=30&sort=tokens&dir=asc', { waitUntil: 'domcontentloaded' });
    await completedResponse;
    await expect(page.locator('#dashboard-range-caption')).toHaveText('Last 7 days');
    await expect(page.locator('#completed-table th[data-sort-key="tokens"]')).toHaveAttribute('aria-sort', 'ascending');
    await waitForCompletedRows(page, 1);

    const searchResponse = waitForDashboardSearchResponse(page, (url) =>
      url.searchParams.get('q') === 'internal' &&
      url.searchParams.get('event_kind') === 'tool_call' &&
      url.searchParams.get('session_id') === SEARCH_SESSION_ID &&
      url.searchParams.get('sort') === 'oldest' &&
      url.searchParams.get('limit') === '60'
    );
    await page.goto(`/?range=7d&q=internal&event_kind=tool_call&session_id=${SEARCH_SESSION_ID}&search_sort=oldest&search_limit=60&activity=error`, { waitUntil: 'domcontentloaded' });
    await searchResponse;
    await expect(page.locator('#dashboard-session-search')).toHaveValue('internal');
    await expect(page.locator('#dashboard-search-kind')).toHaveValue('tool_call');
    await expect(page.locator('#dashboard-search-session')).toHaveValue(SEARCH_SESSION_ID);
    await expect(page.locator('#dashboard-search-sort')).toHaveValue('oldest');
    await waitForDashboardSearchRows(page, 1);
    await expect(page.locator('#timeline-sidebar').getByRole('button', { name: 'Errors' })).toHaveAttribute('aria-pressed', 'true');

    await page.goto('/?range=bogus&search_limit=999&offset=-1&sort=%3Cscript%3E&dir=sideways&activity=bogus', { waitUntil: 'domcontentloaded' });
    await expect(page.locator('#dashboard-range-caption')).toHaveText('Last 24 hours');
    await expect(page.locator('#dashboard-range-control').getByRole('button', { name: '24h' })).toHaveAttribute('aria-pressed', 'true');
    await waitForCompletedRows(page, 30);

    const legacyRangeResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() &&
        url.pathname === '/api/dashboard/sessions' &&
        url.searchParams.get('state') === 'completed' &&
        url.searchParams.get('range') === '7d';
    });
    await page.goto('/?search_range=7d', { waitUntil: 'domcontentloaded' });
    await legacyRangeResponse;
    await expect(page.locator('#dashboard-range-caption')).toHaveText('Last 7 days');
    await expect(page.locator('#dashboard-range-control').getByRole('button', { name: '7d' })).toHaveAttribute('aria-pressed', 'true');
    await page.waitForFunction(() => new URL(window.location.href).searchParams.get('range') === '7d');
    await page.waitForFunction(() => new URL(window.location.href).searchParams.get('search_range') === null);
    await expect(page.locator('#completed-table')).toHaveAttribute('data-table-mode', 'sessions');

    await page.evaluate(() => {
      sessionStorage.removeItem('beacon-dashboard-return-state-v1');
    });
    await page.goto(`/sessions/${TEST_SESSION_ID}`, { waitUntil: 'domcontentloaded' });
    await expect(page.locator('.transcript-back-link')).toHaveAttribute('href', '/');

    await page.evaluate(() => {
      sessionStorage.setItem('beacon-dashboard-return-state-v1', JSON.stringify({
        url: 'https://evil.example/?range=7d',
        transcriptPath: '/sessions/session-older-001',
        savedAt: Date.now(),
      }));
    });
    await page.goto(`/sessions/${TEST_SESSION_ID}`, { waitUntil: 'domcontentloaded' });
    await expect(page.locator('.transcript-back-link')).toHaveAttribute('href', '/');

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
    await expectDashboardTokenChartReady(page);
    expect(Date.now() - resizeStart).toBeLessThan(800);
  });
});
