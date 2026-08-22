#!/usr/bin/env bash
set -euo pipefail

A2A_COMMIT=16ba52690519bf55b9388e34d4db356efa88aa51
GOOGLEAPIS_COMMIT=58bc461d695b3254a0ff88cc3541b6dca7e7a95f
JSONSCHEMA_PLUGIN_VERSION=v0.6.0
JSONSCHEMA_VALIDATOR_VERSION=v6.0.2

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

for command in git go protoc jq python3; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "a2a-schema: $command not found" >&2
    exit 2
  fi
done

git init -q "$work/A2A"
git -C "$work/A2A" remote add origin https://github.com/a2aproject/A2A.git
git -C "$work/A2A" fetch --depth 1 origin "$A2A_COMMIT"
git -C "$work/A2A" checkout --detach FETCH_HEAD
git init -q "$work/googleapis"
git -C "$work/googleapis" remote add origin https://github.com/googleapis/googleapis.git
git -C "$work/googleapis" fetch --depth 1 origin "$GOOGLEAPIS_COMMIT"
git -C "$work/googleapis" checkout --detach FETCH_HEAD

GOBIN="$work/bin" go install "github.com/bufbuild/protoschema-plugins/cmd/protoc-gen-jsonschema@$JSONSCHEMA_PLUGIN_VERSION"
PATH="$work/bin:$PATH" GOOGLEAPIS_DIR="$work/googleapis" \
  "$work/A2A/scripts/proto_to_json_schema.sh" "$work/a2a.json"
jq '{"$schema": .["$schema"], "$ref": "#/$defs/AgentCard", "$defs": .["$defs"]}' \
  "$work/a2a.json" > "$work/agent-card.schema.json"

go build -o "$work/git-a2a" ./cmd/git-a2a
(cd "$root/testdata/catalog" && "$work/git-a2a" card export local-reviewer > "$work/card.json")
(cd "$root/testdata/catalog" && "$work/git-a2a" catalog export > "$work/catalog.json")

validate() {
  (cd "$work/validator" && go run -mod=mod . "$work/agent-card.schema.json" "$1")
}

mkdir -p "$work/validator"
cat > "$work/validator/go.mod" <<EOF
module git-a2a-a2a-validator

go 1.24

require github.com/santhosh-tekuri/jsonschema/v6 $JSONSCHEMA_VALIDATOR_VERSION
EOF
cat > "$work/validator/main.go" <<'EOF'
package main

import (
  "fmt"
  "os"
  "github.com/santhosh-tekuri/jsonschema/v6"
)

func main() {
  compiler := jsonschema.NewCompiler()
  schema, err := compiler.Compile(os.Args[1])
  if err != nil { panic(err) }
  input, err := os.Open(os.Args[2])
  if err != nil { panic(err) }
  defer input.Close()
  value, err := jsonschema.UnmarshalJSON(input)
  if err != nil { panic(err) }
  if err = schema.Validate(value); err != nil { panic(err) }
  fmt.Printf("valid: %s\n", os.Args[2])
}
EOF

validate "$work/card.json"
count=$(jq '[.entries[] | select(.data != null)] | length' "$work/catalog.json")
for ((index=0; index<count; index++)); do
  jq ".entries | map(select(.data != null))[$index].data" "$work/catalog.json" > "$work/catalog-card-$index.json"
  validate "$work/catalog-card-$index.json"
done

echo "A2A conformance: card export and $count embedded catalog card(s) match AgentCard at $A2A_COMMIT"
