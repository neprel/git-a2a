#!/usr/bin/env node
'use strict';
const { spawnSync } = require('node:child_process');
const path = require('node:path');

const platform = process.platform === 'win32' ? 'windows' : process.platform;
const arch = process.arch === 'x64' ? 'amd64' : process.arch;
const executable = platform === 'windows' ? 'git-a2a.exe' : 'git-a2a';
const binary = path.join(__dirname, 'bin', `${platform}-${arch}`, executable);
const result = spawnSync(binary, process.argv.slice(2), { stdio: 'inherit' });
if (result.error) {
  console.error(`git-a2a: cannot start ${platform}/${arch} binary: ${result.error.message}`);
  process.exit(1);
}
process.exit(result.status === null ? 1 : result.status);
