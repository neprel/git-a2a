#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
fonts="$root/fonts"
mkdir -p "$fonts"

ibm_commit=bf260093582f04622aacc1e9f9ca604d7ccd0c42
jetbrains_commit=19371302b95d218af43299bce79ddbddd0bc364d
ibm_base="https://raw.githubusercontent.com/IBM/plex/$ibm_commit/packages/plex-sans/fonts/split/woff2"
jetbrains_base="https://raw.githubusercontent.com/JetBrains/JetBrainsMono/$jetbrains_commit/fonts/webfonts"

curl -fsSL "$ibm_base/IBMPlexSans-Regular-Latin1.woff2" -o "$fonts/ibm-plex-sans-400-latin.woff2"
curl -fsSL "$ibm_base/IBMPlexSans-Medium-Latin1.woff2" -o "$fonts/ibm-plex-sans-500-latin.woff2"
curl -fsSL "$ibm_base/IBMPlexSans-SemiBold-Latin1.woff2" -o "$fonts/ibm-plex-sans-600-latin.woff2"
curl -fsSL "$jetbrains_base/JetBrainsMono-Regular.woff2" -o "$fonts/jetbrains-mono-400-latin.woff2"
curl -fsSL "$jetbrains_base/JetBrainsMono-Medium.woff2" -o "$fonts/jetbrains-mono-500-latin.woff2"
curl -fsSL "$jetbrains_base/JetBrainsMono-Bold.woff2" -o "$fonts/jetbrains-mono-700-latin.woff2"
curl -fsSL "https://raw.githubusercontent.com/JetBrains/JetBrainsMono/$jetbrains_commit/fonts/ttf/JetBrainsMono-Regular.ttf" -o "$root/tools/JetBrainsMono-Regular.ttf"
curl -fsSL "https://raw.githubusercontent.com/JetBrains/JetBrainsMono/$jetbrains_commit/fonts/ttf/JetBrainsMono-Bold.ttf" -o "$root/tools/JetBrainsMono-Bold.ttf"
curl -fsSL "https://raw.githubusercontent.com/IBM/plex/$ibm_commit/LICENSE.txt" -o "$fonts/IBMPlex-OFL.txt"
curl -fsSL "https://raw.githubusercontent.com/JetBrains/JetBrainsMono/$jetbrains_commit/OFL.txt" -o "$fonts/JetBrainsMono-OFL.txt"

echo "downloaded pinned OFL font assets into $fonts"
