import { test } from 'node:test';
import assert from 'node:assert/strict';
import { promote } from './npm-dist-tags.mjs';

const env = { ACTIONS_ID_TOKEN_REQUEST_URL: 'https://actions.example/token?x=1', ACTIONS_ID_TOKEN_REQUEST_TOKEN: 'github-secret' };
function mock(tags, fail = '') {
  const calls = [];
  return { calls, fetcher: async (url, options) => {
    calls.push({ url, ...options });
    const body = url.includes('/token?') ? { value: 'oidc-secret' }
      : url.includes('/exchange/') ? { token: 'npm-secret' }
      : url.endsWith('/dist-tags') ? tags : {};
    return { ok: !fail || !url.includes(fail), status: fail && url.includes(fail) ? 401 : 200, json: async () => body };
  } };
}
test('exchanges package-scoped OIDC credentials and removes only this version recovery tags', async () => {
  const m = mock({ latest: '2.0.0', 'recovery-123-1': '2.1.0', 'recovery-99-1': '2.0.0', next: '2.2.0-rc.1' });
  await promote('@git-a2a/linux-amd64', '2.1.0', 'latest', { ...m, env });
  const writes = m.calls.filter(c => ['PUT', 'DELETE'].includes(c.method));
  assert.deepEqual(writes.map(c => [c.method, c.url.split('/').at(-1)]), [['PUT','latest'],['DELETE','recovery-123-1']]);
  assert.equal(writes[0].body, '"2.1.0"');
  assert.equal(writes[0].headers.Authorization, 'Bearer npm-secret');
  assert.ok(m.calls.find(c => c.url.includes('audience=npm%3Aregistry.npmjs.org')));
  assert.ok(m.calls.find(c => c.url.includes('/exchange/package/%40git-a2a%2Flinux-amd64')));
});
test('older release cleanup never changes latest or next', async () => {
  const m = mock({ latest: '2.2.0', next:'2.3.0-rc.1', 'recovery-123-1':'2.1.0' });
  await promote('git-a2a', '2.1.0', 'none', { ...m, env });
  assert.equal(m.calls.filter(c => c.method === 'PUT').length, 0);
});
test('completed recovery requires no credentials', async () => {
  const m = mock({ latest:'2.1.0' });
  await promote('git-a2a','2.1.0','latest',{ ...m, env:{} });
  assert.equal(m.calls.length, 2);
});
test('failed promotion does not delete recovery tags or leak credentials', async () => {
  const m = mock({ latest:'2.0.0', 'recovery-123-1':'2.1.0' }, '/dist-tags/latest');
  await assert.rejects(promote('git-a2a','2.1.0','latest',{ ...m, env }), { message:'npm release request failed: HTTP 401' });
  assert.equal(m.calls.filter(c => c.method === 'DELETE').length, 0);
});
