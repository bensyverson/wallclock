package classify

import (
	"slices"
	"strings"
)

// prefixRules are the built-in patterns: a command whose leading words equal
// the prefix has that kind. Commands needing argument parsing (make, just,
// xcodebuild, npm and friends) are handled in builtinSpecial.
var prefixRules = []struct {
	kind   Kind
	prefix []string
}{
	// Go
	{KindTest, []string{"go", "test"}},
	{KindBuild, []string{"go", "build"}},
	{KindBuild, []string{"go", "install"}},
	{KindBuild, []string{"go", "run"}},
	{KindLint, []string{"go", "vet"}},
	{KindLint, []string{"gofmt"}},
	{KindLint, []string{"goimports"}},
	{KindLint, []string{"golangci-lint"}},
	{KindLint, []string{"staticcheck"}},
	{KindPackage, []string{"go", "mod"}},
	{KindPackage, []string{"go", "get"}},
	// Swift
	{KindTest, []string{"swift", "test"}},
	{KindBuild, []string{"swift", "build"}},
	{KindBuild, []string{"swift", "run"}},
	{KindLint, []string{"swiftlint"}},
	{KindLint, []string{"swift-format"}},
	{KindPackage, []string{"swift", "package", "resolve"}},
	{KindPackage, []string{"swift", "package", "update"}},
	// Rust
	{KindTest, []string{"cargo", "test"}},
	{KindTest, []string{"cargo", "nextest"}},
	{KindBuild, []string{"cargo", "build"}},
	{KindBuild, []string{"cargo", "run"}},
	{KindBuild, []string{"cargo", "check"}},
	{KindLint, []string{"cargo", "clippy"}},
	{KindLint, []string{"cargo", "fmt"}},
	{KindLint, []string{"rustfmt"}},
	{KindPackage, []string{"cargo", "add"}},
	{KindPackage, []string{"cargo", "update"}},
	{KindPackage, []string{"cargo", "fetch"}},
	// Python
	{KindTest, []string{"pytest"}},
	{KindTest, []string{"py.test"}},
	{KindTest, []string{"unittest"}},
	{KindTest, []string{"tox"}},
	{KindTest, []string{"nox"}},
	{KindLint, []string{"ruff"}},
	{KindLint, []string{"black"}},
	{KindLint, []string{"mypy"}},
	{KindLint, []string{"flake8"}},
	{KindLint, []string{"isort"}},
	{KindLint, []string{"pyright"}},
	{KindPackage, []string{"pip"}},
	{KindPackage, []string{"pip3"}},
	{KindPackage, []string{"uv", "pip"}},
	{KindPackage, []string{"uv", "sync"}},
	{KindPackage, []string{"poetry", "install"}},
	// Waiting
	{KindWait, []string{"sleep"}},
	{KindWait, []string{"gh", "run", "watch"}},
	// JavaScript test runners
	{KindTest, []string{"node", "--test"}},
	// Version control
	{KindVCS, []string{"git"}},
	{KindVCS, []string{"gh"}},
	{KindVCS, []string{"jj"}},
}

func builtin(t []string) Kind {
	if hasPrefix(t, []string{"gh"}) && slices.Contains(t, "--watch") {
		return KindWait
	}
	for _, r := range prefixRules {
		if hasPrefix(t, r.prefix) {
			return r.kind
		}
	}
	switch t[0] {
	case "make", "gmake":
		return runnerKind(t[1:], []string{"-C", "-f", "-I", "-o", "-W"}, true)
	case "just":
		return runnerKind(t[1:], []string{"-f", "-d", "--justfile", "--working-directory"}, false)
	case "xcodebuild":
		return xcodebuildKind(t[1:])
	case "npm", "pnpm", "yarn", "bun":
		return nodeKind(t[0], t[1:])
	}
	return KindBash
}

func hasPrefix(t, prefix []string) bool {
	if len(t) < len(prefix) {
		return false
	}
	for i, p := range prefix {
		if t[i] != p {
			return false
		}
	}
	return true
}

// targetKind maps a make or just target, or an npm script name, to a kind.
func targetKind(name string) Kind {
	base, _, _ := strings.Cut(name, ":")
	switch base {
	case "test", "tests", "check":
		return KindTest
	case "build", "all", "default":
		return KindBuild
	case "lint":
		return KindLint
	}
	return KindBash
}

// runnerKind classifies make and just: the best kind among the targets named,
// or build when none is given, since the default target usually builds.
func runnerKind(args, withValue []string, skipAssign bool) Kind {
	best, any := KindBash, false
	for len(args) > 0 {
		a := args[0]
		switch {
		case strings.HasPrefix(a, "-"):
			args = skipFlags(args, withValue)
			continue
		case skipAssign && strings.Contains(a, "="):
		default:
			any = true
			if k := targetKind(a); priority[k] > priority[best] {
				best = k
			}
		}
		args = args[1:]
	}
	if !any {
		return KindBuild
	}
	return best
}

func xcodebuildKind(args []string) Kind {
	kind := KindBuild
	for _, a := range args {
		switch a {
		case "test", "test-without-building":
			return KindTest
		case "-resolvePackageDependencies":
			kind = KindPackage
		}
	}
	return kind
}

// nodeKind classifies npm, pnpm, yarn and bun invocations.
func nodeKind(manager string, args []string) Kind {
	args = skipFlags(args, []string{"--prefix", "-C", "--cwd", "--filter", "-w", "--workspace"})
	if len(args) == 0 {
		if manager == "yarn" {
			return KindPackage // bare `yarn` installs
		}
		return KindBash
	}
	cmd := args[0]
	switch cmd {
	case "install", "i", "ci", "add", "update", "upgrade", "up", "remove", "uninstall":
		return KindPackage
	case "t", "tst":
		return KindTest
	case "run", "run-script":
		rest := skipFlags(args[1:], nil)
		if len(rest) == 0 {
			return KindBash
		}
		return targetKind(rest[0])
	}
	return targetKind(cmd)
}
