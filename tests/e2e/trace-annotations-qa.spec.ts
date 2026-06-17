import { expect, test, type Page } from '@playwright/test';
import { mkdir } from 'node:fs/promises';
import path from 'node:path';
import {
  TEST_EVENT_ID,
  TEST_SESSION_ID,
  expectNoHorizontalOverflow,
  installDashboardFixtures,
} from './fixtures/dashboard';

const imageDir = path.join(process.cwd(), 'docs/qa/trace-annotations/images');
const qaSessionID = 'session-annotation-qa';
const qaSessionTitle = 'Trace annotation QA review';
const qaMessageEventID = 'event-annotation-message-001';
const qaToolResultEventID = 'event-annotation-tool-result-001';

async function capture(page: Page, name: string, fullPage = true) {
  await page.screenshot({
    path: path.join(imageDir, name),
    fullPage,
    animations: 'disabled',
  });
}

function qaScope() {
  return { auth_scope_applied: false, filters: { source_names: ['source-a'], project_keys: ['beacon'] } };
}

async function brandQASession(page: Page) {
  await page.evaluate(({ originalSessionID, sessionID, title }) => {
    const wrap = document.getElementById('transcript-wrap');
    wrap?.querySelector('h1')?.replaceChildren(title);
    const sessionIDElement = Array.from(wrap?.querySelectorAll('p') || []).find((element) => element.textContent?.includes(originalSessionID));
    sessionIDElement?.replaceChildren(sessionID);
    document.title = `${title} | Beacon`;
  }, { originalSessionID: TEST_SESSION_ID, sessionID: qaSessionID, title: qaSessionTitle });
}

async function brandAnnotationTarget(page: Page, label: string) {
  await page.locator('[data-annotation-target-label]').evaluate((element, value) => {
    element.replaceChildren(value);
  }, label);
  await page.locator('#annotation-drawer-title').evaluate((element, value) => {
    element.replaceChildren(value);
  }, label);
}

function annotatedTraceIndexResponse() {
  return {
    schema: 'beacon.annotated_traces.index.v1',
    scope: qaScope(),
    include_deleted: false,
    offset: 0,
    limit: 25,
    has_more: false,
    items: [
      {
        session: {
          id: qaSessionID,
          title: qaSessionTitle,
          source: 'source-a',
          runtime: 'runtime-a',
          project_key: 'beacon',
          project_path: '/Users/example/projects/beacon',
          provider: 'provider-a',
          status: 'completed',
          started_at: '2026-05-08T17:00:00.000Z',
          ended_at: '2026-05-08T18:00:00.000Z',
          duration: '38m 12s',
          turn_count: 18,
          total_tokens: 123456,
          input_tokens: 62000,
          output_tokens: 47000,
          cache_read_tokens: 14456,
          cache_create_tokens: 0,
          tool_call_count: 14,
          mcp_call_count: 2,
          error_count: 1,
          attention_state: 'error',
          attention_score: 100,
          attention_reasons: ['errors'],
          last_model: 'generic-model-a',
          total_cost_usd: 0.42,
          cost_event_count: 1,
          cost_provenance: 'event_cost_usd',
          working_dir: '/Users/example/projects/beacon',
          has_session_end: true,
          subagent_count: 2,
        },
        counts: {
          annotation_count: 3,
          session_annotation_count: 1,
          message_annotation_count: 1,
          event_annotation_count: 1,
          needs_followup_count: 1,
        },
        first_annotation_at: '2026-05-09T18:00:00.000Z',
        last_annotation_at: '2026-05-09T18:02:00.000Z',
        targets: [
          {
            target_type: 'session',
            annotation_count: 1,
            first_annotation_at: '2026-05-09T18:00:00.000Z',
            last_annotation_at: '2026-05-09T18:00:00.000Z',
          },
          {
            target_type: 'message',
            event_uid: qaMessageEventID,
            annotation_count: 1,
            first_annotation_at: '2026-05-09T18:01:00.000Z',
            last_annotation_at: '2026-05-09T18:01:00.000Z',
          },
          {
            target_type: 'event',
            event_uid: qaToolResultEventID,
            annotation_count: 1,
            first_annotation_at: '2026-05-09T18:02:00.000Z',
            last_annotation_at: '2026-05-09T18:02:00.000Z',
          },
        ],
      },
    ],
  };
}

