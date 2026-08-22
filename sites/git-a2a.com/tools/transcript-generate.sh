#!/bin/sh
set -eu

tool_root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec python3 "$tool_root/generate-transcript.py" "$@"
