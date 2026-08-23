package adapters

import "testing"

func TestVerificationLabels(t *testing.T) {
	for _, ecosystem := range []string{"npm", "pypi", "golang", "cargo", "swift", "pub", "gem", "composer", "hex", "hackage", "zig", "clojure", "nix", "cmake", "maven"} {
		if got := Verification(ecosystem); got != "verified" {
			t.Errorf("Verification(%q) = %q", ecosystem, got)
		}
	}
	if got := Verification("future"); got != "form-verified" {
		t.Fatalf("unknown adapter evidence = %q", got)
	}
}
