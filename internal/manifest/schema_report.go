package manifest

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// SchemaReport returns the schema number and optional schema paths explicitly present in data.
// It is deliberately syntax-aware so false, empty strings, and empty collections remain visible.
func SchemaReport(data []byte, lock bool) (int, []string, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return 0, nil, err
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return 0, nil, fmt.Errorf("document: must be an object")
	}
	root := document.Content[0]
	schema := 0
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "schema" {
			if err := root.Content[i+1].Decode(&schema); err != nil {
				return 0, nil, fmt.Errorf("schema: %w", err)
			}
			break
		}
	}
	paths := map[string]struct{}{}
	collectSchemaPaths(root, "", lock, paths)
	for required := range requiredSchemaPaths(lock) {
		delete(paths, required)
	}
	features := make([]string, 0, len(paths))
	for feature := range paths {
		features = append(features, feature)
	}
	sort.Strings(features)
	return schema, features, nil
}

func collectSchemaPaths(node *yaml.Node, prefix string, lock bool, paths map[string]struct{}) {
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i].Value, node.Content[i+1]
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			paths[path] = struct{}{}
			if isOpenMapPath(path) {
				continue
			}
			if lock && path == "dependencies" && value.Kind == yaml.MappingNode {
				for j := 1; j < len(value.Content); j += 2 {
					collectSchemaPaths(value.Content[j], "dependencies{}", lock, paths)
				}
				continue
			}
			collectSchemaPaths(value, path, lock, paths)
		}
	case yaml.SequenceNode:
		for _, item := range node.Content {
			collectSchemaPaths(item, prefix+"[]", lock, paths)
		}
	}
}

func isOpenMapPath(path string) bool {
	return path == "policy.intents" || strings.HasSuffix(path, ".headers") ||
		strings.HasSuffix(path, ".cards")
}

func requiredSchemaPaths(lock bool) map[string]struct{} {
	if lock {
		return pathSet(
			"schema", "dependencies", "dependencies{}.git", "dependencies{}.ref",
			"dependencies{}.path", "dependencies{}.commit", "dependencies{}.manifest",
		)
	}
	return pathSet(
		"schema", "module", "module.id", "module.moved-to.git",
		"module.exports[].ecosystem", "module.exports[].name",
		"agents[].name", "agents[].role", "agents[].contacts[].intents",
		"agents[].contacts[].kind", "dependencies[].git",
	)
}

func pathSet(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}