function annotatedTraceExportResponse() {
  return {
    schema: 'beacon.annotated_traces.export.v1',
    exported_at: '2026-05-09T18:05:00.000Z',
    scope: qaScope(),
    include_deleted: false,
    offset: 0,
    limit: 25,
    event_limit: 2000,
    has_more: false,
    traces: [
      {
        session: annotatedTraceIndexResponse().items[0].session,
        counts: annotatedTraceIndexResponse().items[0].counts,
        annotations: [
          {
            annotation_id: 'annotation-session-qa',
            target_type: 'session',
            session_id: qaSessionID,
            author_type: 'human',
            source: 'ui',
            category: 'quality',
            outcome: 'kept',
            quality_score: 5,
            confidence: 91,
            needs_followup: false,
            labels: ['dataset:eval', 'rubric:correctness'],
            note: 'Session-level QA annotation.',
            status: 'active',
            schema_version: 1,
            created_at: '2026-05-09T18:00:00.000Z',
            updated_at: '2026-05-09T18:00:00.000Z',
          },
          {
            annotation_id: 'annotation-message-qa',
            target_type: 'message',
            session_id: qaSessionID,
            event_uid: qaMessageEventID,
            author_type: 'agent',
            author_id: 'qa-agent',
            author_name: 'QA Agent',
            source: 'mcp',
            category: 'dataset',
            outcome: 'train',
            quality_score: 4,
            confidence: 88,
            needs_followup: true,
            labels: ['dataset:eval', 'dataset:train'],
            note: 'Message-level MCP annotation.',
            metadata_json: '{"rubric_version":"2026-06"}',
            status: 'active',
            schema_version: 1,
            created_at: '2026-05-09T18:01:00.000Z',
            updated_at: '2026-05-09T18:01:00.000Z',
          },
          {
            annotation_id: 'annotation-event-qa',
            target_type: 'event',
            session_id: qaSessionID,
            event_uid: qaToolResultEventID,
            author_type: 'human',
            source: 'ui',
            category: 'timeline',
            outcome: 'kept',
            quality_score: 4,
            confidence: 82,
            needs_followup: false,
            labels: ['dataset:eval', 'event:timeline'],
            note: 'Event-level timeline annotation.',
            status: 'active',
            schema_version: 1,
            created_at: '2026-05-09T18:02:00.000Z',
            updated_at: '2026-05-09T18:02:00.000Z',
          },
        ],
        events: [
          {
            event_uid: qaMessageEventID,
            session_id: qaSessionID,
            event_kind: 'message',
            payload_type: 'message',
            actor_role: 'user',
            timestamp: '2026-05-09T17:59:00.000Z',
            text_preview: 'Read dashboard fixture payload',
            tool_name: '',
            tool_use_id: '',
            model: 'generic-model-a',
            tokens: 120,
            duration_ms: 0,
          },
          {
            event_uid: qaToolResultEventID,
            session_id: qaSessionID,
            event_kind: 'tool_result',
            payload_type: 'tool_result',
            actor_role: 'tool',
            timestamp: '2026-05-09T17:59:15.000Z',
            text_preview: 'Payload loaded.',
            tool_name: 'Read',
            tool_use_id: 'toolu_qa',
            model: 'generic-model-a',
            tokens: 420,
            duration_ms: 250,
          },
        ],
        event_limit: 2000,
        event_truncated: false,
      },
    ],
    warnings: [],
  };
}

test.describe.configure({ mode: 'serial' });

