package adapter

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

type ToolRequirement struct {
	Ecosystem  string   `json:"ecosystem"`
	Command    string   `json:"command"`
	VersionArg []string `json:"versionArgs"`
	Minimum    string   `json:"minimum,omitempty"`
	Install    string   `json:"install"`
}

type ToolStatus struct {
	ToolRequirement
	Found   bool   `json:"found"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
	Ready   bool   `json:"ready"`
}

type MissingToolError struct{ Requirement ToolRequirement }

func (e MissingToolError) Error() string {
	return fmt.Sprintf("%s: %s not found — install: %s", e.Requirement.Ecosystem, e.Requirement.Command, e.Requirement.Install)
}

type ToolVersionError struct {
	Requirement ToolRequirement
	Version     string
}

func (e ToolVersionError) Error() string {
	return fmt.Sprintf("%s: %s %s is too old (need >= %s) — install: %s", e.Requirement.Ecosystem, e.Requirement.Command, e.Version, e.Requirement.Minimum, e.Requirement.Install)
}

func IsMissingTool(err error) bool {
	var target MissingToolError
	return errors.As(err, &target)
}

func IsToolUnavailable(err error) bool {
	var missing MissingToolError
	var old ToolVersionError
	return errors.As(err, &missing) || errors.As(err, &old)
}

func ToolFor(ecosystem string, variant Variant) ToolRequirement {
	command := ecosystem
	versionArg := []string{"--version"}
	switch ecosystem {
	case "npm":
		command = string(variant)
		if command == "yarn-berry" {
			command = "yarn"
		}
		if command == "" {
			command = "npm"
		}
	case "pypi":
		command = string(variant)
		if command == "pep621" || command == "" {
			command = "pip"
		}
	case "golang":
		command, versionArg = "go", []string{"version"}
	case "pub":
		command = "dart"
	case "gem":
		command = "bundle"
	case "hex":
		command = "mix"
	case "hackage":
		command = string(variant)
		if command == "" {
			command = "cabal"
		}
	case "clojure":
		command, versionArg = "clj", []string{"-Sdescribe"}
	case "maven":
		if strings.HasPrefix(string(variant), "gradle-") {
			command = "gradle"
		} else {
			command = "mvn"
		}
	case "nuget":
		command = "dotnet"
	}
	return ToolRequirement{Ecosystem: ecosystem, Command: command, VersionArg: versionArg, Install: installHint(command)}
}

func GitTool() ToolRequirement {
	return ToolRequirement{Ecosystem: "git", Command: "git", VersionArg: []string{"--version"}, Minimum: "2.25.0", Install: installHint("git")}
}

func InspectTool(ctx context.Context, requirement ToolRequirement) ToolStatus {
	status := ToolStatus{ToolRequirement: requirement}
	path, err := exec.LookPath(requirement.Command)
	if err != nil {
		return status
	}
	status.Found, status.Path = true, path
	output, err := exec.CommandContext(ctx, path, requirement.VersionArg...).CombinedOutput()
	status.Version = firstLine(strings.TrimSpace(string(output)))
	if err != nil && status.Version == "" {
		status.Version = "unknown"
	}
	status.Ready = true
	if requirement.Minimum != "" {
		version := numericVersion(status.Version)
		status.Ready = version != "" && compareVersions(version, requirement.Minimum) >= 0
	}
	return status
}

func RequireTool(ctx context.Context, ecosystem string, variant Variant) error {
	requirement := ToolFor(ecosystem, variant)
	status := InspectTool(ctx, requirement)
	if !status.Found {
		return MissingToolError{Requirement: requirement}
	}
	if !status.Ready {
		return ToolVersionError{Requirement: requirement, Version: numericVersion(status.Version)}
	}
	return nil
}

func firstLine(value string) string {
	if line, _, ok := strings.Cut(value, "\n"); ok {
		return line
	}
	return value
}

func numericVersion(value string) string {
	return regexp.MustCompile(`[0-9]+(?:\.[0-9]+)+`).FindString(value)
}

func compareVersions(left, right string) int {
	a, b := strings.Split(left, "."), strings.Split(right, ".")
	for len(a) < len(b) {
		a = append(a, "0")
	}
	for len(b) < len(a) {
		b = append(b, "0")
	}
	for i := range a {
		av, _ := strconv.Atoi(strings.TrimRightFunc(a[i], func(r rune) bool { return r < '0' || r > '9' }))
		bv, _ := strconv.Atoi(strings.TrimRightFunc(b[i], func(r rune) bool { return r < '0' || r > '9' }))
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func VersionAtLeast(versionText, minimum string) bool {
	version := numericVersion(versionText)
	return version != "" && compareVersions(version, minimum) >= 0
}

func installHint(command string) string {
	if runtime.GOOS == "darwin" {
		packages := map[string]string{"git": "git", "npm": "node", "yarn": "yarn", "pnpm": "pnpm", "bun": "oven-sh/bun/bun", "uv": "uv", "pip": "python", "poetry": "poetry", "pdm": "pdm", "go": "go", "cargo": "rust", "swift": "swift", "dart": "dart", "bundle": "ruby", "composer": "composer", "mix": "elixir", "cabal": "cabal-install", "stack": "haskell-stack", "zig": "zig", "clj": "clojure/tools/clojure", "nix": "nix", "cmake": "cmake", "gradle": "gradle", "mvn": "maven", "meson": "meson"}
		if name := packages[command]; name != "" {
			return "brew install " + name
		}
	}
	if runtime.GOOS == "windows" {
		ids := map[string]string{"git": "Git.Git", "npm": "OpenJS.NodeJS.LTS", "go": "GoLang.Go", "cargo": "Rustlang.Rustup", "dart": "Google.DartSDK", "composer": "Composer.Composer", "zig": "zig.zig", "cmake": "Kitware.CMake", "meson": "mesonbuild.meson"}
		if id := ids[command]; id != "" {
			return "winget install --id " + id
		}
	}
	if runtime.GOOS == "linux" {
		packages := map[string]string{"git": "git", "npm": "nodejs npm", "go": "golang-go", "cargo": "cargo", "ruby": "ruby", "bundle": "ruby-bundler", "composer": "composer", "mix": "elixir", "cabal": "cabal-install", "clj": "clojure", "cmake": "cmake", "gradle": "gradle", "mvn": "maven", "meson": "meson ninja-build"}
		if name := packages[command]; name != "" {
			return "sudo apt-get install " + name
		}
	}
	urls := map[string]string{
		"yarn": "https://yarnpkg.com/getting-started/install", "pnpm": "https://pnpm.io/installation", "bun": "https://bun.sh/docs/installation",
		"uv": "https://docs.astral.sh/uv/getting-started/installation/", "pip": "https://pip.pypa.io/en/stable/installation/", "poetry": "https://python-poetry.org/docs/#installation", "pdm": "https://pdm-project.org/latest/#installation",
		"swift": "https://www.swift.org/install/", "dart": "https://dart.dev/get-dart", "bundle": "https://bundler.io/guides/getting_started.html", "composer": "https://getcomposer.org/download/",
		"mix": "https://elixir-lang.org/install.html", "cabal": "https://www.haskell.org/ghcup/install/", "stack": "https://docs.haskellstack.org/en/stable/install_and_upgrade/", "zig": "https://ziglang.org/download/", "clj": "https://clojure.org/guides/install_clojure", "nix": "https://nixos.org/download/",
		"gradle": "https://gradle.org/install/", "mvn": "https://maven.apache.org/install.html", "meson": "https://mesonbuild.com/Getting-meson.html",
		"dotnet": "https://dotnet.microsoft.com/download",
	}
	if value := urls[command]; value != "" {
		return value
	}
	return "https://" + command + ".org"
}
