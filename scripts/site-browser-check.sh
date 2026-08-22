#!/bin/sh
set -eu

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
browser_root="$repository_root/sites/git-a2a.com/tools/browser"

cd "$browser_root"
npm ci --ignore-scripts
npx playwright install --with-deps chromium
npm test
