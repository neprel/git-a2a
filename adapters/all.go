package adapters

import (
	"github.com/neprel/git-a2a/v2/adapters/cargo"
	"github.com/neprel/git-a2a/v2/adapters/clojure"
	"github.com/neprel/git-a2a/v2/adapters/cmake"
	"github.com/neprel/git-a2a/v2/adapters/composer"
	"github.com/neprel/git-a2a/v2/adapters/gem"
	"github.com/neprel/git-a2a/v2/adapters/golang"
	"github.com/neprel/git-a2a/v2/adapters/gradle"
	"github.com/neprel/git-a2a/v2/adapters/hackage"
	"github.com/neprel/git-a2a/v2/adapters/hex"
	"github.com/neprel/git-a2a/v2/adapters/maven"
	"github.com/neprel/git-a2a/v2/adapters/meson"
	"github.com/neprel/git-a2a/v2/adapters/msbuild"
	"github.com/neprel/git-a2a/v2/adapters/nix"
	"github.com/neprel/git-a2a/v2/adapters/npm"
	pubadapter "github.com/neprel/git-a2a/v2/adapters/pub"
	"github.com/neprel/git-a2a/v2/adapters/pypi"
	"github.com/neprel/git-a2a/v2/adapters/submodule"
	"github.com/neprel/git-a2a/v2/adapters/swift"
	"github.com/neprel/git-a2a/v2/adapters/zig"
	"github.com/neprel/git-a2a/v2/internal/adapter"
)

func All() []adapter.Adapter {
	return []adapter.Adapter{submodule.Adapter{}, npm.Adapter{}, pypi.Adapter{}, golang.Adapter{}, cargo.Adapter{}, swift.Adapter{}, pubadapter.Adapter{}, gem.Adapter{}, composer.Adapter{}, hex.Adapter{}, hackage.Adapter{}, zig.Adapter{}, clojure.Adapter{}, nix.Adapter{}, cmake.Adapter{}, gradle.Adapter{}, msbuild.Adapter{}, maven.Adapter{}, meson.Adapter{}}
}

// Verification reports the strongest real-toolchain evidence currently recorded for an adapter.
func Verification(ecosystem string) string {
	switch ecosystem {
	case "submodule", "npm", "pypi", "golang", "cargo", "swift", "pub", "gem", "composer", "hex", "hackage", "zig", "clojure", "nix", "cmake", "maven", "nuget", "meson":
		return "verified"
	default:
		return "form-verified"
	}
}
