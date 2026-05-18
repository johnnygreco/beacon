import { expect, test } from '@playwright/test';
import { attachPageGuards, expectNoHorizontalOverflow } from './fixtures/dashboard';
import { SEARCH_SESSION_ID, installSearchFixtures } from './fixtures/search';

async function gotoSearch(page: import('@playwright/test').Page) {
  await page.goto('/search', { waitUntil: 'domcontentloaded' });
  await expect(page.getByRole('heading', { name: 'Search' })).toBeVisible();
  await expect(page.locator('#search-input')).toBeVisible();
}

async function expectSearchVerticalFlow(page: import('@playwright/test').Page) {
  const overlaps = await page.evaluate(() => {
    const selectors = [
      '.search-topbar',
      '.search-title-row',
      '.search-form',
      '.search-filter-bar',
      '.search-results-status',
      ...Array.from(document.querySelectorAll('.search-result-card')).slice(0, 12).map((_, index) => `.search-result-card:nth-of-type(${index + 1})`),
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

test.describe('search workflows', () => {
  test('exercises query, filters, clearing, reset, and pagination', async ({ page }) => {
    const guards = attachPageGuards(page);
    const fixture = await installSearchFixtures(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await gotoSearch(page);
    await expectNoHorizontalOverflow(page);

    await page.keyboard.press('/');
    await expect(page.locator('#search-input')).toBeFocused();

    await page.locator('#search-input').fill('dashboard payload');
    await expect(page.locator('.search-result-card')).toHaveCount(1);
    await expect(page.locator('.search-result-card').first()).toContainText('Dashboard payload search');
    await expect(page.locator('#search-clear')).toBeVisible();

    await page.locator('#search-clear').click();
    await expect(page.locator('#search-input')).toHaveValue('');
    await expect(page.locator('[data-search-state="idle"]')).toBeVisible();

    await page.locator('#search-input').fill('search');
    await expect(page.locator('.search-result-card')).toHaveCount(3);
    await page.getByRole('button', { name: 'Tool calls' }).click();
    await expect(page.locator('.search-result-card')).toHaveCount(1);
    await expect(page.locator('.search-result-card').first()).toHaveAttribute('data-event-kind', 'tool_call');

    await page.locator('#filter-session').fill(SEARCH_SESSION_ID);
    await expect(page.locator('.search-result-card')).toHaveCount(1);
    await page.locator('#filter-sort').selectOption('newest');
    await page.getByRole('button', { name: '7d' }).click();
    await expect(page.locator('#filter-range')).toHaveValue('7d');
    await expect(page.getByRole('button', { name: '7d' })).toHaveAttribute('aria-pressed', 'true');

    const lastRequest = fixture.requests.at(-1);
    expect(lastRequest?.searchParams.get('q')).toBe('search');
    expect(lastRequest?.searchParams.get('event_kind')).toBe('tool_call');
    expect(lastRequest?.searchParams.get('session_id')).toBe(SEARCH_SESSION_ID);
    expect(lastRequest?.searchParams.get('sort')).toBe('newest');
    expect(lastRequest?.searchParams.get('range')).toBe('7d');

    await page.getByRole('button', { name: 'Reset filters' }).click();
    await expect(page.locator('#filter-session')).toHaveValue('');
    await expect(page.locator('#filter-sort')).toHaveValue('relevance');
    await expect(page.locator('#filter-limit')).toHaveValue('30');
    await expect(page.locator('#filter-event-kind')).toHaveValue('');

    await page.locator('#search-input').fill('many');
    await expect(page.locator('.search-result-card')).toHaveCount(30);
    await expect(page.getByRole('button', { name: 'Show more' })).toBeVisible();
    await expectSearchVerticalFlow(page);
    await page.getByRole('button', { name: 'Show more' }).click();
    await expect(page.locator('#filter-limit')).toHaveValue('60');
    await expect(page.locator('.search-result-card')).toHaveCount(35);
    await expect(page.getByRole('button', { name: 'Show more' })).toHaveCount(0);

    await guards.expectClean();
  });

  test('keeps the search layout contained on narrow screens', async ({ page }) => {
    const guards = attachPageGuards(page);
    await installSearchFixtures(page);
    await page.setViewportSize({ width: 390, height: 844 });
    await gotoSearch(page);
    await page.locator('#search-input').fill('many');
    await expect(page.locator('.search-result-card')).toHaveCount(30);
    await expectNoHorizontalOverflow(page);
    await expectSearchVerticalFlow(page);

    const metrics = await page.evaluate(() => {
      const wrap = document.getElementById('search-wrap')?.getBoundingClientRect();
      const cards = Array.from(document.querySelectorAll('.search-result-card')).map((card) => card.getBoundingClientRect());
      return {
        wrapLeft: wrap ? Math.round(wrap.left) : 0,
        wrapRight: wrap ? Math.round(wrap.right) : 0,
        cardOverflow: cards.some((card) => card.left < -1 || card.right > window.innerWidth + 1),
      };
    });
    expect(metrics.wrapLeft).toBeGreaterThanOrEqual(0);
    expect(metrics.wrapRight).toBeLessThanOrEqual(390);
    expect(metrics.cardOverflow).toBe(false);

    await guards.expectClean();
  });
});
