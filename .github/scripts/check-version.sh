#!/bin/sh
set -eu

tag=${1:-}
version=$(tr -d '\r\n' < internal/version/VERSION)
case "$tag" in
  "v$version") ;;
  "v$version"-*)
    suffix=${tag#v$version-}
    if ! printf '%s\n' "$suffix" | grep -Eq '^[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*$'; then
      echo "release tag $tag is not a valid prerelease of v$version" >&2
      exit 1
    fi
    ;;
  *)
    echo "release tag $tag does not match version v$version" >&2
    exit 1
    ;;
esac
echo "release tag $tag matches version v$version"
