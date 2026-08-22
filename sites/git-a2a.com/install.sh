#!/bin/sh
set -eu

version=${GIT_A2A_VERSION:-latest}
install_dir=${GIT_A2A_INSTALL_DIR:-/usr/local/bin}
release_base=${GIT_A2A_RELEASE_BASE:-https://github.com/neprel/git-a2a/releases}
dry_run=false

usage() {
  echo "usage: install.sh [--version VERSION] [--dir DIRECTORY] [--dry-run]" >&2
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version) [ "$#" -ge 2 ] || { usage; exit 2; }; version=$2; shift 2 ;;
    --version=*) version=${1#*=}; shift ;;
    --dir) [ "$#" -ge 2 ] || { usage; exit 2; }; install_dir=$2; shift 2 ;;
    --dir=*) install_dir=${1#*=}; shift ;;
    --dry-run) dry_run=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "install.sh: unknown option: $1" >&2; usage; exit 2 ;;
  esac
done

os=${GIT_A2A_TEST_OS:-$(uname -s | tr '[:upper:]' '[:lower:]')}
case "$os" in
  mingw*|msys*|cygwin*) echo "install.sh: Windows is unsupported; use https://git-a2a.com/install.ps1" >&2; exit 1 ;;
  darwin|linux) ;;
  *) echo "install.sh: unsupported operating system: $os" >&2; exit 1 ;;
esac
arch=${GIT_A2A_TEST_ARCH:-$(uname -m)}
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "install.sh: unsupported architecture: $arch" >&2; exit 1 ;;
esac

case "$version" in
  "") echo "install.sh: version must not be empty" >&2; exit 2 ;;
  latest|v*) ;;
  *) version="v$version" ;;
esac

if [ "$dry_run" = true ]; then
  shown_version=$version
  [ "$shown_version" != latest ] || shown_version='v<latest>'
  archive="git-a2a_${shown_version#v}_${os}_${arch}.tar.gz"
  echo "dry-run: resolve $version from $release_base"
  echo "dry-run: download and SHA-256 verify $archive"
  echo "dry-run: install git-a2a to $install_dir/git-a2a"
  exit 0
fi

if [ "$version" = latest ]; then
  latest_url=$(curl -fsSL -o /dev/null -w '%{url_effective}' "$release_base/latest")
  version=${latest_url##*/}
fi
case "$version" in v?*) ;; *) echo "install.sh: could not resolve a release version" >&2; exit 1 ;; esac
archive="git-a2a_${version#v}_${os}_${arch}.tar.gz"
temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT HUP INT TERM
curl -fsSL "$release_base/download/$version/$archive" -o "$temporary/$archive"
curl -fsSL "$release_base/download/$version/checksums.txt" -o "$temporary/checksums.txt"
expected=$(awk -v name="$archive" '$2 == name || $2 == "*" name { print $1; exit }' "$temporary/checksums.txt")
[ -n "$expected" ] || { echo "install.sh: $archive is absent from checksums.txt" >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$temporary/$archive" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  actual=$(shasum -a 256 "$temporary/$archive" | awk '{print $1}')
else
  echo "install.sh: neither sha256sum nor shasum is available; refusing an unverified install" >&2
  exit 1
fi
[ "$actual" = "$expected" ] || { echo "install.sh: SHA-256 mismatch for $archive" >&2; exit 1; }
tar -xzf "$temporary/$archive" -C "$temporary" git-a2a
mkdir -p "$install_dir"
[ -w "$install_dir" ] || { echo "install.sh: $install_dir is not writable; pass --dir or rerun with appropriate privileges" >&2; exit 1; }
install -m 0755 "$temporary/git-a2a" "$install_dir/git-a2a"
echo "installed git-a2a $version to $install_dir/git-a2a"
