#!/bin/sh
set -eu

export COMPOSER_ALLOW_SUPERUSER=1
export GIT_ALLOW_PROTOCOL=file
export GIT_CONFIG_NOSYSTEM=1
export GIT_TERMINAL_PROMPT=0
cli=${GITA2A_CLI_BIN:-/opt/git-a2a}

echo "os=$(uname -s) arch=$(uname -m)"
git --version
php --version | sed -n '1p'
composer --version
"$cli" --version

work=/tmp/git-a2a-composer
mkdir -p "$work/upstream/src" "$work/consumer/unrelated/src"

cd "$work/upstream"
git init -b main
git config user.email native@example.test
git config user.name Native
cat >a2amodule.yml <<'EOF'
schema: 2
component:
  id: fixture-composer
  exports:
    - adapter: composer
      name: acme/fixture
agent:
  card: https://agents.example/fixture-composer.json
EOF
cat >composer.json <<'EOF'
{"name":"acme/fixture","version":"1.0.0","autoload":{"psr-4":{"Acme\\Fixture\\":"src/"}}}
EOF
cat >src/Value.php <<'EOF'
<?php
namespace Acme\Fixture;
final class Value { public static function get(): string { return 'v1'; } }
EOF
git add .
git commit -m v1
first_commit=$(git rev-parse HEAD)

cd "$work/consumer"
cat >unrelated/composer.json <<'EOF'
{"name":"acme/unrelated","version":"1.0.0","autoload":{"psr-4":{"Acme\\Unrelated\\":"src/"}}}
EOF
cat >unrelated/src/Value.php <<'EOF'
<?php
namespace Acme\Unrelated;
final class Value { public static function get(): string { return 'kept'; } }
EOF
cat >composer.json <<'EOF'
{
  "name": "acme/consumer",
  "repositories": [{"type":"path","url":"unrelated","options":{"symlink":false}}],
  "require": {"acme/unrelated":"1.0.0"}
}
EOF

# Begin with a real pre-existing native lock and unrelated installation.
composer install --no-interaction --no-plugins --no-scripts
cp composer.lock "$work/unrelated.lock"

"$cli" init --id consumer
"$cli" add "file://$work/upstream" --name fixture --ref main
test "$(php -r "require 'vendor/autoload.php'; echo Acme\\Fixture\\Value::get().'/'.Acme\\Unrelated\\Value::get();")" = v1/kept
test "$(composer show acme/unrelated --format=json | php -r '$j=json_decode(stream_get_contents(STDIN),true); echo $j["versions"][0];')" = 1.0.0
grep -q "${first_commit}" composer.lock
"$cli" list >/dev/null

# Losing only the installed component must be repaired at the same commit;
# neither the native declaration nor lock should receive meaningless changes.
cp composer.json "$work/composer.json.after-add"
cp composer.lock "$work/composer.lock.after-add"
rm -rf vendor/acme/fixture
"$cli" pull fixture
test "$(php -r "require 'vendor/autoload.php'; echo Acme\\Fixture\\Value::get().'/'.Acme\\Unrelated\\Value::get();")" = v1/kept
cmp "$work/composer.json.after-add" composer.json
cmp "$work/composer.lock.after-add" composer.lock

# A fresh clone has declarations and locks but no vendor directory.
git init -b main
git config user.email native@example.test
git config user.name Native
printf '\n/vendor/\n' >>.gitignore
git add .
git commit -m installed
git clone . "$work/fresh"
cd "$work/fresh"
"$cli" pull fixture
test "$(php -r "require 'vendor/autoload.php'; echo Acme\\Fixture\\Value::get().'/'.Acme\\Unrelated\\Value::get();")" = v1/kept

# Advance the branch without changing the Composer package version. A targeted
# public pull must install exactly the new Git revision and retain unrelated.
cd "$work/upstream"
sed -i "s/'v1'/'v2'/" src/Value.php
git add .
git commit -m v2
second_commit=$(git rev-parse HEAD)

cd "$work/consumer"
"$cli" pull fixture
test "$(php -r "require 'vendor/autoload.php'; echo Acme\\Fixture\\Value::get().'/'.Acme\\Unrelated\\Value::get();")" = v2/kept
grep -q "${second_commit}" composer.lock
! grep -q "${first_commit}" composer.lock
test "$(composer show acme/unrelated --format=json | php -r '$j=json_decode(stream_get_contents(STDIN),true); echo $j["versions"][0];')" = 1.0.0

# An idempotent pull at the saved revision leaves declarations and native lock
# byte-identical.
cp composer.json "$work/composer.json.after-update"
cp composer.lock "$work/composer.lock.after-update"
"$cli" pull fixture
cmp "$work/composer.json.after-update" composer.json
cmp "$work/composer.lock.after-update" composer.lock

"$cli" remove fixture
! grep -q 'acme/fixture' composer.json
! grep -q 'acme/fixture' composer.lock
test ! -e vendor/acme/fixture
test "$(php -r "require 'vendor/autoload.php'; echo Acme\\Unrelated\\Value::get();")" = kept
test "$(composer show acme/unrelated --format=json | php -r '$j=json_decode(stream_get_contents(STDIN),true); echo $j["versions"][0];')" = 1.0.0

echo COMPOSER_NATIVE_PASS
