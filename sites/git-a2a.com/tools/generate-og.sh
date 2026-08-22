#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
chrome=${CHROME_BIN:-/Applications/Google Chrome.app/Contents/MacOS/Google Chrome}
[ -x "$chrome" ] || { echo "generate-og.sh: headless Chrome not found at $chrome" >&2; exit 1; }
url="file://$root/tools/og.html"
temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT HUP INT TERM
capture() {
  size=$1
  destination=$2
  profile="$temporary/profile-$size"
  screenshot="$temporary/$size.png"
  mkdir -p "$profile"
  "$chrome" --headless --disable-gpu --disable-extensions --disable-background-networking --disable-component-update --no-first-run --no-default-browser-check --hide-scrollbars --force-device-scale-factor=1 --user-data-dir="$profile" --window-size="$size" --screenshot="$screenshot" "$url" >/dev/null 2>&1 &
  pid=$!
  attempts=0
  while [ ! -s "$screenshot" ] && [ "$attempts" -lt 100 ]; do
    sleep 0.1
    attempts=$((attempts + 1))
  done
  kill "$pid" >/dev/null 2>&1 || true
  wait "$pid" >/dev/null 2>&1 || true
  [ -s "$screenshot" ] || { echo "generate-og.sh: Chrome did not render $size" >&2; exit 1; }
  mv "$screenshot" "$destination"
}
capture 1200,630 "$root/assets/og.png"
capture 1200,600 "$root/assets/og-twitter.png"
echo "wrote 1200x630 and 1200x600 social images"
