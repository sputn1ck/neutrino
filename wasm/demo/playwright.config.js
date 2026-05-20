const { defineConfig } = require('@playwright/test');

const baseURL = process.env.PLAYWRIGHT_BASE_URL || 'http://127.0.0.1:8090';

module.exports = defineConfig({
  testDir: './tests',
  timeout: 60000,
  use: {
    baseURL
  },
  webServer: process.env.PLAYWRIGHT_BASE_URL ? undefined : {
    command: 'GOCACHE=/tmp/go-build PATH=/usr/local/go/bin:$PATH go run ./server -dir static -addr 127.0.0.1:8090',
    url: 'http://127.0.0.1:8090',
    reuseExistingServer: !process.env.CI
  }
});
