import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  outputDir: '../../.build/tauri-tests',
  use: { baseURL: 'http://127.0.0.1:1427', viewport: { width: 1060, height: 760 } },
  webServer: { command: 'npm run dev -- --port 1427', url: 'http://127.0.0.1:1427', reuseExistingServer: false },
});
