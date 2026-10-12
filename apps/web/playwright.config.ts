import { defineConfig } from '@playwright/test'
export default defineConfig({
  testDir: './browser',
  timeout: 120000,
  workers: 1,
  retries: 0,
  reporter: 'list',
  use: {
    baseURL: 'http://localhost:5173',
    browserName: 'chromium',
    locale: 'fr-FR',
    trace: 'off',
    screenshot: 'only-on-failure',
  },
  // Own the server lifecycle: an old dev process can serve stale optimized dependencies.
  // Keep the configured frontend Origin and regenerate Vite's dependency cache.
  webServer: {
    command: 'npm run dev -- --force',
    url: 'http://localhost:5173',
    reuseExistingServer: false,
  },
})
