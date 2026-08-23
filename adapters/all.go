package adapters

import (
	"github.com/neprel/git-a2a/adapters/cargo"
	"github.com/neprel/git-a2a/adapters/clojure"
	"github.com/neprel/git-a2a/adapters/cmake"
	"github.com/neprel/git-a2a/adapters/composer"
	"github.com/neprel/git-a2a/adapters/gem"
	"github.com/neprel/git-a2a/adapters/golang"
	"github.com/neprel/git-a2a/adapters/gradle"
	"github.com/neprel/git-a2a/adapters/hackage"
	"github.com/neprel/git-a2a/adapters/hex"
	"github.com/neprel/git-a2a/adapters/maven"
	"github.com/neprel/git-a2a/adapters/msbuild"
	"github.com/neprel/git-a2a/adapters/nix"
	"github.com/neprel/git-a2a/adapters/npm"
	pubadapter "github.com/neprel/git-a2a/adapters/pub"
	"github.com/neprel/git-a2a/adapters/pypi"
	"github.com/neprel/git-a2a/adapters/swift"
	"github.com/neprel/git-a2a/adapters/zig"
	"github.com/neprel/git-a2a/internal/adapter"
)

func All() []adapter.Adapter {
	return []adapter.Adapter{npm.Adapter{}, pypi.Adapter{}, golang.Adapter{}, cargo.Adapter{}, swift.Adapter{}, pubadapter.Adapter{}, gem.Adapter{}, composer.Adapter{}, hex.Adapter{}, hackage.Adapter{}, zig.Adapter{}, clojure.Adapter{}, nix.Adapter{}, cmake.Adapter{}, gradle.Adapter{}, msbuild.Adapter{}, maven.Adapter{}}
}

// Verification reports the strongest real-toolchain evidence currently recorded for an adapter.
func Verification(ecosystem string) string {
	switch ecosystem {
	case "npm", "pypi", "golang", "cargo", "swift", "pub", "gem", "composer", "hex", "hackage", "zig", "clojure", "nix", "cmake", "maven", "nuget":
		return "verified"
	default:
		return "form-verified"
	}
}
