package cli

import (
	"encoding/json"

	"github.com/neprel/git-a2a/internal/render"
)

func dependencyMachineObject(value any, origin string, fields ...string) map[string]any {
	body, _ := json.Marshal(value)
	result := map[string]any{}
	_ = json.Unmarshal(body, &result)
	sanitizeMachineValue(result, "")
	result["origin"] = origin
	result["untrustedFields"] = fields
	return result
}

func sanitizeMachineValue(value any, key string) {
	switch value := value.(type) {
	case map[string]any:
		for childKey, child := range value {
			if text, ok := child.(string); ok {
				value[childKey] = render.SanitizeMachineText(text, childKey == "description")
				continue
			}
			sanitizeMachineValue(child, childKey)
		}
	case []any:
		for index, child := range value {
			if text, ok := child.(string); ok {
				value[index] = render.SanitizeMachineText(text, key == "description")
				continue
			}
			sanitizeMachineValue(child, key)
		}
	}
}

func dependencyOrigin(id, commit string) string {
	if len(commit) > 7 {
		commit = commit[:7]
	}
	if commit == "" {
		commit = "unlocked"
	}
	return id + "@" + commit
}
