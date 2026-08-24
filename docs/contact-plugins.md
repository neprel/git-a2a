# Contact driver plugins

A consumer can support an open contact kind by placing `git-a2a-contact-<kind>` on its own
`PATH`. The dependency does not select a path or install code. Plugin discovery precedes built-in
drivers, and the delivery record names `driver=plugin:git-a2a-contact-<kind>`.

git-a2a writes one JSON object to stdin:

```json
{"protocol":1,"kind":"acme-tracker","intent":"change","module":"acme-lib","origin":"https://git.example/acme/lib","contact":{"intents":["change"],"kind":"acme-tracker","queue":"platform"},"message":"Please change the API.","dry-run":false}
```

The plugin writes one JSON object to stdout: `id`, `state` (`created`, `sent`, or `instruction`),
and optional `url` and `note`. Exit `0` means delivered, `1` means failed, and `2` means refused.
git-a2a stops the process after 60 seconds and forwards stderr as prefixed diagnostics. Plugins
receive the consumer process environment; declarations never add variables or credentials.

## Shell example

```sh
#!/bin/sh
set -eu
request=$(cat)
kind=$(printf '%s' "$request" | jq -r .kind)
message=$(printf '%s' "$request" | jq -r .message)
if [ "$kind" != acme-tracker ]; then
  echo 'unsupported kind' >&2
  exit 2
fi
id=$(acme-tracker create --message "$message") || exit 1
jq -n --arg id "$id" \
  '{id: $id, state: "created"}'
```

## Python example

```python
#!/usr/bin/env python3
import json
import subprocess
import sys

request = json.load(sys.stdin)
if request["protocol"] != 1:
    raise SystemExit(2)
result = subprocess.run(
    ["acme-tracker", "create", "--message", request["message"]],
    check=False,
    capture_output=True,
    text=True,
)
if result.returncode:
    print(result.stderr, file=sys.stderr)
    raise SystemExit(1)
json.dump({"id": result.stdout.strip(), "state": "created"}, sys.stdout)
print()
```

Use `git-a2a contact [ID] --list-drivers` to inspect the selected layer without sending a
request. MCP uses the same plugin lookup. The declared `exec` kind is the exception: MCP refuses
it even when the consumer allowlists the binary, because model-triggered local execution remains
a CLI operator decision.
