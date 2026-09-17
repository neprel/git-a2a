#!/bin/sh
set -eux

cli=${GITA2A_CLI_BIN:-/opt/git-a2a}
test -x "$cli"
HOME=${HOME:-/tmp/home}
PUB_CACHE=${PUB_CACHE:-/tmp/pub-cache}
export HOME PUB_CACHE
export GIT_ALLOW_PROTOCOL=file GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0

work=$(mktemp -d /tmp/git-a2a-pub-native.XXXXXX)
mkdir -p "$HOME" "$PUB_CACHE" "$work"

git_init() {
  root=$1
  mkdir -p "$root"
  git -C "$root" init -b main
  git -C "$root" config user.email native@example.test
  git -C "$root" config user.name Native
}

git_commit() {
  root=$1
  message=$2
  git -C "$root" add -A
  git -C "$root" commit -m "$message" >/dev/null
  git -C "$root" rev-parse HEAD
}

write_upstream() {
  root=$1
  component=$2
  package=$3
  value=$4
  mkdir -p "$root/lib"
  cat >"$root/a2amodule.yml" <<EOF
schema: 2
component:
  id: $component
  exports:
    - adapter: pub
      name: $package
agent:
  card: https://agents.example/$component.json
EOF
  cat >"$root/pubspec.yaml" <<EOF
name: $package
version: 1.0.0
environment:
  sdk: ^3.0.0
EOF
  printf "const value = '%s';\n" "$value" >"$root/lib/$package.dart"
}

assert_lock_commit() {
  package=$1
  commit=$2
  grep -A9 "^  $package:" pubspec.lock | grep -q "$commit"
}

target=$work/target
stable=$work/stable
git_init "$target"
write_upstream "$target" fixture-pub fixture_pub v1
target_v1=$(git_commit "$target" target-v1)
git_init "$stable"
write_upstream "$stable" stable-pub stable_pub stable-v1
stable_v1=$(git_commit "$stable" stable-v1)

consumer=$work/consumer
git_init "$consumer"
mkdir -p "$consumer/unrelated/lib"
cat >"$consumer/pubspec.yaml" <<'EOF'
name: consumer
version: 1.0.0
environment:
  sdk: ^3.0.0
dependencies:
  unrelated:
    path: unrelated
EOF
cat >"$consumer/unrelated/pubspec.yaml" <<'EOF'
name: unrelated
version: 7.0.0
environment:
  sdk: ^3.0.0
EOF
printf "const unrelated = 'kept';\n" >"$consumer/unrelated/lib/unrelated.dart"
cat >"$consumer/check.dart" <<'EOF'
import 'package:fixture_pub/fixture_pub.dart' as target;
import 'package:stable_pub/stable_pub.dart' as stable;
import 'package:unrelated/unrelated.dart';
void main() { print('${target.value}/${stable.value}/$unrelated'); }
EOF
cat >"$consumer/check_remaining.dart" <<'EOF'
import 'package:stable_pub/stable_pub.dart';
import 'package:unrelated/unrelated.dart';
void main() { print('$value/$unrelated'); }
EOF

cd "$consumer"
"$cli" init --id consumer
printf '.dart_tool/\n' >>.gitignore
"$cli" add "file://$target" --name fixture --ref main
"$cli" add "file://$stable" --name stable --ref main
test "$(dart run check.dart)" = 'v1/stable-v1/kept'
assert_lock_commit fixture_pub "$target_v1"
assert_lock_commit stable_pub "$stable_v1"
grep -q "$target_v1" a2amodule.lock
grep -q "$stable_v1" a2amodule.lock

# Pull at the unchanged commit must reconstruct project-local materialization.
cp pubspec.lock "$work/pubspec.lock.before-repair"
rm -rf .dart_tool
"$cli" pull fixture
test "$(dart run check.dart)" = 'v1/stable-v1/kept'
cmp "$work/pubspec.lock.before-repair" pubspec.lock

# A fresh Git clone has no .dart_tool state and must become immediately usable.
rm -rf .dart_tool
git add -A
git commit -m installed
git clone "$consumer" "$work/fresh"
cd "$work/fresh"
test ! -e .dart_tool/package_config.json
"$cli" pull fixture
test "$(dart run check.dart)" = 'v1/stable-v1/kept'

# Advance both repositories without changing package versions. Targeted pull
# updates only the requested dependency and its native lock evidence.
write_upstream "$target" fixture-pub fixture_pub v2
target_v2=$(git_commit "$target" target-v2)
write_upstream "$stable" stable-pub stable_pub stable-v2-not-selected
stable_v2=$(git_commit "$stable" stable-v2)
test "$stable_v1" != "$stable_v2"
cd "$consumer"
"$cli" pull fixture
test "$(dart run check.dart)" = 'v2/stable-v1/kept'
assert_lock_commit fixture_pub "$target_v2"
assert_lock_commit stable_pub "$stable_v1"
grep -q "$target_v2" a2amodule.lock
grep -q "$stable_v1" a2amodule.lock
! grep -q "$stable_v2" a2amodule.lock

# A second pull is idempotent across both git-a2a and Pub-owned files.
git add -A
git commit -m target-v2-applied
"$cli" pull fixture
test -z "$(git status --porcelain)"

# Remove converges the root declaration, native lock, and package config while
# preserving both a separate git-a2a dependency and an unrelated path package.
"$cli" remove fixture
! grep -q '^  fixture_pub:' pubspec.yaml
! grep -q '^  fixture_pub:' pubspec.lock
! grep -q '"name": "fixture_pub"' .dart_tool/package_config.json
grep -q '^  stable_pub:' pubspec.yaml
assert_lock_commit stable_pub "$stable_v1"
grep -q '^  unrelated:' pubspec.yaml
grep -q '^  unrelated:' pubspec.lock
test "$(dart run check_remaining.dart)" = 'stable-v1/kept'

dart --version
git --version
echo PUB_PUBLIC_CLI_NATIVE_PASS
