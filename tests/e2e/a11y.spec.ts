import { expect, test, type Page } from '@playwright/test';
import axe from 'axe-core';
import {
  TEST_EVENT_ID,
  TEST_SESSION_ID,
  fillDashboardSearchAndWait,
  gotoDashboard,
  installDashboardFixtures,
  waitForCompletedRows,
  waitForDashboardSearchRows,
} from './fixtures/dashboard';

async function axeSeriousOrCritical(page: Page) {
  await page.evaluate(axe.source);
  const result = await page.evaluate(async () => {
    return await (window as unknown as {
      axe: {
        run: (context: Document, options: unknown) => Promise<{
          violations: Array<{ id: string; impact: string | null; help: string; nodes: Array<{ target: string[] }> }>;
        }>;
      };
    }).axe.run(document, {
      resultTypes: ['violations'],
      runOnly: {
        type: 'tag',
        values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'],
      },
    });
  });
  return result.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical');
}

test.describe('dashboard accessibility', () => {
  test('has no serious or critical axe violations on the default dashboard', async ({ page }) => {
    await installDashboardFixtures(page);
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);

    expect(await axeSeriousOrCritical(page)).toEqual([]);
  });

  test('has no serious or critical axe violations on an empty dashboard', async ({ page }) => {
    await installDashboardFixtures(page, { scenario: 'empty' });
    await gotoDashboard(page);
    await expect(page.locator('#completed-sessions')).toContainText('No completed sessions');

    expect(await axeSeriousOrCritical(page)).toEqual([]);
  });

  test('has no serious or critical axe violations on an error-heavy dashboard', async ({ page }) => {
    await installDashboardFixtures(page, { scenario: 'error-heavy' });
    await gotoDashboard(page);
    await expect(page.locator('#activity-feed')).toContainText('Repeated error burst');

    expect(await axeSeriousOrCritical(page)).toEqual([]);
  });

  test('has no serious or critical axe violations with inspector open', async ({ page }) => {
    await installDashboardFixtures(page);
    await gotoDashboard(page);
    await waitForCompletedRows(page, 30);

    await page.evaluate((id) => {
      (window as unknown as { dashboardSessionIndex: Record<string, unknown>; goToSession: (url: string) => void }).dashboardSessionIndex = {};
      (window as unknown as { goToSession: (url: string) => void }).goToSession(`/sessions/${id}`);
    }, TEST_SESSION_ID);
    await expect(page.locator('#session-inspector')).toBeVisible();
    await expect(page.locator('#inspector-summary')).not.toContainText('Loading');

    expect(await axeSeriousOrCritical(page)).toEqual([]);
  });

  test('has no serious or critical axe violations with collapsed activity bar', async ({ page }) => {
    await installDashboardFixtures(page);
    await gotoDashboard(page);
    await page.locator('#timeline-toggle-btn').click();
    await expect(page.locator('#timeline-sidebar')).toHaveAttribute('inert', '');
    await expect(page.locator('#timeline-sidebar')).toHaveAttribute('aria-hidden', 'true');

    expect(await axeSeriousOrCritical(page)).toEqual([]);
  });

  test('has no serious or critical axe violations on the mobile dashboard', async ({ page }) => {
    await installDashboardFixtures(page);
    await page.setViewportSize({ width: 390, height: 844 });
    await gotoDashboard(page);

    expect(await axeSeriousOrCritical(page)).toEqual([]);
  });

  test('has no serious or critical axe violations on the themed transcript', async ({ page }) => {
    await installDashboardFixtures(page);
    await gotoDashboard(page);
    await page.locator('#dashboard-theme-select').selectOption('catppuccin');
    await page.locator('#dashboard-appearance-toggle').click();

    await page.goto(`/sessions/${TEST_SESSION_ID}`, { waitUntil: 'domcontentloaded' });
    await expect(page.locator('#transcript-wrap')).toBeVisible();

    expect(await axeSeriousOrCritical(page)).toEqual([]);
  });

  test('has no serious or critical axe violations with transcript annotation drawer open', async ({ page }) => {
    await installDashboardFixtures(page);
    await page.goto(`/sessions/${TEST_SESSION_ID}`, { waitUntil: 'domcontentloaded' });
    await page.locator(`#${TEST_EVENT_ID} [data-annotation-button]`).first().click();
    await expect(page.locator('#annotation-drawer')).toBeVisible();
    await expect(page.locator('[data-annotation-form] textarea[name="note"]')).toBeFocused();

    expect(await axeSeriousOrCritical(page)).toEqual([]);
  });

  test('has no serious or critical axe violations on mobile annotation failure state', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 760 });
    await installDashboardFixtures(page, { annotationsUnavailable: true });
    await page.goto(`/sessions/${TEST_SESSION_ID}`, { waitUntil: 'domcontentloaded' });
    await page.getByRole('button', { name: 'Annotate session' }).click();
    await expect(page.locator('#annotation-drawer')).toBeVisible();
    await expect(page.locator('[data-annotation-list]')).toContainText('Unable to load annotations');

    expect(await axeSeriousOrCritical(page)).toEqual([]);
  });

  test('has no serious or critical axe violations on dashboard search results', async ({ page }) => {
    await installDashboardFixtures(page);
    await gotoDashboard(page);
    const typeResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() && url.pathname === '/api/dashboard/search' && url.searchParams.get('event_kind') === 'event';
    });
    await page.locator('#dashboard-search-kind').selectOption('event');
    await typeResponse;
    await fillDashboardSearchAndWait(page, 'many', (url) => url.searchParams.get('q') === 'many' && url.searchParams.get('event_kind') === 'event');
    await waitForDashboardSearchRows(page, 30);

    expect(await axeSeriousOrCritical(page)).toEqual([]);
  });
});
