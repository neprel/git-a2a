package adapters

import "testing"

func TestVerificationLabels(t *testing.T) {
	for _, ecosystem := range []string{"gem", "composer", "hex", "hackage", "zig", "clojure", "nix"} {
		if got := Verification(ecosystem); got != "form-verified" {
			t.Errorf("Verification(%q) = %q", ecosystem, got)
		}
	}
	for _, ecosystem := range []string{"npm", "pypi", "golang", "cargo", "swift", "pub"} {
		if got := Verification(ecosystem); got != "verified" {
			t.Errorf("Verification(%q) = %q", ecosystem, got)
		}
	}
}
