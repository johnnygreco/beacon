import { expect, test } from '@playwright/test';
import {
  TEST_SESSION_ID,
  expectEqualDashboardChartHeights,
  gotoDashboard,
  installDashboardFixtures,
  visualMasks,
  waitForCompletedRows,
} from './fixtures/dashboard';

test.describe('dashboard visual regression baselines', () => {
  test('default populated dashboard at desktop width', async ({ page }) => {
    await installDashboardFixtures(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);
    await expectEqualDashboardChartHeights(page);

    await expect(page.locator('#dashboard-wrap')).toHaveScreenshot('dashboard-default-desktop.png', {
      mask: visualMasks(page),
    });
  });

  test('empty dashboard state', async ({ page }) => {
    await installDashboardFixtures(page, { scenario: 'empty' });
    await page.setViewportSize({ width: 1440, height: 900 });
    await gotoDashboard(page);
    await expect(page.locator('#completed-sessions')).toContainText('No completed sessions');
    await expect(page.locator('#dashboard-wrap')).toHaveScreenshot('dashboard-empty.png', {
      mask: visualMasks(page),
    });
  });

  test('error-heavy dashboard state', async ({ page }) => {
    await installDashboardFixtures(page, { scenario: 'error-heavy' });
    await page.setViewportSize({ width: 1440, height: 900 });
    await gotoDashboard(page);
    await expect(page.locator('#activity-feed')).toContainText('Repeated error burst');
    await expect(page.locator('#dashboard-wrap')).toHaveScreenshot('dashboard-error-heavy.png', {
      mask: visualMasks(page),
    });
  });

  test('many active sessions state', async ({ page }) => {
    await installDashboardFixtures(page, { scenario: 'many-active' });
    await page.setViewportSize({ width: 1440, height: 900 });
    await gotoDashboard(page);
    await expect(page.locator('#active-sessions')).toContainText('Live queue item 8');
    await expect(page.locator('#active-sessions')).toHaveScreenshot('dashboard-many-active.png');
  });

  test('table search, chart controls, timeline states, and inspector payload', async ({ page }) => {
    await installDashboardFixtures(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);

    await page.locator('#dashboard-session-search').fill('migration');
    await waitForCompletedRows(page, 1);
    await expect(page.locator('#completed-table')).toHaveScreenshot('dashboard-table-search.png');

    await expect(page.locator('xpath=//canvas[@id="dashboardTokenCumulativeChart"]/ancestor::div[contains(@class,"bg-gray-800")][1]')).toHaveScreenshot('dashboard-chart-controls.png');

    await page.locator('#timeline-toggle-btn').click();
    await expect(page.locator('#timeline-sidebar')).toHaveClass(/collapsed/);
    await expect(page.locator('#dashboard-wrap')).toHaveScreenshot('dashboard-timeline-collapsed.png', {
      mask: visualMasks(page),
    });

    await page.locator('#timeline-toggle-btn').click();
    await page.waitForFunction(() => {
      const sidebar = document.getElementById('timeline-sidebar');
      return sidebar ? Math.round(sidebar.getBoundingClientRect().width) >= 370 : false;
    });
    const divider = page.locator('#sidebar-divider');
    const box = await divider.boundingBox();
    if (box) {
      const dragY = box.y + box.height / 2;
      await page.mouse.move(box.x + 1, dragY);
      await page.mouse.down();
      await page.mouse.move(980, dragY);
      await page.mouse.up();
    }
    await page.waitForFunction(() => Number(localStorage.getItem('beacon-timeline-width') || 0) > 390);
    await expect(page.locator('#dashboard-wrap')).toHaveScreenshot('dashboard-timeline-resized.png', {
      mask: visualMasks(page),
    });

    await page.evaluate((id) => {
      (window as unknown as { dashboardSessionIndex: Record<string, unknown>; goToSession: (url: string) => void }).dashboardSessionIndex = {};
      (window as unknown as { goToSession: (url: string) => void }).goToSession(`/sessions/${id}`);
    }, TEST_SESSION_ID);
    await expect(page.locator('#session-inspector')).toBeVisible();
    await page.locator('#session-inspector .payload-btn').first().click();
    await expect(page.locator('#session-inspector')).toHaveScreenshot('dashboard-inspector-payload.png');
  });

  test('mobile dashboard and transcript view', async ({ page }) => {
    await installDashboardFixtures(page);
    await page.setViewportSize({ width: 390, height: 844 });
    await gotoDashboard(page);
    await expect(page.locator('#dashboard-wrap')).toHaveScreenshot('dashboard-mobile.png', {
      mask: visualMasks(page),
    });

    await page.goto(`/sessions/${TEST_SESSION_ID}#event-older-001`, { waitUntil: 'domcontentloaded' });
    await expect(page.locator('#chat-view details')).toHaveCount(3);
    await expect(page.locator('main')).toHaveScreenshot('transcript-mobile.png');
  });

  test('desktop transcript view inherits dashboard theme', async ({ page }) => {
    await installDashboardFixtures(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await gotoDashboard(page);
    await page.locator('#dashboard-theme-select').selectOption('catppuccin');
    await page.locator('#dashboard-appearance-toggle').click();
    await expect(page.locator('html')).toHaveAttribute('data-dashboard-theme', 'catppuccin-light');

    await page.goto(`/sessions/${TEST_SESSION_ID}#event-older-001`, { waitUntil: 'domcontentloaded' });
    await expect(page.locator('html')).toHaveAttribute('data-dashboard-theme', 'catppuccin-light');
    await expect(page.locator('#sidebar')).toHaveCount(0);
    await expect(page.locator('#transcript-wrap')).toHaveScreenshot('transcript-desktop-themed.png');
  });
});