test.describe('trace annotation QA screenshots', () => {
  test.skip(process.env.BEACON_QA_CAPTURE !== '1', 'Set BEACON_QA_CAPTURE=1 to regenerate committed QA screenshots.');

  test.beforeAll(async () => {
    await mkdir(imageDir, { recursive: true });
  });

  test('captures desktop session, message, edit, delete, and event annotation states', async ({ page }) => {
    await installDashboardFixtures(page);
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.goto(`/sessions/${TEST_SESSION_ID}`, { waitUntil: 'domcontentloaded' });
    await brandQASession(page);
    await expect(page.locator('#transcript-wrap')).toBeVisible();
    await expect(page.locator('[data-annotation-summary-count]')).toContainText('0 annotations');
    await capture(page, '01-desktop-transcript-empty-annotations.png');

    await page.getByRole('button', { name: 'Annotate session' }).click();
    const drawer = page.locator('#annotation-drawer');
    const form = drawer.locator('[data-annotation-form]');
    await form.locator('input[name="category"]').fill('quality');
    await form.locator('input[name="outcome"]').fill('kept');
    await form.locator('input[name="labels"]').fill('dataset:eval, rubric:correctness');
    await form.locator('textarea[name="note"]').fill('Session-level note for a review dataset.');
    await form.locator('[data-annotation-save]').click();
    await expect(drawer).toContainText('Session-level note for a review dataset.');
    await capture(page, '02-desktop-session-annotation-drawer.png');

    await drawer.getByRole('button', { name: 'Close annotations' }).click();
    await page.locator(`#${TEST_EVENT_ID} [data-annotation-button]`).first().click();
    await form.locator('input[name="category"]').fill('dataset');
    await form.locator('input[name="outcome"]').fill('train');
    await form.locator('select[name="quality_score"]').selectOption('4');
    await form.locator('input[name="confidence"]').fill('88');
    await form.locator('input[name="labels"]').fill('dataset:train, message:correction');
    await form.locator('textarea[name="note"]').fill('Message-level annotation retained for training.');
    await form.locator('input[name="needs_followup"]').check();
    await form.locator('[data-annotation-save]').click();
    await brandAnnotationTarget(page, `Message ${qaMessageEventID}`);
    await expect(drawer).toContainText('Message-level annotation retained for training.');
    await capture(page, '03-desktop-message-annotation-drawer.png');

    await drawer.getByRole('button', { name: 'Edit' }).click();
    await brandAnnotationTarget(page, `Message ${qaMessageEventID}`);
    await expect(form.locator('textarea[name="note"]')).toHaveValue('Message-level annotation retained for training.');
    await capture(page, '04-desktop-message-edit-state.png');

    await drawer.getByRole('button', { name: 'Delete' }).click();
    await brandAnnotationTarget(page, `Message ${qaMessageEventID}`);
    await expect(drawer).toContainText('No annotations yet');
    await capture(page, '05-desktop-message-deleted-empty-state.png');

    await drawer.getByRole('button', { name: 'Close annotations' }).click();
    await page.getByRole('button', { name: 'Timeline' }).click();
    await page.locator('#timeline-view [data-annotation-button][data-annotation-target="event"]').first().click();
    await form.locator('input[name="category"]').fill('timeline');
    await form.locator('input[name="outcome"]').fill('kept');
    await form.locator('input[name="labels"]').fill('dataset:eval, event:timeline');
    await form.locator('textarea[name="note"]').fill('Event-level annotation from the timeline view.');
    await form.locator('[data-annotation-save]').click();
    await brandAnnotationTarget(page, `Event ${qaToolResultEventID}`);
    await expect(drawer).toContainText('Event-level annotation from the timeline view.');
    await expectNoHorizontalOverflow(page);
    await capture(page, '06-desktop-timeline-event-annotation.png');
  });

  test('captures mobile annotation error state', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 760 });
    await installDashboardFixtures(page, { annotationsUnavailable: true });
    await page.goto(`/sessions/${TEST_SESSION_ID}`, { waitUntil: 'domcontentloaded' });
    await brandQASession(page);
    await page.getByRole('button', { name: 'Annotate session' }).click();
    await expect(page.locator('[data-annotation-list]')).toContainText('Unable to load annotations');
    await expectNoHorizontalOverflow(page);
    await capture(page, '07-mobile-annotation-failure-state.png', false);
  });

  test('captures annotated trace discovery and export responses', async ({ page }) => {
    await installDashboardFixtures(page);
    await page.route('**/api/annotations/traces**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(annotatedTraceIndexResponse(), null, 2),
      });
    });
    await page.route('**/api/annotations/export**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(annotatedTraceExportResponse(), null, 2),
      });
    });
    await page.setViewportSize({ width: 1280, height: 900 });

    await page.goto('/api/annotations/traces?label=dataset:eval&limit=25&source_name=source-a&project_key=beacon', { waitUntil: 'domcontentloaded' });
    await expect(page.locator('body')).toContainText('beacon.annotated_traces.index.v1');
    await capture(page, '08-annotated-trace-discovery-json.png');

    await page.goto('/api/annotations/export?label=dataset:eval&event_limit=2000&limit=25&source_name=source-a&project_key=beacon', { waitUntil: 'domcontentloaded' });
    await expect(page.locator('body')).toContainText('beacon.annotated_traces.export.v1');
    await expect(page.locator('body')).toContainText('Message-level MCP annotation.');
    await capture(page, '09-annotated-trace-export-json.png');
  });
});
