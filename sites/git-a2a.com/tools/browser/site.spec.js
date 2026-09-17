import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { pathToFileURL } from 'node:url';

test('all documentation pages render from file URLs', async ({ page }) => {
  for (const [path, heading] of [['../../index.html', 'Import the component.'], ['../../schema/index.html', 'Schema 2'], ['../../spec/index.html', 'Schema 2 specification']]) {
    await page.goto(pathToFileURL(new URL(path, import.meta.url).pathname).href);
    await expect(page.getByRole('heading', { level: 1 })).toContainText(heading);
  }
});

test('desktop diagram preserves the repository and agent composition', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto('/#idea');
  const boxes = page.locator('.diagram-column .diagram-box');
  const positions = await boxes.evaluateAll(elements => elements.map(element => {
    const box = element.getBoundingClientRect();
    return { top: box.top, left: box.left };
  }));
  expect(positions[0].top).toBeCloseTo(positions[2].top, 1);
  expect(positions[1].top).toBeCloseTo(positions[3].top, 1);
  expect(positions[0].left).toBeLessThan(positions[2].left);
  await expect(page.locator('.code-flow')).toContainText('git-a2a pull');
  await expect(page.locator('.agent-flows')).toContainText('external client');
});

test('mobile layout is readable and has no horizontal overflow', async ({ page }) => {
  await page.goto('/#idea');
  const dimensions = await page.evaluate(() => ({ body: document.body.scrollWidth, viewport: innerWidth }));
  expect(dimensions.body).toBe(dimensions.viewport);
  const diagram = await page.locator('.diagram-grid').evaluate(element => {
    const box = element.getBoundingClientRect();
    return { columns: getComputedStyle(element).gridTemplateColumns.split(' ').length, left: box.left, right: box.right };
  });
  expect(diagram.columns).toBe(1);
  expect(diagram.left).toBeGreaterThanOrEqual(0);
  expect(diagram.right).toBeLessThanOrEqual(dimensions.viewport);
  await expect(page.locator('.diagram-box')).toHaveCount(4);
});

test.describe('reduced motion', () => {
  test.use({ reducedMotion: 'reduce' });

  test('renders the complete current owner loop immediately', async ({ page }) => {
    await page.goto('/');
    const terminal = page.locator('#terminal-body');
    await expect(terminal).toContainText('git-a2a list acme-lib-utils');
    await expect(terminal).toContainText('python demo/a2a_client.py change');
    await expect(terminal).toContainText('acme-lib-utils: pulled');
    await expect(terminal).toContainText('Anonymous');
    await expect(terminal.locator('.caret')).toHaveCount(1);
  });
});

test('terminal replay animates the verified sequence', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'replay' }).click();
  await expect(page.locator('#terminal-body')).toContainText('Anonymous', { timeout: 15000 });
});

test('install tabs support keyboard navigation', async ({ page }) => {
  await page.goto('/#install');
  const tabs = page.getByRole('tab');
  await tabs.nth(0).focus();
  await page.keyboard.press('ArrowRight');
  await expect(tabs.nth(1)).toBeFocused();
  await expect(tabs.nth(1)).toHaveAttribute('aria-selected', 'true');
  await expect(page.locator('#panel-go')).toBeVisible();
  await page.keyboard.press('End');
  await expect(tabs.last()).toBeFocused();
  await page.keyboard.press('Home');
  await expect(tabs.nth(0)).toBeFocused();
});

test('copy buttons announce copied state', async ({ page }) => {
  await page.goto('/#manifest');
  const button = page.locator('[data-copy="#manifest-yaml"]');
  const label = button.locator('[aria-live="polite"]');
  await button.click();
  await expect(label).toHaveText('copied');
  await expect(label).toHaveText('copy', { timeout: 1800 });
});

test('demo section retains commands, boundaries, and evidence links', async ({ page }) => {
  await page.goto('/#demo');
  const demo = page.locator('#demo');
  await expect(demo).toContainText('./demo/run.sh');
  await expect(demo).toContainText('DEMO_PROFILE=npm ./demo/run.sh');
  await expect(demo).toContainText('separate demo client uses real A2A transport');
  await expect(demo).toContainText('not permanently available public services');
  await expect(demo.getByRole('link', { name: 'Walkthrough' })).toHaveAttribute('href', 'https://github.com/neprel/git-a2a-demo-acme-app#readme');
  await expect(demo.getByRole('link', { name: 'Verified transcript' })).toHaveAttribute('href', 'https://github.com/neprel/git-a2a-demo-acme-app/blob/main/docs/demo-transcript.md');
  await expect(demo.getByRole('link', { name: 'Library repo' })).toHaveAttribute('href', 'https://github.com/neprel/git-a2a-demo-acme-lib');
});

test('every visible link and button has a focus ring', async ({ page }) => {
  await page.goto('/');
  const controls = page.locator('a:visible, button:visible');
  for (let index = 0; index < await controls.count(); index += 1) {
    const control = controls.nth(index);
    await control.focus();
    const outline = await control.evaluate(element => {
      const style = getComputedStyle(element);
      return { style: style.outlineStyle, width: parseFloat(style.outlineWidth) };
    });
    expect(outline.style, `control ${index} outline style`).not.toBe('none');
    expect(outline.width, `control ${index} outline width`).toBeGreaterThanOrEqual(2);
  }
});

test('landing has no serious accessibility violations', async ({ page }) => {
  await page.goto('/');
  const results = await new AxeBuilder({ page }).analyze();
  expect(results.violations.filter(violation => violation.impact === 'serious')).toEqual([]);
});
