#!/bin/sh
set -eu

required_vars="
GITA2A_PILOT_DIR
GITA2A_PILOT_LIB_URL
GITA2A_PILOT_CLI_URL
GITA2A_PILOT_LIB_CARD
GITA2A_PILOT_CLI_CARD
GITA2A_PILOT_SPEC_CARD
GITA2A_PILOT_SURFACE_FILE
"
for name in $required_vars; do
  eval "value=\${$name-}"
  if [ -z "$value" ]; then
    echo "pilot: $name is required" >&2
    exit 2
  fi
done

case "$GITA2A_PILOT_DIR" in
  /*) ;;
  *) echo "pilot: GITA2A_PILOT_DIR must be absolute" >&2; exit 2 ;;
esac
repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
case "$GITA2A_PILOT_DIR/" in
  "$repo_root/"*) echo "pilot: workspace must be outside the git-a2a repository" >&2; exit 2 ;;
esac
if [ -e "$GITA2A_PILOT_DIR" ]; then
  echo "pilot: workspace already exists: $GITA2A_PILOT_DIR" >&2
  exit 2
fi

git_a2a=${GITA2A_BIN:-git-a2a}
lib_ref=${GITA2A_PILOT_LIB_REF:-main}
cli_ref=${GITA2A_PILOT_CLI_REF:-main}
branch=${GITA2A_PILOT_BRANCH:-git-a2a-pilot}
lib_id=${GITA2A_PILOT_LIB_ID:-acme-lib-utils}
cli_id=${GITA2A_PILOT_CLI_ID:-acme-app-cli}
spec_id=${GITA2A_PILOT_SPEC_ID:-acme-pm}
lib_npm=${GITA2A_PILOT_LIB_NPM:-@acme/lib-utils}
lib_pypi=${GITA2A_PILOT_LIB_PYPI:-acme_lib_utils}
lib_go=${GITA2A_PILOT_LIB_GO:-acme.dev/lib-utils}
cli_npm=${GITA2A_PILOT_CLI_NPM:-@acme/app-cli}
cli_pypi=${GITA2A_PILOT_CLI_PYPI:-acme-app-cli}
cli_go=${GITA2A_PILOT_CLI_GO:-acme.dev/app-cli}

mkdir -p "$GITA2A_PILOT_DIR"
git clone --branch "$lib_ref" --single-branch "$GITA2A_PILOT_LIB_URL" "$GITA2A_PILOT_DIR/library"
git clone --branch "$cli_ref" --single-branch "$GITA2A_PILOT_CLI_URL" "$GITA2A_PILOT_DIR/consumer"
git -C "$GITA2A_PILOT_DIR/library" switch -c "$branch"
git -C "$GITA2A_PILOT_DIR/consumer" switch -c "$branch"

mkdir -p "$GITA2A_PILOT_DIR/library/surface"
cp "$GITA2A_PILOT_SURFACE_FILE" "$GITA2A_PILOT_DIR/library/surface/API.md"
cat >"$GITA2A_PILOT_DIR/library/a2amodule.yml" <<EOF
schema: 1
module:
  id: $lib_id
  description: Shared utilities published for npm, PyPI and Go.
  repository: $GITA2A_PILOT_LIB_URL
  surface: surface/
  release: {channel: $lib_ref, tags: false}
  exports:
    - {ecosystem: npm, name: "$lib_npm"}
    - {ecosystem: pypi, name: $lib_pypi}
    - {ecosystem: golang, name: $lib_go}
agents:
  - name: $lib_id
    role: owner
    scope: ["**"]
    card: $GITA2A_PILOT_LIB_CARD
    contacts:
      - {intents: [question, review], kind: url, url: $GITA2A_PILOT_LIB_CARD}
  - name: $spec_id
    role: spec
    scope: ["specs/**", "openspec/**", ".specify/**"]
    card: $GITA2A_PILOT_SPEC_CARD
    contacts:
      - {intents: [change], kind: url, url: $GITA2A_PILOT_SPEC_CARD}
policy:
  intents: {change: spec}
  consumers: {may: [read-surface, ask], may-not: [commit]}
EOF

"$git_a2a" validate "$GITA2A_PILOT_DIR/library/a2amodule.yml"

consumer="$GITA2A_PILOT_DIR/consumer"
(cd "$consumer" && "$git_a2a" --timeout 120s init --id "$cli_id" \
  --export "npm=$cli_npm" --export "pypi=$cli_pypi" --export "golang=$cli_go")
cat >"$consumer/a2amodule.yml" <<EOF
schema: 1
module:
  id: $cli_id
  description: Consumer CLI built for npm, PyPI and Go.
  repository: $GITA2A_PILOT_CLI_URL
  exports:
    - {ecosystem: npm, name: "$cli_npm"}
    - {ecosystem: pypi, name: $cli_pypi}
    - {ecosystem: golang, name: $cli_go}
agents:
  - name: $cli_id
    role: owner
    scope: ["**"]
    card: $GITA2A_PILOT_CLI_CARD
    contacts:
      - {intents: [question, review, bug], kind: url, url: $GITA2A_PILOT_CLI_CARD}
  - name: $spec_id
    role: spec
    scope: ["specs/**", "openspec/**", ".specify/**"]
    card: $GITA2A_PILOT_SPEC_CARD
    contacts:
      - {intents: [change], kind: url, url: $GITA2A_PILOT_SPEC_CARD}
policy:
  intents: {change: spec}
  consumers: {may: [ask], may-not: [commit]}
EOF
"$git_a2a" validate "$consumer/a2amodule.yml"

echo "pilot: commit and publish $branch in the library, then run:" >&2
echo "  cd $consumer" >&2
echo "  $git_a2a add '$GITA2A_PILOT_LIB_URL#$branch'" >&2
echo "  $git_a2a sync" >&2
echo "  $git_a2a who $lib_id --intent change" >&2
echo "  $git_a2a status $lib_id --offline" >&2
echo "  $git_a2a status $lib_id" >&2
echo "  $git_a2a update --check" >&2
echo "pilot: run yarn install, uv sync, and GOPRIVATE=... go build ./... when present" >&2
echo "pilot: review and commit both branches; this script never pushes or opens a PR" >&2
