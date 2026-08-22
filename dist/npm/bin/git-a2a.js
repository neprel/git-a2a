#!/usr/bin/env node
'use strict';
const { spawnSync } = require('node:child_process');
const path = require('node:path');
const platform = process.platform === 'win32' ? 'windows' : process.platform;
const arch = process.arch === 'x64' ? 'amd64' : process.arch;
const packageName = `@git-a2a/${platform}-${arch}`;
try {
  const pkg = require.resolve(`${packageName}/package.json`);
  const exe = process.platform === 'win32' ? 'git-a2a.exe' : 'git-a2a';
  const result = spawnSync(path.join(path.dirname(pkg), 'bin', exe), process.argv.slice(2), { stdio: 'inherit' });
  if (result.error) throw result.error;
  process.exit(result.status === null ? 1 : result.status);
} catch (error) {
  console.error(`git-a2a: no binary package for ${platform}/${arch}: ${error.message}`);
  process.exit(1);
}
