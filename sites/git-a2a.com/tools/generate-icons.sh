#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
assets="$root/assets"
temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT HUP INT TERM

magick "$root/tools/favicon-16.svg" -resize 16x16 "$temporary/favicon-16.png"
magick "$root/tools/favicon-32.svg" -resize 32x32 "$temporary/favicon-32.png"
magick "$temporary/favicon-16.png" "$temporary/favicon-32.png" "$assets/favicon.ico"
magick "$assets/favicon.svg" -resize 180x180 "$assets/apple-touch-icon.png"
echo "wrote favicon.ico and apple-touch-icon.png"
