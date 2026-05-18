import { defineConfig, devices } from '@playwright/test';

const baseURL = process.env.BEACON_E2E_BASE_URL || 'http://127.0.0.1:4610';
const externalServer = Boolean(process.env.BEACON_E2E_BASE_URL);

export default defineConfig({
  testDir: './tests/e2e',
  timeout: 30_000,
  expect: {
    timeout: 5_000,
    toHaveScreenshot: {
      animations: 'disabled',
      maxDiffPixelRatio: 0.02,
    },
  },
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['github'], ['html', { open: 'never' }]] : [['list'], ['html', { open: 'never' }]],
  webServer: externalServer
    ? undefined
    : {
        command: 'go run ./cmd/beacon --config tests/e2e/beacon.toml serve',
        url: `${baseURL}/health`,
        timeout: 120_000,
        reuseExistingServer: true,
      },
  use: {
    baseURL,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    colorScheme: 'dark',
  },
  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        viewport: { width: 1440, height: 900 },
      },
    },
  ],
  outputDir: 'test-results/playwright',
});
