#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: scripts/site-package.sh DESTINATION" >&2
  exit 2
fi

destination=$1
repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
site_root="$repository_root/sites/git-a2a.com"

if [ -e "$destination" ] && [ ! -d "$destination" ]; then
  echo "site-package: destination is not a directory: $destination" >&2
  exit 1
fi
mkdir -p "$destination"
if [ -n "$(find "$destination" -mindepth 1 -maxdepth 1 -print -quit)" ]; then
  echo "site-package: destination must be empty: $destination" >&2
  exit 1
fi

for file in .htaccess index.html 404.html robots.txt sitemap.xml llms.txt llms-full.txt install.sh install.ps1; do
  cp "$site_root/$file" "$destination/$file"
done
for directory in assets fonts ext schema; do
  cp -R "$site_root/$directory" "$destination/$directory"
done

echo "site-package: staged public files in $destination"
