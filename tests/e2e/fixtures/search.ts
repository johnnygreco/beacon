import { expect, type Page, type Request, type Response } from '@playwright/test';

export const SEARCH_SESSION_ID = 'session-search-001';

type SearchURLPredicate = (url: URL) => boolean;

function isSearchResultsURL(rawURL: string, predicate?: SearchURLPredicate) {
  const url = new URL(rawURL);
  return url.pathname === '/search/results' && (!predicate || predicate(url));
}

export function waitForSearchRequest(page: Page, predicate?: SearchURLPredicate) {
  return page.waitForRequest((request: Request) => isSearchResultsURL(request.url(), predicate));
}

export async function waitForSearchResponse(page: Page, predicate?: SearchURLPredicate) {
  const response = await page.waitForResponse((candidate: Response) => {
    return candidate.status() === 200 && isSearchResultsURL(candidate.url(), predicate);
  });
  expect(response.ok()).toBe(true);
  return response;
}

export async function fillSearchAndWait(page: Page, value: string, predicate?: SearchURLPredicate) {
  const responsePromise = waitForSearchResponse(page, predicate || ((url) => url.searchParams.get('q') === value));
  await page.locator('#search-input').fill(value);
  return responsePromise;
}

export async function triggerSearchAndWait(
  page: Page,
  action: () => Promise<unknown>,
  predicate?: SearchURLPredicate,
) {
  const requestPromise = waitForSearchRequest(page, predicate);
  const responsePromise = waitForSearchResponse(page, predicate);
  await action();
  const [request] = await Promise.all([requestPromise, responsePromise]);
  return new URL(request.url());
}
