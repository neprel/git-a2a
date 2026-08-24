import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { pathToFileURL } from 'node:url';

test('all three pages render directly from file URLs', async ({ page }) => {
  const pages = [
    ['../../index.html', 'A git repository you can import together with the agents that own it.'],
    ['../../ext/module/v1/index.html', 'A2A extension'],
    ['../../schema/index.html', 'Schema'],
  ];
  for (const [path, heading] of pages) {
    await page.goto(pathToFileURL(new URL(path, import.meta.url).pathname).href);
    await expect(page.getByRole('heading', { level: 1 })).toHaveText(heading);
  }
});

test('mobile layout has no horizontal overflow', async ({ page }) => {
  await page.goto('/');
  const dimensions = await page.evaluate(() => ({
    body: document.body.scrollWidth,
    viewport: window.innerWidth,
  }));
  expect(dimensions.body).toBe(dimensions.viewport);
});

test.describe('reduced motion', () => {
  test.use({ reducedMotion: 'reduce' });

  test('renders the completed transcript immediately', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.goto('/');
    await expect(page.locator('#terminal-body')).toContainText('1 dependency: clean');
    await expect(page.locator('#terminal-body .caret')).toHaveCount(1);
  });
});

test('status header and row preserve byte-aligned columns', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.goto('/');
  const lines = page.locator('#terminal-body .term-line');
  const header = lines.filter({ hasText: /^MODULE/ });
  const row = lines.filter({ hasText: /^acme-lib-utils/ });
  const position = async (locator, token) => locator.evaluate((element, value) => {
    const text = element.firstChild;
    const start = text.data.indexOf(value);
    const range = document.createRange();
    range.setStart(text, start);
    range.setEnd(text, start + value.length);
    return { x: range.getBoundingClientRect().x, whiteSpace: getComputedStyle(element).whiteSpace,
      font: getComputedStyle(element).fontFamily };
  }, token);
  for (const [heading, value] of [['SOURCE', 'canonical'], ['WIRING', 'npm clean'], ['SYNC', 'none']]) {
    const expected = await position(header, heading);
    const actual = await position(row, value);
    expect(actual.x).toBeCloseTo(expected.x, 1);
    expect(actual.whiteSpace).toBe('pre');
    expect(actual.font).toContain('JetBrains Mono');
  }
});

test('install tabs support keyboard navigation', async ({ page }) => {
  await page.goto('/');
  const tabs = page.getByRole('tab');
  await tabs.nth(0).focus();
  await page.keyboard.press('ArrowRight');
  await expect(tabs.nth(1)).toBeFocused();
  await expect(tabs.nth(1)).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.press('End');
  await expect(tabs.last()).toBeFocused();
  await page.keyboard.press('Home');
  await expect(tabs.nth(0)).toBeFocused();
});

test('copy buttons announce copied for 1400 ms', async ({ page }) => {
  await page.goto('/');
  const button = page.locator('[data-copy="#manifest-yaml"]');
  const label = button.locator('[aria-live="polite"]');
  await button.click();
  await expect(label).toHaveText('copied');
  await page.waitForTimeout(1300);
  await expect(label).toHaveText('copied');
  await expect(label).toHaveText('copy', { timeout: 300 });
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

test('axe reports no serious accessibility violations', async ({ page }) => {
  await page.goto('/');
  const results = await new AxeBuilder({ page }).analyze();
  expect(results.violations.filter(violation => violation.impact === 'serious')).toEqual([]);
});
