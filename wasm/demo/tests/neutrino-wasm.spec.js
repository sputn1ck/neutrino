const { test, expect } = require('@playwright/test');

test('loads wasm demo and initializes neutrino storage', async ({ page }) => {
  const messages = [];
  page.on('console', (msg) => messages.push(msg.text()));

  await page.goto('/');
  await page.evaluate(() => {
    window.neutrinoDemoDBName = `neutrino-wasm-test-${Date.now()}.sqlite`;
  });
  await expect(page.locator('#status')).toHaveText('ready', { timeout: 30000 });
  await expect(page.locator('#connectBtn')).toBeVisible();
  await expect(page.locator('#peerCount')).toHaveText('0');

  await page.locator('#initBtn').click();
  await expect(page.locator('#log')).toContainText('storage initialized', {
    timeout: 30000
  });
  await expect(page.locator('#log')).toContainText('best block height=0');

  await page.locator('#bestBtn').click();
  await expect(page.locator('#status')).toContainText('best block', {
    timeout: 30000
  });
  await expect(page.locator('#log')).toContainText('hash=000000000019d6689c085ae165831e93');

  await page.locator('#peersBtn').click();
  await expect(page.locator('#peerList')).toContainText('No connected peers.');

  expect(messages.join('\n')).not.toContain('wasm load failed');
});

test('reuses initialized storage when connecting a peer', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(() => {
    window.neutrinoDemoDBName = `neutrino-wasm-test-${Date.now()}.sqlite`;
  });
  await expect(page.locator('#status')).toHaveText('ready', { timeout: 30000 });

  await page.locator('#proxy').fill('ws://127.0.0.1:1/peer-proxy');
  await page.locator('#initBtn').click();
  await expect(page.locator('#log')).toContainText('storage initialized', {
    timeout: 30000
  });

  await page.locator('#connectBtn').click();
  await expect(page.locator('#log')).toContainText('chain service started', {
    timeout: 30000
  });
  await expect(page.locator('#log')).toContainText('peer connection requested');
  await expect(page.locator('#peerCount')).toBeVisible();
  await expect(page.locator('#log')).not.toContainText('database already open');
});

test('skips onion peers before opening websocket proxy connections', async ({ page }) => {
  const messages = [];
  page.on('console', (msg) => messages.push(msg.text()));

  await page.goto('/');
  await page.evaluate(() => {
    window.neutrinoDemoDBName = `neutrino-wasm-test-${Date.now()}.sqlite`;
  });
  await expect(page.locator('#status')).toHaveText('ready', { timeout: 30000 });

  await page.locator('#proxy').fill('ws://127.0.0.1:1/peer-proxy');
  await page.locator('#peer').fill(
    'hswvbdrouzbzzusp6s7onc4xgpydluxnjlylsfgmws66y6c2hradgiid.onion:8333'
  );
  await page.locator('#connectBtn').click();

  await expect(page.locator('#status')).toHaveText('onion peer skipped');
  await expect(page.locator('#log')).toContainText('onion peer skipped');
  expect(messages.join('\n')).not.toContain('WebSocket connection');
});
