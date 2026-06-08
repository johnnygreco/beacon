import { expect, test, type Page } from '@playwright/test';
import {
  ACTIVE_SESSION_ID,
  SEARCH_SESSION_ID,
  TEST_EVENT_ID,
  TEST_SESSION_ID,
  attachPageGuards,
  emitDashboardEvent,
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

async function selectDashboardSearchType(page: Page, type: string) {
  const current = await page.locator('#dashboard-search-kind').inputValue();
  if (current === type) return;
  const response = waitForDashboardSearchResponse(page, (url) => url.searchParams.get('event_kind') === type);
  await page.locator('#dashboard-search-kind').selectOption(type);
  await response;
}

async function fillDashboardEventSearchAndWait(page: Page, value: string) {
  await selectDashboardSearchType(page, 'event');
  return fillDashboardSearchAndWait(
    page,
    value,
    (url) => url.searchParams.get('q') === value && url.searchParams.get('event_kind') === 'event',
  );
}

function waitForDashboardEndpoint(page: Page, path: string, predicate?: (url: URL) => boolean) {
  return page.waitForResponse((response) => {
    const url = new URL(response.url());
    return response.status() === 200 && url.pathname === path && (!predicate || predicate(url));
  });
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
    const scrollStyle = scroll ? window.getComputedStyle(scroll) : null;
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
      return Array.from(card.querySelectorAll('.active-session-card-header, .active-session-title, .active-session-kicker, .active-session-meta-row, .active-session-tracker, .active-session-actions, .active-child-list, .active-child-row, .active-session-path'))
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
      scrollOverflowY: scrollStyle?.overflowY || '',
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
    await expect(page.locator('#dashboard-name-edit')).toHaveCount(0);
    await expect(page.locator('#dashboard-refresh-btn')).toHaveCount(0);
    await expect(page.locator('#dashboard-last-updated')).toHaveCount(0);
    await expect(page.locator('#dashboard-connection-indicator')).toHaveAttribute('aria-label', /Dashboard connection: (Static|Connecting|Live)/);
    await expect(page.locator('[data-dashboard-name-control]')).toContainText('Beacon Realtime Dashboard');
    await expect(page.getByRole('button', { name: 'Beacon Realtime Dashboard' })).toHaveAttribute('id', 'dashboard-title');
    const restingNameMetrics = await page.locator('.dashboard-header').evaluate((header) => {
      const brandRect = header.querySelector('.dashboard-brand-lockup')?.getBoundingClientRect();
      const titleRect = header.querySelector('#dashboard-title')?.getBoundingClientRect();
      return {
        brandRight: brandRect?.right || 0,
        titleLeft: titleRect?.left || 0,
      };
    });
    expect(restingNameMetrics.brandRight).toBeLessThanOrEqual(restingNameMetrics.titleLeft);

    await page.locator('#dashboard-title').focus();
    await expect(page.locator('#dashboard-title')).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(page.locator('#dashboard-name-input')).toBeVisible();
    await expect(page.locator('#dashboard-name-input')).toBeFocused();
    await page.locator('#dashboard-name-input').fill('Cancelled name');
    await page.keyboard.press('Escape');
    await expect(page.locator('#dashboard-name-input')).toBeHidden();
    await expect(page.locator('#dashboard-title')).toBeFocused();
    await expect(page.locator('#dashboard-title')).toHaveText('Beacon Realtime Dashboard');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-name'))).toBeNull();

    await page.locator('#dashboard-title').click();
    await expect(page.locator('#dashboard-name-input')).toBeFocused();
    const editingNameMetrics = await page.locator('.dashboard-header').evaluate((header) => {
      const brandRect = header.querySelector('.dashboard-brand-lockup')?.getBoundingClientRect();
      const inputRect = header.querySelector('#dashboard-name-input')?.getBoundingClientRect();
      return {
        brandRight: brandRect?.right || 0,
        inputLeft: inputRect?.left || 0,
      };
    });
    expect(editingNameMetrics.brandRight).toBeLessThanOrEqual(editingNameMetrics.inputLeft);
    await page.locator('#dashboard-name-input').fill('  Workstation A  ');
    await expect(page).toHaveTitle('Workstation A | Beacon');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-name'))).toBe('Workstation A');
    await page.keyboard.press('Enter');
    await expect(page.locator('#dashboard-title')).toHaveText('Workstation A');

    await page.reload({ waitUntil: 'domcontentloaded' });
    await expect(page.locator('#dashboard-analytics-summary > div')).toHaveCount(4);
    await expect(page.locator('#dashboard-title')).toHaveText('Workstation A');
    await expect(page).toHaveTitle('Workstation A | Beacon');

    await page.locator('#dashboard-title').click();
    await page.locator('#dashboard-name-input').fill('');
    await expect(page).toHaveTitle('Dashboard | Beacon');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-name'))).toBeNull();
    await page.keyboard.press('Enter');
    await expect(page.locator('#dashboard-title')).toHaveText('Beacon Realtime Dashboard');

    await page.setViewportSize({ width: 1280, height: 800 });
    const longName = 'Beacon operations dashboard for west coast release train alpha bravo charlie';
    await page.locator('#dashboard-title').click();
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
    await page.locator('#dashboard-title').click();
    await page.locator('#dashboard-name-input').fill(unsafeName);
    await expect(page).toHaveTitle(`${unsafeName} | Beacon`);
    await page.keyboard.press('Enter');
    await expect(page.locator('#dashboard-title')).toHaveText(unsafeName);
    expect(await page.evaluate(() => (window as Window & { __beaconNameExecuted?: boolean }).__beaconNameExecuted)).toBeUndefined();
    await expect(page.locator('script', { hasText: 'window.__beaconNameExecuted' })).toHaveCount(0);

    for (const status of ['Live', 'Disconnected', 'Static'] as const) {
      await page.evaluate((nextStatus) => {
        (window as Window & { setDashboardConnection: (status: string) => void }).setDashboardConnection(nextStatus);
      }, status);
      await expect(page.locator('#dashboard-connection-label')).toHaveText(status);
      await expect(page.locator('#dashboard-connection-indicator')).toHaveAttribute('data-status', status.toLowerCase());
      await expect(page.locator('#dashboard-connection-indicator')).toHaveAttribute('aria-label', `Dashboard connection: ${status}`);
    }

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
            <h1 id="dashboard-heading">
              <button type="button" id="dashboard-title" data-dashboard-title aria-controls="dashboard-name-input">Configured Station</button>
            </h1>
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
    await expect(page.locator('#dashboard-name-edit')).toHaveCount(0);

    await page.locator('#dashboard-title').click();
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

  test('distinguishes reconnecting and closed dashboard event streams', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page, { mockEventSource: true });
    await gotoDashboard(page);

    await page.evaluate(() => {
      const source = (window as unknown as {
        __beaconEventSources: Array<{ readyState: number; onerror: ((event: Event) => void) | null }>;
      }).__beaconEventSources[0];
      source.readyState = 0;
      source.onerror?.(new Event('error'));
    });
    await expect(page.locator('#dashboard-connection-label')).toHaveText('Connecting');
    await expect(page.locator('#dashboard-connection-indicator')).toHaveAttribute('data-status', 'connecting');

    await page.evaluate(() => {
      const source = (window as unknown as {
        __beaconEventSources: Array<{ readyState: number; onerror: ((event: Event) => void) | null }>;
      }).__beaconEventSources[0];
      source.readyState = 2;
      source.onerror?.(new Event('error'));
    });
    await expect(page.locator('#dashboard-connection-label')).toHaveText('Disconnected');
    await expect(page.locator('#dashboard-connection-indicator')).toHaveAttribute('data-status', 'disconnected');

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
      { width: 768, height: 1024 },
      { width: 390, height: 844 },
      { width: 320, height: 568 },
    ]) {
      await page.setViewportSize(viewport);
      await gotoDashboard(page);
      await expectNoHorizontalOverflow(page);
      await expectDashboardTokenChartReady(page);
      await expect(page.getByRole('searchbox', { name: 'Search table sessions and events' })).toHaveAttribute('placeholder', 'Search table sessions and events');
      await expect(page.locator('.dashboard-search-filters')).toHaveAttribute('aria-label', 'Table filters');
      await expect(page.locator('[data-dashboard-table-toolbar]')).toHaveCount(1);
      await expect(page.getByRole('button', { name: 'Refresh completed sessions table' })).toBeVisible();
      await expect(page.getByLabel('Table type')).toHaveValue('session');
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
        const divider = document.getElementById('sidebar-divider');
        const title = document.querySelector('.completed-table-title')?.getBoundingClientRect();
        const search = document.querySelector('.dashboard-table-search')?.getBoundingClientRect();
        const controls = document.querySelector('.dashboard-table-left')?.getBoundingClientRect();
        const controlScroller = document.querySelector('.dashboard-table-controls-scroll');
        const controlScrollerRect = controlScroller?.getBoundingClientRect();
        const tableScroller = document.querySelector('.dashboard-table-scroll');
        const tableScrollerRect = tableScroller?.getBoundingClientRect();
        const table = document.getElementById('completed-table');
        const chart = document.querySelector('.dashboard-table-chart')?.getBoundingClientRect();
        const summary = document.getElementById('dashboard-analytics-summary')?.getBoundingClientRect();
        const activeBoard = document.querySelector('#active-sessions .active-session-board-scroll');
        const activeBoardStyle = activeBoard ? window.getComputedStyle(activeBoard) : null;
        const filterOverflow = Array.from(document.querySelectorAll('.dashboard-search-filters, .dashboard-search-filter-group')).some((el) => {
          return el.scrollWidth > el.clientWidth + 1;
        });
        const summaryTextOverflow = Array.from(document.querySelectorAll('.dashboard-summary-label, .dashboard-summary-value, .dashboard-summary-subvalue')).some((el) => {
          return el.scrollWidth > el.clientWidth + 1;
        });
        return {
          wrapWidth: Math.round(wrap?.width || 0),
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
          dividerValueMax: Number(divider?.getAttribute('aria-valuemax') || 0),
          titleTop: Math.round(title?.top || 0),
          titleBottom: Math.round(title?.bottom || 0),
          searchTop: Math.round(search?.top || 0),
          controlsTop: Math.round(controls?.top || 0),
          controlsBottom: Math.round(controls?.bottom || 0),
          controlScrollerRight: Math.round(controlScrollerRect?.right || 0),
          controlScrollerClientWidth: Math.round((controlScroller as HTMLElement | null)?.clientWidth || 0),
          controlScrollerScrollWidth: Math.round((controlScroller as HTMLElement | null)?.scrollWidth || 0),
          tableScrollerRight: Math.round(tableScrollerRect?.right || 0),
          tableScrollerClientWidth: Math.round((tableScroller as HTMLElement | null)?.clientWidth || 0),
          tableScrollerScrollWidth: Math.round((tableScroller as HTMLElement | null)?.scrollWidth || 0),
          tableMinWidth: window.getComputedStyle(table || document.body).minWidth,
          summaryLeft: Math.round(summary?.left || 0),
          summaryRight: Math.round(summary?.right || 0),
          filterOverflow,
          summaryTextOverflow,
          activeBoardOverflowY: activeBoardStyle?.overflowY || '',
          activeBoardMaxHeight: activeBoardStyle?.maxHeight || '',
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
      expect(shellLayout.controlScrollerRight).toBeLessThanOrEqual(shellLayout.completedRight + 1);
      expect(shellLayout.tableScrollerRight).toBeLessThanOrEqual(shellLayout.completedRight + 1);
      if (viewport.width > 640) {
        expect(shellLayout.controlScrollerScrollWidth).toBeGreaterThanOrEqual(900);
        expect(shellLayout.tableScrollerScrollWidth).toBeGreaterThanOrEqual(1000);
        expect(shellLayout.tableMinWidth).toBe('1056px');
      } else {
        expect(shellLayout.controlScrollerScrollWidth).toBeLessThanOrEqual(shellLayout.controlScrollerClientWidth + 1);
      }
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
      if (viewport.width > 1100) {
        expect(shellLayout.dividerValueMax).toBe(Math.min(700, Math.max(200, Math.floor(shellLayout.wrapWidth * 0.5))));
      }
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
        expect(shellLayout.activeBoardOverflowY).toBe('visible');
        expect(shellLayout.activeBoardMaxHeight).toBe('none');
      }
      await expect(page.locator('#sidebar')).toHaveCount(0);
      await expect(page.locator('nav a[href="/search"]')).toHaveCount(0);
      await expect(page.locator('#dashboard-session-search')).toBeVisible();
      await expect(page.locator('#dashboard-search #dashboard-range-control')).toHaveCount(1);
      await expect(page.locator('.dashboard-analytics-panel #dashboard-chart-range-control')).toHaveCount(1);
      await expect(page.locator('[data-search-range]')).toHaveCount(0);
    }

    await guards.expectClean();
  });

  test('uses fleet chips as global dashboard scope controls', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page, { scenario: 'many-active' });
    await page.setViewportSize({ width: 1440, height: 900 });
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);
    await expect(page.locator('#dashboard-fleet .dashboard-fleet-node')).toHaveCount(3);
    await expect(page.locator('#dashboard-header-fleet-metrics')).toContainText('active');
    await expect(page.locator('#active-sessions .active-session-card')).toHaveCount(8);

    const nodeScope = (url: URL) => url.searchParams.get('node_id') === 'mac-mini-codex';
    const scopedResponses = [
      waitForDashboardEndpoint(page, '/api/dashboard/fleet', nodeScope),
      waitForDashboardEndpoint(page, '/api/dashboard/sessions', (url) => nodeScope(url) && url.searchParams.get('state') === 'active'),
      waitForDashboardEndpoint(page, '/api/dashboard/sessions', (url) => nodeScope(url) && url.searchParams.get('state') === 'completed'),
      waitForDashboardEndpoint(page, '/api/dashboard/activity', nodeScope),
      waitForDashboardEndpoint(page, '/api/dashboard/charts', nodeScope),
    ];
    await page.locator('.dashboard-fleet-node-main[data-dashboard-scope-value="mac-mini-codex"]').click();
    await Promise.all(scopedResponses);

    await page.waitForFunction(() => new URL(window.location.href).searchParams.get('node_id') === 'mac-mini-codex');
    await expect(page.locator('#dashboard-scope-chips')).toContainText('mac-mini-codex');
    await expect(page.locator('#dashboard-fleet .dashboard-fleet-node')).toHaveCount(1);
    await expect(page.locator('#dashboard-fleet')).toContainText('Mac mini Codex');
    await expect(page.locator('#active-sessions .active-session-card')).toHaveCount(3);
    await waitForCompletedRows(page, 15);
    await expect(page.locator('#activity-feed .activity-bar-item')).toHaveCount(2);
    await expect(page.locator('#active-sessions')).toContainText('mac-mini-codex');
    await expectNoHorizontalOverflow(page);

    const runtimeResponses = [
      waitForDashboardEndpoint(page, '/api/dashboard/fleet', (url) => nodeScope(url) && url.searchParams.get('runtime') === 'codex'),
      waitForDashboardEndpoint(page, '/api/dashboard/sessions', (url) => nodeScope(url) && url.searchParams.get('runtime') === 'codex' && url.searchParams.get('state') === 'active'),
      waitForDashboardEndpoint(page, '/api/dashboard/sessions', (url) => nodeScope(url) && url.searchParams.get('runtime') === 'codex' && url.searchParams.get('state') === 'completed'),
      waitForDashboardEndpoint(page, '/api/dashboard/activity', (url) => nodeScope(url) && url.searchParams.get('runtime') === 'codex'),
      waitForDashboardEndpoint(page, '/api/dashboard/charts', (url) => nodeScope(url) && url.searchParams.get('runtime') === 'codex'),
    ];
    await page.locator('.dashboard-fleet-runtime[data-dashboard-scope-value="codex"]').click();
    await Promise.all(runtimeResponses);
    await page.waitForFunction(() => {
      const url = new URL(window.location.href);
      return url.searchParams.get('node_id') === 'mac-mini-codex' && url.searchParams.get('runtime') === 'codex';
    });
    await expect(page.locator('#dashboard-scope-chips')).toContainText('codex');
    await waitForCompletedRows(page, 15);
    await expect(page.locator('#activity-feed .activity-bar-item')).toHaveCount(2);

    const clearResponses = [
      waitForDashboardEndpoint(page, '/api/dashboard/fleet', (url) => !url.searchParams.has('node_id') && !url.searchParams.has('runtime')),
      waitForDashboardEndpoint(page, '/api/dashboard/sessions', (url) => !url.searchParams.has('node_id') && !url.searchParams.has('runtime') && url.searchParams.get('state') === 'active'),
      waitForDashboardEndpoint(page, '/api/dashboard/sessions', (url) => !url.searchParams.has('node_id') && !url.searchParams.has('runtime') && url.searchParams.get('state') === 'completed'),
      waitForDashboardEndpoint(page, '/api/dashboard/activity', (url) => !url.searchParams.has('node_id') && !url.searchParams.has('runtime')),
      waitForDashboardEndpoint(page, '/api/dashboard/charts', (url) => !url.searchParams.has('node_id') && !url.searchParams.has('runtime')),
    ];
    await page.locator('[data-dashboard-scope-clear="all"]').click();
    await Promise.all(clearResponses);
    await page.waitForFunction(() => {
      const url = new URL(window.location.href);
      return !url.searchParams.has('node_id') && !url.searchParams.has('runtime');
    });
    await expect(page.locator('#dashboard-fleet .dashboard-fleet-node')).toHaveCount(3);
    await expect(page.locator('#active-sessions .active-session-card')).toHaveCount(8);
    await waitForCompletedRows(page, 30);
    await expect(page.locator('#activity-feed .activity-bar-item')).toHaveCount(4);

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
    expect(await tracker.evaluate((node) => Boolean(node.closest('.active-session-card-header')))).toBe(true);
    await expect(tracker).not.toContainText('CTX');
    await expect(page.locator('#active-sessions .active-context')).toHaveCount(0);
    await expect(page.locator('#active-sessions [data-session-context]')).toHaveCount(0);
    await expect(page.locator('#active-sessions [role="progressbar"]')).toHaveCount(0);

    await guards.expectClean();
  });

  test('lays out active sessions as compact full-width rows with pin controls', async ({ page }) => {
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
      expect(Math.abs(single.cards[0].width - single.gridWidth)).toBeLessThanOrEqual(1);
      expect(single.scrollClientHeight).toBeGreaterThan(0);
      expect(single.scrollHeight).toBeGreaterThan(0);
      expect(single.protrusions).toEqual([]);
      await expect(page.locator('#active-sessions .active-session-action-btn')).toHaveCount(1);
    }

    const stableBeforeMany = await readActiveSessionGeometry(page);
    const completedBeforeMany = await page.locator('.completed-table-surface').boundingBox();
    await page.evaluate(() => (window as Window & { loadActiveSessions: () => Promise<void> }).loadActiveSessions());
    await expect(page.locator('#active-sessions')).toContainText('Live queue item 8');
    let many = await readActiveSessionGeometry(page);
    expect(many.cards).toHaveLength(8);
    expect(many.firstRowCount).toBe(1);
    expect(many.rowCount).toBe(8);
    expect(many.panelHeight).toBe(stableBeforeMany.panelHeight);
    expect(many.scrollHeight).toBeGreaterThan(many.scrollClientHeight);
    expect(many.cards.every((card) => card.left >= many.gridLeft && card.right <= many.gridRight)).toBe(true);
    expect(many.cards.every((card) => Math.abs(card.width - many.gridWidth) <= 1)).toBe(true);
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
    await page.waitForFunction(() => new URL(window.location.href).searchParams.get('active_sort') === 'tokens');
    many = await readActiveSessionGeometry(page);
    expect(many.panelHeight).toBe(stableBeforeMany.panelHeight);
    expect(many.scrollHeight).toBeGreaterThan(many.scrollClientHeight);
    const completedAfterSort = await page.locator('.completed-table-surface').boundingBox();
    expect(Math.round(completedAfterSort?.y || 0)).toBe(Math.round(completedBeforeMany?.y || 0));

    const activeOrder = async () => page.locator('#active-sessions .active-session-card').evaluateAll((cards) =>
      cards.map((card) => card.getAttribute('data-active-session-id') || ''),
    );
    const orderBeforeUnpinnedMove = await activeOrder();
    const unpinnedMoveIndex = orderBeforeUnpinnedMove.indexOf('active-parent-003');
    expect(unpinnedMoveIndex).toBeGreaterThan(0);
    const unpinnedMoveUp = page.locator('[data-active-session-id="active-parent-003"] [data-active-session-action="move-up"]');
    await expect(unpinnedMoveUp).toBeEnabled();
    await unpinnedMoveUp.click();
    const orderAfterUnpinnedMove = await activeOrder();
    expect(orderAfterUnpinnedMove[unpinnedMoveIndex - 1]).toBe('active-parent-003');
    expect(orderAfterUnpinnedMove[unpinnedMoveIndex]).toBe(orderBeforeUnpinnedMove[unpinnedMoveIndex - 1]);
    await page.evaluate(() => {
      const dashboard = window as unknown as {
        clearActiveSessionManualOrder?: () => void;
        loadActiveSessions: () => Promise<void>;
      };
      dashboard.clearActiveSessionManualOrder?.();
      return dashboard.loadActiveSessions();
    });
    await expect(page.locator('#active-sessions .active-session-card').first()).toHaveAttribute('data-active-session-id', 'active-parent-008');

    await page.locator('[data-active-session-id="active-parent-003"] [data-active-session-action="toggle-pin"]').focus();
    await page.keyboard.press('Enter');
    await expect(page.locator('#active-sessions .active-session-card').first()).toHaveAttribute('data-active-session-id', 'active-parent-003');
    await expect(page.locator('[data-active-session-id="active-parent-003"] [data-active-session-action="toggle-pin"]')).toBeFocused();
    expect(await page.evaluate(() => localStorage.getItem('beacon-active-session-prefs-v1'))).toContain('active-parent-003');
    await page.locator('[data-active-session-id="active-parent-005"] [data-active-session-action="toggle-pin"]').click();
    await expect(page.locator('#active-sessions .active-session-card').nth(0)).toHaveAttribute('data-active-session-id', 'active-parent-005');
    await expect(page.locator('#active-sessions .active-session-card').nth(1)).toHaveAttribute('data-active-session-id', 'active-parent-003');
    await page.locator('[data-active-session-id="active-parent-003"] [data-active-session-action="move-up"]').click();
    await expect(page.locator('#active-sessions .active-session-card').nth(0)).toHaveAttribute('data-active-session-id', 'active-parent-003');
    await expect(page.locator('#active-sessions .active-session-card').nth(1)).toHaveAttribute('data-active-session-id', 'active-parent-005');
    await page.reload({ waitUntil: 'domcontentloaded' });
    await expect(page.locator('#active-session-sort')).toHaveValue('tokens');
    await expect(page.locator('#active-sessions .active-session-card').nth(0)).toHaveAttribute('data-active-session-id', 'active-parent-003');
    await expect(page.locator('#active-sessions .active-session-card').nth(1)).toHaveAttribute('data-active-session-id', 'active-parent-005');
    await page.evaluate(() => localStorage.setItem('beacon-active-session-prefs-v1', JSON.stringify({ pinned: ['active-parent-005', 'stale-ended', 'active-parent-003'] })));
    await page.reload({ waitUntil: 'domcontentloaded' });
    await expect(page.locator('#active-sessions .active-session-card').nth(0)).toHaveAttribute('data-active-session-id', 'active-parent-005');
    await expect(page.locator('#active-sessions .active-session-card').nth(1)).toHaveAttribute('data-active-session-id', 'active-parent-003');
    await page.locator('[data-active-session-id="active-parent-003"] [data-active-session-action="move-up"]').click();
    await expect(page.locator('#active-sessions .active-session-card').nth(0)).toHaveAttribute('data-active-session-id', 'active-parent-003');
    await expect(page.locator('#active-sessions .active-session-card').nth(1)).toHaveAttribute('data-active-session-id', 'active-parent-005');
    await expect(page.locator('[data-active-session-id="active-parent-003"] [data-active-session-action="toggle-pin"]')).toBeFocused();
    expect(await page.evaluate(() => localStorage.getItem('beacon-active-session-prefs-v1'))).not.toContain('stale-ended');
    await page.locator('[data-active-session-id="active-parent-003"] [data-active-session-action="toggle-pin"]').click();
    await page.locator('[data-active-session-id="active-parent-005"] [data-active-session-action="toggle-pin"]').click();
    await expect(page.locator('#active-sessions .active-session-card').first()).toHaveAttribute('data-active-session-id', 'active-parent-008');

    await page.goto('/?active_sort=tokens', { waitUntil: 'domcontentloaded' });
    await expect(page.locator('#active-session-sort')).toHaveValue('tokens');
    await expect(page.locator('#active-sessions .active-session-card').first()).toHaveAttribute('data-active-session-id', 'active-parent-008');

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
      const trackerLayout = await page.locator('#active-sessions .active-session-tracker').first().evaluate((tracker) => {
        const trackerRect = tracker.getBoundingClientRect();
        const cells = Array.from(tracker.querySelectorAll('.active-tracker-cell')).map((cell) => {
          const rect = cell.getBoundingClientRect();
          return {
            left: Math.round(rect.left),
            right: Math.round(rect.right),
          };
        });
        const last = cells[cells.length - 1] || { left: 0, right: 0 };
        return {
          count: cells.length,
          trackerLeft: Math.round(trackerRect.left),
          trackerRight: Math.round(trackerRect.right),
          lastLeft: last.left,
          lastRight: last.right,
        };
      });
      expect(trackerLayout.count).toBe(3);
      expect(trackerLayout.lastLeft).toBeGreaterThanOrEqual(trackerLayout.trackerLeft);
      expect(trackerLayout.lastRight).toBeLessThanOrEqual(trackerLayout.trackerRight);
    }

    await guards.expectClean();
  });

  test('keeps active-session board height fixed across content refreshes', async ({ page }) => {
    const guards = attachPageGuards(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await installDashboardFixtures(page, { activeScenarioSequence: ['default', 'many-active', 'long-active'] });
    await gotoDashboard(page);

    await expect(page.locator('#active-sessions')).toContainText('Realtime dashboard smoke run');
    const initial = await readActiveSessionGeometry(page);
    const completedInitial = await page.locator('.completed-table-surface').boundingBox();
    expect(initial.cards).toHaveLength(1);
    expect(initial.panelHeight).toBeGreaterThan(0);
    expect(initial.scrollClientHeight).toBeGreaterThan(0);
    expect(['auto', 'scroll']).toContain(initial.scrollOverflowY);

    await page.evaluate(() => (window as Window & { loadActiveSessions: () => Promise<void> }).loadActiveSessions());
    await expect(page.locator('#active-sessions')).toContainText('Live queue item 8');
    const many = await readActiveSessionGeometry(page);
    expect(many.cards).toHaveLength(8);
    expect(Math.abs(many.panelHeight - initial.panelHeight)).toBeLessThanOrEqual(1);
    expect(Math.abs(many.scrollClientHeight - initial.scrollClientHeight)).toBeLessThanOrEqual(1);
    expect(many.scrollHeight).toBeGreaterThan(many.scrollClientHeight);
    expect(['auto', 'scroll']).toContain(many.scrollOverflowY);
    expect(many.protrusions).toEqual([]);
    const completedAfterMany = await page.locator('.completed-table-surface').boundingBox();
    expect(Math.round(completedAfterMany?.y || 0)).toBe(Math.round(completedInitial?.y || 0));

    const internalScroll = await page.locator('#active-sessions .active-session-board-scroll').evaluate((scroll) => {
      const el = scroll as HTMLElement;
      el.scrollTop = el.scrollHeight;
      return {
        scrollTop: Math.round(el.scrollTop),
        clientHeight: Math.round(el.clientHeight),
        scrollHeight: Math.round(el.scrollHeight),
        dashboardTop: Math.round(document.getElementById('dashboard-main')?.scrollTop || 0),
        windowY: Math.round(window.scrollY || window.pageYOffset || 0),
      };
    });
    expect(internalScroll.scrollHeight).toBeGreaterThan(internalScroll.clientHeight);
    expect(internalScroll.scrollTop).toBeGreaterThan(0);
    expect(internalScroll.dashboardTop).toBe(0);
    expect(internalScroll.windowY).toBe(0);

    await page.evaluate(() => (window as Window & { loadActiveSessions: () => Promise<void> }).loadActiveSessions());
    await expect(page.locator('#active-sessions')).toContainText('overflow validation');
    const long = await readActiveSessionGeometry(page);
    expect(long.cards).toHaveLength(4);
    expect(Math.abs(long.panelHeight - initial.panelHeight)).toBeLessThanOrEqual(1);
    expect(Math.abs(long.scrollClientHeight - initial.scrollClientHeight)).toBeLessThanOrEqual(1);
    expect(long.protrusions).toEqual([]);
    const completedAfterLong = await page.locator('.completed-table-surface').boundingBox();
    expect(Math.round(completedAfterLong?.y || 0)).toBe(Math.round(completedInitial?.y || 0));
    await expectNoHorizontalOverflow(page);

    await guards.expectClean();
  });

  test('renders active sessions in realtime while completed panel request is delayed', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page, {
      mockEventSource: true,
      activeScenarioSequence: ['default', 'many-active'],
    });
    let releaseCompletedRequest: () => void = () => {};
    const completedGate = new Promise<void>((resolve) => {
      releaseCompletedRequest = resolve;
    });
    let holdNextCompletedRequest = true;
    await page.route('**/api/dashboard/sessions**', async (route) => {
      const url = new URL(route.request().url());
      if (holdNextCompletedRequest && url.pathname === '/api/dashboard/sessions' && url.searchParams.get('state') === 'completed') {
        holdNextCompletedRequest = false;
        await completedGate;
      }
      return route.fallback();
    });
    const completedRequest = page.waitForRequest((request) => {
      const url = new URL(request.url());
      return url.pathname === '/api/dashboard/sessions' && url.searchParams.get('state') === 'completed';
    });
    let completedSettled = false;
    const completedResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() && url.pathname === '/api/dashboard/sessions' && url.searchParams.get('state') === 'completed';
    }).then(() => {
      completedSettled = true;
    });

    await gotoDashboard(page);
    await completedRequest;
    await expect(page.locator('#active-sessions')).toContainText('Realtime dashboard smoke run');

    const activeResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() && url.pathname === '/api/dashboard/sessions' && url.searchParams.get('state') === 'active';
    });
    await emitDashboardEvent(page, 'active-sessions-update');
    await activeResponse;
    await expect(page.locator('#active-sessions')).toContainText('Live queue item 8', { timeout: 1000 });
    expect(completedSettled).toBe(false);

    releaseCompletedRequest();
    await completedResponse;
    await waitForCompletedRows(page, 30);
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
        expect(geometry.firstRowCount).toBe(1);
        expect(geometry.rowCount).toBe(4);
        expect(geometry.cards.every((card) => card.left >= geometry.gridLeft && card.right <= geometry.gridRight)).toBe(true);
        expect(geometry.cards.every((card) => Math.abs(card.width - geometry.gridWidth) <= 1)).toBe(true);
      } else {
        expect(geometry.firstRowCount).toBe(1);
        expect(geometry.rowCount).toBe(4);
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

  test('loads the collapsed activity-bar fixture state before dashboard scripts run', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page, { scenario: 'collapsed-activity' });
    await gotoDashboard(page);

    await expect(page.locator('html')).toHaveAttribute('data-beacon-timeline-collapsed', 'true');
    await expect(page.locator('#timeline-sidebar')).toHaveClass(/collapsed/);
    await expect(page.locator('#timeline-sidebar')).toHaveAttribute('inert', '');
    await expect(page.locator('#timeline-sidebar')).toHaveAttribute('aria-hidden', 'true');
    await expect(page.locator('#timeline-toggle-btn')).toHaveAttribute('aria-expanded', 'false');
    await expect(page.locator('#sidebar-divider')).toHaveAttribute('aria-valuenow', '0');
    await expect(page.locator('#sidebar-divider')).toHaveAttribute('aria-valuetext', 'Activity bar collapsed');
    expect(await page.evaluate(() => localStorage.getItem('beacon-timeline-width'))).toBe('0');
    expect(await page.evaluate(() => localStorage.getItem('beacon-timeline-prev-width'))).toBe('420');
    await expectNoHorizontalOverflow(page);

    await guards.expectClean();
  });

  test('loads the resized activity-bar fixture state before dashboard scripts run', async ({ page }) => {
    const guards = attachPageGuards(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await installDashboardFixtures(page, { scenario: 'resized-activity' });
    await gotoDashboard(page);

    await expect(page.locator('#timeline-sidebar')).not.toHaveClass(/collapsed/);
    await expect(page.locator('#timeline-sidebar')).not.toHaveAttribute('inert', '');
    await expect(page.locator('#timeline-toggle-btn')).toHaveAttribute('aria-expanded', 'true');
    await expect(page.locator('#sidebar-divider')).toHaveAttribute('aria-valuenow', '520');
    const sidebarMetrics = await page.evaluate(() => {
      const sidebar = document.getElementById('timeline-sidebar');
      const divider = document.getElementById('sidebar-divider');
      return {
        width: Math.round(sidebar?.getBoundingClientRect().width || 0),
        max: Number(divider?.getAttribute('aria-valuemax') || 0),
      };
    });
    expect(sidebarMetrics.width).toBeGreaterThan(450);
    expect(sidebarMetrics.width).toBeLessThanOrEqual(sidebarMetrics.max + 1);
    expect(await page.evaluate(() => localStorage.getItem('beacon-timeline-width'))).toBe('520');
    await expectNoHorizontalOverflow(page);

    await guards.expectClean();
  });

  test('loads the light theme fixture state before dashboard scripts run', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page, { scenario: 'light-theme' });
    await gotoDashboard(page);

    await expect(page.locator('html')).toHaveAttribute('data-dashboard-theme', 'catppuccin-light');
    await expect(page.locator('#dashboard-theme-select')).toHaveValue('catppuccin');
    await expect(page.locator('#dashboard-appearance-toggle')).toHaveAttribute('aria-checked', 'false');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-theme'))).toBe('catppuccin');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-appearance'))).toBe('light');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-resolved-theme'))).toBe('catppuccin-light');

    await guards.expectClean();
  });

  test('loads the fixed dark theme fixture state before dashboard scripts run', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page, { scenario: 'fixed-dark-theme' });
    await gotoDashboard(page);

    await expect(page.locator('html')).toHaveAttribute('data-dashboard-theme', 'dracula-dark');
    await expect(page.locator('#dashboard-theme-select')).toHaveValue('dracula');
    await expect(page.locator('#dashboard-appearance-toggle')).toBeDisabled();
    await expect(page.locator('#dashboard-appearance-toggle')).toHaveAttribute('aria-checked', 'true');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-theme'))).toBe('dracula');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-appearance'))).toBe('dark');
    expect(await page.evaluate(() => localStorage.getItem('beacon-dashboard-resolved-theme'))).toBe('dracula-dark');

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

      const metrics = await page.locator('#timeline-toggle-btn').evaluate((el) => {
        const timelineRect = document.getElementById('timeline-toggle-btn')?.getBoundingClientRect();
        const titleRect = document.getElementById('dashboard-title')?.getBoundingClientRect();
        return {
          titleLeft: Math.floor(titleRect?.left || 0),
          titleRight: Math.ceil(titleRect?.right || 0),
          timelineLeft: Math.floor(timelineRect?.left || 0),
          timelineRight: Math.ceil(timelineRect?.right || 0),
          viewportWidth: window.innerWidth,
        };
      });
      expect(metrics.titleLeft).toBeGreaterThanOrEqual(0);
      expect(metrics.titleRight).toBeLessThanOrEqual(metrics.viewportWidth);
      expect(metrics.timelineLeft).toBeGreaterThanOrEqual(0);
      expect(metrics.timelineRight).toBeLessThanOrEqual(metrics.viewportWidth);
    }

    await guards.expectClean();
  });

  test('keeps primary dashboard controls reachable in keyboard order', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page, { scenario: 'empty' });
    await page.setViewportSize({ width: 1440, height: 900 });
    await gotoDashboard(page);
    await expect(page.locator('#completed-sessions')).toContainText('No completed sessions');

    await page.evaluate(() => {
      (document.activeElement as HTMLElement | null)?.blur();
    });
    const seen: string[] = [];
    for (let i = 0; i < 50; i += 1) {
      await page.keyboard.press('Tab');
      const key = await page.evaluate(() => {
        const el = document.activeElement as HTMLElement | null;
        if (!el || el === document.body) return '';
        const id = el.id || '';
        const parentId = el.closest('[id]')?.id || '';
        const label = el.getAttribute('aria-label') || el.textContent?.trim().replace(/\s+/g, ' ') || '';
        return id || `${parentId}:${label}`;
      });
      if (key && !seen.includes(key)) seen.push(key);
    }

    const expectedOrder = [
      'dashboard-title',
      'dashboard-theme-select',
      'dashboard-appearance-toggle',
      'timeline-toggle-btn',
      'active-session-sort',
      'dashboard-chart-metric',
      'dashboard-chart-range-control:All',
      'dashboard-chart-refresh-btn',
      'dashboard-session-search',
      'dashboard-range-control:All',
      'dashboard-search-kind',
      'dashboard-search-session',
      'dashboard-search-sort',
      'dashboard-search-reset',
      'dashboard-table-refresh-btn',
      'timeline-sidebar:All',
    ];
    const indexes = expectedOrder.map((key) => seen.indexOf(key));
    expect(indexes.every((index) => index >= 0)).toBe(true);
    for (let i = 1; i < indexes.length; i += 1) {
      expect(indexes[i]).toBeGreaterThan(indexes[i - 1]);
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

  test('keeps high-volume activity inside the fixed activity bar', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page, { scenario: 'error-heavy' });
    await page.setViewportSize({ width: 1440, height: 620 });
    await gotoDashboard(page);

    await expect(page.locator('#activity-feed a[data-transcript-link]')).toHaveCount(11);
    const initial = await page.evaluate(() => {
      const shell = document.getElementById('dashboard-wrap');
      const sidebar = document.getElementById('timeline-sidebar');
      const feed = document.getElementById('activity-feed');
      if (!shell || !sidebar || !feed) throw new Error('Missing activity bar geometry target');
      return {
        shellHeight: Math.round(shell.getBoundingClientRect().height),
        sidebarHeight: Math.round(sidebar.getBoundingClientRect().height),
        feedClientHeight: feed.clientHeight,
        feedScrollHeight: feed.scrollHeight,
        feedOverflowY: getComputedStyle(feed).overflowY,
      };
    });
    expect(Math.abs(initial.sidebarHeight - initial.shellHeight)).toBeLessThanOrEqual(1);
    expect(initial.feedOverflowY).toBe('auto');
    expect(initial.feedScrollHeight).toBeGreaterThan(initial.feedClientHeight + 20);

    const afterScroll = await page.locator('#activity-feed').evaluate((feed) => {
      feed.scrollTop = feed.scrollHeight;
      const sidebar = document.getElementById('timeline-sidebar');
      return {
        feedScrollTop: Math.round(feed.scrollTop),
        sidebarHeight: Math.round(sidebar?.getBoundingClientRect().height || 0),
        windowY: Math.round(window.scrollY),
      };
    });
    expect(afterScroll.feedScrollTop).toBeGreaterThan(0);
    expect(afterScroll.sidebarHeight).toBe(initial.sidebarHeight);
    expect(afterScroll.windowY).toBe(0);

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
    await expect(page.locator('#dashboard-chart-range-caption')).toHaveText('All time');

    const chart24hRequest = page.waitForRequest((request) => {
      const url = new URL(request.url());
      return url.pathname === '/api/dashboard/charts' && url.searchParams.get('chart_range') === '24h';
    });
    await page.locator('#dashboard-chart-range-control').getByRole('button', { name: '24h' }).click();
    await chart24hRequest;
    await expect(page.locator('#dashboard-chart-range-caption')).toHaveText('Last 24 hours');
    await expect(page.locator('#dashboard-chart-range-control').getByRole('button', { name: '24h' })).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#dashboard-range-caption')).toHaveText('Last 7 days');

    const allActivityRequest = page.waitForRequest((request) => {
      const url = new URL(request.url());
      return url.pathname === '/api/dashboard/activity' && url.searchParams.has('activity_range') && url.searchParams.get('activity_range') === '';
    });
    const allCompletedRequest = page.waitForRequest((request) => {
      const url = new URL(request.url());
      return url.pathname === '/api/dashboard/sessions' && url.searchParams.get('state') === 'completed' && url.searchParams.has('completed_range') && url.searchParams.get('completed_range') === '';
    });
    await page.locator('#dashboard-range-control').getByRole('button', { name: 'All' }).click();
    await Promise.all([allActivityRequest, allCompletedRequest]);
    await expect(page.locator('#dashboard-range-caption')).toHaveText('All time');
    await expect(page.locator('#dashboard-range-control').getByRole('button', { name: 'All' })).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#dashboard-chart-range-caption')).toHaveText('Last 24 hours');

    const tableRefreshRequest = page.waitForRequest((request) => {
      const url = new URL(request.url());
      return url.pathname === '/api/dashboard/sessions' && url.searchParams.get('state') === 'completed' && url.searchParams.has('completed_range') && url.searchParams.get('completed_range') === '';
    });
    await page.getByRole('button', { name: 'Refresh completed sessions table' }).click();
    await tableRefreshRequest;
    await waitForCompletedRows(page, 30);

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
    const expectedDividerMax = await page.evaluate(() => {
      const wrap = document.getElementById('dashboard-wrap');
      const wrapWidth = wrap?.offsetWidth || 0;
      return Math.min(700, Math.max(200, Math.floor(wrapWidth * 0.5)));
    });
    await expect(divider).toHaveAttribute('aria-valuemax', String(expectedDividerMax));
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
    await page.keyboard.press('ArrowRight');
    await expect(divider).toHaveAttribute('aria-valuenow', '356');
    expect(await page.evaluate(() => localStorage.getItem('beacon-timeline-width'))).toBe('356');
    await page.keyboard.press('Enter');
    await expect(page.locator('#timeline-sidebar')).toHaveClass(/collapsed/);
    await expect(divider).toHaveAttribute('aria-valuenow', '0');
    await expect(divider).toHaveAttribute('aria-valuetext', 'Activity bar collapsed');
    await page.keyboard.press('Space');
    await expect(page.locator('#timeline-sidebar')).not.toHaveClass(/collapsed/);
    await expect(divider).toHaveAttribute('aria-valuenow', '356');
    await page.keyboard.press('End');
    await expect(divider).toHaveAttribute('aria-valuenow', '380');
    await expectDashboardTokenChartReady(page);

    await fillDashboardEventSearchAndWait(page, 'migration');
    await waitForDashboardSearchRows(page, 1);
    await expect(page.locator('#completed-table-title')).toHaveText('Search Results');
    await expect(page.locator('#completed-session-status')).toHaveText('All time');
    await expect(page.locator('#dashboard-search-clear')).toHaveCount(0);
    await page.keyboard.press('Escape');
    await waitForCompletedRows(page, 30);
    await expect(page.locator('#completed-table')).toHaveAttribute('data-table-mode', 'sessions');
    await expect(page.locator('#dashboard-search-kind')).toHaveValue('session');

    await fillDashboardEventSearchAndWait(page, 'dashboard payload');
    await waitForDashboardSearchRows(page, 1);
    await expect(page.locator('#completed-session-status')).toHaveText('All time');
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
    const subagentToggle = page.locator(`button.json-subagent-toggle[data-session-id="${TEST_SESSION_ID}"]`);
    await expect(subagentToggle).toHaveAttribute('aria-label', 'Toggle 2 subagents for Legacy migration replay');
    await subagentToggle.click();
    await expect(page.locator(`tr[data-parent="${TEST_SESSION_ID}"]`)).toHaveCount(2);
    await expect(subagentToggle).toHaveAttribute('aria-expanded', 'true');
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
    await expect(page.locator('#dashboard-refresh-btn')).toHaveCount(0);
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

  test('opens inspector from active and completed sessions, loads older summaries, and shows transcript content', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page);
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);

    const activeLink = page.locator(`#active-sessions a[href="/sessions/${ACTIVE_SESSION_ID}"]`).first();
    await activeLink.focus();
    await page.keyboard.press('Enter');
    await expect(page.locator('#session-inspector')).toBeVisible();
    await expect(page.locator('#session-inspector')).toHaveAttribute('aria-modal', 'false');
    await expect(page.locator('#dashboard-main')).not.toHaveAttribute('inert', '');
    await expect(page.locator('#sidebar-divider')).not.toHaveAttribute('inert', '');
    await expect(page.locator('#timeline-sidebar')).not.toHaveAttribute('aria-hidden', 'true');
    await expect(page.locator('#inspector-full-link')).toHaveText('View Transcript');
    await expect(page.locator('#inspector-description')).toContainText('Stats and transcript content');
    await expect(page.locator('#inspector-events-title')).toHaveText('Transcript');
    await expect(page.locator('#inspector-events')).toContainText('Read dashboard fixture payload');
    await expect(page.locator('#session-inspector #chat-view')).toBeVisible();
    await expect(page.locator('#inspector-events')).toHaveAttribute('tabindex', '0');
    await expect(page.locator('#inspector-events')).toHaveAttribute('aria-labelledby', 'inspector-events-title');
    await page.locator('#dashboard-session-search').click();
    await expect(page.locator('#session-inspector')).toHaveClass(/hidden/);
    await expect(page.locator('#dashboard-session-search')).toBeFocused();

    await activeLink.focus();
    await page.keyboard.press('Enter');
    await expect(page.locator('#session-inspector')).toBeVisible();
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
    await page.locator('#dashboard-session-search').click();
    await expect(page.locator('#session-inspector')).toHaveClass(/hidden/);
    await expect(page.locator('#dashboard-session-search')).toBeFocused();

    await page.evaluate((id) => {
      (window as unknown as { dashboardSessionIndex: Record<string, unknown>; goToSession: (url: string) => void }).dashboardSessionIndex = {};
      (window as unknown as { goToSession: (url: string) => void }).goToSession(`/sessions/${id}`);
    }, TEST_SESSION_ID);
    await expect(page.locator('#session-inspector')).toBeVisible();
    await expect(page.locator('#inspector-title')).toHaveText('Legacy migration replay');
    await expect(page.locator('#inspector-summary')).toContainText('38m 12s');
    await expect(page.locator('#inspector-summary')).toContainText('123.5K');
    await expect(page.locator('#inspector-summary')).not.toContainText('Loading');
    await expect(page.locator('#session-inspector [aria-label="Close"]')).toBeFocused();
    await expect(page.locator('#session-inspector #chat-view details')).toHaveCount(3);
    await expect(page.locator('#session-inspector #chat-view')).toContainText('internal/views/pages/dashboard.templ');

    await page.locator('#inspector-full-link').click();
    await expect(page).toHaveURL(new RegExp(`/sessions/${TEST_SESSION_ID}$`));
    await expect(page.locator('#btn-collapse-all')).toBeVisible();

    await guards.expectClean();
  });

  test('keeps the quick-view inspector dismissible on narrow viewports', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page);
    await page.setViewportSize({ width: 390, height: 844 });
    await gotoDashboard(page);

    await page.evaluate((id) => {
      (window as unknown as { goToSession: (url: string) => void }).goToSession(`/sessions/${id}`);
    }, TEST_SESSION_ID);
    await expect(page.locator('#session-inspector')).toBeVisible();

    const inspectorBounds = await page.locator('#session-inspector').evaluate((el) => {
      const rect = el.getBoundingClientRect();
      return {
        left: Math.round(rect.left),
        right: Math.round(rect.right),
        width: Math.round(rect.width),
        viewportWidth: window.innerWidth,
      };
    });
    expect(inspectorBounds.left).toBeGreaterThan(0);
    expect(inspectorBounds.right).toBe(inspectorBounds.viewportWidth);
    expect(inspectorBounds.width).toBeLessThan(inspectorBounds.viewportWidth);

    await page.mouse.click(4, 80);
    await expect(page.locator('#session-inspector')).toHaveClass(/hidden/);
    await expect(page.locator('#dashboard-main')).not.toHaveAttribute('inert', '');

    await guards.expectClean();
  });

  test('keeps token analytics range caption stable while refresh is pending', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installDashboardFixtures(page, { chartDelayMs: 700 });
    await page.setViewportSize({ width: 1440, height: 900 });
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);
    await expect(page.locator('#dashboard-chart-range-caption')).toHaveText('All time');

    const allChartsRequest = page.waitForRequest((request) => {
      const url = new URL(request.url());
      return url.pathname === '/api/dashboard/charts' && url.searchParams.has('chart_range') && url.searchParams.get('chart_range') === '';
    });
    const allChartsResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() && url.pathname === '/api/dashboard/charts' && url.searchParams.has('chart_range') && url.searchParams.get('chart_range') === '';
    });
    await page.getByRole('button', { name: 'Refresh token analytics' }).click();
    await allChartsRequest;
    await page.waitForTimeout(100);
    await expect(page.locator('#dashboard-chart-range-caption')).toHaveText('All time');
    await expect(page.locator('#dashboard-chart-range-caption')).not.toContainText(/loading|unable/i);
    await allChartsResponse;
    await expect(page.locator('#dashboard-chart-range-caption')).toHaveText('All time');

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
      return response.ok() && url.pathname === '/api/dashboard/sessions' && url.searchParams.get('completed_range') === '7d';
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

    await fillDashboardEventSearchAndWait(page, 'many');
    await waitForDashboardSearchRows(page, 30);
    const moreResponse = waitForDashboardSearchResponse(page, (url) => url.searchParams.get('q') === 'many' && url.searchParams.get('event_kind') === 'event' && url.searchParams.get('limit') === '60');
    await page.getByRole('button', { name: 'Show more' }).click();
    await moreResponse;
    await waitForDashboardSearchRows(page, 35);
    await page.locator('#completed-sessions a[data-transcript-link]').first().click();
    await expect(page).toHaveURL(/\/sessions\/session-search-/);
    await expect(page.locator('.transcript-back-link')).toHaveAttribute('href', /q=many/);
    await expect(page.locator('.transcript-back-link')).toHaveAttribute('href', /event_kind=event/);
    await expect(page.locator('.transcript-back-link')).toHaveAttribute('href', /search_limit=60/);

    await page.locator('.transcript-back-link').click();
    await expect(page.getByRole('heading', { name: 'Beacon Realtime Dashboard' })).toBeVisible();
    await expect(page.locator('#dashboard-session-search')).toHaveValue('many');
    await expect(page.locator('#dashboard-search-kind')).toHaveValue('event');
    await waitForDashboardSearchRows(page, 35);
    await page.waitForFunction(() => new URL(window.location.href).searchParams.get('search_limit') === '60');

    await page.locator('#dashboard-search-reset').click();
    await waitForCompletedRows(page, 30);
    const rangeResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() && url.pathname === '/api/dashboard/activity' && url.searchParams.get('activity_range') === '7d';
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
        url.searchParams.get('completed_range') === '7d' &&
        url.searchParams.get('offset') === '30' &&
        url.searchParams.get('sort') === 'tokens' &&
        url.searchParams.get('direction') === 'asc';
    });
    const chartResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() &&
        url.pathname === '/api/dashboard/charts' &&
        url.searchParams.get('chart_range') === '1h';
    });
    await page.goto('/?range=7d&chart_range=1h&offset=30&sort=tokens&dir=asc', { waitUntil: 'domcontentloaded' });
    await Promise.all([completedResponse, chartResponse]);
    await expect(page.locator('#dashboard-range-caption')).toHaveText('Last 7 days');
    await expect(page.locator('#dashboard-chart-range-caption')).toHaveText('Last hour');
    await expect(page.locator('#dashboard-chart-range-control').getByRole('button', { name: '1h' })).toHaveAttribute('aria-pressed', 'true');
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

    await page.goto('/?range=bogus&chart_range=bogus&chart_metric=bogus&activity_range=bogus&active_sort=bogus&event_kind=bogus&search_sort=bogus&search_limit=999&offset=-1&sort=%3Cscript%3E&dir=sideways&activity=bogus', { waitUntil: 'domcontentloaded' });
    await expect(page.locator('#dashboard-range-caption')).toHaveText('All time');
    await expect(page.locator('#dashboard-range-control').getByRole('button', { name: 'All' })).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#dashboard-chart-range-control').getByRole('button', { name: 'All' })).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#dashboard-chart-metric')).toHaveValue('total_tokens');
    await expect(page.locator('#active-session-sort')).toHaveValue('recent');
    await waitForCompletedRows(page, 30);
    await page.waitForFunction(() => {
      const params = new URL(window.location.href).searchParams;
      return ['range', 'chart_range', 'chart_metric', 'activity_range', 'active_sort', 'event_kind', 'search_sort', 'search_limit', 'offset', 'sort', 'dir', 'activity']
        .every((name) => !params.has(name));
    });

    const legacyRangeResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() &&
        url.pathname === '/api/dashboard/sessions' &&
        url.searchParams.get('state') === 'completed' &&
        url.searchParams.get('completed_range') === '7d';
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
    await expect(page.locator('#timeline-sidebar')).toHaveClass(/collapsed/);
    await expect(page.locator('#timeline-sidebar')).toHaveAttribute('inert', '');
    await expect(page.locator('#timeline-sidebar')).toHaveAttribute('aria-hidden', 'true');
    await expect(page.locator('#timeline-toggle-btn')).toHaveAttribute('aria-expanded', 'false');
    await expect(page.locator('#timeline-toggle-btn')).toHaveAttribute('aria-label', 'Expand activity bar');
    await expect(page.locator('#sidebar-divider')).toHaveAttribute('aria-valuenow', '0');
    await expect(page.locator('#sidebar-divider')).toHaveAttribute('aria-valuetext', 'Activity bar collapsed');
    const sidebarCanReceiveFocus = await page.locator('#timeline-sidebar .activity-filter-btn').first().evaluate((el) => {
      (el as HTMLElement).focus();
      return document.activeElement === el;
    });
    expect(sidebarCanReceiveFocus).toBe(false);
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

    await expect(page.locator('#activity-bar-title')).toContainText('Activity Bar');
    await expect(page.locator('#timeline-sidebar .activity-bar-filters')).toHaveAttribute('aria-label', 'Activity type filter');
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
    await page.locator('#dashboard-chart-refresh-btn').click();
    await expect(page.locator('#dashboard-analytics-summary')).toContainText('2.6M');
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
    await expect(page.locator('#completed-session-status')).toHaveText('Last hour');
    expect(Date.now() - rangeStart).toBeLessThan(800);

    const searchStart = Date.now();
    await fillDashboardEventSearchAndWait(page, 'migration');
    await waitForDashboardSearchRows(page, 1);
    expect(Date.now() - searchStart).toBeLessThan(800);

    const resizeStart = Date.now();
    await page.locator('#timeline-toggle-btn').click();
    await expectDashboardTokenChartReady(page);
    expect(Date.now() - resizeStart).toBeLessThan(800);
  });
});
