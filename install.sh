#!/bin/sh
set -eu
version="${GIT_A2A_VERSION:-latest}"
repo="https://github.com/neprel/git-a2a"
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  mingw*|msys*|cygwin*) echo "install.sh does not support Windows shells; use Scoop or GitHub Releases" >&2; exit 1;;
  darwin|linux) ;;
  *) echo "unsupported operating system: $os" >&2; exit 1;;
esac
arch=$(uname -m)
case "$arch" in x86_64) arch=amd64;; arm64|aarch64) arch=arm64;; *) echo "unsupported architecture: $arch" >&2; exit 1;; esac
if [ "$version" = latest ]; then
  latest_url=$(curl -fsSL -o /dev/null -w '%{url_effective}' "$repo/releases/latest")
  version=${latest_url##*/}
fi
case "$version" in v*) ;; *) version="v$version";; esac
case "$version" in v?*) ;; *) echo "could not resolve a release version" >&2; exit 1;; esac
archive="git-a2a_${version#v}_${os}_${arch}.tar.gz"
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
curl -fsSL "$repo/releases/download/$version/$archive" -o "$tmp/$archive"
curl -fsSL "$repo/releases/download/$version/checksums.txt" -o "$tmp/checksums.txt"
(cd "$tmp" && grep " $archive\$" checksums.txt > selected-checksum.txt)
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$tmp" && sha256sum -c selected-checksum.txt)
elif command -v shasum >/dev/null 2>&1; then
  (cd "$tmp" && shasum -a 256 -c selected-checksum.txt)
else
  echo "neither sha256sum nor shasum is available; refusing an unverified install" >&2
  exit 1
fi
tar -xzf "$tmp/$archive" -C "$tmp" git-a2a
install_dir=${GIT_A2A_INSTALL_DIR:-/usr/local/bin}
if [ ! -d "$install_dir" ] || [ ! -w "$install_dir" ]; then
  echo "$install_dir is not writable; set GIT_A2A_INSTALL_DIR to a writable directory or rerun with sudo -E" >&2
  exit 1
fi
install -m 0755 "$tmp/git-a2a" "$install_dir/git-a2a"
