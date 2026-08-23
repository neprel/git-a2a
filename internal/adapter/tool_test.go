package adapter

import (
	"context"
	"strings"
	"testing"
)

func TestToolMappingUsesDetectedVariant(t *testing.T) {
	for _, test := range []struct {
		ecosystem string
		variant   Variant
		want      string
	}{
		{"npm", "yarn-berry", "yarn"}, {"npm", "pnpm", "pnpm"}, {"pypi", "uv", "uv"},
		{"pypi", "pep621", "pip"}, {"golang", "go", "go"}, {"hackage", "stack", "stack"},
		{"maven", "gradle-kts", "gradle"}, {"maven", "maven", "mvn"},
		{"nuget", "msbuild-csharp", "dotnet"},
	} {
		if got := ToolFor(test.ecosystem, test.variant).Command; got != test.want {
			t.Errorf("ToolFor(%s, %s) = %s, want %s", test.ecosystem, test.variant, got, test.want)
		}
	}
}

func TestRequireToolUsesActionableSharedError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := RequireTool(context.Background(), "zig", "zon")
	if err == nil || !IsToolUnavailable(err) || !strings.Contains(err.Error(), "zig: zig not found — install:") {
		t.Fatalf("err=%v", err)
	}
}

func TestNumericVersionComparison(t *testing.T) {
	if got := numericVersion("git version 2.25.1.windows.1"); got != "2.25.1" {
		t.Fatalf("version=%q", got)
	}
	if compareVersions("2.24.9", "2.25.0") >= 0 {
		t.Fatal("old version accepted")
	}
	if compareVersions("2.27", "2.25.0") < 0 {
		t.Fatal("new version rejected")
	}
}
