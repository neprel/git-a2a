#!/bin/sh
set -eu
version="${GIT_A2A_VERSION:-latest}"
repo="https://github.com/neprel/git-a2a"
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in x86_64) arch=amd64;; arm64|aarch64) arch=arm64;; *) echo "unsupported architecture: $arch" >&2; exit 1;; esac
if [ "$version" = latest ]; then version=$(curl -fsSL "$repo/releases/latest" | sed -n 's|.*tag/\(v[^\"]*\).*|\1|p' | head -n1); fi
archive="git-a2a_${version#v}_${os}_${arch}.tar.gz"
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
curl -fsSL "$repo/releases/download/$version/$archive" -o "$tmp/$archive"
curl -fsSL "$repo/releases/download/$version/checksums.txt" -o "$tmp/checksums.txt"
(cd "$tmp" && grep " $archive\$" checksums.txt | shasum -a 256 -c -)
tar -xzf "$tmp/$archive" -C "$tmp" git-a2a
install -m 0755 "$tmp/git-a2a" "${GIT_A2A_INSTALL_DIR:-/usr/local/bin}/git-a2a"
