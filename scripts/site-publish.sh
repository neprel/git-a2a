#!/usr/bin/env bash
set -euo pipefail

repository_root=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
environment_file=${SITE_DEPLOY_ENV_FILE:-"$repository_root/.env"}

if [[ ! -f "$environment_file" ]]; then
  echo "site-publish: environment file not found: $environment_file" >&2
  echo "site-publish: copy .env.example to .env and fill its values" >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
source "$environment_file"
set +a

required=(SITE_DEPLOY_HOST SITE_DEPLOY_USER SITE_DEPLOY_PATH)
for name in "${required[@]}"; do
  if [[ -z "${!name:-}" ]]; then
    echo "site-publish: required value is missing from .env: $name" >&2
    exit 1
  fi
done
SITE_DEPLOY_PORT=${SITE_DEPLOY_PORT:-22}

temporary=$(mktemp -d "${TMPDIR:-/tmp}/git-a2a-site-publish.XXXXXX")
trap 'rm -rf -- "$temporary"' EXIT
package="$temporary/public"
ssh_config="$temporary/ssh_config"

"$repository_root/scripts/site-package.sh" "$package"

{
  echo 'Host site-production'
  echo "  HostName $SITE_DEPLOY_HOST"
  echo "  User $SITE_DEPLOY_USER"
  echo "  Port $SITE_DEPLOY_PORT"
  echo '  BatchMode yes'
  echo '  IdentitiesOnly yes'
  echo '  StrictHostKeyChecking yes'
  echo '  LogLevel ERROR'
  if [[ -n "${SITE_DEPLOY_IDENTITY_FILE:-}" ]]; then
    printf '  IdentityFile "%s"\n' "$SITE_DEPLOY_IDENTITY_FILE"
  fi
  if [[ -n "${SITE_DEPLOY_KNOWN_HOSTS_FILE:-}" ]]; then
    printf '  UserKnownHostsFile "%s"\n' "$SITE_DEPLOY_KNOWN_HOSTS_FILE"
  fi
} > "$ssh_config"
chmod 600 "$ssh_config"

scp -F "$ssh_config" -r "$package/." "site-production:${SITE_DEPLOY_PATH}"
echo "site-publish: upload complete"
