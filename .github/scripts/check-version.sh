#!/bin/sh
set -eu

tag=${1:-}
version=$(tr -d '\r\n' < internal/version/VERSION)
if [ "$tag" != "v$version" ]; then
  echo "release tag $tag does not match version v$version" >&2
  exit 1
fi
echo "release tag $tag matches version v$version"
