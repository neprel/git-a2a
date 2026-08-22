package version

import (
	"regexp"
	"testing"
)

func TestCurrentComesFromEmbeddedVersionFile(t *testing.T) {
	if got := Current(); !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`).MatchString(got) {
		t.Fatalf("Current() = %q, want a release version without leading v", got)
	}
}
