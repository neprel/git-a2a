import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { pathToFileURL } from 'node:url';

test('all documentation pages render from file URLs', async ({ page }) => {
  for (const [path, heading] of [['../../index.html', 'Import the code.'], ['../../schema/index.html', 'Schema 2'], ['../../spec/index.html', 'Schema 2 specification']]) {
    await page.goto(pathToFileURL(new URL(path, import.meta.url).pathname).href);
    await expect(page.getByRole('heading', { level: 1 })).toContainText(heading);
  }
});
test('mobile landing has no horizontal overflow', async ({ page }) => { await page.goto('/'); expect(await page.evaluate(() => document.body.scrollWidth)).toBe(await page.evaluate(() => innerWidth)); });
test('landing has no serious accessibility violations', async ({ page }) => { await page.goto('/'); const results = await new AxeBuilder({ page }).analyze(); expect(results.violations.filter(v => v.impact === 'serious')).toEqual([]); });
test('landing documents the runnable demo and its evidence', async ({ page }) => {
  await page.goto('/#demo');
  const demo = page.locator('#demo');
  await expect(demo).toContainText('./demo/run.sh');
  await expect(demo).toContainText('DEMO_PROFILE=npm ./demo/run.sh');
  await expect(demo).toContainText('separate demo client performs A2A communication');
  await expect(demo.getByRole('link', { name: 'ACME app walkthrough' })).toHaveAttribute('href', 'https://github.com/neprel/git-a2a-demo-acme-app#readme');
  await expect(demo.getByRole('link', { name: 'library', exact: true })).toHaveAttribute('href', 'https://github.com/neprel/git-a2a-demo-acme-lib');
  await expect(demo.getByRole('link', { name: 'app', exact: true })).toHaveAttribute('href', 'https://github.com/neprel/git-a2a-demo-acme-app');
  await expect(demo.getByRole('link', { name: 'verified full-profile transcript' })).toHaveAttribute('href', 'https://github.com/neprel/git-a2a-demo-acme-app/blob/main/docs/demo-transcript.md');
});
