package a2a

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCheckOriginsRepositoryExtensionAndPins(t *testing.T) {
	card := map[string]any{
		"name":                "acme-owner",
		"supportedInterfaces": []any{map[string]any{"url": "https://agents.acme.example/a2a"}},
		"capabilities":        map[string]any{},
	}
	raw, _ := json.Marshal(card)
	if err := CheckOrigins(raw, "https://agents.acme.example/card", "https://agents.acme.example/acme/lib", "https://github.com/acme/lib", nil); err != nil {
		t.Fatal(err)
	}
	if err := CheckOrigins(raw, "https://foreign.example/card", "https://agents.acme.example/acme/lib", "https://github.com/acme/lib", nil); err == nil || !strings.Contains(err.Error(), "origin mismatch") {
		t.Fatalf("mismatch error = %v", err)
	}
	if err := CheckOrigins(raw, "https://agents.acme.example/card", "https://agents.acme.example/acme/lib", "https://github.com/acme/lib", []string{"https://agents.acme.example"}); err != nil {
		t.Fatal(err)
	}
	card["capabilities"] = map[string]any{"extensions": []any{map[string]any{
		"uri":    "https://git-a2a.com/ext/module/v1",
		"params": map[string]any{"repository": "git@github.com:acme/lib.git"},
	}}}
	raw, _ = json.Marshal(card)
	if err := CheckOrigins(raw, "https://foreign.example/card", "https://agents.acme.example/acme/lib", "https://github.com/acme/lib", nil); err != nil {
		t.Fatalf("canonical extension binding: %v", err)
	}
}
