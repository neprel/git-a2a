package template

import (
	"fmt"
	"strings"
)

// Expand substitutes lowercase-word placeholders and decodes doubled literal braces.
// Braces that do not form {[a-z]+} are copied verbatim.
func Expand(value string, replacements map[string]string) (string, error) {
	var result strings.Builder
	result.Grow(len(value))
	for i := 0; i < len(value); {
		if i+1 < len(value) && value[i] == '{' && value[i+1] == '{' {
			result.WriteByte('{')
			i += 2
			continue
		}
		if i+1 < len(value) && value[i] == '}' && value[i+1] == '}' {
			result.WriteByte('}')
			i += 2
			continue
		}
		if value[i] == '{' {
			end := i + 1
			for end < len(value) && value[end] >= 'a' && value[end] <= 'z' {
				end++
			}
			if end > i+1 && end < len(value) && value[end] == '}' {
				name := value[i+1 : end]
				replacement, ok := replacements[name]
				if !ok {
					return "", fmt.Errorf("unsupported placeholder {%s}", name)
				}
				result.WriteString(replacement)
				i = end + 1
				continue
			}
		}
		result.WriteByte(value[i])
		i++
	}
	return result.String(), nil
}

// Names returns unescaped lowercase-word placeholder names in declaration order.
func Names(value string) []string {
	var result []string
	seen := map[string]bool{}
	for i := 0; i < len(value); {
		if i+1 < len(value) && ((value[i] == '{' && value[i+1] == '{') || (value[i] == '}' && value[i+1] == '}')) {
			i += 2
			continue
		}
		if value[i] != '{' {
			i++
			continue
		}
		end := i + 1
		for end < len(value) && value[end] >= 'a' && value[end] <= 'z' {
			end++
		}
		if end > i+1 && end < len(value) && value[end] == '}' {
			name := value[i+1 : end]
			if !seen[name] {
				result = append(result, name)
				seen[name] = true
			}
			i = end + 1
			continue
		}
		i++
	}
	return result
}
