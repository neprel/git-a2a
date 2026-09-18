// npm publish performs this exchange internally; npm dist-tag does not.
// Keep package-scoped credentials in memory and never log response bodies.
import { pathToFileURL } from 'node:url';

export async function promote(name, version, tag, { fetcher = fetch, env = process.env } = {}) {
  if (!/^(@git-a2a\/(darwin|linux|windows)-(amd64|arm64)|git-a2a)$/.test(name)) throw new Error('Unexpected package');
  if (!/^\d+\.\d+\.\d+(?:-[\w.-]+)?$/.test(version)) throw new Error('Invalid version');
  if (!['latest', 'next', 'none'].includes(tag)) throw new Error('Invalid promotion tag');
  const registry = 'https://registry.npmjs.org';
  const packagePath = encodeURIComponent(name);
  const request = async (url, options = {}) => {
    const response = await fetcher(url, { ...options, redirect: 'error', signal: AbortSignal.timeout(30000) });
    if (!response.ok) throw new Error(`npm release request failed: HTTP ${response.status}`);
    return response;
  };
  // Never promote a version that isn't visible in the registry.
  await request(`${registry}/${packagePath}/${version}`);
  const tagsURL = `${registry}/-/package/${packagePath}/dist-tags`;
  const tags = await (await request(tagsURL)).json();
  const obsolete = Object.entries(tags).filter(([key, value]) => /^recovery-\d+-\d+$/.test(key) && value === version).map(([key]) => key);
  if ((tag === 'none' || tags[tag] === version) && !obsolete.length) return;
  if (!env.ACTIONS_ID_TOKEN_REQUEST_URL || !env.ACTIONS_ID_TOKEN_REQUEST_TOKEN) throw new Error('GitHub OIDC environment required');
  const url = new URL(env.ACTIONS_ID_TOKEN_REQUEST_URL);
  url.searchParams.set('audience', 'npm:registry.npmjs.org');
  const identity = await (await request(url.href, { headers: { Authorization: `Bearer ${env.ACTIONS_ID_TOKEN_REQUEST_TOKEN}` } })).json();
  if (!identity.value) throw new Error('Missing GitHub OIDC token');
  const exchange = await (await request(`${registry}/-/npm/v1/oidc/token/exchange/package/${packagePath}`, {
    method: 'POST', headers: { Authorization: `Bearer ${identity.value}` },
  })).json();
  if (!exchange.token) throw new Error('Missing package-scoped npm token');
  const headers = { Authorization: `Bearer ${exchange.token}`, 'Content-Type': 'application/json' };
  if (tag !== 'none' && tags[tag] !== version) {
    await request(`${tagsURL}/${tag}`, { method: 'PUT', headers, body: JSON.stringify(version) });
  }
  for (const key of obsolete) await request(`${tagsURL}/${key}`, { method: 'DELETE', headers });
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    await promote(...process.argv.slice(2));
    console.log(`npm tags reconciled for ${process.argv[2]}@${process.argv[3]}`);
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
